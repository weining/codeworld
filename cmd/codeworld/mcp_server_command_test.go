package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"codeworld/internal/app"
	"codeworld/internal/mcp"
	"codeworld/internal/mcpserver"
	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
)

func TestMCPServerInteroperatesWithStdioClient(t *testing.T) {
	if os.Getenv("CODEWORLD_MCP_SERVER_HELPER") == "1" {
		if err := runMCPServerCommand(context.Background(), os.Stdin, os.Stdout, os.TempDir(), globalOptions{}, nil); err != nil {
			t.Fatal(err)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMCPServerInteroperatesWithStdioClient")
	cmd.Env = append(os.Environ(), "CODEWORLD_MCP_SERVER_HELPER=1")
	client, err := mcp.StartStdio(context.Background(), mcp.ServerConfig{Name: "codeworld", Command: cmd.Path, Args: cmd.Args[1:], Env: cmd.Env})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Name != "codex" || tools[1].Name != "codex-reply" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestMCPConfigOverridesFlattenDeterministically(t *testing.T) {
	got, err := mcpConfigOverrides(map[string]any{
		"model": "demo", "features": map[string]any{"plugins": true}, "max_steps": float64(12),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"features.plugins=true", "max_steps=12", `model="demo"`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("overrides = %#v, want %#v", got, want)
	}
}

func TestApplyMCPInstructions(t *testing.T) {
	rt := app.Runtime{}
	rt.Runner.SystemPrompt = "default"
	applyMCPInstructions(&rt, "base", "developer")
	if rt.Runner.SystemPrompt != "base\n\nDeveloper instructions:\ndeveloper" {
		t.Fatalf("system prompt = %q", rt.Runner.SystemPrompt)
	}
}

func TestMCPThreadStoreRoundTripUsesPrivateHashedFile(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	network := false
	store := newMCPThreadStore(home)
	threadID := "../../thread:one"
	state := mcpThread{
		options: app.Options{
			Root: root, Profile: "fast", Model: "model-x", SessionID: threadID,
			CompactPrompt: "compact", AdditionalDirs: []string{"../shared"},
			ApprovalMode: permissions.ModeNever, SandboxMode: sandbox.ModeReadOnly,
			SandboxNetwork: &network, NativeSearch: true,
			ConfigOverrides: []string{"max_steps=12"}, SkipUserConfig: true,
			IgnoreRules: true, BypassHookTrust: true,
		},
		baseInstructions: "base", developerInstructions: "developer",
	}
	if err := store.Save(threadID, state); err != nil {
		t.Fatal(err)
	}
	path := store.path(threadID)
	dir := filepath.Join(home, "mcp-threads")
	if filepath.Dir(path) != dir || strings.Contains(filepath.Base(path), "thread") {
		t.Fatalf("unsafe thread path %q", path)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode=%v err=%v", info, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode=%v err=%v", info, err)
	}
	loaded, err := store.Load(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.options.Root != root || loaded.options.SessionID != threadID || loaded.options.Model != "model-x" || loaded.options.ApprovalMode != permissions.ModeNever || loaded.options.SandboxMode != sandbox.ModeReadOnly || loaded.options.SandboxNetwork == nil || *loaded.options.SandboxNetwork || !loaded.options.NativeSearch || !loaded.options.SkipUserConfig || !loaded.options.IgnoreRules || !loaded.options.BypassHookTrust || loaded.options.Out == nil || loaded.options.Err == nil || loaded.baseInstructions != "base" || loaded.developerInstructions != "developer" {
		t.Fatalf("loaded state=%#v", loaded)
	}
}

func TestMCPRunnerRestoresThreadAfterRestart(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	store := newMCPThreadStore(home)
	first := &codeworldMCPRunner{
		root: root, store: store, threads: map[string]mcpThread{},
		runThread: func(_ context.Context, state mcpThread, prompt string) (mcpserver.Response, error) {
			if !state.options.NewSession || prompt != "start" {
				t.Fatalf("initial state=%#v prompt=%q", state, prompt)
			}
			return mcpserver.Response{ThreadID: "thread-1", Content: "first"}, nil
		},
	}
	if _, err := first.Start(context.Background(), mcpserver.StartRequest{
		Prompt: "start", Model: "model-x", ApprovalPolicy: "never", Sandbox: "read-only",
		BaseInstructions: "base", DeveloperInstructions: "developer", CompactPrompt: "compact",
	}); err != nil {
		t.Fatal(err)
	}

	restarted := &codeworldMCPRunner{
		root: root, store: store, threads: map[string]mcpThread{},
		runThread: func(_ context.Context, state mcpThread, prompt string) (mcpserver.Response, error) {
			if state.options.NewSession || state.options.SessionID != "thread-1" || state.options.Model != "model-x" || state.options.ApprovalMode != permissions.ModeNever || state.options.SandboxMode != sandbox.ModeReadOnly || state.baseInstructions != "base" || state.developerInstructions != "developer" || prompt != "continue" {
				t.Fatalf("restored state=%#v prompt=%q", state, prompt)
			}
			return mcpserver.Response{ThreadID: "thread-1", Content: "second"}, nil
		},
	}
	response, err := restarted.Reply(context.Background(), mcpserver.ReplyRequest{ThreadID: "thread-1", Prompt: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "second" {
		t.Fatalf("response=%#v", response)
	}
	if _, ok := restarted.threads["thread-1"]; !ok {
		t.Fatal("restored thread was not cached")
	}
}

func TestMCPThreadStoreRejectsCorruptRecord(t *testing.T) {
	store := newMCPThreadStore(t.TempDir())
	if err := writeMCPThreadFile(store.path("broken"), []byte("{")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("broken"); err == nil {
		t.Fatal("corrupt record accepted")
	}
}
