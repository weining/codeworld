package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

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

func TestDefaultRegistryIncludesIndexWorkspaceTool(t *testing.T) {
	registry := NewDefaultRegistry(newTestWorkspace(t))

	if _, ok := registry.Get("index_workspace"); !ok {
		t.Fatalf("default registry missing index_workspace")
	}
}

func TestDefaultRegistryIncludesWebSearchTool(t *testing.T) {
	registry := NewDefaultRegistry(newTestWorkspace(t))

	if _, ok := registry.Get("web_search"); !ok {
		t.Fatalf("default registry missing web_search")
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

func TestWriteFilePermissionRequest(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewWriteFileTool(ws)

	req, err := tool.PermissionRequest(json.RawMessage(`{"path":"notes/new.txt","content":"hello"}`))
	if err != nil {
		t.Fatalf("PermissionRequest returned error: %v", err)
	}
	if req.Action != permissions.ActionWrite || req.Risk != permissions.RiskWrite {
		t.Fatalf("PermissionRequest = (%q, %q), want write/write", req.Action, req.Risk)
	}
	if req.Target != "notes/new.txt" {
		t.Fatalf("Target = %q, want notes/new.txt", req.Target)
	}
	if req.Reason == "" {
		t.Fatalf("Reason is empty")
	}
}

func TestWriteFilePermissionRequestUsesReason(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewWriteFileTool(ws)

	req, err := tool.PermissionRequest(json.RawMessage(`{"path":"notes/new.txt","content":"hello","reason":"create notes"}`))
	if err != nil {
		t.Fatalf("PermissionRequest returned error: %v", err)
	}
	if req.Reason != "create notes" {
		t.Fatalf("Reason = %q, want create notes", req.Reason)
	}
}

func TestWriteFileCreatesParentDirectoriesAndWritesContent(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewWriteFileTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"notes/2026/today.txt","content":"hello\nworld"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws.Root, "notes", "2026", "today.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello\nworld" {
		t.Fatalf("written content = %q, want hello\\nworld", data)
	}
	if got := result.Metadata["path"]; got != "notes/2026/today.txt" {
		t.Fatalf("metadata path = %#v, want notes/2026/today.txt", got)
	}
	if got := result.Metadata["bytes"]; got != len("hello\nworld") {
		t.Fatalf("metadata bytes = %#v, want %d", got, len("hello\nworld"))
	}
}

func TestWriteFileRejectsPathEscape(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewWriteFileTool(ws)

	if _, err := tool.PermissionRequest(json.RawMessage(`{"path":"../secret.txt","content":"nope"}`)); err == nil {
		t.Fatalf("PermissionRequest accepted path escape")
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"../secret.txt","content":"nope"}`)); err == nil {
		t.Fatalf("Execute accepted path escape")
	}
}

func TestWriteFileRejectsInvalidJSON(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewWriteFileTool(ws)

	if _, err := tool.PermissionRequest(json.RawMessage(`{"path":`)); err == nil {
		t.Fatalf("PermissionRequest accepted invalid JSON")
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":`)); err == nil {
		t.Fatalf("Execute accepted invalid JSON")
	}
}

func TestWriteFileRejectsMissingOrNullContent(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewWriteFileTool(ws)
	cases := []string{
		`{"path":"empty.txt"}`,
		`{"path":"empty.txt","content":null}`,
	}

	for _, tc := range cases {
		if _, err := tool.PermissionRequest(json.RawMessage(tc)); err == nil {
			t.Fatalf("PermissionRequest accepted %s", tc)
		}
		if _, err := tool.Execute(context.Background(), json.RawMessage(tc)); err == nil {
			t.Fatalf("Execute accepted %s", tc)
		}
	}

	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"empty.txt","content":""}`)); err != nil {
		t.Fatalf("Execute rejected explicit empty content: %v", err)
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

func TestApplyPatchRejectsPathEscape(t *testing.T) {
	ws := newTestWorkspace(t)
	patch := `diff --git a/../secret.txt b/../secret.txt
--- a/../secret.txt
+++ b/../secret.txt
@@ -0,0 +1 @@
+secret
`
	args, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := NewApplyPatchTool(ws).Execute(context.Background(), args); err == nil {
		t.Fatalf("Execute accepted path escape")
	}
}

func TestApplyPatchRejectsQuotedHeaderPathEscape(t *testing.T) {
	ws := newTestWorkspace(t)
	cases := []struct {
		name  string
		patch string
	}{
		{
			name:  "diff git",
			patch: "diff --git \"a/inside file\" \"b/../outside file\"\n",
		},
		{
			name: "file header",
			patch: `diff --git "a/inside file" "b/inside file"
--- "a/inside file"
+++ "b/../outside file"
@@ -0,0 +1 @@
+secret
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"patch": tc.patch})
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if _, err := NewApplyPatchTool(ws).PermissionRequest(args); err == nil {
				t.Fatalf("PermissionRequest accepted quoted path escape")
			}
		})
	}
}

func TestApplyPatchRejectsExtraUnquotedHeaderPathEscape(t *testing.T) {
	ws := newTestWorkspace(t)
	patch := `diff --git a/foo b/foo
--- a/foo
+++ b/foo ../../../outside
@@ -0,0 +1 @@
+secret
`
	args, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := NewApplyPatchTool(ws).PermissionRequest(args); err == nil {
		t.Fatalf("PermissionRequest accepted extra unquoted header path escape")
	}
}

func TestApplyPatchAllowsHunkBodyThatLooksLikeHeader(t *testing.T) {
	ws := newTestWorkspace(t)
	writeFile(t, ws.Root, "notes.txt", "old\n")
	patch := `diff --git a/notes.txt b/notes.txt
--- a/notes.txt
+++ b/notes.txt
@@ -1 +1,2 @@
 old
+++ ../outside
`
	args, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	result, err := NewApplyPatchTool(ws).Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned error: %v\n%s", err, result.Content)
	}
	data, err := os.ReadFile(filepath.Join(ws.Root, "notes.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "old\n++ ../outside\n" {
		t.Fatalf("patched content = %q, want hunk body preserved", data)
	}
}

func TestApplyPatchRejectsPlainMultifileHeaderPathEscape(t *testing.T) {
	ws := newTestWorkspace(t)
	patch := `--- a/one.txt
+++ b/one.txt
@@ -0,0 +1 @@
+one
--- a/two.txt
+++ b/../outside.txt
@@ -0,0 +1 @@
+two
`
	args, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := NewApplyPatchTool(ws).PermissionRequest(args); err == nil {
		t.Fatalf("PermissionRequest accepted plain multifile header path escape")
	}
}

func TestApplyPatchRejectsDefaultStripPathEscape(t *testing.T) {
	ws := newTestWorkspace(t)
	patch := `--- safe/../target.txt
+++ safe/../target.txt
@@ -0,0 +1 @@
+target
`
	args, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := NewApplyPatchTool(ws).PermissionRequest(args); err == nil {
		t.Fatalf("PermissionRequest accepted path that escapes after git apply -p1 stripping")
	}
}

func TestApplyPatchRejectsExtendedHeaderPathEscape(t *testing.T) {
	ws := newTestWorkspace(t)
	cases := []struct {
		name  string
		patch string
	}{
		{
			name: "rename to",
			patch: `diff --git a/old.txt b/new.txt
similarity index 100%
rename from old.txt
rename to ../outside.txt
`,
		},
		{
			name: "copy to",
			patch: `diff --git a/old.txt b/new.txt
similarity index 100%
copy from old.txt
copy to ../outside.txt
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"patch": tc.patch})
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if _, err := NewApplyPatchTool(ws).PermissionRequest(args); err == nil {
				t.Fatalf("PermissionRequest accepted %s path escape", tc.name)
			}
		})
	}
}

func TestApplyPatchPermissionRequestUsesReason(t *testing.T) {
	ws := newTestWorkspace(t)
	patch := `diff --git a/notes.txt b/notes.txt
--- a/notes.txt
+++ b/notes.txt
@@ -1 +1 @@
-old
+new
`
	args, err := json.Marshal(map[string]string{"patch": patch, "reason": "update notes"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	req, err := NewApplyPatchTool(ws).PermissionRequest(args)
	if err != nil {
		t.Fatalf("PermissionRequest returned error: %v", err)
	}
	if req.Reason != "update notes" {
		t.Fatalf("Reason = %q, want update notes", req.Reason)
	}
}

func TestApplyPatchAllowsDevNullPath(t *testing.T) {
	ws := newTestWorkspace(t)
	patch := `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..ce01362
--- /dev/null
+++ b/new.txt
@@ -0,0 +1 @@
+new
`
	args, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	req, err := NewApplyPatchTool(ws).PermissionRequest(args)
	if err != nil {
		t.Fatalf("PermissionRequest returned error: %v", err)
	}
	if req.Action != permissions.ActionWrite || req.Risk != permissions.RiskWrite {
		t.Fatalf("PermissionRequest = (%q, %q), want write/write", req.Action, req.Risk)
	}
}

func TestApplyPatchChecksThenAppliesPatch(t *testing.T) {
	ws := newTestWorkspace(t)
	writeFile(t, ws.Root, "notes.txt", "old\n")
	patch := `diff --git a/notes.txt b/notes.txt
index 3367afd..3e75765 100644
--- a/notes.txt
+++ b/notes.txt
@@ -1 +1 @@
-old
+new
`
	args, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	result, err := NewApplyPatchTool(ws).Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned error: %v\n%s", err, result.Content)
	}
	data, err := os.ReadFile(filepath.Join(ws.Root, "notes.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new\n" {
		t.Fatalf("patched content = %q, want new\\n", data)
	}
	if got := result.Metadata["command"]; got != "git apply" {
		t.Fatalf("metadata command = %#v, want git apply", got)
	}
	if got := result.Metadata["exit_code"]; got != 0 {
		t.Fatalf("metadata exit_code = %#v, want 0", got)
	}
}

func TestApplyPatchPreservesCheckFailureOutput(t *testing.T) {
	ws := newTestWorkspace(t)
	writeFile(t, ws.Root, "notes.txt", "actual\n")
	patch := `diff --git a/notes.txt b/notes.txt
index 3367afd..3e75765 100644
--- a/notes.txt
+++ b/notes.txt
@@ -1 +1 @@
-old
+new
`
	args, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	result, err := NewApplyPatchTool(ws).Execute(context.Background(), args)
	if err == nil {
		t.Fatalf("Execute returned nil error for rejected patch")
	}
	if result.Content == "" {
		t.Fatalf("Content is empty, want git apply --check failure output")
	}
	if got := result.Metadata["command"]; got != "git apply --check" {
		t.Fatalf("metadata command = %#v, want git apply --check", got)
	}
	if got := result.Metadata["exit_code"]; got == 0 {
		t.Fatalf("metadata exit_code = %#v, want nonzero", got)
	}
}

func TestApplyPatchRejectsInvalidJSON(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewApplyPatchTool(ws)

	if _, err := tool.PermissionRequest(json.RawMessage(`{"patch":`)); err == nil {
		t.Fatalf("PermissionRequest accepted invalid JSON")
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"patch":`)); err == nil {
		t.Fatalf("Execute accepted invalid JSON")
	}
}

func TestApplyPatchPreservesLargeCheckFailureStderr(t *testing.T) {
	ws := newTestWorkspace(t)
	var patch strings.Builder
	for i := 0; i < 1000; i++ {
		_, _ = fmt.Fprintf(&patch, `diff --git a/missing%d.txt b/missing%d.txt
--- a/missing%d.txt
+++ b/missing%d.txt
@@ -1 +1 @@
-old
+new
`, i, i, i, i)
	}
	args, err := json.Marshal(map[string]string{"patch": patch.String()})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	result, err := NewApplyPatchTool(ws).Execute(context.Background(), args)
	if err == nil {
		t.Fatalf("Execute returned nil error for rejected patch")
	}
	if result.Content == "" {
		t.Fatalf("Content is empty, want git apply --check stderr")
	}
	if len(result.Content) > int(defaultReadLimit) {
		t.Fatalf("content length = %d, want at most %d", len(result.Content), defaultReadLimit)
	}
	if got := result.Metadata["command"]; got != "git apply --check" {
		t.Fatalf("metadata command = %#v, want git apply --check", got)
	}
	if got := result.Metadata["truncated"]; got != true {
		t.Fatalf("metadata truncated = %#v, want true", got)
	}
	if got := result.Metadata["stderr_truncated"]; got != true {
		t.Fatalf("metadata stderr_truncated = %#v, want true", got)
	}
}

func TestShellPermissionRequestClassifiesGoTestAsExecute(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)

	req, err := tool.PermissionRequest(json.RawMessage(`{"command":"go test ./..."}`))
	if err != nil {
		t.Fatalf("PermissionRequest returned error: %v", err)
	}
	if req.Action != permissions.ActionShell || req.Risk != permissions.RiskExecute {
		t.Fatalf("PermissionRequest = (%q, %q), want shell/execute", req.Action, req.Risk)
	}
	if req.Target != "go test ./..." {
		t.Fatalf("Target = %q, want command", req.Target)
	}
}

func TestShellPermissionRequestUsesReason(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)

	req, err := tool.PermissionRequest(json.RawMessage(`{"command":"go test ./...","reason":"run tests"}`))
	if err != nil {
		t.Fatalf("PermissionRequest returned error: %v", err)
	}
	if req.Reason != "run tests" {
		t.Fatalf("Reason = %q, want run tests", req.Reason)
	}
}

func TestShellPermissionRequestClassifiesCommonRisks(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)
	cases := []struct {
		command string
		risk    permissions.Risk
	}{
		{command: "cat notes.txt", risk: permissions.RiskRead},
		{command: "go vet ./... 2>&1", risk: permissions.RiskExecute},
		{command: "touch notes.txt", risk: permissions.RiskWrite},
		{command: "mkdir notes", risk: permissions.RiskWrite},
		{command: "printf hello > notes.txt", risk: permissions.RiskWrite},
		{command: "git reset --hard", risk: permissions.RiskDestructive},
		{command: "git clean -fd", risk: permissions.RiskDestructive},
		{command: "git -C . reset --hard", risk: permissions.RiskDestructive},
		{command: "git -C . clean -fd", risk: permissions.RiskDestructive},
		{command: "git -C . push", risk: permissions.RiskNetwork},
		{command: "git submodule update --init --recursive", risk: permissions.RiskNetwork},
		{command: "git apply patch.diff", risk: permissions.RiskWrite},
		{command: "git rm notes.txt", risk: permissions.RiskDestructive},
		{command: "git stash", risk: permissions.RiskWrite},
		{command: "git branch -D feature", risk: permissions.RiskDestructive},
		{command: "git branch -d feature", risk: permissions.RiskDestructive},
		{command: "go mod tidy", risk: permissions.RiskWrite},
		{command: "go install example.com/tool@latest", risk: permissions.RiskNetwork},
		{command: "brew install git", risk: permissions.RiskNetwork},
		{command: "apt install git", risk: permissions.RiskNetwork},
		{command: "cargo install ripgrep", risk: permissions.RiskNetwork},
		{command: "curl https://example.com", risk: permissions.RiskNetwork},
		{command: "find . -delete", risk: permissions.RiskDestructive},
		{command: "find . -exec rm -rf {} +", risk: permissions.RiskDestructive},
		{command: "find . -exec sh -c 'rm -rf \"$1\"' sh {} \\;", risk: permissions.RiskDestructive},
		{command: "find . -exec true {} \\; -exec rm -rf {} \\;", risk: permissions.RiskDestructive},
		{command: "find . -exec curl https://example.com {} \\;", risk: permissions.RiskNetwork},
		{command: "find . -exec sed -i s/a/b/g {} \\;", risk: permissions.RiskWrite},
		{command: "sed -i s/a/b/g notes.txt", risk: permissions.RiskWrite},
		{command: "sed --in-place s/a/b/g notes.txt", risk: permissions.RiskWrite},
		{command: "sed -Ei s/a/b/g notes.txt", risk: permissions.RiskWrite},
		{command: "find . -print0 | xargs -0 rm -rf", risk: permissions.RiskDestructive},
		{command: "xargs rm -rf < files.txt", risk: permissions.RiskDestructive},
		{command: "pip install requests", risk: permissions.RiskNetwork},
		{command: "tar -xf archive.tar", risk: permissions.RiskWrite},
		{command: "tar xf archive.tar", risk: permissions.RiskWrite},
		{command: "tar xzf archive.tar", risk: permissions.RiskWrite},
		{command: "cat README.md; rm -rf .", risk: permissions.RiskDestructive},
		{command: "cat README.md && curl https://example.com", risk: permissions.RiskNetwork},
		{command: "cat README.md & rm -rf .", risk: permissions.RiskDestructive},
		{command: "(rm -rf .)", risk: permissions.RiskDestructive},
		{command: "echo $(curl https://example.com)", risk: permissions.RiskNetwork},
		{command: "sudo rm -rf .", risk: permissions.RiskDestructive},
		{command: "sudo -E rm -rf .", risk: permissions.RiskDestructive},
		{command: "sudo -u root rm -rf .", risk: permissions.RiskDestructive},
		{command: "FOO=1 rm -rf .", risk: permissions.RiskDestructive},
		{command: "FOO=1 curl https://example.com", risk: permissions.RiskNetwork},
		{command: "env curl https://example.com", risk: permissions.RiskNetwork},
		{command: "env -u PATH curl https://example.com", risk: permissions.RiskNetwork},
		{command: "timeout 5 rm -rf .", risk: permissions.RiskDestructive},
		{command: "timeout --preserve-status 5 curl https://example.com", risk: permissions.RiskNetwork},
		{command: "bash -c 'rm -rf .'", risk: permissions.RiskDestructive},
		{command: "bash -lc 'curl https://example.com'", risk: permissions.RiskNetwork},
	}

	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"command": tc.command})
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			req, err := tool.PermissionRequest(args)
			if err != nil {
				t.Fatalf("PermissionRequest returned error: %v", err)
			}
			if req.Risk != tc.risk {
				t.Fatalf("Risk = %q, want %q", req.Risk, tc.risk)
			}
		})
	}
}

func TestShellRejectsInvalidJSON(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)

	if _, err := tool.PermissionRequest(json.RawMessage(`{"command":`)); err == nil {
		t.Fatalf("PermissionRequest accepted invalid JSON")
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"command":`)); err == nil {
		t.Fatalf("Execute accepted invalid JSON")
	}
}

func TestShellRejectsCwdEscape(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)

	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"pwd","cwd":".."}`)); err == nil {
		t.Fatalf("Execute accepted cwd escape")
	}
}

func TestShellDoesNotStartWithCanceledContext(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := tool.Execute(ctx, json.RawMessage(`{"command":"touch should_not_exist"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context canceled", err)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, "should_not_exist")); !os.IsNotExist(err) {
		t.Fatalf("shell command ran despite canceled context, stat err=%v", err)
	}
}

func TestShellReturnsContextCanceledAfterStartedCommandIsCanceled(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	var result Result
	var err error
	go func() {
		result, err = tool.Execute(ctx, json.RawMessage(`{"command":"echo started > started.txt; sleep 30"}`))
		close(done)
	}()

	startedPath := filepath.Join(ws.Root, "started.txt")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, statErr := os.Stat(startedPath); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			t.Fatalf("Stat started marker: %v", statErr)
		}
		if time.Now().After(deadline) {
			t.Fatalf("shell command did not start before cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Execute did not return after context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context canceled", err)
	}
	if got := result.Metadata["timed_out"]; got != false {
		t.Fatalf("metadata timed_out = %#v, want false", got)
	}
}

func TestShellUsesDefaultTimeoutWhenTimeoutOmitted(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"printf ok"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("Content = %q, want ok", result.Content)
	}
	if got := result.Metadata["timed_out"]; got != false {
		t.Fatalf("metadata timed_out = %#v, want false", got)
	}
}

func TestShellReportsTimedOut(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"sleep 1","timeout_ms":1}`))
	if err == nil {
		t.Fatalf("Execute returned nil error for timed out command")
	}
	if got := result.Metadata["timed_out"]; got != true {
		t.Fatalf("metadata timed_out = %#v, want true", got)
	}
}

func TestShellTimeoutKillsChildProcesses(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)

	start := time.Now()
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"sleep 30 & echo $! > child.pid; wait","timeout_ms":10}`))
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Execute took %s after timeout, want child cleanup within 2s", elapsed)
	}
	if err == nil {
		t.Fatalf("Execute returned nil error for timed out command")
	}
	if got := result.Metadata["timed_out"]; got != true {
		t.Fatalf("metadata timed_out = %#v, want true", got)
	}

	data, err := os.ReadFile(filepath.Join(ws.Root, "child.pid"))
	if err != nil {
		t.Fatalf("ReadFile child.pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("Atoi child pid: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	})
	if waitForProcessExit(pid, 2*time.Second) {
		return
	}
	t.Fatalf("child process %d is still running after shell timeout", pid)
}

func TestShellCleansBackgroundChildAfterSuccessfulCommand(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"sleep 30 >/dev/null 2>&1 & echo $! > child.pid"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got := result.Metadata["timed_out"]; got != false {
		t.Fatalf("metadata timed_out = %#v, want false", got)
	}

	data, err := os.ReadFile(filepath.Join(ws.Root, "child.pid"))
	if err != nil {
		t.Fatalf("ReadFile child.pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("Atoi child pid: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	})
	if waitForProcessExit(pid, 2*time.Second) {
		return
	}
	t.Fatalf("background child process %d is still running after shell returned", pid)
}

func TestShellCleansBackgroundChildHoldingOutputPipesAfterSuccess(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)

	start := time.Now()
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"sleep 30 & echo $! > child.pid; echo ok"}`))
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Execute took %s after shell success, want child cleanup within 2s", elapsed)
	}
	if err != nil {
		t.Fatalf("Execute returned error: %v\n%s", err, result.Content)
	}
	if !strings.Contains(result.Content, "ok") {
		t.Fatalf("Content = %q, want ok", result.Content)
	}
	if got := result.Metadata["timed_out"]; got != false {
		t.Fatalf("metadata timed_out = %#v, want false", got)
	}

	data, err := os.ReadFile(filepath.Join(ws.Root, "child.pid"))
	if err != nil {
		t.Fatalf("ReadFile child.pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("Atoi child pid: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	})
	if waitForProcessExit(pid, 2*time.Second) {
		return
	}
	t.Fatalf("background child process %d is still running after shell returned", pid)
}

func TestShellCapsRequestedTimeout(t *testing.T) {
	ws := newTestWorkspace(t)
	tool := NewShellTool(ws)

	args := fmt.Sprintf(`{"command":"printf ok","timeout_ms":%d}`, int64(math.MaxInt64))
	result, err := tool.Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got := result.Metadata["timeout_ms"]; got != int64(60000) {
		t.Fatalf("metadata timeout_ms = %#v, want 60000", got)
	}
}

func TestShellCapturesNonZeroExitOutputAndCapsOutput(t *testing.T) {
	ws := newTestWorkspace(t)
	command := `i=0; while [ "$i" -lt 25000 ]; do printf x; i=$((i+1)); done; printf failure >&2; exit 7`
	args, err := json.Marshal(map[string]any{"command": command})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	result, err := NewShellTool(ws).Execute(context.Background(), args)
	if err == nil {
		t.Fatalf("Execute returned nil error for nonzero command")
	}
	if len(result.Content) != int(defaultReadLimit) {
		t.Fatalf("content length = %d, want %d", len(result.Content), defaultReadLimit)
	}
	if !strings.Contains(result.Content, "failure") {
		t.Fatalf("content does not preserve stderr diagnostics: %q", result.Content)
	}
	if got := result.Metadata["exit_code"]; got != 7 {
		t.Fatalf("metadata exit_code = %#v, want 7", got)
	}
	if got := result.Metadata["truncated"]; got != true {
		t.Fatalf("metadata truncated = %#v, want true", got)
	}
	if got := result.Metadata["timed_out"]; got != false {
		t.Fatalf("metadata timed_out = %#v, want false", got)
	}
	if _, ok := result.Metadata["duration_ms"].(int64); !ok {
		t.Fatalf("metadata duration_ms = %#v, want int64", result.Metadata["duration_ms"])
	}
}

func TestWebSearchPermissionRequestRequiresNetworkConfirmation(t *testing.T) {
	tool := NewWebSearchTool()

	req, err := tool.PermissionRequest(json.RawMessage(`{"query":"golang testing"}`))
	if err != nil {
		t.Fatalf("PermissionRequest returned error: %v", err)
	}
	if req.Action != permissions.ActionRead || req.Risk != permissions.RiskNetwork {
		t.Fatalf("PermissionRequest = (%q, %q), want read/network", req.Action, req.Risk)
	}
	if req.Target != "golang testing" {
		t.Fatalf("Target = %q, want query", req.Target)
	}
}

func TestWebSearchExecutesAgainstSearchEndpoint(t *testing.T) {
	var requestedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`
<html><body>
  <a rel="nofollow" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F" class="result-link">The Go Documentation</a>
  <td class="result-snippet">Documentation for the Go programming language.</td>
  <a class="result-link" href="https://pkg.go.dev/testing">testing package</a>
  <td class="result-snippet">Package testing provides support for automated testing.</td>
</body></html>`))
	}))
	defer server.Close()

	tool := webSearchTool{endpoint: server.URL, client: server.Client()}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"golang docs","limit":2}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if requestedQuery != "golang docs" {
		t.Fatalf("query = %q, want golang docs", requestedQuery)
	}
	if !strings.Contains(result.Content, "1. The Go Documentation") || !strings.Contains(result.Content, "https://go.dev/doc/") {
		t.Fatalf("Content missing decoded first result:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "2. testing package") || !strings.Contains(result.Content, "Package testing provides") {
		t.Fatalf("Content missing second result:\n%s", result.Content)
	}
	if got := result.Metadata["results"]; got != 2 {
		t.Fatalf("metadata results = %#v, want 2", got)
	}
	if got := result.Metadata["truncated"]; got != false {
		t.Fatalf("metadata truncated = %#v, want false", got)
	}
}

func TestWebSearchRejectsEmptyQuery(t *testing.T) {
	tool := NewWebSearchTool()

	if _, err := tool.PermissionRequest(json.RawMessage(`{"query":""}`)); err == nil {
		t.Fatalf("PermissionRequest accepted empty query")
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":""}`)); err == nil {
		t.Fatalf("Execute accepted empty query")
	}
}

func waitForProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return syscall.Kill(pid, 0) == syscall.ESRCH
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

func TestEmptyObjectSchemasOmitRequired(t *testing.T) {
	ws := newTestWorkspace(t)

	for _, tool := range []Tool{NewGitStatusTool(ws), NewGitDiffTool(ws)} {
		schema := tool.Definition().InputSchema
		if _, ok := schema["required"]; ok {
			t.Fatalf("%s schema has required field: %#v", tool.Definition().Name, schema["required"])
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
