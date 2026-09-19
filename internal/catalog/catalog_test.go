package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BrandonFilth/Astar-MCP/internal/storage"
)

func TestDirectoryReplacementIsAtomic(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	broken := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("pagination[page]")
		if broken && page == "2" {
			w.WriteHeader(503)
			return
		}
		chain := "Astar Network"
		if page == "2" {
			chain = "Unrelated"
		}
		fmt.Fprintf(w, `{"data":[{"id":%s,"attributes":{"name":"Example","website":"javascript:alert(1)","project_chains":{"data":[{"attributes":{"name":%q}}]},"project_categories":{"data":[{"attributes":{"name":"DeFi"}}]}}}],"meta":{"pagination":{"page":%s,"pageCount":2,"total":2}}}`, page, chain, page)
	}))
	defer upstream.Close()
	if err = ImportProjects(ctx, upstream.Client(), store, upstream.URL); err != nil {
		t.Fatal(err)
	}
	before, err := store.Snapshot(ctx, "ecosystem")
	if err != nil {
		t.Fatal(err)
	}
	var projects []Project
	if err = json.Unmarshal(before.Data, &projects); err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Website != "" || projects[0].Networks[0] != "astar" {
		t.Fatal(projects)
	}
	broken = true
	if err = ImportProjects(ctx, upstream.Client(), store, upstream.URL); err == nil {
		t.Fatal("partial import accepted")
	}
	after, _ := store.Snapshot(ctx, "ecosystem")
	if string(before.Data) != string(after.Data) || before.UpdatedAt != after.UpdatedAt {
		t.Fatal("valid snapshot overwritten")
	}
}

type reader struct{ fail bool }

func (r *reader) ReadFile(context.Context, string, string, string) (string, error) {
	if r.fail {
		return "", errors.New("missing")
	}
	return "# Staking\nDocumentation", nil
}
func TestDocumentReplacementAndRemoval(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r := &reader{}
	rev := strings.Repeat("a", 40)
	if err = ImportDocuments(ctx, r, store, rev, []string{"docs/old.md"}); err != nil {
		t.Fatal(err)
	}
	r.fail = true
	if err = ImportDocuments(ctx, r, store, rev, []string{"docs/new.md"}); err == nil {
		t.Fatal("missing source accepted")
	}
	snap, _ := store.Snapshot(ctx, "astar-docs")
	if !strings.Contains(string(snap.Data), "old.md") {
		t.Fatal("old document lost")
	}
	r.fail = false
	if err = ImportDocuments(ctx, r, store, rev, []string{"docs/new.md"}); err != nil {
		t.Fatal(err)
	}
	snap, _ = store.Snapshot(ctx, "astar-docs")
	if strings.Contains(string(snap.Data), "old.md") || !strings.Contains(string(snap.Data), "new.md") {
		t.Fatal("removed document remains")
	}
}
