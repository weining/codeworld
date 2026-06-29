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
	if rt.Diff == nil {
		t.Fatalf("runtime diff function missing")
	}
}

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
	if !strings.Contains(rt.Runner.SystemPrompt, "Project skills:") || !strings.Contains(rt.Runner.SystemPrompt, "Always check tests before completion.") {
		t.Fatalf("system prompt missing project skills:\n%s", rt.Runner.SystemPrompt)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

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
