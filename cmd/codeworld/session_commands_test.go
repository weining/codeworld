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
	if err := runSessionsCommand(&out, root, globalOptions{}, nil); err != nil || !strings.Contains(out.String(), sess.ID) {
		t.Fatalf("sessions output=%q err=%v", out.String(), err)
	}
	out.Reset()
	if err := runSessionsCommand(&out, root, globalOptions{}, []string{"rename", sess.ID, "managed", "work"}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if resolved, err := store.Resolve("managed work"); err != nil || resolved.ID != sess.ID {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
	out.Reset()
	if err := runArchiveCommand(&out, root, globalOptions{}, []string{"managed work"}, false); err != nil {
		t.Fatalf("archive: %v", err)
	}
	out.Reset()
	if err := runSessionsCommand(&out, root, globalOptions{}, []string{"--archived"}); err != nil || !strings.Contains(out.String(), sess.ID) {
		t.Fatalf("archived output=%q err=%v", out.String(), err)
	}
	if err := runArchiveCommand(&out, root, globalOptions{}, []string{sess.ID}, true); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if err := runDeleteCommand(&out, root, globalOptions{}, []string{sess.ID}); err != nil {
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
	if err := runForkCommand(context.Background(), strings.NewReader("/exit\n"), &out, &stderr, root, globalOptions{}, []string{"--last"}); err != nil {
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

func TestResumePickerSelectsNumberWithoutConsumingExtraInput(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	store := session.NewStore(canonicalRoot)
	for _, id := range []string{"first-session", "second-session"} {
		sess := session.New(canonicalRoot, "deepseek", id+"-model")
		sess.ID = id
		if err := store.SaveCurrent(sess); err != nil {
			t.Fatal(err)
		}
	}
	var out, stderr bytes.Buffer
	if err := runResumeCommand(context.Background(), strings.NewReader("2\nremaining-input\n"), &out, &stderr, root, globalOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	current, err := store.LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != "first-session" {
		t.Fatalf("current session = %q", current.ID)
	}
	if !strings.Contains(out.String(), "Select a session to resume") || !strings.Contains(out.String(), "first-session") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestSelectSessionArgumentSupportsFlagsPromptAndValidation(t *testing.T) {
	root := t.TempDir()
	store := session.NewStore(root)
	sess := session.New(root, "deepseek", "model")
	sess.ID = "session-one"
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatal(err)
	}
	id, prompt, err := selectSessionArgument(strings.NewReader(""), &bytes.Buffer{}, store, []string{"--all", "--last", "continue", "now"}, "resume")
	if err != nil || id != sess.ID || prompt != "continue now" {
		t.Fatalf("id=%q prompt=%q err=%v", id, prompt, err)
	}
	if _, _, err := selectSessionArgument(strings.NewReader("9\n"), &bytes.Buffer{}, store, nil, "resume"); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("range error = %v", err)
	}
}
