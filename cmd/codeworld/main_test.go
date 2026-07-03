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

	err := runWithIO(strings.NewReader("/help\n/exit\n"), &out, &stderr, nil)
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

func TestReplCommandShowsHelpWithoutAPIKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	var out bytes.Buffer
	var stderr bytes.Buffer

	err := runWithIO(strings.NewReader("/help\n/exit\n"), &out, &stderr, []string{"repl"})
	if err != nil {
		t.Fatalf("runWithIO returned error: %v", err)
	}
	if !strings.Contains(out.String(), "/help /model /status /diff /clear /exit") {
		t.Fatalf("stdout = %q, want repl help", out.String())
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
	err = runWithIO(strings.NewReader("/status\n/exit\n"), &out, &stderr, nil)
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

func TestRunCommandRequiresPrompt(t *testing.T) {
	var out bytes.Buffer
	var stderr bytes.Buffer

	err := runWithIO(strings.NewReader(""), &out, &stderr, []string{"run"})
	if err == nil || !strings.Contains(err.Error(), "usage: codeworld run") {
		t.Fatalf("err = %v, want run usage", err)
	}
}

func TestResumeLastRestoresNewestArchivedSession(t *testing.T) {
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
	oldSess := session.New(canonicalRoot, "deepseek", "deepseek-v4-old")
	oldSess.ID = "old-session"
	if err := store.SaveCurrent(oldSess); err != nil {
		t.Fatalf("SaveCurrent old: %v", err)
	}
	newSess := session.New(canonicalRoot, "deepseek", "deepseek-v4-new")
	newSess.ID = "new-session"
	if err := store.SaveCurrent(newSess); err != nil {
		t.Fatalf("SaveCurrent new: %v", err)
	}
	if err := store.SetCurrent("old-session"); err != nil {
		t.Fatalf("SetCurrent old: %v", err)
	}

	var out bytes.Buffer
	var stderr bytes.Buffer
	err = runWithIO(strings.NewReader("/status\n/exit\n"), &out, &stderr, []string{"resume", "--last"})
	if err != nil {
		t.Fatalf("runWithIO returned error: %v", err)
	}
	if !strings.Contains(out.String(), "model=deepseek-v4-new") {
		t.Fatalf("stdout = %q, want newest session model", out.String())
	}
}

func TestResumeSpecificSessionID(t *testing.T) {
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
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-resumed")
	sess.ID = "target-session"
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}

	var out bytes.Buffer
	var stderr bytes.Buffer
	err = runWithIO(strings.NewReader("/status\n/exit\n"), &out, &stderr, []string{"resume", "target-session"})
	if err != nil {
		t.Fatalf("runWithIO returned error: %v", err)
	}
	if !strings.Contains(out.String(), "model=deepseek-v4-resumed") {
		t.Fatalf("stdout = %q, want resumed model", out.String())
	}
}

func TestResumeWithoutSessionsReturnsHelpfulError(t *testing.T) {
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

	var out bytes.Buffer
	var stderr bytes.Buffer
	err = runWithIO(strings.NewReader(""), &out, &stderr, []string{"resume", "--last"})
	if err == nil || !strings.Contains(err.Error(), "no sessions") {
		t.Fatalf("err = %v, want no sessions error", err)
	}
}

func TestIndexCommandWritesWorkspaceIndex(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var out bytes.Buffer
	var stderr bytes.Buffer

	err = runWithIO(strings.NewReader(""), &out, &stderr, []string{"index"})
	if err != nil {
		t.Fatalf("runWithIO returned error: %v", err)
	}
	if !strings.Contains(out.String(), "indexed files=") {
		t.Fatalf("stdout = %q, want indexed summary", out.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".codeworld", "index.json")); err != nil {
		t.Fatalf("index file missing: %v", err)
	}
}

func TestTUICommandStartsFullScreenAppInTestMode(t *testing.T) {
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
	var out bytes.Buffer
	var stderr bytes.Buffer

	err = runWithIO(strings.NewReader("/exit\n"), &out, &stderr, []string{"tui"})
	if err != nil {
		t.Fatalf("runWithIO returned error: %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"codeworld",
		"provider=deepseek",
		"model=deepseek-v4-pro",
		"input=0",
		"output=0",
		"cache=0",
		"total=0",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout missing %q in:\n%s", want, output)
		}
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
