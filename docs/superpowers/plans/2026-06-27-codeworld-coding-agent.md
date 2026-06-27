# Codeworld Coding Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first `codeworld` Go CLI: an interactive local coding agent REPL using DeepSeek `deepseek-v4-pro`, conservative permissions, workspace tools, patch application, shell execution with confirmation, and session persistence.

**Architecture:** A single Go binary wires small internal packages: config, workspace, session, permissions, model, DeepSeek provider, tools, agent loop, and REPL. The agent loop depends on interfaces so provider, tool, and confirmation behavior can evolve without rewriting the runtime.

**Tech Stack:** Go standard library, DeepSeek OpenAI-compatible HTTP API, JSON session files, git command-line integration for status/diff/patch, shell execution through `sh -c` with timeout.

---

## File Structure

Create these files:

```text
go.mod
cmd/codeworld/main.go
internal/agent/agent.go
internal/agent/agent_test.go
internal/config/config.go
internal/config/config_test.go
internal/model/model.go
internal/model/deepseek/client.go
internal/model/deepseek/client_test.go
internal/permissions/permissions.go
internal/permissions/permissions_test.go
internal/repl/repl.go
internal/session/session.go
internal/session/session_test.go
internal/tools/git_tools.go
internal/tools/mutation_tools.go
internal/tools/read_tools.go
internal/tools/registry.go
internal/tools/shell_tool.go
internal/tools/tools.go
internal/tools/tools_test.go
internal/workspace/workspace.go
internal/workspace/workspace_test.go
```

Responsibilities:

- `cmd/codeworld/main.go`: dependency wiring and REPL startup.
- `internal/config`: defaults, project config parsing, environment overrides.
- `internal/model`: provider-neutral message, tool, and generation contracts.
- `internal/model/deepseek`: DeepSeek HTTP client and response parsing.
- `internal/permissions`: conservative policy and structured permission requests.
- `internal/workspace`: path resolution, workspace escape prevention, git detection, summary.
- `internal/session`: JSON session save/load.
- `internal/tools`: tool contracts, registry, read tools, git tools, write/patch/shell tools.
- `internal/agent`: tool-calling loop, max step enforcement, tool error feedback.
- `internal/repl`: terminal loop, slash commands, confirmation prompts.

---

### Task 1: Initialize Go Module

**Files:**
- Create: `go.mod`

- [ ] **Step 1: Initialize the module**

Run:

```bash
go mod init codeworld
```

Expected:

```text
go: creating new go.mod: module codeworld
```

- [ ] **Step 2: Verify module metadata**

Run:

```bash
cat go.mod
```

Expected:

```text
module codeworld

go 1.23.0
```

If the generated Go version is newer or older, keep the local toolchain version that `go mod init` wrote.

- [ ] **Step 3: Commit**

```bash
git add go.mod
git commit -m "chore: initialize go module"
```

---

### Task 2: Add Config And Core Contracts

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `internal/model/model.go`
- Create: `internal/permissions/permissions.go`
- Create: `internal/permissions/permissions_test.go`

- [ ] **Step 1: Write config tests**

Create `internal/config/config_test.go`:

```go
package config

import "testing"

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.Provider != "deepseek" {
		t.Fatalf("Provider = %q, want deepseek", cfg.Provider)
	}
	if cfg.Model != "deepseek-v4-pro" {
		t.Fatalf("Model = %q, want deepseek-v4-pro", cfg.Model)
	}
	if cfg.MaxSteps != 20 {
		t.Fatalf("MaxSteps = %d, want 20", cfg.MaxSteps)
	}
	if cfg.Workspace != "." {
		t.Fatalf("Workspace = %q, want .", cfg.Workspace)
	}
}

func TestLoadProjectConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	writeFile(t, dir+"/.codeworld/config.toml", "provider = \"deepseek\"\nmodel = \"deepseek-v4-flash\"\nmax_steps = 7\nworkspace = \".\"\n")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Model != "deepseek-v4-flash" {
		t.Fatalf("Model = %q, want deepseek-v4-flash", cfg.Model)
	}
	if cfg.MaxSteps != 7 {
		t.Fatalf("MaxSteps = %d, want 7", cfg.MaxSteps)
	}
	if cfg.APIKey != "test-key" {
		t.Fatalf("APIKey was not loaded from environment")
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
```

- [ ] **Step 2: Add missing imports to the config test**

Update the import block in `internal/config/config_test.go`:

```go
import (
	"os"
	"path/filepath"
	"testing"
)
```

- [ ] **Step 3: Run config tests and verify they fail**

Run:

```bash
go test ./internal/config
```

Expected:

```text
FAIL
```

The package should fail because `Default` and `Load` are not implemented yet.

- [ ] **Step 4: Implement config loading**

Create `internal/config/config.go`:

```go
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Provider  string
	Model     string
	MaxSteps  int
	Workspace string
	APIKey    string
}

func Default() Config {
	return Config{
		Provider:  "deepseek",
		Model:     "deepseek-v4-pro",
		MaxSteps:  20,
		Workspace: ".",
	}
}

func Load(root string) (Config, error) {
	cfg := Default()
	path := filepath.Join(root, ".codeworld", "config.toml")

	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return Config{}, err
		}
		cfg.APIKey = os.Getenv("DEEPSEEK_API_KEY")
		return cfg, nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("invalid config line %q", line)
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"")
		switch key {
		case "provider":
			cfg.Provider = value
		case "model":
			cfg.Model = value
		case "max_steps":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid max_steps %q: %w", value, err)
			}
			cfg.MaxSteps = n
		case "workspace":
			cfg.Workspace = value
		default:
			return Config{}, fmt.Errorf("unknown config key %q", key)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}

	cfg.APIKey = os.Getenv("DEEPSEEK_API_KEY")
	return cfg, nil
}
```

- [ ] **Step 5: Create model contracts**

Create `internal/model/model.go`:

```go
package model

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type GenerateRequest struct {
	Model    string           `json:"model"`
	Messages []Message        `json:"messages"`
	Tools    []ToolDefinition `json:"tools"`
}

type GenerateResponse struct {
	Message   Message    `json:"message"`
	ToolCalls []ToolCall `json:"tool_calls"`
	FinalText string     `json:"final_text"`
}

type Client interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
}
```

- [ ] **Step 6: Write permission tests**

Create `internal/permissions/permissions_test.go`:

```go
package permissions

import (
	"context"
	"testing"
)

func TestConservativePolicyAllowsRead(t *testing.T) {
	policy := ConservativePolicy{}
	decision, err := policy.Check(context.Background(), Request{Action: ActionRead, Risk: RiskRead, Target: "README.md"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if decision.Kind != DecisionAllow {
		t.Fatalf("Kind = %q, want allow", decision.Kind)
	}
}

func TestConservativePolicyAsksForPatchWriteAndShell(t *testing.T) {
	policy := ConservativePolicy{}
	cases := []Request{
		{Action: ActionPatch, Risk: RiskWrite, Target: "patch"},
		{Action: ActionWrite, Risk: RiskWrite, Target: "main.go"},
		{Action: ActionShell, Risk: RiskExecute, Target: "go test ./..."},
	}

	for _, req := range cases {
		decision, err := policy.Check(context.Background(), req)
		if err != nil {
			t.Fatalf("Check returned error: %v", err)
		}
		if decision.Kind != DecisionAsk {
			t.Fatalf("Kind for %s = %q, want ask", req.Action, decision.Kind)
		}
	}
}

func TestConservativePolicyDeniesOutsideWorkspace(t *testing.T) {
	policy := ConservativePolicy{}
	decision, err := policy.Check(context.Background(), Request{Action: ActionRead, Risk: RiskOutsideWorkspace, Target: "../secret"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if decision.Kind != DecisionDeny {
		t.Fatalf("Kind = %q, want deny", decision.Kind)
	}
}
```

- [ ] **Step 7: Implement permissions**

Create `internal/permissions/permissions.go`:

```go
package permissions

import "context"

type Action string

const (
	ActionRead  Action = "read"
	ActionWrite Action = "write"
	ActionPatch Action = "patch"
	ActionShell Action = "shell"
)

type Risk string

const (
	RiskRead             Risk = "read"
	RiskWrite            Risk = "write"
	RiskExecute          Risk = "execute"
	RiskDestructive      Risk = "destructive"
	RiskNetwork          Risk = "network"
	RiskOutsideWorkspace Risk = "outside_workspace"
)

type Request struct {
	Action  Action
	Target  string
	Risk    Risk
	Reason  string
	Preview string
}

type DecisionKind string

const (
	DecisionAllow DecisionKind = "allow"
	DecisionAsk   DecisionKind = "ask"
	DecisionDeny  DecisionKind = "deny"
)

type Decision struct {
	Kind   DecisionKind
	Reason string
}

type Policy interface {
	Check(ctx context.Context, req Request) (Decision, error)
}

type ConservativePolicy struct{}

func (ConservativePolicy) Check(ctx context.Context, req Request) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if req.Risk == RiskOutsideWorkspace {
		return Decision{Kind: DecisionDeny, Reason: "path is outside the workspace"}, nil
	}
	if req.Action == ActionRead && req.Risk == RiskRead {
		return Decision{Kind: DecisionAllow, Reason: "read-only workspace action"}, nil
	}
	return Decision{Kind: DecisionAsk, Reason: "action requires confirmation"}, nil
}
```

- [ ] **Step 8: Run tests**

Run:

```bash
go test ./internal/config ./internal/permissions
```

Expected:

```text
ok  	codeworld/internal/config
ok  	codeworld/internal/permissions
```

- [ ] **Step 9: Commit**

```bash
git add internal/config internal/model internal/permissions
git commit -m "feat: add config model and permission contracts"
```

---

### Task 3: Implement Workspace Package

**Files:**
- Create: `internal/workspace/workspace.go`
- Create: `internal/workspace/workspace_test.go`

- [ ] **Step 1: Write workspace tests**

Create `internal/workspace/workspace_test.go`:

```go
package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAllowsWorkspacePath(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	got, err := ws.Resolve("sub/file.go")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	want := filepath.Join(root, "sub", "file.go")
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestResolveRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = ws.Resolve("../outside.txt")
	if err == nil {
		t.Fatalf("Resolve accepted path outside workspace")
	}
}

func TestIsGitRepo(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if !ws.IsGitRepo() {
		t.Fatalf("IsGitRepo = false, want true")
	}
}

func TestSummarySkipsHeavyDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example\n")
	writeFile(t, filepath.Join(root, "internal", "app.go"), "package internal\n")
	writeFile(t, filepath.Join(root, "node_modules", "x.js"), "ignored\n")

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	summary, err := ws.Summary(20)
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}

	if !strings.Contains(summary, "go.mod") {
		t.Fatalf("summary missing go.mod: %s", summary)
	}
	if strings.Contains(summary, "node_modules") {
		t.Fatalf("summary included node_modules: %s", summary)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
```

- [ ] **Step 2: Run workspace tests and verify they fail**

Run:

```bash
go test ./internal/workspace
```

Expected:

```text
FAIL
```

- [ ] **Step 3: Implement workspace package**

Create `internal/workspace/workspace.go`:

```go
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Workspace struct {
	Root string
}

func New(root string) (Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Workspace{}, err
	}
	return Workspace{Root: filepath.Clean(abs)}, nil
}

func (w Workspace) Resolve(path string) (string, error) {
	if path == "" {
		path = "."
	}
	var target string
	if filepath.IsAbs(path) {
		target = filepath.Clean(path)
	} else {
		target = filepath.Clean(filepath.Join(w.Root, path))
	}
	if target != w.Root && !strings.HasPrefix(target, w.Root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q is outside workspace %q", path, w.Root)
	}
	return target, nil
}

func (w Workspace) Rel(path string) (string, error) {
	abs, err := w.Resolve(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(w.Root, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func (w Workspace) IsGitRepo() bool {
	info, err := os.Stat(filepath.Join(w.Root, ".git"))
	return err == nil && info.IsDir()
}

func (w Workspace) Summary(limit int) (string, error) {
	if limit <= 0 {
		limit = 100
	}
	var paths []string
	err := filepath.WalkDir(w.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() && shouldSkipDir(name) && path != w.Root {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(w.Root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	if len(paths) > limit {
		paths = paths[:limit]
		paths = append(paths, "[truncated]")
	}
	return strings.Join(paths, "\n"), nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".codeworld", "node_modules", "vendor", "dist", "build", "target", ".cache":
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Run workspace tests**

Run:

```bash
go test ./internal/workspace
```

Expected:

```text
ok  	codeworld/internal/workspace
```

- [ ] **Step 5: Commit**

```bash
git add internal/workspace
git commit -m "feat: add workspace path safety"
```

---

### Task 4: Implement Session Storage

**Files:**
- Create: `internal/session/session.go`
- Create: `internal/session/session_test.go`

- [ ] **Step 1: Write session tests**

Create `internal/session/session_test.go`:

```go
package session

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadCurrent(t *testing.T) {
	store := NewStore(t.TempDir())
	s := New("workspace", "deepseek", "deepseek-v4-pro")
	s.Messages = append(s.Messages, Message{Role: "user", Content: "hello"})

	if err := store.SaveCurrent(s); err != nil {
		t.Fatalf("SaveCurrent returned error: %v", err)
	}

	loaded, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent returned error: %v", err)
	}
	if loaded.ID != s.ID {
		t.Fatalf("loaded ID = %q, want %q", loaded.ID, s.ID)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "hello" {
		t.Fatalf("messages not preserved: %#v", loaded.Messages)
	}
}

func TestSessionPathUsesCodeworldDirectory(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)

	got := store.CurrentPath()
	want := filepath.Join(root, ".codeworld", "current-session.json")
	if got != want {
		t.Fatalf("CurrentPath = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run session tests and verify they fail**

Run:

```bash
go test ./internal/session
```

Expected:

```text
FAIL
```

- [ ] **Step 3: Implement session storage**

Create `internal/session/session.go`:

```go
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Message struct {
	Role       string `json:"role"`
	Content    string `json:"content,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type ToolEvent struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type Session struct {
	ID        string      `json:"id"`
	Workspace string      `json:"workspace"`
	Provider  string      `json:"provider"`
	Model     string      `json:"model"`
	Messages  []Message   `json:"messages"`
	Tools     []ToolEvent `json:"tools"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type Store struct {
	root string
}

func New(workspace string, provider string, model string) Session {
	now := time.Now().UTC()
	return Session{
		ID:        now.Format("20060102T150405.000000000Z"),
		Workspace: workspace,
		Provider:  provider,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func NewStore(root string) Store {
	return Store{root: root}
}

func (s Store) CurrentPath() string {
	return filepath.Join(s.root, ".codeworld", "current-session.json")
}

func (s Store) SaveCurrent(sess Session) error {
	sess.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.CurrentPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.CurrentPath(), data, 0o600); err != nil {
		return err
	}
	archive := filepath.Join(s.root, ".codeworld", "sessions", sess.ID+".json")
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		return err
	}
	return os.WriteFile(archive, data, 0o600)
}

func (s Store) LoadCurrent() (Session, error) {
	data, err := os.ReadFile(s.CurrentPath())
	if err != nil {
		return Session{}, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return Session{}, err
	}
	return sess, nil
}
```

- [ ] **Step 4: Run session tests**

Run:

```bash
go test ./internal/session
```

Expected:

```text
ok  	codeworld/internal/session
```

- [ ] **Step 5: Commit**

```bash
git add internal/session
git commit -m "feat: add session storage"
```

---

### Task 5: Add Tool Registry And Read Tools

**Files:**
- Create: `internal/tools/tools.go`
- Create: `internal/tools/registry.go`
- Create: `internal/tools/read_tools.go`
- Create: `internal/tools/git_tools.go`
- Create: `internal/tools/tools_test.go`

- [ ] **Step 1: Write tool tests**

Create `internal/tools/tools_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/permissions"
	"codeworld/internal/workspace"
)

func TestRegistryRejectsUnknownTool(t *testing.T) {
	reg := NewRegistry(nil, nil)
	_, err := reg.Execute(context.Background(), "missing", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("Execute accepted unknown tool")
	}
}

func TestReadFileToolReadsLimitedContent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "abcdef")
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	tool := NewReadFileTool(ws)

	req, err := tool.PermissionRequest(json.RawMessage(`{"path":"a.txt","offset":1,"limit":3}`))
	if err != nil {
		t.Fatalf("PermissionRequest: %v", err)
	}
	if req.Action != permissions.ActionRead {
		t.Fatalf("Action = %q, want read", req.Action)
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"a.txt","offset":1,"limit":3}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Content != "bcd" {
		t.Fatalf("Content = %q, want bcd", result.Content)
	}
}

func TestListDirToolListsEntries(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "a")
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	tool := NewListDirTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Content, "a.txt") {
		t.Fatalf("Content missing a.txt: %q", result.Content)
	}
}

func TestSearchToolFindsText(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "alpha\nneedle\n")
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	tool := NewSearchTool(ws)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"needle","path":"."}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Content, "a.txt:2:needle") {
		t.Fatalf("Content missing match: %q", result.Content)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
```

- [ ] **Step 2: Run tool tests and verify they fail**

Run:

```bash
go test ./internal/tools
```

Expected:

```text
FAIL
```

- [ ] **Step 3: Implement tool contracts**

Create `internal/tools/tools.go`:

```go
package tools

import (
	"context"
	"encoding/json"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
)

type Result struct {
	Content   string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Tool interface {
	Definition() model.ToolDefinition
	PermissionRequest(args json.RawMessage) (permissions.Request, error)
	Execute(ctx context.Context, args json.RawMessage) (Result, error)
}
```

- [ ] **Step 4: Implement registry**

Create `internal/tools/registry.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"codeworld/internal/model"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry(items []Tool, extra []Tool) *Registry {
	reg := &Registry{tools: make(map[string]Tool)}
	for _, item := range items {
		reg.Register(item)
	}
	for _, item := range extra {
		reg.Register(item)
	}
	return reg
}

func (r *Registry) Register(tool Tool) {
	r.tools[tool.Definition().Name] = tool
}

func (r *Registry) Definitions() []model.ToolDefinition {
	defs := make([]model.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, tool.Definition())
	}
	return defs
}

func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	tool, ok := r.Get(name)
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", name)
	}
	return tool.Execute(ctx, args)
}
```

- [ ] **Step 5: Implement read tools**

Create `internal/tools/read_tools.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/workspace"
)

type ListDirTool struct {
	workspace workspace.Workspace
}

func NewListDirTool(ws workspace.Workspace) ListDirTool {
	return ListDirTool{workspace: ws}
}

func (t ListDirTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "list_dir",
		Description: "List entries under a workspace directory.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		},
	}
}

func (t ListDirTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	var input struct{ Path string `json:"path"` }
	if err := json.Unmarshal(args, &input); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionRead, Risk: permissions.RiskRead, Target: input.Path}, nil
}

func (t ListDirTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var input struct{ Path string `json:"path"` }
	if err := json.Unmarshal(args, &input); err != nil {
		return Result{}, err
	}
	path, err := t.workspace.Resolve(input.Path)
	if err != nil {
		return Result{}, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return Result{}, err
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		lines = append(lines, name)
	}
	sort.Strings(lines)
	return Result{Content: strings.Join(lines, "\n")}, nil
}

type ReadFileTool struct {
	workspace workspace.Workspace
}

func NewReadFileTool(ws workspace.Workspace) ReadFileTool {
	return ReadFileTool{workspace: ws}
}

func (t ReadFileTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "read_file",
		Description: "Read a byte range from a workspace file.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":   map[string]any{"type": "string"},
				"offset": map[string]any{"type": "integer"},
				"limit":  map[string]any{"type": "integer"},
			},
			"required": []string{"path"},
		},
	}
}

func (t ReadFileTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	var input struct{ Path string `json:"path"` }
	if err := json.Unmarshal(args, &input); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionRead, Risk: permissions.RiskRead, Target: input.Path}, nil
}

func (t ReadFileTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var input struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return Result{}, err
	}
	if input.Limit <= 0 {
		input.Limit = 20000
	}
	path, err := t.workspace.Resolve(input.Path)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	if input.Offset < 0 {
		return Result{}, fmt.Errorf("offset must be non-negative")
	}
	if input.Offset > len(data) {
		return Result{Content: "", Metadata: map[string]any{"truncated": false}}, nil
	}
	end := input.Offset + input.Limit
	if end > len(data) {
		end = len(data)
	}
	return Result{
		Content: string(data[input.Offset:end]),
		Metadata: map[string]any{
			"path":      filepath.ToSlash(input.Path),
			"offset":    input.Offset,
			"limit":     input.Limit,
			"truncated": end < len(data),
		},
	}, nil
}

type SearchTool struct {
	workspace workspace.Workspace
}

func NewSearchTool(ws workspace.Workspace) SearchTool {
	return SearchTool{workspace: ws}
}

func (t SearchTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "search",
		Description: "Search workspace text files for a literal query.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"path":  map[string]any{"type": "string"},
			},
			"required": []string{"query"},
		},
	}
}

func (t SearchTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	var input struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionRead, Risk: permissions.RiskRead, Target: input.Path + " " + input.Query}, nil
}

func (t SearchTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var input struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return Result{}, err
	}
	if input.Path == "" {
		input.Path = "."
	}
	root, err := t.workspace.Resolve(input.Path)
	if err != nil {
		return Result{}, err
	}
	var lines []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".codeworld", "node_modules", "vendor", "dist", "build", "target", ".cache":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		fileLines := strings.Split(string(data), "\n")
		rel, err := filepath.Rel(t.workspace.Root, path)
		if err != nil {
			return err
		}
		for i, line := range fileLines {
			if strings.Contains(line, input.Query) {
				lines = append(lines, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(rel), i+1, line))
				if len(lines) >= 100 {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Content: strings.Join(lines, "\n"), Metadata: map[string]any{"matches": len(lines), "truncated": len(lines) >= 100}}, nil
}
```

- [ ] **Step 6: Implement git tools**

Create `internal/tools/git_tools.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"os/exec"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/workspace"
)

type GitStatusTool struct {
	workspace workspace.Workspace
}

func NewGitStatusTool(ws workspace.Workspace) GitStatusTool {
	return GitStatusTool{workspace: ws}
}

func (t GitStatusTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "git_status", Description: "Show git status --short.", InputSchema: map[string]any{"type": "object"}}
}

func (t GitStatusTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Action: permissions.ActionRead, Risk: permissions.RiskRead, Target: "git status --short"}, nil
}

func (t GitStatusTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--short")
	cmd.Dir = t.workspace.Root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Content: string(out)}, err
	}
	return Result{Content: string(out)}, nil
}

type GitDiffTool struct {
	workspace workspace.Workspace
}

func NewGitDiffTool(ws workspace.Workspace) GitDiffTool {
	return GitDiffTool{workspace: ws}
}

func (t GitDiffTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "git_diff", Description: "Show the current git diff.", InputSchema: map[string]any{"type": "object"}}
}

func (t GitDiffTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Action: permissions.ActionRead, Risk: permissions.RiskRead, Target: "git diff"}, nil
}

func (t GitDiffTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	cmd := exec.CommandContext(ctx, "git", "diff")
	cmd.Dir = t.workspace.Root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Content: string(out)}, err
	}
	return Result{Content: string(out)}, nil
}
```

- [ ] **Step 7: Run tool tests**

Run:

```bash
go test ./internal/tools
```

Expected:

```text
ok  	codeworld/internal/tools
```

- [ ] **Step 8: Commit**

```bash
git add internal/tools
git commit -m "feat: add read and git tools"
```

---

### Task 6: Add Mutation And Shell Tools

**Files:**
- Modify: `internal/tools/tools_test.go`
- Create: `internal/tools/mutation_tools.go`
- Create: `internal/tools/shell_tool.go`

- [ ] **Step 1: Add tests for write and shell permission requests**

Append to `internal/tools/tools_test.go`:

```go
func TestWriteFileToolRequiresWritePermission(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	tool := NewWriteFileTool(ws)

	req, err := tool.PermissionRequest(json.RawMessage(`{"path":"new.txt","content":"hello"}`))
	if err != nil {
		t.Fatalf("PermissionRequest: %v", err)
	}
	if req.Action != permissions.ActionWrite || req.Risk != permissions.RiskWrite {
		t.Fatalf("request = %#v, want write risk", req)
	}
}

func TestShellToolRequiresExecutePermission(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	tool := NewShellTool(ws, 0)

	req, err := tool.PermissionRequest(json.RawMessage(`{"command":"go test ./...","cwd":"."}`))
	if err != nil {
		t.Fatalf("PermissionRequest: %v", err)
	}
	if req.Action != permissions.ActionShell || req.Risk != permissions.RiskExecute {
		t.Fatalf("request = %#v, want shell execute risk", req)
	}
}

func TestApplyPatchRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	tool := NewApplyPatchTool(ws)

	_, err = tool.Execute(context.Background(), json.RawMessage(`{"patch":"--- a/../secret\n+++ b/../secret\n@@ -0,0 +1 @@\n+bad\n"}`))
	if err == nil {
		t.Fatalf("Execute accepted patch outside workspace")
	}
}
```

- [ ] **Step 2: Run tool tests and verify they fail**

Run:

```bash
go test ./internal/tools
```

Expected:

```text
FAIL
```

- [ ] **Step 3: Implement mutation tools**

Create `internal/tools/mutation_tools.go`:

```go
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/workspace"
)

type WriteFileTool struct {
	workspace workspace.Workspace
}

func NewWriteFileTool(ws workspace.Workspace) WriteFileTool {
	return WriteFileTool{workspace: ws}
}

func (t WriteFileTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "write_file",
		Description: "Create or rewrite a workspace file after confirmation.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
				"reason":  map[string]any{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	}
}

func (t WriteFileTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	var input struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionWrite, Risk: permissions.RiskWrite, Target: input.Path, Reason: input.Reason}, nil
}

func (t WriteFileTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return Result{}, err
	}
	path, err := t.workspace.Resolve(input.Path)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(path, []byte(input.Content), 0o644); err != nil {
		return Result{}, err
	}
	return Result{Content: "wrote " + input.Path}, nil
}

type ApplyPatchTool struct {
	workspace workspace.Workspace
}

func NewApplyPatchTool(ws workspace.Workspace) ApplyPatchTool {
	return ApplyPatchTool{workspace: ws}
}

func (t ApplyPatchTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "apply_patch",
		Description: "Apply a unified diff inside the workspace after confirmation.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"patch":  map[string]any{"type": "string"},
				"reason": map[string]any{"type": "string"},
			},
			"required": []string{"patch"},
		},
	}
}

func (t ApplyPatchTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	var input struct {
		Patch  string `json:"patch"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionPatch, Risk: permissions.RiskWrite, Target: "patch", Reason: input.Reason, Preview: input.Patch}, nil
}

func (t ApplyPatchTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	var input struct {
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return Result{}, err
	}
	if err := validatePatchPaths(t.workspace, input.Patch); err != nil {
		return Result{}, err
	}
	check := exec.CommandContext(ctx, "git", "apply", "--check", "-")
	check.Dir = t.workspace.Root
	check.Stdin = bytes.NewBufferString(input.Patch)
	if out, err := check.CombinedOutput(); err != nil {
		return Result{Content: string(out)}, err
	}
	apply := exec.CommandContext(ctx, "git", "apply", "-")
	apply.Dir = t.workspace.Root
	apply.Stdin = bytes.NewBufferString(input.Patch)
	out, err := apply.CombinedOutput()
	if err != nil {
		return Result{Content: string(out)}, err
	}
	return Result{Content: "patch applied"}, nil
}

func validatePatchPaths(ws workspace.Workspace, patch string) error {
	for _, line := range strings.Split(patch, "\n") {
		if !strings.HasPrefix(line, "--- ") && !strings.HasPrefix(line, "+++ ") {
			continue
		}
		path := strings.TrimSpace(line[4:])
		if path == "/dev/null" {
			continue
		}
		path = strings.TrimPrefix(path, "a/")
		path = strings.TrimPrefix(path, "b/")
		if _, err := ws.Resolve(path); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Implement shell tool**

Create `internal/tools/shell_tool.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"time"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/workspace"
)

type ShellTool struct {
	workspace workspace.Workspace
	timeout   time.Duration
}

func NewShellTool(ws workspace.Workspace, timeout time.Duration) ShellTool {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return ShellTool{workspace: ws, timeout: timeout}
}

func (t ShellTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "shell",
		Description: "Run a shell command inside the workspace after confirmation.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
				"cwd":     map[string]any{"type": "string"},
				"reason":  map[string]any{"type": "string"},
			},
			"required": []string{"command"},
		},
	}
}

func (t ShellTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	var input struct {
		Command string `json:"command"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionShell, Risk: classifyShellRisk(input.Command), Target: input.Command, Reason: input.Reason, Preview: input.Command}, nil
}

func (t ShellTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	var input struct {
		Command string `json:"command"`
		CWD     string `json:"cwd"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return Result{}, err
	}
	if input.CWD == "" {
		input.CWD = "."
	}
	cwd, err := t.workspace.Resolve(input.CWD)
	if err != nil {
		return Result{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	started := time.Now()
	cmd := exec.CommandContext(runCtx, "sh", "-c", input.Command)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	duration := time.Since(started)
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	result := Result{
		Content: string(out),
		Metadata: map[string]any{
			"command":     input.Command,
			"duration_ms": duration.Milliseconds(),
			"exit_code":   exitCode,
			"timed_out":   runCtx.Err() == context.DeadlineExceeded,
		},
	}
	return result, err
}

func classifyShellRisk(command string) permissions.Risk {
	switch {
	case hasPrefix(command, "curl "), hasPrefix(command, "wget "), hasPrefix(command, "go get "), hasPrefix(command, "npm install"):
		return permissions.RiskNetwork
	case hasPrefix(command, "rm "), hasPrefix(command, "git reset"), hasPrefix(command, "git clean"), hasPrefix(command, "mv "):
		return permissions.RiskDestructive
	default:
		return permissions.RiskExecute
	}
}

func hasPrefix(command string, prefix string) bool {
	return len(command) >= len(prefix) && command[:len(prefix)] == prefix
}
```

- [ ] **Step 5: Run tool tests**

Run:

```bash
go test ./internal/tools
```

Expected:

```text
ok  	codeworld/internal/tools
```

- [ ] **Step 6: Commit**

```bash
git add internal/tools
git commit -m "feat: add mutation and shell tools"
```

---

### Task 7: Implement Agent Loop

**Files:**
- Create: `internal/agent/agent.go`
- Create: `internal/agent/agent_test.go`

- [ ] **Step 1: Write agent loop tests**

Create `internal/agent/agent_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"testing"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/tools"
)

func TestRunTurnExecutesReadToolAndReturnsFinalText(t *testing.T) {
	client := &fakeClient{
		responses: []model.GenerateResponse{
			{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)}}},
			{FinalText: "done"},
		},
	}
	reg := tools.NewRegistry([]tools.Tool{echoTool{}}, nil)
	runner := Runner{Model: client, Tools: reg, Policy: permissions.ConservativePolicy{}, Confirmer: allowConfirmer{}, MaxSteps: 3, ModelName: "test-model"}

	result, err := runner.RunTurn(context.Background(), nil, "start")
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if result.FinalText != "done" {
		t.Fatalf("FinalText = %q, want done", result.FinalText)
	}
	if client.calls != 2 {
		t.Fatalf("model calls = %d, want 2", client.calls)
	}
}

func TestRunTurnStopsAtMaxSteps(t *testing.T) {
	client := &fakeClient{
		responses: []model.GenerateResponse{
			{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"again"}`)}}},
			{ToolCalls: []model.ToolCall{{ID: "call-2", Name: "echo", Arguments: json.RawMessage(`{"text":"again"}`)}}},
		},
	}
	reg := tools.NewRegistry([]tools.Tool{echoTool{}}, nil)
	runner := Runner{Model: client, Tools: reg, Policy: permissions.ConservativePolicy{}, Confirmer: allowConfirmer{}, MaxSteps: 1, ModelName: "test-model"}

	_, err := runner.RunTurn(context.Background(), nil, "start")
	if err == nil {
		t.Fatalf("RunTurn succeeded after max steps")
	}
}

type fakeClient struct {
	responses []model.GenerateResponse
	calls     int
}

func (f *fakeClient) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	if f.calls >= len(f.responses) {
		return model.GenerateResponse{FinalText: "fallback"}, nil
	}
	resp := f.responses[f.calls]
	f.calls++
	return resp, nil
}

type echoTool struct{}

func (echoTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "echo", Description: "echo", InputSchema: map[string]any{"type": "object"}}
}

func (echoTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Action: permissions.ActionRead, Risk: permissions.RiskRead, Target: "echo"}, nil
}

func (echoTool) Execute(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	return tools.Result{Content: string(args)}, nil
}

type allowConfirmer struct{}

func (allowConfirmer) Confirm(ctx context.Context, req permissions.Request, decision permissions.Decision) (bool, error) {
	return true, nil
}
```

- [ ] **Step 2: Run agent tests and verify they fail**

Run:

```bash
go test ./internal/agent
```

Expected:

```text
FAIL
```

- [ ] **Step 3: Implement agent loop**

Create `internal/agent/agent.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/tools"
)

type Confirmer interface {
	Confirm(ctx context.Context, req permissions.Request, decision permissions.Decision) (bool, error)
}

type Runner struct {
	Model     model.Client
	Tools     *tools.Registry
	Policy    permissions.Policy
	Confirmer Confirmer
	MaxSteps  int
	ModelName string
	SystemPrompt string
}

type TurnResult struct {
	FinalText string
	Messages  []model.Message
}

func (r Runner) RunTurn(ctx context.Context, history []model.Message, input string) (TurnResult, error) {
	maxSteps := r.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 20
	}
	messages := []model.Message{{Role: model.RoleSystem, Content: firstNonEmpty(r.SystemPrompt, DefaultSystemPrompt)}}
	messages = append(messages, history...)
	messages = append(messages, model.Message{Role: model.RoleUser, Content: input})

	for step := 0; step < maxSteps; step++ {
		resp, err := r.Model.Generate(ctx, model.GenerateRequest{
			Model:    r.ModelName,
			Messages: messages,
			Tools:    r.Tools.Definitions(),
		})
		if err != nil {
			return TurnResult{}, err
		}
		if resp.FinalText != "" {
			messages = append(messages, model.Message{Role: model.RoleAssistant, Content: resp.FinalText})
			return TurnResult{FinalText: resp.FinalText, Messages: messages}, nil
		}
		if len(resp.ToolCalls) == 0 {
			return TurnResult{}, fmt.Errorf("model returned no final text and no tool calls")
		}
		messages = append(messages, model.Message{Role: model.RoleAssistant, ToolCalls: resp.ToolCalls})
		for _, call := range resp.ToolCalls {
			content := r.executeTool(ctx, call)
			messages = append(messages, model.Message{Role: model.RoleTool, ToolCallID: call.ID, Content: content})
		}
	}
	return TurnResult{}, fmt.Errorf("agent exceeded max steps %d", maxSteps)
}

func (r Runner) executeTool(ctx context.Context, call model.ToolCall) string {
	tool, ok := r.Tools.Get(call.Name)
	if !ok {
		return "tool error: unknown tool " + call.Name
	}
	req, err := tool.PermissionRequest(call.Arguments)
	if err != nil {
		return "permission request error: " + err.Error()
	}
	decision, err := r.Policy.Check(ctx, req)
	if err != nil {
		return "permission policy error: " + err.Error()
	}
	switch decision.Kind {
	case permissions.DecisionDeny:
		return "permission denied: " + decision.Reason
	case permissions.DecisionAsk:
		if r.Confirmer == nil {
			return "permission denied: confirmation required"
		}
		allowed, err := r.Confirmer.Confirm(ctx, req, decision)
		if err != nil {
			return "confirmation error: " + err.Error()
		}
		if !allowed {
			return "permission denied by user"
		}
	}
	result, err := tool.Execute(ctx, call.Arguments)
	if err != nil {
		return encodeToolResult(result, "tool error: "+err.Error())
	}
	return encodeToolResult(result, "")
}

func encodeToolResult(result tools.Result, prefix string) string {
	data, err := json.Marshal(result)
	if err != nil {
		if prefix != "" {
			return prefix
		}
		return result.Content
	}
	if prefix != "" {
		return prefix + "\n" + string(data)
	}
	return string(data)
}

const DefaultSystemPrompt = `You are codeworld, a conservative local coding agent.
Inspect files before editing them.
Keep changes narrow and explain why permissioned actions are needed.
Prefer apply_patch for existing source files.
Never invent file contents, command output, or test results.
When a tool fails, use the error result to decide the next step.`

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
```

- [ ] **Step 4: Run agent tests**

Run:

```bash
go test ./internal/agent
```

Expected:

```text
ok  	codeworld/internal/agent
```

- [ ] **Step 5: Commit**

```bash
git add internal/agent
git commit -m "feat: add agent tool loop"
```

---

### Task 8: Implement DeepSeek Client

**Files:**
- Create: `internal/model/deepseek/client.go`
- Create: `internal/model/deepseek/client_test.go`

- [ ] **Step 1: Write DeepSeek client tests**

Create `internal/model/deepseek/client_test.go`:

```go
package deepseek

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codeworld/internal/model"
)

func TestGenerateSendsAuthorizationAndParsesFinalText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q, want Bearer test-key", got)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatalf("path = %q, want /chat/completions suffix", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "deepseek-v4-pro")
	client.baseURL = server.URL

	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Model:    "deepseek-v4-pro",
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.FinalText != "hello" {
		t.Fatalf("FinalText = %q, want hello", resp.FinalText)
	}
}

func TestGenerateParsesToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"go.mod\"}"}}]}}]}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "deepseek-v4-pro")
	client.baseURL = server.URL

	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Model:    "deepseek-v4-pro",
		Messages: []model.Message{{Role: model.RoleUser, Content: "read"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "read_file" {
		t.Fatalf("tool name = %q, want read_file", resp.ToolCalls[0].Name)
	}
}
```

- [ ] **Step 2: Run DeepSeek tests and verify they fail**

Run:

```bash
go test ./internal/model/deepseek
```

Expected:

```text
FAIL
```

- [ ] **Step 3: Implement DeepSeek client**

Create `internal/model/deepseek/client.go`:

```go
package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"codeworld/internal/model"
)

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey string, modelName string) *Client {
	return &Client{
		apiKey:     apiKey,
		model:      modelName,
		baseURL:    "https://api.deepseek.com",
		httpClient: http.DefaultClient,
	}
}

func (c *Client) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	if c.apiKey == "" {
		return model.GenerateResponse{}, fmt.Errorf("DEEPSEEK_API_KEY is not set")
	}
	body := requestBody{
		Model:    firstNonEmpty(req.Model, c.model),
		Messages: toProviderMessages(req.Messages),
		Tools:    toProviderTools(req.Tools),
	}
	data, err := json.Marshal(body)
	if err != nil {
		return model.GenerateResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return model.GenerateResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return model.GenerateResponse{}, err
	}
	defer httpResp.Body.Close()

	respData, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return model.GenerateResponse{}, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return model.GenerateResponse{}, fmt.Errorf("deepseek status %d: %s", httpResp.StatusCode, string(respData))
	}
	var providerResp responseBody
	if err := json.Unmarshal(respData, &providerResp); err != nil {
		return model.GenerateResponse{}, err
	}
	if len(providerResp.Choices) == 0 {
		return model.GenerateResponse{}, fmt.Errorf("deepseek returned no choices")
	}
	msg := providerResp.Choices[0].Message
	toolCalls := make([]model.ToolCall, 0, len(msg.ToolCalls))
	for _, call := range msg.ToolCalls {
		toolCalls = append(toolCalls, model.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: json.RawMessage(call.Function.Arguments),
		})
	}
	return model.GenerateResponse{
		Message:   model.Message{Role: model.RoleAssistant, Content: msg.Content, ToolCalls: toolCalls},
		ToolCalls: toolCalls,
		FinalText: msg.Content,
	}, nil
}

type requestBody struct {
	Model    string            `json:"model"`
	Messages []providerMessage `json:"messages"`
	Tools    []providerTool    `json:"tools,omitempty"`
}

type providerMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	ToolCalls  []providerToolCall `json:"tool_calls,omitempty"`
}

type providerTool struct {
	Type     string           `json:"type"`
	Function providerFunction `json:"function"`
}

type providerFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type providerToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function providerCallFunction `json:"function"`
}

type providerCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responseBody struct {
	Choices []struct {
		Message providerMessage `json:"message"`
	} `json:"choices"`
}

func toProviderMessages(messages []model.Message) []providerMessage {
	out := make([]providerMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, providerMessage{
			Role:       string(msg.Role),
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
			ToolCalls:  toProviderToolCalls(msg.ToolCalls),
		})
	}
	return out
}

func toProviderToolCalls(calls []model.ToolCall) []providerToolCall {
	out := make([]providerToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, providerToolCall{
			ID:   call.ID,
			Type: "function",
			Function: providerCallFunction{
				Name:      call.Name,
				Arguments: string(call.Arguments),
			},
		})
	}
	return out
}

func toProviderTools(defs []model.ToolDefinition) []providerTool {
	out := make([]providerTool, 0, len(defs))
	for _, def := range defs {
		out = append(out, providerTool{
			Type: "function",
			Function: providerFunction{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  def.InputSchema,
			},
		})
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
```

- [ ] **Step 4: Fix final text behavior for tool calls**

Modify the return block in `Generate` so `FinalText` is empty when tool calls exist:

```go
finalText := msg.Content
if len(toolCalls) > 0 {
	finalText = ""
}
return model.GenerateResponse{
	Message:   model.Message{Role: model.RoleAssistant, Content: msg.Content, ToolCalls: toolCalls},
	ToolCalls: toolCalls,
	FinalText: finalText,
}, nil
```

- [ ] **Step 5: Run DeepSeek tests**

Run:

```bash
go test ./internal/model/deepseek
```

Expected:

```text
ok  	codeworld/internal/model/deepseek
```

- [ ] **Step 6: Commit**

```bash
git add internal/model/deepseek
git commit -m "feat: add deepseek model client"
```

---

### Task 9: Implement REPL

**Files:**
- Create: `internal/repl/repl.go`

- [ ] **Step 1: Create REPL implementation**

Create `internal/repl/repl.go`:

```go
package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"codeworld/internal/agent"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/session"
)

type REPL struct {
	In       io.Reader
	Out      io.Writer
	Runner   agent.Runner
	Store    session.Store
	Session  session.Session
	Messages []model.Message
	Diff     func(context.Context) (string, error)
}

func (r *REPL) Run(ctx context.Context) error {
	if r.In == nil {
		return fmt.Errorf("input is nil")
	}
	if r.Out == nil {
		return fmt.Errorf("output is nil")
	}
	scanner := bufio.NewScanner(r.In)
	fmt.Fprintln(r.Out, "codeworld")
	fmt.Fprintln(r.Out, "Type /help for commands.")
	for {
		fmt.Fprint(r.Out, "codeworld> ")
		if !scanner.Scan() {
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			if r.handleCommand(ctx, line) {
				return nil
			}
			continue
		}
		result, err := r.Runner.RunTurn(ctx, r.Messages, line)
		if err != nil {
			fmt.Fprintf(r.Out, "error: %v\n", err)
			continue
		}
		r.Messages = result.Messages
		fmt.Fprintln(r.Out, result.FinalText)
		r.syncSession()
		if err := r.Store.SaveCurrent(r.Session); err != nil {
			fmt.Fprintf(r.Out, "session save error: %v\n", err)
		}
	}
}

func (r *REPL) handleCommand(ctx context.Context, line string) bool {
	switch {
	case line == "/help":
		fmt.Fprintln(r.Out, "/help /model /status /diff /clear /exit")
	case line == "/model":
		fmt.Fprintf(r.Out, "%s\n", r.Session.Model)
	case strings.HasPrefix(line, "/model "):
		next := strings.TrimSpace(strings.TrimPrefix(line, "/model "))
		if next == "" {
			fmt.Fprintf(r.Out, "%s\n", r.Session.Model)
			return false
		}
		r.Session.Model = next
		r.Runner.ModelName = next
		fmt.Fprintf(r.Out, "model=%s\n", next)
	case line == "/status":
		fmt.Fprintf(r.Out, "workspace=%s provider=%s model=%s messages=%d\n", r.Session.Workspace, r.Session.Provider, r.Session.Model, len(r.Messages))
	case line == "/diff":
		if r.Diff == nil {
			fmt.Fprintln(r.Out, "diff unavailable")
			return false
		}
		diff, err := r.Diff(ctx)
		if err != nil {
			fmt.Fprintf(r.Out, "diff error: %v\n", err)
			return false
		}
		if diff == "" {
			fmt.Fprintln(r.Out, "no diff")
			return false
		}
		fmt.Fprintln(r.Out, diff)
	case line == "/clear":
		r.Messages = nil
		r.Session.Messages = nil
		fmt.Fprintln(r.Out, "cleared")
	case line == "/exit":
		return true
	default:
		fmt.Fprintf(r.Out, "unknown command: %s\n", line)
	}
	return false
}

func (r *REPL) syncSession() {
	r.Session.Messages = r.Session.Messages[:0]
	for _, msg := range r.Messages {
		r.Session.Messages = append(r.Session.Messages, session.Message{
			Role:       string(msg.Role),
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
		})
	}
}

type Confirmer struct {
	In  io.Reader
	Out io.Writer
}

func (c Confirmer) Confirm(ctx context.Context, req permissions.Request, decision permissions.Decision) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	fmt.Fprintf(c.Out, "\nPermission required: %s\nTarget: %s\nRisk: %s\nReason: %s\n", req.Action, req.Target, req.Risk, req.Reason)
	if req.Preview != "" {
		fmt.Fprintf(c.Out, "Preview:\n%s\n", req.Preview)
	}
	fmt.Fprint(c.Out, "Allow? [y/N] ")
	reader := bufio.NewReader(c.In)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "y"), nil
}
```

- [ ] **Step 2: Run package tests**

Run:

```bash
go test ./internal/repl
```

Expected:

```text
ok  	codeworld/internal/repl
```

- [ ] **Step 3: Commit**

```bash
git add internal/repl
git commit -m "feat: add interactive repl"
```

---

### Task 10: Wire CLI Main

**Files:**
- Create: `cmd/codeworld/main.go`

- [ ] **Step 1: Create main package**

Create `cmd/codeworld/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"

	"codeworld/internal/agent"
	"codeworld/internal/config"
	"codeworld/internal/model/deepseek"
	"codeworld/internal/permissions"
	"codeworld/internal/repl"
	"codeworld/internal/session"
	"codeworld/internal/tools"
	"codeworld/internal/workspace"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "codeworld:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	ws, err := workspace.New(root)
	if err != nil {
		return err
	}
	if !ws.IsGitRepo() {
		fmt.Fprintln(os.Stderr, "warning: current workspace is not a git repository; patch workflows are safer in git repositories")
	}
	summary, err := ws.Summary(120)
	if err != nil {
		summary = "workspace summary unavailable: " + err.Error()
	}
	systemPrompt := agent.DefaultSystemPrompt + "\n\nWorkspace files:\n" + summary

	store := session.NewStore(ws.Root)
	sess := session.New(ws.Root, cfg.Provider, cfg.Model)
	client := deepseek.NewClient(cfg.APIKey, cfg.Model)
	diffTool := tools.NewGitDiffTool(ws)
	registry := tools.NewRegistry([]tools.Tool{
		tools.NewListDirTool(ws),
		tools.NewReadFileTool(ws),
		tools.NewSearchTool(ws),
		tools.NewGitStatusTool(ws),
		diffTool,
		tools.NewWriteFileTool(ws),
		tools.NewApplyPatchTool(ws),
		tools.NewShellTool(ws, 0),
	}, nil)

	confirmer := repl.Confirmer{In: os.Stdin, Out: os.Stdout}
	runner := agent.Runner{
		Model:     client,
		Tools:     registry,
		Policy:    permissions.ConservativePolicy{},
		Confirmer: confirmer,
		MaxSteps:  cfg.MaxSteps,
		ModelName: cfg.Model,
		SystemPrompt: systemPrompt,
	}
	app := repl.REPL{
		In:      os.Stdin,
		Out:     os.Stdout,
		Runner:  runner,
		Store:   store,
		Session: sess,
		Diff: func(ctx context.Context) (string, error) {
			result, err := diffTool.Execute(ctx, nil)
			return result.Content, err
		},
	}
	return app.Run(context.Background())
}
```

- [ ] **Step 2: Run all tests**

Run:

```bash
go test ./...
```

Expected:

```text
ok  	codeworld/internal/agent
ok  	codeworld/internal/config
ok  	codeworld/internal/model/deepseek
ok  	codeworld/internal/permissions
ok  	codeworld/internal/repl
ok  	codeworld/internal/session
ok  	codeworld/internal/tools
ok  	codeworld/internal/workspace
```

- [ ] **Step 3: Build CLI**

Run:

```bash
go build ./cmd/codeworld
```

Expected:

```text
```

The command exits with status 0 and writes a `codeworld` binary in the repository root.

- [ ] **Step 4: Remove local build artifact**

Run:

```bash
rm codeworld
```

Expected:

```text
```

- [ ] **Step 5: Commit**

```bash
git add cmd/codeworld
git commit -m "feat: wire codeworld cli"
```

---

### Task 11: Final Verification

**Files:**
- Modify: files changed by prior tasks only if verification exposes defects.

- [ ] **Step 1: Run formatter**

Run:

```bash
gofmt -w cmd internal
```

Expected:

```text
```

- [ ] **Step 2: Run all tests**

Run:

```bash
go test ./...
```

Expected:

```text
ok  	codeworld/internal/agent
ok  	codeworld/internal/config
ok  	codeworld/internal/model/deepseek
ok  	codeworld/internal/permissions
ok  	codeworld/internal/repl
ok  	codeworld/internal/session
ok  	codeworld/internal/tools
ok  	codeworld/internal/workspace
```

- [ ] **Step 3: Build CLI**

Run:

```bash
go build ./cmd/codeworld
```

Expected:

```text
```

- [ ] **Step 4: Run non-API REPL smoke check**

Run:

```bash
printf "/help\n/exit\n" | ./codeworld
```

Expected output contains:

```text
codeworld
Type /help for commands.
/help /model /status /diff /clear /exit
```

- [ ] **Step 5: Remove local build artifact**

Run:

```bash
rm codeworld
```

Expected:

```text
```

- [ ] **Step 6: Check git status**

Run:

```bash
git status --short
```

Expected:

```text
```

- [ ] **Step 7: Commit verification fixes if any were needed**

If formatter or tests changed files, commit them:

```bash
git add cmd internal
git commit -m "chore: finalize codeworld mvp"
```

If no files changed, skip this commit.

## Plan Self-Review

- Spec coverage: The tasks cover module setup, config, provider-neutral model contracts, DeepSeek provider, conservative permission policy, workspace safety, read tools, mutation tools, shell execution, agent loop, REPL, session persistence, build, and verification.
- Scope check: The plan does not include single-shot mode, multiple providers, plugins, MCP, multi-agent orchestration, IDE integration, web UI, memory summarization, session-wide allowlists, or Codex OAuth as a model credential.
- Type consistency: The plan uses `model.Client`, `model.GenerateRequest`, `model.GenerateResponse`, `tools.Tool`, `tools.Registry`, `permissions.Policy`, `permissions.Request`, and `workspace.Workspace` consistently across packages.
- Verification: Each package gets focused tests before implementation, followed by `go test ./...`, `go build ./cmd/codeworld`, and a non-API REPL smoke check.
