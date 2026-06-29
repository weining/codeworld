package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestContextToolsRefreshSearchAndOpen(t *testing.T) {
	ws := newTestWorkspace(t)
	writeFile(t, ws.Root, "main.go", `package main

func RunServer() {}
`)
	store := NewContextStore(ws, 256*1024)
	refresh := NewContextRefreshTool(store)
	search := NewContextSearchTool(store)
	open := NewContextOpenTool(store)

	refreshed, err := refresh.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("refresh Execute returned error: %v", err)
	}
	if !strings.Contains(refreshed.Content, "main.go go symbols=1") {
		t.Fatalf("refresh content = %q, want graph summary", refreshed.Content)
	}

	found, err := search.Execute(context.Background(), json.RawMessage(`{"query":"server"}`))
	if err != nil {
		t.Fatalf("search Execute returned error: %v", err)
	}
	if !strings.Contains(found.Content, "cmd") && !strings.Contains(found.Content, "main.go") {
		t.Fatalf("search content = %q, want main.go result", found.Content)
	}

	opened, err := open.Execute(context.Background(), json.RawMessage(`{"path":"main.go"}`))
	if err != nil {
		t.Fatalf("open Execute returned error: %v", err)
	}
	if !strings.Contains(opened.Content, "func RunServer") || !strings.Contains(opened.Content, "symbols: func RunServer") {
		t.Fatalf("open content = %q, want file content and symbols", opened.Content)
	}
}

func TestDefaultRegistryIncludesContextTools(t *testing.T) {
	registry := NewDefaultRegistry(newTestWorkspace(t))
	for _, name := range []string{"context_refresh", "context_search", "context_open"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("default registry missing %s", name)
		}
	}
}
