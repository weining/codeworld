package app

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/context/indexer"
	"codeworld/internal/session"
)

func TestNewRuntimeBuildsREPLDependencies(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   strings.NewReader("/exit\n"),
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if rt.Workspace.Root != canonicalRoot {
		t.Fatalf("workspace root = %q, want %q", rt.Workspace.Root, canonicalRoot)
	}
	if rt.Session.Provider != "deepseek" || rt.Session.Model != "deepseek-v4-pro" {
		t.Fatalf("session provider/model = %s/%s, want deepseek/deepseek-v4-pro", rt.Session.Provider, rt.Session.Model)
	}
	if rt.Runner.Model == nil || rt.Runner.Tools == nil {
		t.Fatalf("runtime runner not wired: %#v", rt.Runner)
	}
	if rt.Diff == nil {
		t.Fatalf("runtime diff function missing")
	}
}

func TestNewRuntimeRestoresSessionMessagesAndUsage(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	store := session.NewStore(canonicalRoot)
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-pro")
	sess.Messages = []session.Message{{Role: "user", Content: "old"}}
	sess.Usage = session.Usage{InputTokens: 10, OutputTokens: 2, CacheTokens: 3, TotalTokens: 12}
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if len(rt.Messages) != 1 || rt.Messages[0].Content != "old" {
		t.Fatalf("messages = %#v, want restored user message", rt.Messages)
	}
	if rt.Usage.InputTokens != 10 || rt.Usage.OutputTokens != 2 || rt.Usage.CacheTokens != 3 || rt.Usage.TotalTokens != 12 {
		t.Fatalf("usage = %#v, want restored session usage", rt.Usage)
	}
}

func TestNewRuntimeIncludesWorkspaceIndexSummaryInSystemPrompt(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	idx := indexer.Index{Root: canonicalRoot, Entries: []indexer.Entry{{Path: "main.go", Language: "go", Size: 12}}}
	if err := indexer.Save(indexer.DefaultPath(canonicalRoot), idx); err != nil {
		t.Fatalf("Save index: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if !strings.Contains(rt.Runner.SystemPrompt, "Workspace index:") || !strings.Contains(rt.Runner.SystemPrompt, "main.go go") {
		t.Fatalf("system prompt missing index summary:\n%s", rt.Runner.SystemPrompt)
	}
}
