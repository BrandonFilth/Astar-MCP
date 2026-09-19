// Package catalog imports bounded public source snapshots for local queries.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	gh "github.com/BrandonFilth/Astar-MCP/internal/github"
	"github.com/BrandonFilth/Astar-MCP/internal/storage"
)

const EcosystemURL = "https://strapi-prod.astar.network/api/projects"

type Project struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Website    string   `json:"website,omitempty"`
	Networks   []string `json:"networks"`
	Categories []string `json:"categories"`
	Source     string   `json:"source"`
}
type Document struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Source   string `json:"source"`
	Revision string `json:"revision"`
}
type FileReader interface {
	ReadFile(context.Context, string, string, string) (string, error)
}

func HTTPClient() *http.Client {
	return &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect rejected") }}
}

func Fetch(ctx context.Context, client *http.Client, address string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errors.New("invalid source")
	}
	req.Header.Set("User-Agent", "Astar-MCP/0.1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("source unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("source returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil || int64(len(raw)) > max {
		return nil, errors.New("source response exceeds limit or failed")
	}
	return raw, nil
}

// ImportProjects accepts a fixed source in production; base is injectable for tests.
func ImportProjects(ctx context.Context, client *http.Client, store *storage.Store, base string) error {
	projects := []Project{}
	seen := map[int]bool{}
	total := -1
	pages := 0
	for page := 1; page <= 100; page++ {
		u, err := url.Parse(base)
		if err != nil {
			return errors.New("invalid source")
		}
		q := u.Query()
		q.Set("populate", "project_categories,project_chains")
		q.Set("pagination[page]", strconv.Itoa(page))
		q.Set("pagination[pageSize]", "100")
		q.Set("sort[0]", "id:asc")
		u.RawQuery = q.Encode()
		raw, err := Fetch(ctx, client, u.String(), 4*1024*1024)
		if err != nil {
			return err
		}
		type relation struct {
			Data []struct {
				Attributes struct {
					Name string `json:"name"`
				} `json:"attributes"`
			} `json:"data"`
		}
		var response struct {
			Data []struct {
				ID         int `json:"id"`
				Attributes struct {
					Name       string   `json:"name"`
					Website    string   `json:"website"`
					Chains     relation `json:"project_chains"`
					Categories relation `json:"project_categories"`
				} `json:"attributes"`
			} `json:"data"`
			Meta struct {
				Pagination struct{ Page, PageCount, Total int } `json:"pagination"`
			} `json:"meta"`
		}
		if json.Unmarshal(raw, &response) != nil {
			return errors.New("invalid project response")
		}
		meta := response.Meta.Pagination
		if total < 0 {
			total = meta.Total
			pages = meta.PageCount
		}
		if total < 1 || total > 10000 || pages < 1 || pages > 100 || meta.Total != total || meta.PageCount != pages || meta.Page != page {
			return errors.New("inconsistent directory pagination")
		}
		for _, item := range response.Data {
			if item.ID <= 0 || seen[item.ID] || strings.TrimSpace(item.Attributes.Name) == "" || len(item.Attributes.Name) > 512 || len(item.Attributes.Website) > 2048 || len(item.Attributes.Categories.Data) > 30 || len(item.Attributes.Chains.Data) > 20 {
				return errors.New("invalid project record")
			}
			seen[item.ID] = true
			p := Project{ID: strconv.Itoa(item.ID), Name: item.Attributes.Name, Networks: []string{}, Categories: []string{}, Source: "https://astar.network/ecosystem/"}
			for _, chain := range item.Attributes.Chains.Data {
				switch chain.Attributes.Name {
				case "Astar Network":
					p.Networks = append(p.Networks, "astar")
				case "Soneium":
					p.Networks = append(p.Networks, "soneium")
				}
			}
			if len(p.Networks) == 0 {
				continue
			}
			for _, c := range item.Attributes.Categories.Data {
				if len(c.Attributes.Name) > 256 {
					return errors.New("invalid category")
				}
				if c.Attributes.Name != "" {
					p.Categories = append(p.Categories, c.Attributes.Name)
				}
			}
			if u, err := url.Parse(item.Attributes.Website); err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil {
				p.Website = u.String()
			}
			projects = append(projects, p)
		}
		if page == pages {
			break
		}
	}
	if len(seen) != total {
		return errors.New("incomplete directory")
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	return store.ReplaceSnapshot(ctx, "ecosystem", "", projects)
}

// ImportDocuments uses an operator-selected list of official documentation paths.
// Public callers cannot choose sources or revisions for ingestion.
func ImportDocuments(ctx context.Context, reader FileReader, store *storage.Store, revision string, paths []string) error {
	if len(paths) == 0 || len(paths) > 100 {
		return errors.New("select between 1 and 100 documents")
	}
	docs := []Document{}
	seen := map[string]bool{}
	bytes := 0
	for _, file := range paths {
		if err := gh.Validate("AstarNetwork/astar-docs", file, revision); err != nil {
			return err
		}
		if !strings.HasPrefix(file, "docs/") || (!strings.HasSuffix(file, ".md") && !strings.HasSuffix(file, ".mdx")) || strings.Contains(file, "shiden") || strings.Contains(file, "shibuya") || seen[file] {
			return errors.New("invalid or duplicate document path")
		}
		seen[file] = true
		text, err := reader.ReadFile(ctx, "AstarNetwork/astar-docs", file, revision)
		if err != nil {
			return err
		}
		if len(text) > 256*1024 {
			return errors.New("document too large")
		}
		bytes += len(text)
		if bytes > 8*1024*1024 {
			return errors.New("document corpus too large")
		}
		title := file
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "# ") {
				title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
				break
			}
		}
		u := url.URL{Scheme: "https", Host: "github.com", Path: "/AstarNetwork/astar-docs/blob/" + revision + "/" + file}
		docs = append(docs, Document{ID: "astar:" + file, Title: title, Content: text, Source: u.String(), Revision: revision})
	}
	return store.ReplaceSnapshot(ctx, "astar-docs", revision, docs)
}

func ImportAradiaGuide(ctx context.Context, client *http.Client, store *storage.Store) error {
	raw, err := Fetch(ctx, client, "https://docs.aradia.app/llms-full.txt", 8*1024*1024)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return errors.New("empty Aradia guide")
	}
	// HTTP documentation has a content digest, not a Git revision.
	revision := Digest(raw)
	docs := []Document{{ID: "aradia:guide", Title: "Aradia integration guide", Content: string(raw), Source: "https://docs.aradia.app/llms-full.txt", Revision: revision}}
	return store.ReplaceSnapshot(ctx, "aradia-docs", revision, docs)
}
