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
	"codeworld/internal/session"
)

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
	if rt.Diff == nil {
		t.Fatalf("runtime diff function missing")
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
		Root: root,
		In:   &bytes.Buffer{},
		Out:  &bytes.Buffer{},
		Err:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "lifecycle.out"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "startstop" {
		t.Fatalf("hook output = %q, want startstop", data)
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
		In:   &bytes.Buffer{},
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
		_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(data), data)
	}
}

// readAppMCPMessage 是测试辅助函数，用于复用测试准备或断言逻辑。
func readAppMCPMessage(reader *bufio.Reader) ([]byte, error) {
	var length int
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if _, err := fmt.Sscanf(line, "Content-Length: %d", &length); err != nil {
			return nil, err
		}
	}
	data := make([]byte, length)
	_, err := io.ReadFull(reader, data)
	return data, err
}
