package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/session"
)

func TestRunShowsHelpWithoutAPIKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	var out bytes.Buffer
	var stderr bytes.Buffer

	err := runWithIO(strings.NewReader("/help\n/exit\n"), &out, &stderr)
	if err != nil {
		t.Fatalf("runWithIO returned error: %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"codeworld",
		"Type /help for commands.",
		"/help /model /status /diff /clear /exit",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout missing %q in:\n%s", want, output)
		}
	}
}

func TestRunUsesRestoredSessionModel(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	root := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir temp root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	store := session.NewStore(root)
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-flash")
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}

	app, err := newAppWithIO(strings.NewReader("/exit\n"), &outDiscard{}, &outDiscard{}, root)
	if err != nil {
		t.Fatalf("newAppWithIO returned error: %v", err)
	}
	if app.Runner.ModelName != "deepseek-v4-flash" {
		t.Fatalf("runner model = %q, want restored session model", app.Runner.ModelName)
	}

	var out bytes.Buffer
	var stderr bytes.Buffer
	err = runWithIO(strings.NewReader("/status\n/exit\n"), &out, &stderr)
	if err != nil {
		t.Fatalf("runWithIO returned error: %v", err)
	}
	if !strings.Contains(out.String(), "model=deepseek-v4-flash") {
		t.Fatalf("stdout = %q, want restored model in status", out.String())
	}
	current, err := os.ReadFile(filepath.Join(root, ".codeworld", "current-session.json"))
	if err != nil {
		t.Fatalf("Read current session: %v", err)
	}
	if !strings.Contains(string(current), "deepseek-v4-flash") {
		t.Fatalf("current session = %s, want restored model preserved", current)
	}
}

func TestModelCallLogPath(t *testing.T) {
	root := filepath.Join("tmp", "workspace")
	got := modelCallLogPath(root)
	want := filepath.Join(root, ".codeworld", "logs", "model-calls.jsonl")
	if got != want {
		t.Fatalf("modelCallLogPath = %q, want %q", got, want)
	}
}

type outDiscard struct{}

func (outDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
