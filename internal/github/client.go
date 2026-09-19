// Package github reads allowlisted source files through the official GitHub MCP.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	ErrUnavailable  = errors.New("UPSTREAM_UNAVAILABLE")
	ErrIncompatible = errors.New("INCOMPATIBLE_PROVIDER")
	ErrScope        = errors.New("OUT_OF_SCOPE")
	ErrInput        = errors.New("INVALID_ARGUMENT")
	ErrRef          = errors.New("UNSUPPORTED_REF: provide a full commit SHA")
	shaPattern      = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

const MaxFileBytes = 1024 * 1024

func AllowedRepository(repo string) bool {
	switch repo {
	case "AstarNetwork/Astar", "AstarNetwork/astar-docs", "AstarNetwork/astar-apps":
		return true
	}
	return false
}

func Validate(repo, file, ref string) error {
	if !AllowedRepository(repo) {
		return ErrScope
	}
	if file == "" || len(file) > 1024 || path.Clean(file) != file || strings.HasPrefix(file, "/") || strings.ContainsAny(file, "\\\x00\r\n%?#") {
		return ErrInput
	}
	for _, part := range strings.Split(file, "/") {
		if part == ".." || part == "." {
			return ErrInput
		}
	}
	if !shaPattern.MatchString(ref) {
		return ErrRef
	}
	return nil
}

type Client struct {
	mu            sync.Mutex
	gate          chan struct{}
	binary, token string
	session       *mcp.ClientSession
	status        atomic.Value
	retryAt       time.Time
	closed        bool
}

func New(binary, token string) *Client {
	status := "not_connected"
	if binary == "" || token == "" {
		status = "not_configured"
	}
	c := &Client{binary: binary, token: token, gate: make(chan struct{}, 1)}
	c.status.Store(status)
	return c
}
func (c *Client) Status() string { return c.status.Load().(string) }
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.disconnect()
	c.status.Store("closed")
}
func (c *Client) disconnect() {
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
	}
}

func (c *Client) connect(ctx context.Context) error {
	if c.closed || c.token == "" || c.binary == "" {
		return ErrUnavailable
	}
	if c.session != nil {
		return nil
	}
	if time.Now().Before(c.retryAt) {
		return ErrUnavailable
	}
	c.retryAt = time.Now().Add(5 * time.Second)
	if !filepath.IsAbs(c.binary) {
		c.status.Store("invalid_configuration")
		return ErrUnavailable
	}
	cmd := exec.Command(c.binary, "stdio", "--read-only", "--toolsets=", "--tools=get_file_contents")
	// Provider credentials are isolated from Aradia and host application secrets.
	cmd.Env = []string{"GITHUB_PERSONAL_ACCESS_TOKEN=" + c.token, "PATH=" + os.Getenv("PATH")}
	client := mcp.NewClient(&mcp.Implementation{Name: "astar-mcp-github", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd, TerminateDuration: time.Second}, nil)
	if err != nil {
		c.status.Store("unavailable")
		return ErrUnavailable
	}
	c.session = session
	found := false
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			c.disconnect()
			c.status.Store("unavailable")
			return ErrUnavailable
		}
		if tool.Name != "get_file_contents" || !validSchema(tool) || found {
			c.disconnect()
			c.status.Store("incompatible")
			return ErrIncompatible
		}
		found = true
	}
	if !found {
		c.disconnect()
		c.status.Store("incompatible")
		return ErrIncompatible
	}
	c.status.Store("ready")
	return nil
}

func validSchema(tool *mcp.Tool) bool {
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		return false
	}
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return false
	}
	var s struct {
		Type       string
		Properties map[string]struct{ Type string }
		Required   []string
	}
	if json.Unmarshal(raw, &s) != nil || s.Type != "object" {
		return false
	}
	for _, field := range []string{"owner", "repo", "path", "sha"} {
		if s.Properties[field].Type != "string" {
			return false
		}
	}
	allowed := map[string]bool{"owner": true, "repo": true, "path": true, "sha": true}
	for _, field := range s.Required {
		if !allowed[field] {
			return false
		}
	}
	return true
}

// ReadFile accepts immutable revisions only. Branch resolution is intentionally not guessed.
func (c *Client) ReadFile(ctx context.Context, repo, file, ref string) (string, error) {
	if err := Validate(repo, file, ref); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Waiting calls can be cancelled before acquiring the process lifecycle lock.
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return "", ErrUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx.Err() != nil {
		return "", ErrUnavailable
	}
	if err := c.connect(ctx); err != nil {
		return "", err
	}
	parts := strings.Split(repo, "/")
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: "get_file_contents", Arguments: map[string]any{"owner": parts[0], "repo": parts[1], "path": file, "sha": ref}})
	if err != nil {
		c.disconnect()
		c.status.Store("unavailable")
		c.retryAt = time.Now().Add(5 * time.Second)
		return "", ErrUnavailable
	}
	if result == nil || result.IsError {
		return "", ErrUnavailable
	}
	return decodeFile(result, repo, file, ref)
}

func decodeFile(result *mcp.CallToolResult, repo, file, ref string) (string, error) {
	expected := "repo://" + repo + "/sha/" + ref + "/contents/" + file
	var text string
	count := 0
	for _, item := range result.Content {
		resource, ok := item.(*mcp.EmbeddedResource)
		if !ok {
			continue
		}
		if resource.Resource == nil {
			return "", ErrIncompatible
		}
		uri, err := url.PathUnescape(resource.Resource.URI)
		if err != nil || uri != expected || len(resource.Resource.Blob) > 0 || len(resource.Resource.Text) > MaxFileBytes {
			return "", ErrIncompatible
		}
		count++
		text = resource.Resource.Text
	}
	if count != 1 {
		return "", ErrIncompatible
	}
	return text, nil
}
