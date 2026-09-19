package github

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const revision = "1111111111111111111111111111111111111111"

func TestScope(t *testing.T) {
	for _, file := range []string{"../secret", "a/../../x", "a//b", "/etc/passwd", "a%2fb", "a\\b", "."} {
		if Validate("AstarNetwork/Astar", file, revision) == nil {
			t.Fatal(file)
		}
	}
	if !errors.Is(Validate("someone/private", "Cargo.toml", revision), ErrScope) {
		t.Fatal("scope")
	}
	if !errors.Is(Validate("AstarNetwork/Astar", "Cargo.toml", "master"), ErrRef) {
		t.Fatal("mutable ref")
	}
}
func TestDecodeEnforcesIdentity(t *testing.T) {
	valid := "repo://AstarNetwork/Astar/sha/" + revision + "/contents/Cargo.toml"
	for _, uri := range []string{valid, strings.Replace(valid, "AstarNetwork", "attacker", 1), strings.Replace(valid, revision, strings.Repeat("2", 40), 1)} {
		result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: uri, Text: "source"}}}}
		text, err := decodeFile(result, "AstarNetwork/Astar", "Cargo.toml", revision)
		if uri == valid {
			if err != nil || text != "source" {
				t.Fatal(text, err)
			}
		} else if err == nil {
			t.Fatal("accepted mismatched evidence")
		}
	}
}

// The test executable acts as an independent MCP process when launched by Client.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "stdio" {
		if os.Getenv("ARADIA_API_TOKEN") != "" || os.Getenv("SECRET_TEST_VALUE") != "" {
			os.Exit(9)
		}
		s := mcp.NewServer(&mcp.Implementation{Name: "test-upstream", Version: "1"}, nil)
		schema := map[string]any{"type": "object", "properties": map[string]any{"owner": map[string]any{"type": "string"}, "repo": map[string]any{"type": "string"}, "path": map[string]any{"type": "string"}, "sha": map[string]any{"type": "string"}}, "required": []string{"owner", "repo"}}
		s.AddTool(&mcp.Tool{Name: "get_file_contents", InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN") == "slow-test" {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "repo://AstarNetwork/Astar/sha/" + revision + "/contents/Cargo.toml", Text: "[package]\nname = \"astar\""}}}}, nil
		})
		if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestSubprocessReadAndIsolation(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARADIA_API_TOKEN", "must-not-leak")
	t.Setenv("SECRET_TEST_VALUE", "must-not-leak")
	c := New(binary, "test-only")
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	value, err := c.ReadFile(ctx, "AstarNetwork/Astar", "Cargo.toml", revision)
	if err != nil || !strings.Contains(value, "astar") {
		t.Fatal(value, err)
	}
	if c.Status() != "ready" {
		t.Fatal(c.Status())
	}
	c.Close()
	if _, err = c.ReadFile(ctx, "AstarNetwork/Astar", "Cargo.toml", revision); err == nil {
		t.Fatal("closed client accepted call")
	}
}
func TestCancellation(t *testing.T) {
	binary, _ := os.Executable()
	c := New(binary, "slow-test")
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.ReadFile(ctx, "AstarNetwork/Astar", "Cargo.toml", revision)
	if err == nil || time.Since(start) > 4*time.Second {
		t.Fatal("cancellation not bounded", err)
	}
}
func TestMissingCredentials(t *testing.T) {
	c := New("", "")
	defer c.Close()
	if _, err := c.ReadFile(context.Background(), "AstarNetwork/Astar", "Cargo.toml", revision); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if c.Status() != "not_configured" {
		t.Fatal(c.Status())
	}
}

func TestSchemaRejectsIncompatibleProvider(t *testing.T) {
	tool := &mcp.Tool{Name: "get_file_contents", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, InputSchema: map[string]any{"type": "object", "properties": map[string]any{"owner": map[string]any{"type": "string"}, "repo": map[string]any{"type": "string"}, "path": map[string]any{"type": "string"}, "sha": map[string]any{"type": "string"}}}}
	if !validSchema(tool) {
		t.Fatal("valid schema rejected")
	}
	tool.Annotations.ReadOnlyHint = false
	if validSchema(tool) {
		t.Fatal("write-capable provider accepted")
	}
	tool.Annotations.ReadOnlyHint = true
	tool.InputSchema = map[string]any{"type": "object", "properties": map[string]any{"sha": map[string]any{"type": "number"}}}
	if validSchema(tool) {
		t.Fatal("incompatible arguments accepted")
	}
}
func TestWaitingCallCancellationAndStatus(t *testing.T) {
	c := New("", "")
	defer c.Close()
	c.gate <- struct{}{}
	defer func() { <-c.gate }()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.ReadFile(ctx, "AstarNetwork/Astar", "Cargo.toml", revision); err == nil {
		t.Fatal("expected cancellation")
	}
	if time.Since(start) > time.Second {
		t.Fatal("waiting call ignored deadline")
	}
	if c.Status() != "not_configured" {
		t.Fatal("status unavailable")
	}
}
