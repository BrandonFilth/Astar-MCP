// Package server exposes implemented read-only tools over MCP.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	gh "github.com/BrandonFilth/Astar-MCP/internal/github"
	"github.com/BrandonFilth/Astar-MCP/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Reader interface {
	ReadFile(context.Context, string, string, string) (string, error)
	Status() string
}
type FileInput struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Ref        string `json:"ref"`
	StartLine  int    `json:"start_line,omitempty"`
	EndLine    int    `json:"end_line,omitempty"`
}
type Source struct {
	URL      string `json:"url"`
	Revision string `json:"revision,omitempty"`
}
type Output struct {
	Data          any      `json:"data"`
	Sources       []Source `json:"sources"`
	RetrievedAt   string   `json:"retrieved_at"`
	Warnings      []string `json:"warnings"`
	EvidenceState string   `json:"evidence_state"`
}
type File struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Revision   string `json:"revision"`
	Text       string `json:"text"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	NextLine   int    `json:"next_line,omitempty"`
}

func envelope(data any, state string) Output {
	return Output{data, []Source{}, time.Now().UTC().Format(time.RFC3339), []string{}, state}
}
func failure(err error) (*mcp.CallToolResult, Output, error) {
	code := "UPSTREAM_UNAVAILABLE"
	if err.Error() == "NOT_FOUND" {
		code = "NOT_FOUND"
	}
	for _, known := range []error{gh.ErrInput, gh.ErrScope, gh.ErrRef, gh.ErrIncompatible} {
		if errors.Is(err, known) {
			code = known.Error()
		}
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: code}}}, Output{}, nil
}
func New(reader Reader, store *storage.Store) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "astar-mcp", Version: "0.1.0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "get_service_status", Description: "Report implemented module availability and stored provider observations.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, Output, error) {
		observations, err := store.Observations(ctx)
		if err != nil {
			return failure(err)
		}
		return nil, envelope(map[string]any{"modules": map[string]string{"github": reader.Status(), "storage": "ready", "aradia": "not_implemented", "docs": catalogStatus(ctx, store, "astar-docs", "aradia-docs"), "ecosystem": catalogStatus(ctx, store, "ecosystem"), "network": "not_implemented"}, "observations": observations}, "unknown"), nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "get_code_file", Description: "Read up to 300 lines of an Astar source file at an explicit full commit SHA. Branch names are not supported. Repository code does not prove active chain behavior.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in FileInput) (*mcp.CallToolResult, Output, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := gh.Validate(in.Repository, in.Path, in.Ref); err != nil {
			return failure(err)
		}
		if in.StartLine == 0 {
			in.StartLine = 1
		}
		if in.StartLine < 1 || in.StartLine > 1000000 || in.EndLine < 0 {
			return failure(gh.ErrInput)
		}
		if in.EndLine == 0 {
			in.EndLine = in.StartLine + 299
		}
		if in.EndLine < in.StartLine || in.EndLine-in.StartLine >= 300 {
			return failure(gh.ErrInput)
		}
		text, err := reader.ReadFile(ctx, in.Repository, in.Path, in.Ref)
		status := "ready"
		if err != nil {
			status = "failed"
		}
		recordErr := store.Record(ctx, "github", status)
		if err != nil {
			return failure(err)
		}
		if len(text) > gh.MaxFileBytes {
			return failure(gh.ErrIncompatible)
		}
		lines := strings.Split(text, "\n")
		if in.StartLine > len(lines) {
			return failure(gh.ErrInput)
		}
		if in.EndLine > len(lines) {
			in.EndLine = len(lines)
		}
		file := File{Repository: in.Repository, Path: in.Path, Revision: in.Ref, StartLine: in.StartLine, EndLine: in.EndLine, Text: strings.Join(lines[in.StartLine-1:in.EndLine], "\n")}
		if in.EndLine < len(lines) {
			file.NextLine = in.EndLine + 1
		}
		out := envelope(file, "repository")
		u := url.URL{Scheme: "https", Host: "github.com", Path: "/" + in.Repository + "/blob/" + in.Ref + "/" + in.Path, Fragment: fmt.Sprintf("L%d-L%d", file.StartLine, file.EndLine)}
		out.Sources = []Source{{u.String(), in.Ref}}
		out.Warnings = append(out.Warnings, "Repository evidence only; deployment on Astar has not been verified.")
		if recordErr != nil {
			out.Warnings = append(out.Warnings, "Provider observation could not be persisted.")
		}
		// Budget both structured and compatibility text content, which duplicate the output.
		raw, _ := json.Marshal(out)
		if len(raw) > 60*1024 {
			return failure(errors.New("response too large"))
		}
		return nil, out, nil
	})
	addCatalog(s, store)
	return s
}
