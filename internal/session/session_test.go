package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCurrentPathReturnsWorkspaceCodeworldPath(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)

	got := store.CurrentPath()
	want := filepath.Join(root, ".codeworld", "current-session.json")
	if got != want {
		t.Fatalf("CurrentPath = %q, want %q", got, want)
	}
}

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

func TestLoadCurrentReturnsSavedSession(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	sess := New(root, "deepseek", "deepseek-v4-pro")
	sess.Messages = []Message{
		{Role: "user", Content: "run tests"},
		{Role: "assistant", Content: "ok"},
	}
	sess.Tools = []ToolEvent{
		{ID: "tool-1", Name: "go test", Summary: "passed", CreatedAt: sess.CreatedAt.Add(time.Second)},
	}
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
	if len(got.Messages) != 2 || got.Messages[0].Content != "run tests" || got.Messages[1].Role != "assistant" {
		t.Fatalf("LoadCurrent messages = %#v", got.Messages)
	}
	if len(got.Tools) != 1 || got.Tools[0].ID != "tool-1" || !got.Tools[0].CreatedAt.Equal(sess.Tools[0].CreatedAt) {
		t.Fatalf("LoadCurrent tools = %#v", got.Tools)
	}
}

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
