package graph

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestBuildExtractsFilesGoSymbolsAndGitChanges 验证对应场景的行为，避免后续改动破坏既有约束。
func TestBuildExtractsFilesGoSymbolsAndGitChanges(t *testing.T) {
	root := t.TempDir()
	writeGraphFile(t, root, "main.go", `package main

type Server struct{}

// Hello 是测试辅助函数，用于复用测试准备或断言逻辑。
func Hello() {}
`)
	writeGraphFile(t, root, "README.md", "hello docs\n")
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	g, err := Build(root, Options{MaxFileBytes: 256 * 1024})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	mainFile, ok := g.File("main.go")
	if !ok {
		t.Fatalf("main.go missing from graph: %#v", g.Files)
	}
	if !mainFile.Changed {
		t.Fatalf("main.go Changed = false, want true for untracked git file")
	}
	if !hasSymbol(mainFile.Symbols, "type", "Server") || !hasSymbol(mainFile.Symbols, "func", "Hello") {
		t.Fatalf("symbols = %#v, want Server type and Hello func", mainFile.Symbols)
	}
}

// TestSearchMatchesPathsContentAndSymbols 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSearchMatchesPathsContentAndSymbols(t *testing.T) {
	root := t.TempDir()
	writeGraphFile(t, root, "cmd/app/main.go", `package main

// RunServer 是测试辅助函数，用于复用测试准备或断言逻辑。
func RunServer() {}
`)
	writeGraphFile(t, root, "docs/notes.md", "deployment notes\n")

	g, err := Build(root, Options{MaxFileBytes: 256 * 1024})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	results := g.Search("server", 10)
	if len(results) == 0 || results[0].Path != "cmd/app/main.go" {
		t.Fatalf("results = %#v, want server match in Go file first", results)
	}
	results = g.Search("deployment", 10)
	if len(results) == 0 || results[0].Path != "docs/notes.md" {
		t.Fatalf("results = %#v, want content match in notes", results)
	}
}

// hasSymbol 是测试辅助函数，用于复用测试准备或断言逻辑。
func hasSymbol(symbols []Symbol, kind, name string) bool {
	for _, symbol := range symbols {
		if symbol.Kind == kind && symbol.Name == name {
			return true
		}
	}
	return false
}

// writeGraphFile 是测试辅助函数，用于复用测试准备或断言逻辑。
func writeGraphFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
