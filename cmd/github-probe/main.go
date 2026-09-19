// github-probe checks the official MCP server without persisting credentials or source bodies.
package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	gh "github.com/BrandonFilth/Astar-MCP/internal/github"
	"github.com/BrandonFilth/Astar-MCP/internal/server"
	"github.com/BrandonFilth/Astar-MCP/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func main() {
	checks, ok := run()
	json.NewEncoder(os.Stdout).Encode(struct {
		CheckedAt string  `json:"checked_at"`
		Checks    []check `json:"checks"`
	}{time.Now().UTC().Format(time.RFC3339), checks})
	if !ok {
		os.Exit(1)
	}
}
func run() ([]check, bool) {
	token := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")
	if token == "" {
		return []check{{"github_credentials", "blocked", "GITHUB_PERSONAL_ACCESS_TOKEN is required; never paste it into reports"}}, false
	}
	binary := os.Getenv("GITHUB_MCP_BINARY")
	if binary == "" {
		return []check{{"github_binary", "blocked", "GITHUB_MCP_BINARY is required"}}, false
	}
	if !filepath.IsAbs(binary) {
		return []check{{"github_binary", "failed", "binary path must be absolute"}}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.Command(binary, "stdio", "--read-only", "--toolsets=", "--tools=get_file_contents,list_pull_requests,pull_request_read")
	cmd.Env = []string{"GITHUB_PERSONAL_ACCESS_TOKEN=" + token, "PATH=" + os.Getenv("PATH")}
	// Child stderr may contain provider details: do not include it in the evidence report.
	client := mcp.NewClient(&mcp.Implementation{Name: "astar-github-probe", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd, TerminateDuration: time.Second}, nil)
	if err != nil {
		return []check{{"github_connect", "failed", "connection failed; inspect configuration without logging credentials"}}, false
	}
	defer session.Close()
	checks := []check{{"github_connect", "passed", "official server connected"}}
	found := map[string]bool{}
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return append(checks, check{"github_discovery", "failed", "tool discovery failed"}), false
		}
		switch tool.Name {
		case "get_file_contents", "list_pull_requests", "pull_request_read":
			found[tool.Name] = true
		default:
			return append(checks, check{"github_discovery", "failed", "unexpected tool advertised"}), false
		}
		if tool.InputSchema == nil {
			return append(checks, check{"github_schema", "failed", "missing input schema"}), false
		}
	}
	if len(found) != 3 {
		return append(checks, check{"github_discovery", "failed", "required tool absent"}), false
	}
	checks = append(checks, check{"github_discovery", "passed", "three explicitly selected tools"})
	calls := []struct {
		name, tool string
		args       map[string]any
	}{
		{"astar_file", "get_file_contents", map[string]any{"owner": "AstarNetwork", "repo": "Astar", "path": "Cargo.toml", "ref": "refs/heads/master"}},
		{"astar_open_prs", "list_pull_requests", map[string]any{"owner": "AstarNetwork", "repo": "Astar", "state": "open", "perPage": 1}},
	}
	for _, c := range calls {
		r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
		if e != nil || r == nil || r.IsError {
			return append(checks, check{c.name, "failed", "provider call failed; no response body persisted"}), false
		}
		checks = append(checks, check{c.name, "passed", "non-error MCP response"})
	}
	// Validate the review/file operations on an explicit PR selected by the operator.
	pr := os.Getenv("GITHUB_PROBE_PR")
	if pr == "" {
		return append(checks, check{"astar_pr_details", "blocked", "set GITHUB_PROBE_PR to a verified PR number"}), false
	}
	number, err := strconv.Atoi(pr)
	if err != nil || number < 1 {
		return append(checks, check{"astar_pr_details", "failed", "invalid PR number"}), false
	}
	for _, method := range []string{"get", "get_files", "get_reviews"} {
		r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "pull_request_read", Arguments: map[string]any{"owner": "AstarNetwork", "repo": "Astar", "pullNumber": number, "method": method, "perPage": 1}})
		if e != nil || r == nil || r.IsError {
			return append(checks, check{"astar_pr_" + method, "failed", "provider call failed"}), false
		}
		checks = append(checks, check{"astar_pr_" + method, "passed", "non-error MCP response"})
	}

	revision := os.Getenv("GITHUB_PROBE_REVISION")
	if revision == "" {
		return append(checks, check{"astar_service_file", "blocked", "GITHUB_PROBE_REVISION must identify an immutable Astar commit"}), false
	}
	if !probeService(ctx, binary, token, revision) {
		return append(checks, check{"astar_service_file", "failed", "service file read or provenance validation failed"}), false
	}
	return append(checks, check{"astar_service_file", "passed", "Astar MCP tool -> official GitHub MCP -> immutable file and source reference"}), true
}

func probeService(ctx context.Context, binary, token, revision string) bool {
	dir, err := os.MkdirTemp("", "astar-live-probe-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	store, err := storage.Open(ctx, dir)
	if err != nil {
		return false
	}
	defer store.Close()
	provider := gh.New(binary, token)
	defer provider.Close()
	service := server.New(provider, store)
	a, b := mcp.NewInMemoryTransports()
	ss, err := service.Connect(ctx, a, nil)
	if err != nil {
		return false
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "astar-service-probe", Version: "0.1.0"}, nil)
	cs, err := client.Connect(ctx, b, nil)
	if err != nil {
		return false
	}
	defer cs.Close()
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_code_file", Arguments: map[string]any{
		"repository": "AstarNetwork/Astar", "path": "Cargo.toml", "ref": revision, "start_line": 1, "end_line": 10,
	}})
	if err != nil || result == nil || result.IsError {
		return false
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return false
	}
	var output struct {
		Data struct {
			Text     string `json:"text"`
			Revision string `json:"revision"`
		}
		Sources       []server.Source `json:"sources"`
		EvidenceState string          `json:"evidence_state"`
	}
	if json.Unmarshal(raw, &output) != nil {
		return false
	}
	expected := "https://github.com/AstarNetwork/Astar/blob/" + revision + "/Cargo.toml#L1-L10"
	return output.Data.Text != "" && output.Data.Revision == revision && output.EvidenceState == "repository" &&
		len(output.Sources) == 1 && output.Sources[0].URL == expected && output.Sources[0].Revision == revision
}
