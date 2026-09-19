package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	gh "github.com/BrandonFilth/Astar-MCP/internal/github"
	"github.com/BrandonFilth/Astar-MCP/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeReader struct {
	fail  bool
	calls int
}

func (f *fakeReader) Status() string {
	if f.fail {
		return "unavailable"
	}
	return "ready"
}
func (f *fakeReader) ReadFile(context.Context, string, string, string) (string, error) {
	f.calls++
	if f.fail {
		return "", gh.ErrUnavailable
	}
	return strings.Repeat("line\n", 400), nil
}
func TestProtocolAndFailureIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	provider := &fakeReader{fail: true}
	s := New(provider, store)
	a, b := mcp.NewInMemoryTransports()
	serverSession, err := s.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 7 {
		t.Fatal(list, err)
	}
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	args := map[string]any{"repository": "AstarNetwork/Astar", "path": "Cargo.toml", "ref": strings.Repeat("1", 40)}
	if !call("get_code_file", args).IsError {
		t.Fatal("failure hidden")
	}
	if call("get_service_status", map[string]any{}).IsError {
		t.Fatal("status unavailable after provider failure")
	}
	provider.fail = false
	args["repository"] = "attacker/private"
	before := provider.calls
	if !call("get_code_file", args).IsError || provider.calls != before {
		t.Fatal("scope forwarded")
	}
	args["repository"] = "AstarNetwork/Astar"
	args["injected_url"] = "https://example.com"
	r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_code_file", Arguments: args})
	if e == nil && !r.IsError {
		t.Fatal("unknown argument accepted")
	}
	delete(args, "injected_url")
	r = call("get_code_file", args)
	if r.IsError {
		t.Fatal(r.Content)
	}
	raw, _ := json.Marshal(r.StructuredContent)
	if !strings.Contains(string(raw), `"next_line":301`) || !strings.Contains(string(raw), `"evidence_state":"repository"`) || !strings.Contains(string(raw), "/blob/") {
		t.Fatal(string(raw))
	}
	args["end_line"] = 301
	if !call("get_code_file", args).IsError {
		t.Fatal("line budget ignored")
	}
}
