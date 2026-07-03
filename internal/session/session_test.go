package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCurrentPathReturnsWorkspaceCodeworldPath 验证对应场景的行为，避免后续改动破坏既有约束。
func TestCurrentPathReturnsWorkspaceCodeworldPath(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)

	got := store.CurrentPath()
	want := filepath.Join(root, ".codeworld", "current-session.json")
	if got != want {
		t.Fatalf("CurrentPath = %q, want %q", got, want)
	}
}

// TestSaveCurrentWritesCurrentAndArchiveWithRestrictivePermissions 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSaveCurrentWritesCurrentAndArchiveWithRestrictivePermissions(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	created := time.Date(2026, 6, 28, 4, 5, 6, 7, time.UTC)
	updated := created.Add(2 * time.Minute)
	sess := Session{
		ID:        "session-test",
		Workspace: root,
		Provider:  "deepseek",
		Model:     "deepseek-v4-pro",
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "tool", Content: "done", ToolCallID: "call-1"},
		},
		Tools: []ToolEvent{
			{ID: "tool-1", Name: "list", Summary: "listed files", CreatedAt: created},
		},
		CreatedAt: created,
		UpdatedAt: updated,
	}

	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent returned error: %v", err)
	}

	currentInfo := requireRegularFile(t, store.CurrentPath())
	if mode := currentInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("current session permissions = %o, want 600", mode)
	}

	archivePath := filepath.Join(root, ".codeworld", "sessions", sess.ID+".json")
	archiveInfo := requireRegularFile(t, archivePath)
	if mode := archiveInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("archive session permissions = %o, want 600", mode)
	}
}

// TestSaveCurrentRejectsTraversalSessionID 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSaveCurrentRejectsTraversalSessionID(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	outside := filepath.Join(root, ".codeworld", "escape.json")
	sess := New(root, "deepseek", "deepseek-v4-pro")
	sess.ID = "../escape"

	if err := store.SaveCurrent(sess); err == nil {
		t.Fatalf("SaveCurrent accepted traversal session ID")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("SaveCurrent wrote outside sessions directory: stat err=%v", err)
	}
}

// TestSaveCurrentRefreshesUpdatedAt 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSaveCurrentRefreshesUpdatedAt(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	oldUpdated := time.Now().UTC().Add(-time.Minute)
	sess := Session{
		ID:        "session-test",
		Workspace: root,
		Provider:  "deepseek",
		Model:     "deepseek-v4-pro",
		CreatedAt: oldUpdated.Add(-time.Minute),
		UpdatedAt: oldUpdated,
	}

	beforeSave := time.Now().UTC()
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent returned error: %v", err)
	}

	got, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent returned error: %v", err)
	}
	if !got.UpdatedAt.After(oldUpdated) {
		t.Fatalf("updated_at = %v, want after %v", got.UpdatedAt, oldUpdated)
	}
	if got.UpdatedAt.Before(beforeSave) {
		t.Fatalf("updated_at = %v, want no earlier than save start %v", got.UpdatedAt, beforeSave)
	}
	if got.UpdatedAt.Location() != time.UTC {
		t.Fatalf("updated_at location = %v, want UTC", got.UpdatedAt.Location())
	}
}

// TestLoadCurrentReturnsSavedSession 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadCurrentReturnsSavedSession(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	sess := New(root, "deepseek", "deepseek-v4-pro")
	sess.Messages = []Message{
		{Role: "user", Content: "run tests"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call-1", Name: "read_file", Arguments: []byte(`{"path":"go.mod"}`)}}},
		{Role: "tool", Content: "module codeworld", ToolCallID: "call-1"},
		{Role: "assistant", Content: "ok"},
	}
	sess.Summary = "previous conversation summary"
	sess.Tools = []ToolEvent{
		{ID: "tool-1", Name: "go test", Summary: "passed", CreatedAt: sess.CreatedAt.Add(time.Second)},
	}
	sess.Approvals = []Approval{{Kind: "shell", Command: "go test ./..."}}
	sess.Usage = Usage{InputTokens: 120, OutputTokens: 30, CacheTokens: 40, TotalTokens: 150}
	sess.UpdatedAt = time.Now().UTC().Add(-time.Minute)

	if sess.ID == "" {
		t.Fatalf("New returned empty ID")
	}
	if sess.CreatedAt.Location() != time.UTC || sess.UpdatedAt.Location() != time.UTC {
		t.Fatalf("New returned non-UTC timestamps: created=%v updated=%v", sess.CreatedAt.Location(), sess.UpdatedAt.Location())
	}

	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent returned error: %v", err)
	}

	got, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent returned error: %v", err)
	}

	if got.ID != sess.ID || got.Workspace != root || got.Provider != "deepseek" || got.Model != "deepseek-v4-pro" {
		t.Fatalf("LoadCurrent session metadata = %#v, want ID/workspace/provider/model from saved session", got)
	}
	if !got.CreatedAt.Equal(sess.CreatedAt) {
		t.Fatalf("LoadCurrent created_at = %v, want %v", got.CreatedAt, sess.CreatedAt)
	}
	if !got.UpdatedAt.After(sess.UpdatedAt) {
		t.Fatalf("LoadCurrent updated_at = %v, want after pre-save value %v", got.UpdatedAt, sess.UpdatedAt)
	}
	if len(got.Messages) != 4 || got.Messages[0].Content != "run tests" || got.Messages[3].Role != "assistant" {
		t.Fatalf("LoadCurrent messages = %#v", got.Messages)
	}
	if got.Summary != "previous conversation summary" {
		t.Fatalf("LoadCurrent summary = %q", got.Summary)
	}
	if len(got.Messages[1].ToolCalls) != 1 || got.Messages[1].ToolCalls[0].ID != "call-1" || got.Messages[1].ToolCalls[0].Name != "read_file" {
		t.Fatalf("LoadCurrent tool calls = %#v", got.Messages[1].ToolCalls)
	}
	assertJSONEqual(t, got.Messages[1].ToolCalls[0].Arguments, `{"path":"go.mod"}`)
	if len(got.Tools) != 1 || got.Tools[0].ID != "tool-1" || !got.Tools[0].CreatedAt.Equal(sess.Tools[0].CreatedAt) {
		t.Fatalf("LoadCurrent tools = %#v", got.Tools)
	}
	if len(got.Approvals) != 1 || got.Approvals[0].Kind != "shell" || got.Approvals[0].Command != "go test ./..." {
		t.Fatalf("LoadCurrent approvals = %#v", got.Approvals)
	}
	if got.Usage != sess.Usage {
		t.Fatalf("LoadCurrent usage = %#v, want %#v", got.Usage, sess.Usage)
	}
}

// requireRegularFile 是测试辅助函数，用于复用测试准备或断言逻辑。
func requireRegularFile(t *testing.T, path string) os.FileInfo {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %q: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%q is not a regular file: %v", path, info.Mode())
	}
	return info
}

// assertJSONEqual 是测试辅助函数，用于复用测试准备或断言逻辑。
func assertJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("Unmarshal got JSON: %v", err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("Unmarshal want JSON: %v", err)
	}
	gotData, _ := json.Marshal(gotValue)
	wantData, _ := json.Marshal(wantValue)
	if string(gotData) != string(wantData) {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}
