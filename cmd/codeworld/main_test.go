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
		"/help /model /status /diff /agents /clear /exit",
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
	if !strings.Contains(out.String(), "/help /model /status /diff /agents /clear /exit") {
		t.Fatalf("stdout = %q, want repl help", out.String())
	}
}

func TestCLIHelpListsAutomationCommands(t *testing.T) {
	var out bytes.Buffer
	if err := runWithIO(strings.NewReader(""), &out, &bytes.Buffer{}, []string{"--help"}); err != nil {
		t.Fatalf("runWithIO: %v", err)
	}
	for _, want := range []string{"codeworld exec", "codeworld review", "codeworld sessions", "codeworld fork", "--output-schema", "--ephemeral", "--approval-mode"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, out.String())
		}
	}
}

func TestGlobalProfileSelectsProfileConfig(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "profiles"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "profiles", "fast.toml"), []byte("model = \"profile-model\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("CODEWORLD_HOME", home)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })
	var out, stderr bytes.Buffer
	if err := runWithIO(strings.NewReader("/status\n/exit\n"), &out, &stderr, []string{"--profile", "fast"}); err != nil {
		t.Fatalf("runWithIO: %v", err)
	}
	if !strings.Contains(out.String(), "model=profile-model") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestParseGlobalOptions(t *testing.T) {
	opts, args, err := parseGlobalOptions([]string{"--profile", "fast", "exec", "inspect"})
	if err != nil || opts.Profile != "fast" || strings.Join(args, " ") != "exec inspect" {
		t.Fatalf("opts=%#v args=%#v err=%v", opts, args, err)
	}
	if _, _, err := parseGlobalOptions([]string{"--profile"}); err == nil {
		t.Fatal("missing profile name accepted")
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

func TestParseAutomationArgsReadsPromptFromStdin(t *testing.T) {
	opts, err := parseAutomationArgs(strings.NewReader("inspect this repo\n"), []string{"--json", "--ephemeral", "-"}, execUsage, true, true)
	if err != nil {
		t.Fatalf("parseAutomationArgs: %v", err)
	}
	if opts.prompt != "inspect this repo" || !opts.json || !opts.ephemeral {
		t.Fatalf("options = %#v", opts)
	}
}

func TestParseAutomationArgsRequiresExecPrompt(t *testing.T) {
	_, err := parseAutomationArgs(strings.NewReader(""), []string{"--json"}, execUsage, true, true)
	if err == nil || !strings.Contains(err.Error(), "usage: codeworld exec") {
		t.Fatalf("err = %v, want exec usage", err)
	}
}

func TestParseAutomationArgsValidatesApprovalMode(t *testing.T) {
	opts, err := parseAutomationArgs(strings.NewReader(""), []string{"--approval-mode", "full-access", "inspect"}, execUsage, true, true)
	if err != nil {
		t.Fatalf("parseAutomationArgs: %v", err)
	}
	if opts.approvalMode != "full-access" {
		t.Fatalf("approval mode = %q", opts.approvalMode)
	}
	if _, err := parseAutomationArgs(strings.NewReader(""), []string{"--approval-mode", "unsafe", "inspect"}, execUsage, true, true); err == nil {
		t.Fatal("invalid approval mode accepted")
	}
}

func TestParseAutomationArgsValidatesSandbox(t *testing.T) {
	opts, err := parseAutomationArgs(strings.NewReader(""), []string{"--sandbox", "workspace-write", "--network", "inspect"}, execUsage, true, true)
	if err != nil {
		t.Fatalf("parseAutomationArgs: %v", err)
	}
	if opts.sandboxMode != "workspace-write" || opts.network == nil || !*opts.network {
		t.Fatalf("sandbox options = %#v", opts)
	}
	if _, err := parseAutomationArgs(strings.NewReader(""), []string{"--sandbox", "unsafe", "inspect"}, execUsage, true, true); err == nil {
		t.Fatal("invalid sandbox mode accepted")
	}
	if _, err := parseAutomationArgs(strings.NewReader(""), []string{"--sandbox", "danger-full-access", "--no-network", "inspect"}, execUsage, true, true); err == nil {
		t.Fatal("misleading danger-full-access network restriction accepted")
	}
}

func TestParseReviewArgsAllowsFlagsWithoutInstructions(t *testing.T) {
	target, opts, err := parseReviewArgs(strings.NewReader(""), []string{"--base", "main", "--json"})
	if err != nil {
		t.Fatalf("parseReviewArgs: %v", err)
	}
	if target.Base != "main" || !opts.json || opts.prompt != "" {
		t.Fatalf("target/options = %#v %#v", target, opts)
	}
}

func TestParseReviewArgsRejectsMultipleTargets(t *testing.T) {
	_, _, err := parseReviewArgs(strings.NewReader(""), []string{"--base", "main", "--commit", "HEAD"})
	if err == nil || !strings.Contains(err.Error(), "only one") {
		t.Fatalf("err = %v, want target conflict", err)
	}
}

func TestReviewRejectsApprovalModeOverride(t *testing.T) {
	err := runReviewCommand(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, t.TempDir(), "", []string{"--approval-mode", "full-access"})
	if err == nil || !strings.Contains(err.Error(), "always runs in read-only") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRunArgsLoadsImageParts(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "sample.png")
	if err := os.WriteFile(imagePath, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	prompt, images, err := parseRunArgs([]string{"--image", imagePath, "describe", "this"})
	if err != nil {
		t.Fatalf("parseRunArgs returned error: %v", err)
	}
	if prompt != "describe this" {
		t.Fatalf("prompt = %q, want describe this", prompt)
	}
	if len(images) != 1 || images[0].MediaType != "image/png" {
		t.Fatalf("images = %#v, want one png image", images)
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
	if !strings.Contains(out.String(), "deepseek-v4-new") {
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
	if !strings.Contains(out.String(), "deepseek-v4-resumed") {
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
		"CODEWORLD",
		"deepseek",
		"deepseek-v4-pro",
		"0 tok",
		"0 msg",
		"0 approvals",
		"Welcome to Codeworld",
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
