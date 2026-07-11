package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/session"
)

func TestSessionCommandsListArchiveUnarchiveAndDelete(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	store := session.NewStore(canonicalRoot)
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-pro")
	sess.ID = "managed-session"
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	var out bytes.Buffer
	if err := runSessionsCommand(&out, root, "", nil); err != nil || !strings.Contains(out.String(), sess.ID) {
		t.Fatalf("sessions output=%q err=%v", out.String(), err)
	}
	out.Reset()
	if err := runArchiveCommand(&out, root, "", []string{sess.ID}, false); err != nil {
		t.Fatalf("archive: %v", err)
	}
	out.Reset()
	if err := runSessionsCommand(&out, root, "", []string{"--archived"}); err != nil || !strings.Contains(out.String(), sess.ID) {
		t.Fatalf("archived output=%q err=%v", out.String(), err)
	}
	if err := runArchiveCommand(&out, root, "", []string{sess.ID}, true); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if err := runDeleteCommand(&out, root, "", []string{sess.ID}); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestForkCommandCreatesNewInteractiveSession(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	store := session.NewStore(canonicalRoot)
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-pro")
	sess.ID = "source-session"
	sess.Messages = []session.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}}
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	var out, stderr bytes.Buffer
	if err := runForkCommand(context.Background(), strings.NewReader("/exit\n"), &out, &stderr, root, "", []string{"--last"}); err != nil {
		t.Fatalf("runForkCommand: %v", err)
	}
	current, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if current.ID == sess.ID || len(current.Messages) != 2 {
		t.Fatalf("forked current = %#v", current)
	}
	if !strings.Contains(stderr.String(), "forked session") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(canonicalRoot, ".codeworld", "sessions", sess.ID+".json")); err != nil {
		t.Fatalf("source session missing: %v", err)
	}
}
