package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildIndexSkipsGitCodeworldLogsAndLargeFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: main\n")
	writeFile(t, filepath.Join(root, ".codeworld", "logs", "model-calls.jsonl"), "{}\n")
	writeFile(t, filepath.Join(root, "large.txt"), strings.Repeat("x", 20))
	writeFile(t, filepath.Join(root, "bin.dat"), "abc\x00def")

	idx, err := Build(root, 10)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if _, ok := findEntry(idx, "main.go"); !ok {
		t.Fatalf("main.go missing from index: %#v", idx.Entries)
	}
	if _, ok := findEntry(idx, ".git/HEAD"); ok {
		t.Fatalf(".git entry should be skipped entirely")
	}
	if _, ok := findEntry(idx, ".codeworld/logs/model-calls.jsonl"); ok {
		t.Fatalf("logs entry should be skipped entirely")
	}
	large, ok := findEntry(idx, "large.txt")
	if !ok || !large.Skipped || large.Reason != "too_large" {
		t.Fatalf("large entry = %#v, want too_large skip", large)
	}
	binary, ok := findEntry(idx, "bin.dat")
	if !ok || !binary.Skipped || binary.Reason != "binary" {
		t.Fatalf("binary entry = %#v, want binary skip", binary)
	}
}

func TestSaveLoadAndSummaryAreDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "b_test.go"), "package b\n")
	writeFile(t, filepath.Join(root, "a.go"), "package a\n")

	idx, err := Build(root, 1024)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	path := filepath.Join(root, ".codeworld", "index.json")
	if err := Save(path, idx); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	summary := Summary(loaded, 10)
	if !strings.Contains(summary, "a.go go") || !strings.Contains(summary, "b_test.go go") {
		t.Fatalf("summary = %q, want go files", summary)
	}
	if strings.Index(summary, "a.go") > strings.Index(summary, "b_test.go") {
		t.Fatalf("summary order = %q, want deterministic path order", summary)
	}
}

func TestLanguageFromExtension(t *testing.T) {
	tests := map[string]string{
		"main.go":     "go",
		"app.ts":      "typescript",
		"script.py":   "python",
		"README.md":   "markdown",
		"unknown.zzz": "",
	}
	for path, want := range tests {
		if got := LanguageForPath(path); got != want {
			t.Fatalf("LanguageForPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func findEntry(idx Index, path string) (Entry, bool) {
	for _, entry := range idx.Entries {
		if entry.Path == path {
			return entry, true
		}
	}
	return Entry{}, false
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
