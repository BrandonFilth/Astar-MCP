package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/BrandonFilth/Astar-MCP/internal/catalog"
	"github.com/BrandonFilth/Astar-MCP/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCatalogProtocol(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects := []catalog.Project{{ID: "1", Name: "First", Networks: []string{"astar"}, Categories: []string{"NFT"}, Source: "https://astar.network/ecosystem/"}, {ID: "2", Name: "Second", Networks: []string{"soneium"}, Categories: []string{"NFT"}, Source: "https://astar.network/ecosystem/"}}
	if err = store.ReplaceSnapshot(ctx, "ecosystem", "", projects); err != nil {
		t.Fatal(err)
	}
	docs := []catalog.Document{{ID: "astar:docs/staking.md", Title: "Staking", Content: "# Staking\nRewards evidence", Source: "https://github.com/AstarNetwork/astar-docs/blob/abc/docs/staking.md", Revision: "abc"}}
	if err = store.ReplaceSnapshot(ctx, "astar-docs", "abc", docs); err != nil {
		t.Fatal(err)
	}
	a, b := mcp.NewInMemoryTransports()
	ss, err := New(&fakeReader{fail: true}, store).Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		r, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	for name, args := range map[string]map[string]any{"search_projects": {"network": "astar"}, "get_project": {"id": "1"}, "list_categories": {}, "search_docs": {"query": "rewards"}, "get_document": {"id": docs[0].ID}} {
		r := call(name, args)
		if r.IsError {
			t.Fatalf("%s: %v", name, r.Content)
		}
		raw, _ := json.Marshal(r.StructuredContent)
		if !strings.Contains(string(raw), "sources") || !strings.Contains(string(raw), "retrieved_at") {
			t.Fatal(string(raw))
		}
	}
	if !call("get_document", map[string]any{"id": "removed"}).IsError {
		t.Fatal("missing document accepted")
	}
	if !call("search_projects", map[string]any{"network": "other"}).IsError {
		t.Fatal("scope accepted")
	}
	r := call("search_projects", map[string]any{"limit": 1})
	raw, _ := json.Marshal(r.StructuredContent)
	var out struct {
		Data struct {
			Next string `json:"next_cursor"`
		}
	}
	if err = json.Unmarshal(raw, &out); err != nil || out.Data.Next == "" {
		t.Fatal(string(raw), err)
	}
	if call("search_projects", map[string]any{"cursor": out.Data.Next}).IsError {
		t.Fatal("valid cursor rejected")
	}
	if !call("search_projects", map[string]any{"cursor": out.Data.Next, "network": "astar"}).IsError {
		t.Fatal("cross-query cursor accepted")
	}
	if err = store.ReplaceSnapshot(ctx, "ecosystem", "", projects[:1]); err != nil {
		t.Fatal(err)
	}
	if !call("search_projects", map[string]any{"cursor": out.Data.Next}).IsError {
		t.Fatal("stale cursor accepted")
	}
	resources, err := cs.ListResources(ctx, nil)
	if err != nil || len(resources.Resources) != 3 {
		t.Fatal(resources, err)
	}
	for _, resource := range resources.Resources {
		r, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: resource.URI})
		if err != nil || len(r.Contents) != 1 {
			t.Fatal(r, err)
		}
	}
}
