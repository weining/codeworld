package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
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
	for _, want := range []string{"codeworld exec", "codeworld review", "codeworld login", "codeworld logout", "codeworld sessions", "codeworld fork", "codeworld doctor", "codeworld completion", "codeworld sandbox", "--version", "--cd", "--model", "--output-schema", "--ephemeral", "--approval-mode"} {
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
	opts, args, err := parseGlobalOptions([]string{"--profile", "fast", "-C", "workspace", "--model", "model-x", "-a", "read-only", "-s", "workspace-write", "--search", "-c", `max_steps=30`, "--config", `model="configured"`, "--strict-config", "exec", "inspect"})
	if err != nil || opts.Profile != "fast" || opts.WorkingDir != "workspace" || opts.Model != "model-x" || opts.Approval != "read-only" || opts.Sandbox != "workspace-write" || opts.Network == nil || !*opts.Network || !opts.Search || len(opts.Config) != 2 || !opts.Strict || strings.Join(args, " ") != "exec inspect" {
		t.Fatalf("opts=%#v args=%#v err=%v", opts, args, err)
	}
	if _, _, err := parseGlobalOptions([]string{"--profile"}); err == nil {
		t.Fatal("missing profile name accepted")
	}
	for _, args := range [][]string{
		{"--cd"}, {"--model"}, {"--approval-mode"}, {"--sandbox"}, {"--config"},
		{"-C", "one", "--cd", "two"}, {"-m", "one", "--model", "two"},
		{"-a", "invalid"}, {"-s", "invalid"}, {"--network", "--search", "exec"},
		{"--sandbox", "danger-full-access", "--no-network", "exec"},
		{"--strict-config", "--strict-config", "exec"},
	} {
		if _, _, err := parseGlobalOptions(args); err == nil {
			t.Fatalf("global args %#v accepted", args)
		}
	}
}

func TestParsersAcceptLongOptionEqualsSyntax(t *testing.T) {
	global, remaining, err := parseGlobalOptions([]string{
		"--profile=fast", "--model=model-x", "--approval-mode=never", "--sandbox=read-only", "--config=max_steps=30", "exec", "inspect",
	})
	if err != nil {
		t.Fatal(err)
	}
	if global.Profile != "fast" || global.Model != "model-x" || global.Approval != permissions.ModeNever || global.Sandbox != sandbox.ModeReadOnly || len(global.Config) != 1 || global.Config[0] != "max_steps=30" || strings.Join(remaining, " ") != "exec inspect" {
		t.Fatalf("global=%#v remaining=%#v", global, remaining)
	}

	automation, err := parseAutomationArgs(strings.NewReader(""), []string{
		"--model=model-y", "--color=always", "--approval-mode=on-request", "--sandbox=workspace-write", "inspect",
	}, execUsage, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if automation.modelName != "model-y" || automation.color != "always" || automation.approvalMode != permissions.ModeOnRequest || automation.sandboxMode != sandbox.ModeWorkspaceWrite || automation.prompt != "inspect" {
		t.Fatalf("automation=%#v", automation)
	}

	target, reviewOptions, err := parseReviewArgs(strings.NewReader(""), []string{"--base=main", "--title=Focused review", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if target.Base != "main" || target.Title != "Focused review" || !reviewOptions.json {
		t.Fatalf("target=%#v options=%#v", target, reviewOptions)
	}
}

func TestExpandLongOptionValuesPreservesCommandAfterDelimiter(t *testing.T) {
	got := expandLongOptionValues([]string{"--model=model-x", "--", "tool", "--model=tool-value"})
	want := []string{"--model", "model-x", "--", "tool", "--model=tool-value"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("expanded=%#v want=%#v", got, want)
	}
}

func TestParseGlobalCodexCompatibilityFlags(t *testing.T) {
	opts, remaining, err := parseGlobalOptions([]string{"--oss", "--local-provider", "ollama", "--add-dir", "../shared", "--add-dir", "/tmp/cache", "--dangerously-bypass-hook-trust", "exec", "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 || !opts.OSS || opts.Local != "ollama" || !opts.BypassHooks || !reflect.DeepEqual(opts.AddDirs, []string{"../shared", "/tmp/cache"}) || strings.Join(opts.Config, ",") != "provider=local,local_base_url=http://127.0.0.1:11434/v1" {
		t.Fatalf("opts=%#v remaining=%#v", opts, remaining)
	}
	bypass, _, err := parseGlobalOptions([]string{"--dangerously-bypass-approvals-and-sandbox", "exec", "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if bypass.Approval != permissions.ModeFullAccess || bypass.Sandbox != sandbox.ModeDangerFullAccess || !bypass.Bypass {
		t.Fatalf("bypass = %#v", bypass)
	}
	if _, _, err := parseGlobalOptions([]string{"--dangerously-bypass-approvals-and-sandbox", "--network", "exec", "inspect"}); err == nil {
		t.Fatal("dangerous bypass accepted network override")
	}
	if _, _, err := parseGlobalOptions([]string{"--local-provider", "unknown", "exec", "inspect"}); err == nil {
		t.Fatal("unknown local provider accepted")
	}
}

func TestParseGlobalInteractivePromptOptions(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "sample.png")
	if err := os.WriteFile(imagePath, []byte{0x89, 0x50, 0x4e, 0x47}, 0o600); err != nil {
		t.Fatal(err)
	}
	opts, remaining, err := parseGlobalOptions([]string{"-i", imagePath, "--no-alt-screen", "inspect", "this"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.NoAltScreen || len(opts.Images) != 1 || opts.Images[0].MediaType != "image/png" || strings.Join(remaining, " ") != "inspect this" {
		t.Fatalf("opts=%#v remaining=%#v", opts, remaining)
	}
	for _, args := range [][]string{{"--image"}, {"--no-alt-screen", "--no-alt-screen"}} {
		if _, _, err := parseGlobalOptions(args); err == nil {
			t.Fatalf("global args %#v accepted", args)
		}
	}
}

func TestCLIConfigOverrideAndExecAlias(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "")
	var out, stderr bytes.Buffer
	err := runWithIO(strings.NewReader("/status\n/exit\n"), &out, &stderr, []string{"-C", root, "-c", `model="config-model"`, "repl"})
	if err != nil {
		t.Fatalf("runWithIO: %v", err)
	}
	if !strings.Contains(out.String(), "model=config-model") {
		t.Fatalf("stdout = %q", out.String())
	}
	err = runWithIO(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, []string{"e"})
	if err == nil || !strings.Contains(err.Error(), "codeworld exec") {
		t.Fatalf("exec alias error = %v", err)
	}
	err = runWithIO(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, []string{"-C", t.TempDir(), "exec", "review", "--uncommitted"})
	if err == nil || !strings.Contains(err.Error(), "review requires a Git repository") {
		t.Fatalf("exec review error = %v", err)
	}
}

func TestGlobalWorkingDirectoryAndModelOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "")
	var out, stderr bytes.Buffer
	err := runWithIO(strings.NewReader("/status\n/exit\n"), &out, &stderr, []string{"-C", root, "-m", "override-model", "repl"})
	if err != nil {
		t.Fatalf("runWithIO: %v\nstderr: %s", err, stderr.String())
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "model=override-model") || !strings.Contains(out.String(), "workspace="+canonicalRoot) {
		t.Fatalf("stdout = %q", out.String())
	}
	current, err := os.ReadFile(filepath.Join(root, ".codeworld", "current-session.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), `"model": "override-model"`) {
		t.Fatalf("session = %s", current)
	}
}

func TestGlobalRuntimePolicyOverridesAppearInREPL(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "")
	network := true
	replApp, err := newAppWithGlobal(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, root, globalOptions{
		Approval: permissions.ModeFullAccess,
		Sandbox:  "read-only",
		Network:  &network,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer replApp.Close()
	policy, ok := replApp.Runner.Policy.(permissions.ModePolicy)
	if !ok || policy.Mode != permissions.ModeFullAccess || replApp.SandboxMode != "read-only" || !replApp.SandboxNetwork {
		t.Fatalf("repl = policy=%#v sandbox=%s network=%t", replApp.Runner.Policy, replApp.SandboxMode, replApp.SandboxNetwork)
	}
}

func TestResolveGlobalWorkingDirValidatesDirectory(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveGlobalWorkingDir(root, "child")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(child)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != want {
		t.Fatalf("resolved = %q, want %q", resolved, want)
	}
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveGlobalWorkingDir(root, file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file path error = %v", err)
	}
	if _, err := resolveGlobalWorkingDir(root, "missing"); err == nil {
		t.Fatal("missing directory accepted")
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

func TestGlobalModelOverridesRestoredSession(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	store := session.NewStore(root)
	sess := session.New(canonicalRoot, "deepseek", "saved-model")
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "")
	var out, stderr bytes.Buffer
	if err := runWithIO(strings.NewReader("/status\n/exit\n"), &out, &stderr, []string{"-C", root, "--model", "invocation-model", "repl"}); err != nil {
		t.Fatalf("runWithIO: %v", err)
	}
	if !strings.Contains(out.String(), "model=invocation-model") {
		t.Fatalf("stdout = %q", out.String())
	}
	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model != "invocation-model" {
		t.Fatalf("session model = %q", loaded.Model)
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
	opts, err := parseAutomationArgs(strings.NewReader("inspect this repo\n"), []string{"-c", "max_steps=30", "--strict-config", "--json", "--ephemeral", "-"}, execUsage, true, true)
	if err != nil {
		t.Fatalf("parseAutomationArgs: %v", err)
	}
	if opts.prompt != "inspect this repo" || !opts.json || !opts.ephemeral || len(opts.config) != 1 || !opts.strict {
		t.Fatalf("options = %#v", opts)
	}
}

func TestExecColorModeSelection(t *testing.T) {
	var out bytes.Buffer
	if !shouldUseExecColor("always", &out) {
		t.Fatal("always did not enable color")
	}
	if shouldUseExecColor("never", os.Stderr) || shouldUseExecColor("auto", &out) {
		t.Fatal("never or non-terminal auto enabled color")
	}
	t.Setenv("NO_COLOR", "1")
	if shouldUseExecColor("auto", os.Stderr) {
		t.Fatal("NO_COLOR was ignored")
	}
}

func TestParseAutomationArgsSupportsFeatureOverrides(t *testing.T) {
	opts, err := parseAutomationArgs(strings.NewReader(""), []string{"--enable", "plugins", "--disable", "model_call_logging", "inspect"}, execUsage, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(opts.config, ",") != "features.plugins=true,features.model_call_logging=false" {
		t.Fatalf("config = %#v", opts.config)
	}
	if _, err := parseAutomationArgs(strings.NewReader(""), []string{"--enable", "unknown", "inspect"}, execUsage, true, true); err == nil {
		t.Fatal("unknown feature accepted")
	}
}

func TestExecAndReviewSubcommandHelpAndVersion(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"exec", "--help"}, {"review", "--help"}} {
		var out bytes.Buffer
		if err := runWithIO(strings.NewReader(""), &out, io.Discard, append([]string{"-C", root}, args...)); err != nil {
			t.Fatalf("args=%v err=%v", args, err)
		}
		if !strings.Contains(out.String(), "usage: codeworld "+args[0]) {
			t.Fatalf("args=%v output=%q", args, out.String())
		}
	}
	var out bytes.Buffer
	if err := runWithIO(strings.NewReader(""), &out, io.Discard, []string{"exec", "--version"}); err != nil || !strings.Contains(out.String(), "codeworld") {
		t.Fatalf("version output=%q err=%v", out.String(), err)
	}
}

func TestParseAutomationArgsReadsImplicitAndAppendedStdin(t *testing.T) {
	opts, err := parseAutomationArgs(strings.NewReader("piped only\n"), nil, execUsage, true, true)
	if err != nil || opts.prompt != "piped only" {
		t.Fatalf("implicit stdin options=%#v err=%v", opts, err)
	}
	opts, err = parseAutomationArgs(strings.NewReader("extra context\n"), []string{"inspect", "repo"}, execUsage, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if opts.prompt != "inspect repo <stdin>\nextra context\n</stdin>" {
		t.Fatalf("combined prompt = %q", opts.prompt)
	}
}

func TestParseAutomationArgsAcceptsCodexPositionedOptions(t *testing.T) {
	root := t.TempDir()
	opts, err := parseAutomationArgs(strings.NewReader(""), []string{
		"-m", "model-x", "-p", "fast", "-C", root,
		"-a", "never", "-s", "read-only", "-c", "max_steps=30",
		"--color", "never", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check",
		"inspect",
	}, execUsage, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if opts.modelName != "model-x" || opts.profile != "fast" || opts.workingDir != root || opts.approvalMode != permissions.ModeNever || opts.sandboxMode != "read-only" || len(opts.config) != 1 || opts.color != "never" || !opts.ignoreUser || !opts.ignoreRules {
		t.Fatalf("options = %#v", opts)
	}
	if _, err := parseApprovalMode("on-request"); err != nil {
		t.Fatal(err)
	}
	if mode, err := parseApprovalMode("untrusted"); err != nil || mode != permissions.ModeUntrusted {
		t.Fatalf("untrusted mode=%q err=%v", mode, err)
	}
	if _, err := parseAutomationArgs(strings.NewReader(""), []string{"--color", "rainbow", "inspect"}, execUsage, true, true); err == nil {
		t.Fatal("invalid color accepted")
	}
}

func TestApplyAutomationLocationLetsLocalOptionsWin(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, global, err := applyAutomationLocation(root, globalOptions{Profile: "global", Model: "global-model"}, automationOptions{
		workingDir: "child", profile: "local", modelName: "local-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(child)
	if resolved != want || global.Profile != "local" || global.Model != "local-model" {
		t.Fatalf("root=%q global=%#v", resolved, global)
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

func TestParseAutomationCodexCompatibilityFlags(t *testing.T) {
	opts, err := parseAutomationArgs(strings.NewReader(""), []string{
		"--local-provider", "lmstudio", "--add-dir", "../shared", "--dangerously-bypass-hook-trust", "inspect",
	}, execUsage, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.oss || opts.local != "lmstudio" || !opts.bypassHooks || !reflect.DeepEqual(opts.addDirs, []string{"../shared"}) {
		t.Fatalf("opts = %#v", opts)
	}
	bypass, err := parseAutomationArgs(strings.NewReader(""), []string{"--dangerously-bypass-approvals-and-sandbox", "inspect"}, execUsage, true, true)
	if err != nil {
		t.Fatal(err)
	}
	runtimeOpts, err := runtimeOptionsForAutomation(t.TempDir(), globalOptions{}, strings.NewReader(""), io.Discard, io.Discard, bypass)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeOpts.ApprovalMode != permissions.ModeFullAccess || runtimeOpts.SandboxMode != sandbox.ModeDangerFullAccess || runtimeOpts.SandboxNetwork != nil {
		t.Fatalf("runtime opts = %#v", runtimeOpts)
	}
	if _, err := parseAutomationArgs(strings.NewReader(""), []string{"--dangerously-bypass-approvals-and-sandbox", "--sandbox", "read-only", "inspect"}, execUsage, true, true); err == nil {
		t.Fatal("dangerous bypass accepted sandbox override")
	}
}

func TestExecOptionsOverrideGlobalRuntimePolicy(t *testing.T) {
	globalNetwork := false
	localNetwork := true
	runtimeOpts, err := runtimeOptionsForAutomation("/workspace", globalOptions{
		Approval: permissions.ModeReadOnly,
		Sandbox:  "read-only",
		Network:  &globalNetwork,
		Config:   []string{`model="global"`},
	}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, automationOptions{
		approvalMode: permissions.ModeFullAccess,
		sandboxMode:  "workspace-write",
		network:      &localNetwork,
		config:       []string{`model="local"`},
		ignoreRules:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtimeOpts.ApprovalMode != permissions.ModeFullAccess || runtimeOpts.SandboxMode != "workspace-write" || runtimeOpts.SandboxNetwork == nil || !*runtimeOpts.SandboxNetwork || strings.Join(runtimeOpts.ConfigOverrides, ",") != `model="global",model="local"` || !runtimeOpts.IgnoreRules {
		t.Fatalf("runtime options = %#v", runtimeOpts)
	}
	_, err = runtimeOptionsForAutomation("/workspace", globalOptions{Network: &globalNetwork}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, automationOptions{sandboxMode: "danger-full-access"})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("danger full access error = %v", err)
	}
}

func TestParseExecArgsResume(t *testing.T) {
	opts, err := parseExecArgs(strings.NewReader(""), []string{"resume", "session-1", "--json", "continue"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.sessionID != "session-1" || opts.resumeLast || !opts.json || opts.prompt != "continue" {
		t.Fatalf("options = %#v", opts)
	}
	opts, err = parseExecArgs(strings.NewReader(""), []string{"resume", "--last", "continue"})
	if err != nil || !opts.resumeLast {
		t.Fatalf("last options = %#v, err=%v", opts, err)
	}
	if _, err := parseExecArgs(strings.NewReader(""), []string{"resume", "--last", "--ephemeral", "continue"}); err == nil {
		t.Fatal("resume accepted --ephemeral")
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

func TestParseReviewArgsSupportsUncommittedAndTitle(t *testing.T) {
	target, opts, err := parseReviewArgs(strings.NewReader(""), []string{"--uncommitted", "--title", "PR 42", "focus on races"})
	if err != nil {
		t.Fatal(err)
	}
	if !target.Uncommitted || target.Title != "PR 42" || opts.prompt != "focus on races" {
		t.Fatalf("target=%#v opts=%#v", target, opts)
	}
}

func TestParseReviewArgsRejectsMultipleTargets(t *testing.T) {
	_, _, err := parseReviewArgs(strings.NewReader(""), []string{"--base", "main", "--commit", "HEAD"})
	if err == nil || !strings.Contains(err.Error(), "only one") {
		t.Fatalf("err = %v, want target conflict", err)
	}
}

func TestReviewRejectsApprovalModeOverride(t *testing.T) {
	err := runReviewCommand(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, t.TempDir(), globalOptions{}, []string{"--approval-mode", "full-access"})
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
