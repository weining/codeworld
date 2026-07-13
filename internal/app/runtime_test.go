package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/context/indexer"
	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
	"codeworld/internal/session"
)

func TestNewRuntimeAppliesInvocationPolicyOverrides(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "")
	network := true
	rt, err := NewRuntime(context.Background(), Options{
		Root: root, In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		ApprovalMode: permissions.ModeFullAccess,
		SandboxMode:  sandbox.ModeReadOnly, SandboxNetwork: &network,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	policy, ok := rt.Runner.Policy.(permissions.ModePolicy)
	if !ok || policy.Mode != permissions.ModeFullAccess {
		t.Fatalf("runner policy = %#v", rt.Runner.Policy)
	}
	if rt.Config.ApprovalMode != "full-access" || rt.Config.SandboxMode != "read-only" || !rt.Config.SandboxNetwork {
		t.Fatalf("config = %#v", rt.Config)
	}
}

func TestNewRuntimeRejectsDangerFullAccessNetworkOverride(t *testing.T) {
	network := false
	_, err := NewRuntime(context.Background(), Options{
		Root: t.TempDir(), In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		SandboxMode: sandbox.ModeDangerFullAccess, SandboxNetwork: &network,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewRuntimeCanIgnoreUserConfig(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "config.toml"), `model = "user-model"`)
	t.Setenv("CODEWORLD_HOME", home)
	t.Setenv("DEEPSEEK_API_KEY", "")
	rt, err := NewRuntime(context.Background(), Options{
		Root: root, In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, SkipUserConfig: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.Config.Model == "user-model" {
		t.Fatalf("user config was loaded: %#v", rt.Config)
	}
}

func TestNewRuntimeLoadsAndCanIgnoreExecRules(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(home, "rules", "default.rules"), "prefix_rule(pattern=[\"git\", \"status\"], decision=\"allow\")\n"+
		"prefix_rule(pattern=[\"git\", \"push\"], decision=\"prompt\")")
	t.Setenv("CODEWORLD_HOME", home)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	newRuntime := func(ignore bool) Runtime {
		rt, err := NewRuntime(context.Background(), Options{
			Root: root, In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Ephemeral: true, IgnoreRules: ignore,
		})
		if err != nil {
			t.Fatal(err)
		}
		return rt
	}
	request := permissions.Request{Action: permissions.ActionShell, Risk: permissions.RiskExecute, Target: "git status"}

	rt := newRuntime(false)
	decision, err := rt.Runner.Policy.Check(context.Background(), request)
	if err != nil || decision.Kind != permissions.DecisionAllow {
		t.Fatalf("rules decision=%#v err=%v", decision, err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}

	rt = newRuntime(false)
	rt.SetApprovalMode(permissions.ModeNever)
	decision, err = rt.Runner.Policy.Check(context.Background(), permissions.Request{Action: permissions.ActionShell, Risk: permissions.RiskNetwork, Target: "git push"})
	if err != nil || decision.Kind != permissions.DecisionAsk {
		t.Fatalf("preserved rules decision=%#v err=%v", decision, err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}

	ignored := newRuntime(true)
	defer ignored.Close()
	decision, err = ignored.Runner.Policy.Check(context.Background(), request)
	if err != nil || decision.Kind != permissions.DecisionAsk {
		t.Fatalf("ignored rules decision=%#v err=%v", decision, err)
	}
}

// TestNewRuntimeBuildsREPLDependencies 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewRuntimeBuildsREPLDependencies(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   strings.NewReader("/exit\n"),
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if rt.Workspace.Root != canonicalRoot {
		t.Fatalf("workspace root = %q, want %q", rt.Workspace.Root, canonicalRoot)
	}
	if rt.Session.Provider != "deepseek" || rt.Session.Model != "deepseek-v4-pro" {
		t.Fatalf("session provider/model = %s/%s, want deepseek/deepseek-v4-pro", rt.Session.Provider, rt.Session.Model)
	}
	if rt.Runner.Model == nil || rt.Runner.Tools == nil {
		t.Fatalf("runtime runner not wired: %#v", rt.Runner)
	}
	if rt.Subagents == nil {
		t.Fatalf("subagent manager missing")
	}
	if _, ok := rt.Runner.Tools.Get("subagent_start"); !ok {
		t.Fatalf("subagent_start tool was not registered")
	}
	if _, ok := rt.Runner.Tools.Get("subagent_status"); !ok {
		t.Fatalf("subagent_status tool was not registered")
	}
	for _, name := range []string{"shell_start", "shell_poll", "shell_write", "shell_resize", "shell_terminate"} {
		if _, ok := rt.Runner.Tools.Get(name); !ok {
			t.Fatalf("%s tool was not registered", name)
		}
	}
	if rt.CommandSessions == nil {
		t.Fatal("command session manager missing")
	}
	if rt.Diff == nil {
		t.Fatalf("runtime diff function missing")
	}
}

func TestNewRuntimeEphemeralDoesNotCreateCurrentSession(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	rt, err := NewRuntime(context.Background(), Options{
		Root: root, In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Ephemeral: true,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close()
	if _, err := os.Stat(session.NewStore(rt.Workspace.Root).CurrentPath()); !os.IsNotExist(err) {
		t.Fatalf("current session exists in ephemeral mode: %v", err)
	}
	if len(rt.Messages) != 0 {
		t.Fatalf("ephemeral runtime restored messages: %#v", rt.Messages)
	}
}

func TestNewRuntimeUsesConfiguredWorkspaceWithinRoot(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "project")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(root, ".codeworld", "config.toml"), `workspace = "project"`)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{Root: root, In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close()
	want, _ := filepath.EvalSymlinks(workspaceRoot)
	if rt.Workspace.Root != want {
		t.Fatalf("workspace = %q, want %q", rt.Workspace.Root, want)
	}
}

func TestNewRuntimeRejectsConfiguredWorkspaceOutsideRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".codeworld", "config.toml"), `workspace = ".."`)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	if _, err := NewRuntime(context.Background(), Options{Root: root, In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}); err == nil {
		t.Fatal("NewRuntime accepted workspace outside config root")
	}
}

// TestRuntimeRunsLifecycleHooks 验证 runtime 启动和关闭时会触发生命周期 hook。
func TestRuntimeRunsLifecycleHooks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".codeworld", "hooks.json"), `{
		"hooks": {
			"SessionStart": [
				{"hooks": [{"type": "command", "command": "printf start >> lifecycle.out"}]}
			],
			"Stop": [
				{"hooks": [{"type": "command", "command": "printf stop >> lifecycle.out"}]}
			]
		}
	}`)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root:        root,
		In:          strings.NewReader("a\na\n"),
		Out:         &bytes.Buffer{},
		Err:         &bytes.Buffer{},
		SandboxMode: sandbox.ModeDangerFullAccess,
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "lifecycle.out"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "startstop" {
		t.Fatalf("hook output = %q, want startstop", data)
	}
	saved, err := session.NewStore(rt.Workspace.Root).LoadCurrent()
	if err != nil || len(saved.Approvals) != 2 {
		t.Fatalf("hook approvals = %#v, err=%v, want two persisted commands", saved.Approvals, err)
	}
	if _, err := NewRuntime(context.Background(), Options{Root: root, In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}); err == nil {
		t.Fatal("NewRuntime trusted approvals loaded from workspace session")
	}
}

func TestRuntimeCanExplicitlyBypassHookTrust(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".codeworld", "hooks.json"), `{
		"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "printf bypassed > bypass.out"}]}]}
	}`)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	rt, err := NewRuntime(context.Background(), Options{
		Root: root, In: strings.NewReader(""), Out: io.Discard, Err: io.Discard,
		SandboxMode: sandbox.ModeDangerFullAccess, BypassHookTrust: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "bypass.out"))
	if err != nil || string(data) != "bypassed" {
		t.Fatalf("hook output=%q err=%v", data, err)
	}
}

// TestNewRuntimeRestoresSessionMessagesAndUsage 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewRuntimeRestoresSessionMessagesAndUsage(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	store := session.NewStore(canonicalRoot)
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-pro")
	sess.Messages = []session.Message{{Role: "user", Content: "old"}}
	sess.Usage = session.Usage{InputTokens: 10, OutputTokens: 2, CacheTokens: 3, TotalTokens: 12}
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if len(rt.Messages) != 1 || rt.Messages[0].Content != "old" {
		t.Fatalf("messages = %#v, want restored user message", rt.Messages)
	}
	if rt.Usage.InputTokens != 10 || rt.Usage.OutputTokens != 2 || rt.Usage.CacheTokens != 3 || rt.Usage.TotalTokens != 12 {
		t.Fatalf("usage = %#v, want restored session usage", rt.Usage)
	}
}

func TestNewRuntimeRejectsCorruptCurrentSession(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".codeworld", "current-session.json"), `{not-json`)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	_, err := NewRuntime(context.Background(), Options{Root: root, In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "load current session") {
		t.Fatalf("NewRuntime error = %v, want corrupt session error", err)
	}
}

func TestNewRuntimeRehydratesPersistedImageFromPath(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	imagePath := filepath.Join(root, "image.png")
	writeFile(t, imagePath, "png")
	store := session.NewStore(canonicalRoot)
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-pro")
	sess.Messages = []session.Message{{Role: "user", Parts: []session.ContentPart{{Type: "image", MediaType: "image/png", Path: imagePath}}}}
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{Root: root, In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close()
	part := rt.Messages[0].Parts[0]
	if part.Data == "" || part.ImageURL == "" {
		t.Fatalf("image part was not rehydrated: %#v", part)
	}
}

func TestNewRuntimeDoesNotRehydrateImageOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	outsidePath := filepath.Join(t.TempDir(), "secret.png")
	writeFile(t, outsidePath, "secret")
	store := session.NewStore(canonicalRoot)
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-pro")
	sess.Messages = []session.Message{{Role: "user", Parts: []session.ContentPart{{Type: "image", MediaType: "image/png", Path: outsidePath}}}}
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{Root: root, In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close()
	part := rt.Messages[0].Parts[0]
	if part.Type != "text" || !strings.Contains(part.Text, "image unavailable") || part.Data != "" {
		t.Fatalf("outside image was restored: %#v", part)
	}
}

// TestNewRuntimeDropsOrphanToolMessagesFromSession 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewRuntimeDropsOrphanToolMessagesFromSession(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	store := session.NewStore(canonicalRoot)
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-pro")
	sess.Messages = []session.Message{
		{Role: "tool", Content: "orphan", ToolCallID: "missing"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", ToolCalls: []session.ToolCall{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"go.mod"}`)}}},
		{Role: "tool", Content: "valid", ToolCallID: "call-1"},
	}
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if len(rt.Messages) != 3 {
		t.Fatalf("messages = %#v, want orphan tool dropped", rt.Messages)
	}
	if rt.Messages[0].Content != "hello" {
		t.Fatalf("first message = %#v, want user hello", rt.Messages[0])
	}
	if rt.Messages[2].ToolCallID != "call-1" {
		t.Fatalf("tool message = %#v, want matching call-1", rt.Messages[2])
	}
}

// TestNewRuntimeDropsIncompleteAssistantToolCallGroups 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewRuntimeDropsIncompleteAssistantToolCallGroups(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	store := session.NewStore(canonicalRoot)
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-pro")
	sess.Messages = []session.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", ToolCalls: []session.ToolCall{
			{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"go.mod"}`)},
			{ID: "call-2", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"}`)},
		}},
		{Role: "tool", Content: "one result", ToolCallID: "call-1"},
		{Role: "user", Content: "next"},
	}
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if len(rt.Messages) != 2 {
		t.Fatalf("messages = %#v, want incomplete tool call group dropped", rt.Messages)
	}
	if rt.Messages[0].Content != "hello" || rt.Messages[1].Content != "next" {
		t.Fatalf("messages = %#v, want only user messages", rt.Messages)
	}
}

// TestNewRuntimeIncludesWorkspaceIndexSummaryInSystemPrompt 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewRuntimeIncludesWorkspaceIndexSummaryInSystemPrompt(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	idx := indexer.Index{Root: canonicalRoot, Entries: []indexer.Entry{{Path: "main.go", Language: "go", Size: 12}}}
	if err := indexer.Save(indexer.DefaultPath(canonicalRoot), idx); err != nil {
		t.Fatalf("Save index: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if !strings.Contains(rt.Runner.SystemPrompt, "Workspace index:") || !strings.Contains(rt.Runner.SystemPrompt, "main.go go") {
		t.Fatalf("system prompt missing index summary:\n%s", rt.Runner.SystemPrompt)
	}
}

// TestNewRuntimeIncludesContextGraphSummaryInSystemPrompt 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewRuntimeIncludesContextGraphSummaryInSystemPrompt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), `package main

// RunServer 是测试辅助函数，用于复用测试准备或断言逻辑。
func RunServer() {}
`)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if !strings.Contains(rt.Runner.SystemPrompt, "Workspace context graph:") || !strings.Contains(rt.Runner.SystemPrompt, "main.go go symbols=1") {
		t.Fatalf("system prompt missing context graph summary:\n%s", rt.Runner.SystemPrompt)
	}
}

// TestNewRuntimeIncludesAgentsInstructionsInSystemPrompt 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewRuntimeIncludesAgentsInstructionsInSystemPrompt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "Always run focused tests.")
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if !strings.Contains(rt.Runner.SystemPrompt, "Project instructions:") || !strings.Contains(rt.Runner.SystemPrompt, "Always run focused tests.") {
		t.Fatalf("system prompt missing AGENTS instructions:\n%s", rt.Runner.SystemPrompt)
	}
}

// TestNewRuntimeIncludesSessionSummaryInSystemPrompt 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewRuntimeIncludesSessionSummaryInSystemPrompt(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	store := session.NewStore(canonicalRoot)
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-pro")
	sess.Summary = "remembered context"
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if !strings.Contains(rt.Runner.SystemPrompt, "Conversation summary:") || !strings.Contains(rt.Runner.SystemPrompt, "remembered context") {
		t.Fatalf("system prompt missing session summary:\n%s", rt.Runner.SystemPrompt)
	}
}

// TestNewRuntimeIncludesGoalAndPlanModeInSystemPrompt 验证 goal 和 plan mode 会进入模型上下文。
func TestNewRuntimeIncludesGoalAndPlanModeInSystemPrompt(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	store := session.NewStore(canonicalRoot)
	sess := session.New(canonicalRoot, "deepseek", "deepseek-v4-pro")
	sess.Goal = "finish the migration"
	sess.Mode = "plan"
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	for _, want := range []string{"Current goal:", "finish the migration", "Plan mode:"} {
		if !strings.Contains(rt.Runner.SystemPrompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, rt.Runner.SystemPrompt)
		}
	}
}

// TestNewRuntimeRegistersEnabledPluginTools 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewRuntimeRegistersEnabledPluginTools(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	writeFile(t, filepath.Join(root, ".codeworld", "config.toml"), "plugins_enabled = true\n")
	writeFile(t, filepath.Join(root, ".codeworld", "plugins", "demo", "plugin.json"), `{
		"name": "demo",
		"tools": [{
			"name": "demo.echo",
			"description": "echo input",
			"command": "printf",
			"args": ["{{text}}"],
			"input_schema": {
				"type": "object",
				"properties": {"text": {"type": "string"}},
				"required": ["text"]
			},
			"risk": "read"
		}]
	}`)

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if _, ok := rt.Runner.Tools.Get("demo.echo"); !ok {
		t.Fatalf("plugin tool demo.echo was not registered")
	}
}

// TestNewRuntimeRegistersMCPTools 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewRuntimeRegistersMCPTools(t *testing.T) {
	if os.Getenv("CODEWORLD_APP_MCP_TEST_SERVER") == "1" {
		runAppFakeMCPServer()
		return
	}
	root := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("CODEWORLD_APP_MCP_TEST_SERVER", "1")
	writeFile(t, filepath.Join(root, ".codeworld", "config.toml"), fmt.Sprintf(`[[mcp_servers]]
name = "demo"
command = %q
args = ["-test.run=TestNewRuntimeRegistersMCPTools"]
`, os.Args[0]))

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   strings.NewReader("a\n"),
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer rt.Close()
	if _, ok := rt.Runner.Tools.Get("mcp.demo.echo"); !ok {
		t.Fatalf("mcp tool mcp.demo.echo was not registered")
	}
}

// TestNewRuntimeIncludesProjectSkillsInSystemPrompt 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewRuntimeIncludesProjectSkillsInSystemPrompt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	writeFile(t, filepath.Join(root, ".codeworld", "skills", "reviewer", "SKILL.md"), `---
name: reviewer
description: Review Go changes.
---

Always check tests before completion.
`)

	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if !strings.Contains(rt.Runner.SystemPrompt, "Available skills:") || !strings.Contains(rt.Runner.SystemPrompt, "reviewer") {
		t.Fatalf("system prompt missing skill index:\n%s", rt.Runner.SystemPrompt)
	}
	if strings.Contains(rt.Runner.SystemPrompt, "Always check tests before completion.") {
		t.Fatalf("system prompt included full skill body:\n%s", rt.Runner.SystemPrompt)
	}
	if _, ok := rt.Runner.Tools.Get("skill_open"); !ok {
		t.Fatalf("skill_open tool was not registered")
	}
}

func TestRuntimeNewSessionDoesNotReuseCurrentThread(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEWORLD_HOME", t.TempDir())
	store := session.NewStore(root)
	existing := session.New(root, "deepseek", "deepseek-v4-pro")
	if err := store.SaveCurrent(existing); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(context.Background(), Options{Root: root, NewSession: true, In: strings.NewReader(""), Out: io.Discard, Err: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.Session.ID == existing.ID {
		t.Fatalf("new session reused current id %s", existing.ID)
	}
}

func TestRuntimePropagatesAdditionalWritableRoots(t *testing.T) {
	root := t.TempDir()
	extra := t.TempDir()
	t.Setenv("CODEWORLD_HOME", t.TempDir())
	rt, err := NewRuntime(context.Background(), Options{Root: root, AdditionalDirs: []string{extra}, In: strings.NewReader(""), Out: io.Discard, Err: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	canonicalExtra, err := filepath.EvalSymlinks(extra)
	if err != nil {
		t.Fatal(err)
	}
	if len(rt.Workspace.AdditionalRoots) != 1 || rt.Workspace.AdditionalRoots[0] != canonicalExtra {
		t.Fatalf("workspace = %#v", rt.Workspace)
	}
}

// writeFile 是测试辅助函数，用于复用测试准备或断言逻辑。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// runAppFakeMCPServer 是测试辅助函数，用于复用测试准备或断言逻辑。
func runAppFakeMCPServer() {
	reader := bufio.NewReader(os.Stdin)
	for {
		msg, err := readAppMCPMessage(reader)
		if err != nil {
			if err != io.EOF {
				fmt.Fprintln(os.Stderr, err)
			}
			return
		}
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(msg, &req); err != nil {
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05"}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "echo", "description": "Echo", "inputSchema": map[string]any{"type": "object"}}}}
		default:
			result = map[string]any{}
		}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		_, _ = fmt.Fprintln(os.Stdout, string(data))
	}
}

// readAppMCPMessage 是测试辅助函数，用于复用测试准备或断言逻辑。
func readAppMCPMessage(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	return []byte(strings.TrimRight(string(line), "\r\n")), err
}
