package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/permissions"
	"codeworld/internal/workspace"
)

func TestRegistryRejectsUnknownTool(t *testing.T) {
	registry := NewRegistry(nil, nil)

	if _, ok := registry.Get("missing"); ok {
		t.Fatalf("Get returned ok for unknown tool")
	}
	if _, err := registry.Execute(context.Background(), "missing", json.RawMessage(`{}`)); err == nil {
		t.Fatalf("Execute accepted unknown tool")
	}
}

func TestReadFileLimitedContentMetadataAndPermission(t *testing.T) {
	ws := newTestWorkspace(t)
	writeFile(t, ws.Root, "notes.txt", "0123456789")
	tool := NewReadFileTool(ws)

	req, err := tool.PermissionRequest(json.RawMessage(`{"path":"notes.txt"}`))
	if err != nil {
		t.Fatalf("PermissionRequest returned error: %v", err)
	}
	if req.Action != permissions.ActionRead || req.Risk != permissions.RiskRead {
		t.Fatalf("PermissionRequest = (%q, %q), want read/read", req.Action, req.Risk)
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"notes.txt","offset":2,"limit":4}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Content != "2345" {
		t.Fatalf("Content = %q, want 2345", result.Content)
	}
	if got := result.Metadata["path"]; got != "notes.txt" {
		t.Fatalf("metadata path = %#v, want notes.txt", got)
	}
	if got := result.Metadata["offset"]; got != int64(2) {
		t.Fatalf("metadata offset = %#v, want int64(2)", got)
	}
	if got := result.Metadata["limit"]; got != int64(4) {
		t.Fatalf("metadata limit = %#v, want int64(4)", got)
	}
	if got := result.Metadata["truncated"]; got != true {
		t.Fatalf("metadata truncated = %#v, want true", got)
	}
}

func TestReadFileRejectsPathEscapeAndNegativeOffset(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewReadFileTool(ws)

	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"../secret.txt"}`)); err == nil {
		t.Fatalf("Execute accepted path escape")
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"notes.txt","offset":-1}`)); err == nil {
		t.Fatalf("Execute accepted negative offset")
	}
}

func TestReadFileRejectsInvalidJSON(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewReadFileTool(ws)

	if _, err := tool.PermissionRequest(json.RawMessage(`{"path":`)); err == nil {
		t.Fatalf("PermissionRequest accepted invalid JSON")
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":`)); err == nil {
		t.Fatalf("Execute accepted invalid JSON")
	}
}

func TestReadFileClampsRequestedLimitToDefaultReadLimit(t *testing.T) {
	ws := newTestWorkspace(t)
	content := strings.Repeat("a", int(defaultReadLimit)+10)
	writeFile(t, ws.Root, "large.txt", content)
	tool := NewReadFileTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"large.txt","limit":1000000}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(result.Content) != int(defaultReadLimit) {
		t.Fatalf("content length = %d, want %d", len(result.Content), defaultReadLimit)
	}
	if got := result.Metadata["limit"]; got != defaultReadLimit {
		t.Fatalf("metadata limit = %#v, want %d", got, defaultReadLimit)
	}
	if got := result.Metadata["truncated"]; got != true {
		t.Fatalf("metadata truncated = %#v, want true", got)
	}
}

func TestListDirReturnsSortedEntriesWithDirectorySuffix(t *testing.T) {
	ws := newTestWorkspace(t)
	writeFile(t, ws.Root, "src/main.go", "package main\n")
	writeFile(t, ws.Root, "README.md", "readme")
	tool := NewListDirTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	want := "README.md\nsrc/"
	if result.Content != want {
		t.Fatalf("Content = %q, want %q", result.Content, want)
	}
}

func TestListDirCapsHugeDirectoryOutput(t *testing.T) {
	ws := newTestWorkspace(t)
	for i := 0; i < 3000; i++ {
		writeFile(t, ws.Root, filepath.Join("many", strings.Repeat("x", 20)+fmt.Sprintf("-%04d.txt", i)), "x")
	}
	tool := NewListDirTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"many"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(result.Content) > int(defaultReadLimit) {
		t.Fatalf("content length = %d, want at most %d", len(result.Content), defaultReadLimit)
	}
	if got := result.Metadata["truncated"]; got != true {
		t.Fatalf("metadata truncated = %#v, want true", got)
	}
}

func TestSearchFindsLiteralTextAndSkipsHeavyDirs(t *testing.T) {
	ws := newTestWorkspace(t)
	writeFile(t, ws.Root, "README.md", "needle here\n")
	writeFile(t, ws.Root, "src/main.go", "package main\n// needle\n")
	writeFile(t, ws.Root, "node_modules/pkg/index.js", "needle should be skipped\n")
	tool := NewSearchTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"needle","path":"."}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	for _, want := range []string{"README.md:1:needle here", "src/main.go:2:// needle"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("Content missing %q in:\n%s", want, result.Content)
		}
	}
	if strings.Contains(result.Content, "node_modules") {
		t.Fatalf("Content included heavy directory:\n%s", result.Content)
	}
	if got := result.Metadata["truncated"]; got != false {
		t.Fatalf("metadata truncated = %#v, want false", got)
	}
}

func TestSearchStopsAtTotalScanBudgetForNoMatches(t *testing.T) {
	ws := newTestWorkspace(t)
	for i := 0; i < 210; i++ {
		writeFile(t, ws.Root, fmt.Sprintf("file-%03d.txt", i), strings.Repeat("a", 200))
	}
	tool := NewSearchTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"missing","path":"."}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got := result.Metadata["search_truncated"]; got != true {
		t.Fatalf("metadata search_truncated = %#v, want true", got)
	}
	scannedFiles, ok := result.Metadata["scanned_files"].(int)
	if !ok {
		t.Fatalf("metadata scanned_files = %#v, want int", result.Metadata["scanned_files"])
	}
	if scannedFiles >= 210 {
		t.Fatalf("scanned_files = %d, want less than total files", scannedFiles)
	}
	scannedBytes, ok := result.Metadata["scanned_bytes"].(int64)
	if !ok {
		t.Fatalf("metadata scanned_bytes = %#v, want int64", result.Metadata["scanned_bytes"])
	}
	if scannedBytes <= 0 {
		t.Fatalf("scanned_bytes = %d, want positive", scannedBytes)
	}
}

func TestSearchStopsAtDirectoryTraversalBudget(t *testing.T) {
	ws := newTestWorkspace(t)
	for i := 0; i < 210; i++ {
		if err := os.MkdirAll(filepath.Join(ws.Root, fmt.Sprintf("dir-%03d", i)), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	tool := NewSearchTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"missing","path":"."}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got := result.Metadata["search_truncated"]; got != true {
		t.Fatalf("metadata search_truncated = %#v, want true", got)
	}
	scannedDirs, ok := result.Metadata["scanned_dirs"].(int)
	if !ok {
		t.Fatalf("metadata scanned_dirs = %#v, want int", result.Metadata["scanned_dirs"])
	}
	if scannedDirs >= 211 {
		t.Fatalf("scanned_dirs = %d, want less than total directories", scannedDirs)
	}
}

func TestSearchStopsAtEntryTraversalBudget(t *testing.T) {
	ws := newTestWorkspace(t)
	for i := 0; i < 300; i++ {
		writeFile(t, ws.Root, fmt.Sprintf("entry-%03d.txt", i), "")
	}
	tool := NewSearchTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"missing","path":"."}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(result.Content) > int(defaultReadLimit) {
		t.Fatalf("content length = %d, want at most %d", len(result.Content), defaultReadLimit)
	}
	if got := result.Metadata["search_truncated"]; got != true {
		t.Fatalf("metadata search_truncated = %#v, want true", got)
	}
	scannedEntries, ok := result.Metadata["scanned_entries"].(int)
	if !ok {
		t.Fatalf("metadata scanned_entries = %#v, want int", result.Metadata["scanned_entries"])
	}
	if scannedEntries >= 300 {
		t.Fatalf("scanned_entries = %d, want less than total entries", scannedEntries)
	}
}

func TestSearchLimitsMatchesAndMarksTruncated(t *testing.T) {
	ws := newTestWorkspace(t)
	var content strings.Builder
	for i := 0; i < 101; i++ {
		content.WriteString("needle\n")
	}
	writeFile(t, ws.Root, "many.txt", content.String())
	tool := NewSearchTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"needle","path":"."}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	lines := strings.Split(result.Content, "\n")
	if len(lines) != 100 {
		t.Fatalf("match count = %d, want 100", len(lines))
	}
	if got := result.Metadata["truncated"]; got != true {
		t.Fatalf("metadata truncated = %#v, want true", got)
	}
}

func TestSearchCapsLargeFileMatches(t *testing.T) {
	ws := newTestWorkspace(t)
	writeFile(t, ws.Root, "huge.txt", "needle "+strings.Repeat("x", int(defaultReadLimit)*2)+"\n")
	tool := NewSearchTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"needle","path":"."}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(result.Content) > int(defaultReadLimit) {
		t.Fatalf("content length = %d, want at most %d", len(result.Content), defaultReadLimit)
	}
	if got := result.Metadata["read_truncated"]; got != true {
		t.Fatalf("metadata read_truncated = %#v, want true", got)
	}
}

func TestRegistryDefinitionsAndExecution(t *testing.T) {
	ws := newTestWorkspace(t)
	writeFile(t, ws.Root, "README.md", "readme")
	registry := NewRegistry([]Tool{NewListDirTool(ws)}, nil)

	defs := registry.Definitions()
	if len(defs) != 1 || defs[0].Name != "list_dir" {
		t.Fatalf("Definitions = %#v, want one list_dir definition", defs)
	}

	result, err := registry.Execute(context.Background(), "list_dir", json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Content != "README.md" {
		t.Fatalf("Content = %q, want README.md", result.Content)
	}
}

func TestNewRegistryRegistersItemsAndExtra(t *testing.T) {
	ws := newTestWorkspace(t)
	registry := NewRegistry([]Tool{NewListDirTool(ws)}, []Tool{NewReadFileTool(ws)})

	for _, name := range []string{"list_dir", "read_file"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("Get(%q) returned !ok", name)
		}
	}
}

func TestGitToolsReturnOutputWithNonZeroExit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	ws := newTestWorkspace(t)

	for _, tool := range []Tool{NewGitStatusTool(ws), NewGitDiffTool(ws)} {
		result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
		if err == nil {
			t.Fatalf("%s returned nil error outside git repo", tool.Definition().Name)
		}
		if result.Content == "" {
			t.Fatalf("%s discarded git stderr", tool.Definition().Name)
		}
		if !strings.Contains(strings.ToLower(result.Content), "git repository") {
			t.Fatalf("%s content = %q, want git error output", tool.Definition().Name, result.Content)
		}
		if got := result.Metadata["command"]; got == "" {
			t.Fatalf("%s metadata command = %#v, want command", tool.Definition().Name, got)
		}
		if got := result.Metadata["exit_code"]; got == nil {
			t.Fatalf("%s metadata exit_code missing", tool.Definition().Name)
		}
	}
}

func TestGitDiffCapsOutputAndReportsMetadata(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	ws := newTestWorkspace(t)
	runGit(t, git, "-C", ws.Root, "init")
	writeFile(t, ws.Root, "large.txt", strings.Repeat("a\n", int(defaultReadLimit)))
	runGit(t, git, "-C", ws.Root, "add", "large.txt")
	runGit(t, git, "-C", ws.Root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	writeFile(t, ws.Root, "large.txt", strings.Repeat("b\n", int(defaultReadLimit)))

	result, err := NewGitDiffTool(ws).Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(result.Content) != int(defaultReadLimit) {
		t.Fatalf("content length = %d, want %d", len(result.Content), defaultReadLimit)
	}
	if got := result.Metadata["truncated"]; got != true {
		t.Fatalf("metadata truncated = %#v, want true", got)
	}
	if got := result.Metadata["command"]; got != "git diff" {
		t.Fatalf("metadata command = %#v, want git diff", got)
	}
	if got := result.Metadata["exit_code"]; got != 0 {
		t.Fatalf("metadata exit_code = %#v, want 0", got)
	}
}

func newTestWorkspace(t *testing.T) workspace.Workspace {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatalf("workspace.New returned error: %v", err)
	}
	return ws
}

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()
	fullPath := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func runGit(t *testing.T, git string, args ...string) {
	t.Helper()
	cmd := exec.Command(git, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}
