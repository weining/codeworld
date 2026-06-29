package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"codeworld/internal/context/graph"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/workspace"
)

type ContextStore struct {
	workspace    workspace.Workspace
	maxFileBytes int64
	mu           sync.Mutex
	graph        graph.Graph
	loaded       bool
}

func NewContextStore(ws workspace.Workspace, maxFileBytes int64) *ContextStore {
	return &ContextStore{workspace: ws, maxFileBytes: maxFileBytes}
}

func (s *ContextStore) Refresh() (graph.Graph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := graph.Build(s.workspace.Root, graph.Options{MaxFileBytes: s.maxFileBytes})
	if err != nil {
		return graph.Graph{}, err
	}
	s.graph = g
	s.loaded = true
	return g, nil
}

func (s *ContextStore) Graph() (graph.Graph, error) {
	s.mu.Lock()
	if s.loaded {
		g := s.graph
		s.mu.Unlock()
		return g, nil
	}
	s.mu.Unlock()
	return s.Refresh()
}

type contextRefreshTool struct {
	store *ContextStore
}

func NewContextRefreshTool(store *ContextStore) Tool {
	return contextRefreshTool{store: store}
}

func (t contextRefreshTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "context_refresh",
		Description: "Refresh the workspace context graph, including files, Go symbols, and git changed files.",
		InputSchema: objectSchema(map[string]any{}, nil),
	}
}

func (t contextRefreshTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	if err := parseEmptyArgs(args); err != nil {
		return permissions.Request{}, err
	}
	return readRequest("context_refresh", "."), nil
}

func (t contextRefreshTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := parseEmptyArgs(args); err != nil {
		return Result{}, err
	}
	g, err := t.store.Refresh()
	if err != nil {
		return Result{}, err
	}
	return Result{Content: g.SummaryText(80), Metadata: map[string]any{"files": len(g.Files)}}, nil
}

type contextSearchTool struct {
	store *ContextStore
}

type contextSearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func NewContextSearchTool(store *ContextStore) Tool {
	return contextSearchTool{store: store}
}

func (t contextSearchTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "context_search",
		Description: "Search the workspace context graph by path, content, or symbol name.",
		InputSchema: objectSchema(map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "integer", "minimum": 1},
		}, []string{"query"}),
	}
}

func (t contextSearchTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseContextSearchArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	return readRequest("context_search", parsed.Query), nil
}

func (t contextSearchTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	parsed, err := parseContextSearchArgs(args)
	if err != nil {
		return Result{}, err
	}
	g, err := t.store.Graph()
	if err != nil {
		return Result{}, err
	}
	results := g.Search(parsed.Query, parsed.Limit)
	lines := make([]string, 0, len(results))
	for _, result := range results {
		if result.Name != "" {
			lines = append(lines, fmt.Sprintf("%s:%d %s %s score=%d", result.Path, result.Line, result.Kind, result.Name, result.Score))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s score=%d %s", result.Path, result.Kind, result.Score, result.Preview))
	}
	return Result{Content: strings.Join(lines, "\n"), Metadata: map[string]any{"matches": len(results)}}, nil
}

func parseContextSearchArgs(args json.RawMessage) (contextSearchArgs, error) {
	var parsed contextSearchArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return contextSearchArgs{}, err
	}
	parsed.Query = strings.TrimSpace(parsed.Query)
	if parsed.Query == "" {
		return contextSearchArgs{}, fmt.Errorf("query is required")
	}
	if parsed.Limit <= 0 {
		parsed.Limit = 20
	}
	return parsed, nil
}

type contextOpenTool struct {
	store *ContextStore
}

func NewContextOpenTool(store *ContextStore) Tool {
	return contextOpenTool{store: store}
}

func (t contextOpenTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "context_open",
		Description: "Open a file from the workspace context graph with extracted symbol metadata.",
		InputSchema: objectSchema(map[string]any{
			"path": map[string]any{"type": "string"},
		}, []string{"path"}),
	}
}

func (t contextOpenTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parsePathArgs(args, "")
	if err != nil {
		return permissions.Request{}, err
	}
	return readRequest("context_open", parsed.Path), nil
}

func (t contextOpenTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	parsed, err := parsePathArgs(args, "")
	if err != nil {
		return Result{}, err
	}
	g, err := t.store.Graph()
	if err != nil {
		return Result{}, err
	}
	file, ok := g.File(parsed.Path)
	if !ok {
		return Result{}, fmt.Errorf("context path %q not found", parsed.Path)
	}
	var b strings.Builder
	if len(file.Symbols) > 0 {
		parts := make([]string, 0, len(file.Symbols))
		for _, symbol := range file.Symbols {
			parts = append(parts, symbol.Kind+" "+symbol.Name)
		}
		b.WriteString("symbols: " + strings.Join(parts, ", ") + "\n\n")
	}
	b.WriteString(file.Content)
	return Result{Content: b.String(), Metadata: map[string]any{"path": file.Path, "language": file.Language, "symbols": len(file.Symbols), "changed": file.Changed}}, nil
}
