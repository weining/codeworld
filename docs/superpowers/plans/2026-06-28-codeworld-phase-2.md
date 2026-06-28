# Codeworld Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `codeworld run`, session shell approvals, optional TUI, multiple providers, workspace indexing, conversation summarization, and plugin-ready tool registration while preserving the current REPL.

**Architecture:** Extract app construction from `cmd/codeworld` into `internal/app` so REPL, run mode, index mode, and TUI share one runtime. Keep provider-specific HTTP mapping in provider packages and keep tool/plugin/context features behind small interfaces that the agent loop can reuse.

**Tech Stack:** Go standard library for core runtime, existing DeepSeek client, OpenAI-compatible HTTP client for OpenAI/local providers, Anthropic Messages API mapper, JSON session/index/plugin files, optional Bubble Tea/Lip Gloss for the full-screen TUI.

---

## File Structure

Create:

- `internal/app/runtime.go`: shared runtime construction and command-facing methods.
- `internal/app/runtime_test.go`: runtime wiring tests with fake clients.
- `internal/run/run.go`: single-shot command behavior.
- `internal/run/run_test.go`: run mode tests.
- `internal/approvals/approvals.go`: exact shell command normalization and matching.
- `internal/approvals/approvals_test.go`: approval matching tests.
- `internal/model/provider/factory.go`: provider selection and construction.
- `internal/model/provider/factory_test.go`: provider factory tests.
- `internal/model/openai/client.go`: OpenAI-compatible chat-completions client.
- `internal/model/openai/client_test.go`: OpenAI-compatible request/response tests.
- `internal/model/anthropic/client.go`: Anthropic Messages API client.
- `internal/model/anthropic/client_test.go`: Anthropic mapping tests.
- `internal/context/indexer/indexer.go`: workspace index build/load/summary.
- `internal/context/indexer/indexer_test.go`: indexer tests.
- `internal/context/summarizer/summarizer.go`: session summarization logic.
- `internal/context/summarizer/summarizer_test.go`: summarizer tests.
- `internal/plugin/manifest.go`: plugin manifest parsing and validation.
- `internal/plugin/manifest_test.go`: plugin manifest tests.
- `internal/tools/plugin_tool.go`: plugin shell-backed tool implementation.
- `internal/tools/plugin_tool_test.go`: plugin argument template rendering and permission tests.
- `internal/tui/tui.go`: optional Bubble Tea app entry.
- `internal/tui/tui_test.go`: TUI state tests.

Modify:

- `cmd/codeworld/main.go`: command dispatch for default REPL, `run`, `index`, and `tui`.
- `cmd/codeworld/main_test.go`: CLI dispatch tests.
- `internal/config/config.go`: new config fields.
- `internal/config/config_test.go`: config parsing tests.
- `internal/session/session.go`: approvals and summary fields.
- `internal/session/session_test.go`: persistence tests.
- `internal/repl/repl.go`: approval prompt, status output, injected summary/context.
- `internal/repl/repl_test.go`: approval/status tests.
- `internal/agent/agent.go`: summary-aware history support only if needed by runtime.
- `internal/tools/registry.go`: expose tool names or counts if needed by plugin tests.
- `go.mod` and `go.sum`: Bubble Tea/Lip Gloss dependency, only in TUI task.

---

### Task 1: Extract Shared App Runtime

**Files:**
- Create: `internal/app/runtime.go`
- Create: `internal/app/runtime_test.go`
- Modify: `cmd/codeworld/main.go`
- Modify: `cmd/codeworld/main_test.go`

- [ ] **Step 1: Write failing runtime construction tests**

Add `internal/app/runtime_test.go` with tests:

```go
func TestNewRuntimeBuildsREPLDependencies(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In: strings.NewReader("/exit\n"),
		Out: &bytes.Buffer{},
		Err: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if rt.Workspace.Root != root {
		t.Fatalf("workspace root = %q, want %q", rt.Workspace.Root, root)
	}
	if rt.Session.Provider != "deepseek" || rt.Session.Model != "deepseek-v4-pro" {
		t.Fatalf("session provider/model = %s/%s", rt.Session.Provider, rt.Session.Model)
	}
	if rt.Runner.Model == nil || rt.Runner.Tools == nil {
		t.Fatalf("runtime runner not wired: %#v", rt.Runner)
	}
}

func TestNewRuntimeRestoresSessionMessagesAndUsage(t *testing.T) {
	root := t.TempDir()
	store := session.NewStore(root)
	sess := session.New(root, "deepseek", "deepseek-v4-pro")
	sess.Messages = []session.Message{{Role: "user", Content: "old"}}
	sess.Usage = session.Usage{InputTokens: 10, OutputTokens: 2, CacheTokens: 3, TotalTokens: 12}
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	rt, err := NewRuntime(context.Background(), Options{
		Root: root,
		In: &bytes.Buffer{},
		Out: &bytes.Buffer{},
		Err: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if len(rt.Messages) != 1 || rt.Messages[0].Content != "old" {
		t.Fatalf("messages = %#v", rt.Messages)
	}
	if rt.Usage.InputTokens != 10 || rt.Usage.CacheTokens != 3 {
		t.Fatalf("usage = %#v", rt.Usage)
	}
}
```

- [ ] **Step 2: Run the new tests and verify they fail**

Run:

```bash
mise exec -- go test -count=1 ./internal/app
```

Expected: build failure because `internal/app` and `NewRuntime` do not exist.

- [ ] **Step 3: Implement `internal/app.Runtime`**

Create `internal/app/runtime.go` with:

```go
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"codeworld/internal/agent"
	"codeworld/internal/config"
	"codeworld/internal/model"
	"codeworld/internal/model/deepseek"
	"codeworld/internal/permissions"
	"codeworld/internal/repl"
	"codeworld/internal/session"
	"codeworld/internal/tools"
	"codeworld/internal/workspace"
)

type Options struct {
	Root string
	In   io.Reader
	Out  io.Writer
	Err  io.Writer
}

type Runtime struct {
	Config    config.Config
	Workspace workspace.Workspace
	Store     session.Store
	Session   session.Session
	Messages  []model.Message
	Usage     model.Usage
	Runner    agent.Runner
	Diff      func(context.Context) (string, error)
	In        io.Reader
	Out       io.Writer
	Err       io.Writer
}

func NewRuntime(ctx context.Context, opts Options) (Runtime, error) {
	root := opts.Root
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return Runtime{}, err
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		return Runtime{}, err
	}
	ws, err := workspace.New(root)
	if err != nil {
		return Runtime{}, err
	}
	if opts.Err != nil && !ws.IsGitRepo() {
		fmt.Fprintln(opts.Err, "warning: current workspace is not a git repository; patch workflows are safer in git repositories")
	}
	summary, err := ws.Summary(120)
	if err != nil {
		summary = "workspace summary unavailable: " + err.Error()
	}
	systemPrompt := agent.DefaultSystemPrompt + "\n\nWorkspace files:\n" + summary
	store := session.NewStore(ws.Root)
	sess := loadOrCreateSession(store, ws.Root, cfg.Provider, cfg.Model)
	messages := sessionMessagesToModel(sess.Messages)
	modelName := firstNonEmpty(sess.Model, cfg.Model)
	client := deepseek.NewClient(cfg.APIKey, modelName)
	client.SetLogger(deepseek.NewFileJSONLLogger(modelCallLogPath(ws.Root)))
	registry := tools.NewDefaultRegistry(ws)
	diffTool := tools.NewGitDiffTool(ws)
	confirmer := repl.Confirmer{In: opts.In, Out: opts.Out}
	runner := agent.Runner{
		Model:        client,
		Tools:        registry,
		Policy:       permissions.ConservativePolicy{},
		Confirmer:    confirmer,
		MaxSteps:     cfg.MaxSteps,
		ModelName:    modelName,
		SystemPrompt: systemPrompt,
	}
	return Runtime{
		Config: cfg, Workspace: ws, Store: store, Session: sess,
		Messages: messages,
		Usage: model.Usage{
			InputTokens: sess.Usage.InputTokens, OutputTokens: sess.Usage.OutputTokens,
			CacheTokens: sess.Usage.CacheTokens, TotalTokens: sess.Usage.TotalTokens,
		},
		Runner: runner, In: opts.In, Out: opts.Out, Err: opts.Err,
		Diff: func(ctx context.Context) (string, error) {
			result, err := diffTool.Execute(ctx, json.RawMessage(`{}`))
			return result.Content, err
		},
	}, nil
}
```

Move existing helper functions from `cmd/codeworld/main.go` into this package:
`loadOrCreateSession`, `sessionMessagesToModel`, `sessionToolCallsToModel`,
`modelCallLogPath`, and `firstNonEmpty`.

- [ ] **Step 4: Update `cmd/codeworld/main.go` to use runtime**

Change `runWithIO` to call `app.NewRuntime`, then build a `repl.REPL` from the
runtime. Keep `shouldShowTerminalTitle` in `cmd/codeworld` for now.

- [ ] **Step 5: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/app ./cmd/codeworld ./...
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/app cmd/codeworld/main.go cmd/codeworld/main_test.go
git commit -m "refactor: extract app runtime wiring"
```

---

### Task 2: Add Single-Shot `codeworld run`

**Files:**
- Create: `internal/run/run.go`
- Create: `internal/run/run_test.go`
- Modify: `cmd/codeworld/main.go`
- Modify: `cmd/codeworld/main_test.go`
- Modify: `internal/app/runtime.go`
- Test: `internal/run/run_test.go`

- [ ] **Step 1: Write failing run-mode tests**

Add `internal/run/run_test.go`:

```go
func TestOnceRunsTurnPrintsFinalTextAndSavesSession(t *testing.T) {
	store := session.NewStore(t.TempDir())
	rt := app.Runtime{
		Out: &bytes.Buffer{},
		Store: store,
		Session: session.New("workspace", "deepseek", "deepseek-v4-pro"),
		Runner: agent.Runner{
			Model: &fakeModel{responses: []model.GenerateResponse{
				{FinalText: "done", Usage: model.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}},
			}},
			Tools: tools.NewRegistry(nil, nil),
			MaxSteps: 3,
		},
	}
	err := Once(context.Background(), &rt, "inspect")
	if err != nil {
		t.Fatalf("Once returned error: %v", err)
	}
	if !strings.Contains(rt.Out.(*bytes.Buffer).String(), "done\n") {
		t.Fatalf("output = %q, want final text", rt.Out.(*bytes.Buffer).String())
	}
	saved, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if len(saved.Messages) != 2 || saved.Usage.TotalTokens != 5 {
		t.Fatalf("saved session = %#v", saved)
	}
}
```

Add `cmd/codeworld/main_test.go` coverage:

```go
func TestRunCommandRequiresPrompt(t *testing.T) {
	var out, stderr bytes.Buffer
	err := runWithIO(strings.NewReader(""), &out, &stderr, []string{"run"})
	if err == nil || !strings.Contains(err.Error(), "usage: codeworld run") {
		t.Fatalf("err = %v, want run usage", err)
	}
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
mise exec -- go test -count=1 ./internal/run ./cmd/codeworld
```

Expected: build failure because `internal/run` and argv-aware `runWithIO` do not exist.

- [ ] **Step 3: Implement `run.Once`**

Create `internal/run/run.go`:

```go
package run

import (
	"context"
	"fmt"

	"codeworld/internal/app"
	"codeworld/internal/model"
	"codeworld/internal/repl"
)

func Once(ctx context.Context, rt *app.Runtime, input string) error {
	if input == "" {
		return fmt.Errorf("run input is empty")
	}
	rt.Runner.Reporter = repl.Reporter{Out: rt.Out}
	result, err := rt.Runner.RunTurn(ctx, rt.Messages, input)
	if err != nil {
		return err
	}
	rt.Messages = historyMessages(result.Messages)
	rt.Usage = rt.Usage.Add(result.Usage)
	rt.Session.Messages = modelMessagesToSession(rt.Messages)
	rt.Session.Usage = sessionUsage(rt.Usage)
	if err := rt.Store.SaveCurrent(rt.Session); err != nil {
		return err
	}
	_, err = fmt.Fprintln(rt.Out, result.FinalText)
	return err
}
```

If `repl.Reporter`, `modelMessagesToSession`, or `sessionUsage` are not exported
yet, either export the existing helpers from `internal/repl` or place shared
history/session conversion helpers in `internal/app`.

- [ ] **Step 4: Add argv command dispatch**

Change `main()` to call:

```go
if err := runWithIO(os.Stdin, os.Stdout, os.Stderr, os.Args[1:]); err != nil { ... }
```

Change `runWithIO` signature to:

```go
func runWithIO(in io.Reader, out io.Writer, stderr io.Writer, args []string) error
```

Dispatch:

- no args: default REPL;
- `run <text...>`: join args after `run` with spaces and call `run.Once`;
- unknown command: return `unknown command`.

- [ ] **Step 5: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/run ./cmd/codeworld ./...
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/run cmd/codeworld internal/app internal/repl
git commit -m "feat: add single-shot run mode"
```

---

### Task 3: Add Session-Scoped Shell Approvals

**Files:**
- Create: `internal/approvals/approvals.go`
- Create: `internal/approvals/approvals_test.go`
- Modify: `internal/session/session.go`
- Modify: `internal/session/session_test.go`
- Modify: `internal/repl/repl.go`
- Modify: `internal/repl/repl_test.go`
- Modify: `internal/app/runtime.go`

- [ ] **Step 1: Write failing approval tests**

Add `internal/approvals/approvals_test.go`:

```go
func TestNormalizeCommandCollapsesWhitespace(t *testing.T) {
	got := NormalizeCommand("  go   test   ./...  ")
	want := "go test ./..."
	if got != want {
		t.Fatalf("NormalizeCommand = %q, want %q", got, want)
	}
}

func TestSetAllowsExactNormalizedShellCommand(t *testing.T) {
	set := Set{Commands: []string{"go test ./..."}}
	if !set.Allows(" go   test ./... ") {
		t.Fatalf("expected normalized command to be allowed")
	}
	if set.Allows("go test ./internal/...") {
		t.Fatalf("different command should not be allowed")
	}
}
```

Add session persistence test:

```go
sess.Approvals = []session.Approval{{Kind: "shell", Command: "go test ./..."}}
```

Assert it round-trips through `SaveCurrent`/`LoadCurrent`.

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
mise exec -- go test -count=1 ./internal/approvals ./internal/session
```

Expected: build failure for missing package/types.

- [ ] **Step 3: Implement approvals package and session fields**

Create:

```go
package approvals

import "strings"

type Set struct {
	Commands []string
}

func NormalizeCommand(command string) string {
	return strings.Join(strings.Fields(command), " ")
}

func (s Set) Allows(command string) bool {
	normalized := NormalizeCommand(command)
	for _, item := range s.Commands {
		if NormalizeCommand(item) == normalized {
			return true
		}
	}
	return false
}

func (s *Set) Add(command string) {
	normalized := NormalizeCommand(command)
	if normalized == "" || s.Allows(normalized) {
		return
	}
	s.Commands = append(s.Commands, normalized)
}
```

Add to `internal/session/session.go`:

```go
type Approval struct {
	Kind    string `json:"kind"`
	Command string `json:"command,omitempty"`
}

Approvals []Approval `json:"approvals,omitempty"`
```

- [ ] **Step 4: Extend confirmation behavior**

Add a confirmer wrapper in `internal/repl` that:

- checks session approvals before prompting shell requests;
- on input `a`, appends a shell approval to session and confirms the current request;
- writes the updated session through the REPL save path after the turn.

Prompt text:

```text
Allow? [y/N/a=session]
```

Only show `a=session` for `permissions.ActionShell`.

- [ ] **Step 5: Update status and clear**

`/status` includes:

```text
approvals=<n>
```

`/clear` clears `Session.Approvals`.

- [ ] **Step 6: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/approvals ./internal/session ./internal/repl ./...
```

Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/approvals internal/session internal/repl internal/app
git commit -m "feat: add session shell approvals"
```

---

### Task 4: Add Provider Factory And OpenAI-Compatible Providers

**Files:**
- Create: `internal/model/provider/factory.go`
- Create: `internal/model/provider/factory_test.go`
- Create: `internal/model/openai/client.go`
- Create: `internal/model/openai/client_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/app/runtime.go`

- [ ] **Step 1: Write failing config tests**

Add config cases for:

```toml
provider = "openai"
model = "gpt-4.1"
local_base_url = "http://127.0.0.1:11434/v1"
plugins_enabled = true
summary_max_messages = 40
index_max_file_bytes = 65536
```

Assert fields:

```go
if cfg.LocalBaseURL != "http://127.0.0.1:11434/v1" { ... }
if !cfg.PluginsEnabled { ... }
if cfg.SummaryMaxMessages != 40 { ... }
if cfg.IndexMaxFileBytes != 65536 { ... }
```

- [ ] **Step 2: Write failing provider factory tests**

Add `internal/model/provider/factory_test.go`:

```go
func TestNewClientCreatesDeepSeekByDefault(t *testing.T) {
	client, err := NewClient(Config{
		Provider: "deepseek",
		Model: "deepseek-v4-pro",
		DeepSeekAPIKey: "deepseek-key",
		LogPath: filepath.Join(t.TempDir(), "calls.jsonl"),
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("client nil")
	}
}

func TestNewClientRejectsUnknownProvider(t *testing.T) {
	_, err := NewClient(Config{Provider: "unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("err = %v, want unknown provider", err)
	}
}
```

- [ ] **Step 3: Write failing OpenAI-compatible client tests**

Add `internal/model/openai/client_test.go` using `httptest.Server`:

- request path `/chat/completions`;
- `Authorization: Bearer test-key`;
- request has `model`, `messages`, and `tools`;
- response with assistant text parses `FinalText`;
- response with `tool_calls` parses `model.ToolCall`;
- `usage.prompt_tokens`, `usage.completion_tokens`, `usage.total_tokens`, and
  `usage.prompt_tokens_details.cached_tokens` map to `model.Usage`;
- log entry includes only `body_json`.

- [ ] **Step 4: Run tests and verify failure**

Run:

```bash
mise exec -- go test -count=1 ./internal/config ./internal/model/provider ./internal/model/openai
```

Expected: failures for missing fields/packages.

- [ ] **Step 5: Implement config fields**

Extend `config.Config`:

```go
LocalBaseURL        string
PluginsEnabled      bool
SummaryMaxMessages  int
IndexMaxFileBytes   int
OpenAIAPIKey        string
AnthropicAPIKey     string
```

Defaults:

```go
SummaryMaxMessages: 40
IndexMaxFileBytes:  256 * 1024
```

Environment:

```go
cfg.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
cfg.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
cfg.APIKey = os.Getenv("DEEPSEEK_API_KEY")
if cfg.LocalBaseURL == "" {
	cfg.LocalBaseURL = os.Getenv("CODEWORLD_LOCAL_BASE_URL")
}
```

Parse bool with `strconv.ParseBool`.

- [ ] **Step 6: Implement OpenAI-compatible client**

Implement `internal/model/openai.Client` with constructor:

```go
func NewClient(apiKey, modelName, baseURL string) *Client
```

Use the DeepSeek mapper pattern, but make:

```go
baseURL default = "https://api.openai.com/v1"
provider label = "openai"
```

Support local by passing local base URL and empty API key.

- [ ] **Step 7: Implement provider factory**

Create:

```go
type Config struct {
	Provider string
	Model string
	DeepSeekAPIKey string
	OpenAIAPIKey string
	AnthropicAPIKey string
	LocalBaseURL string
	LogPath string
}

func NewClient(cfg Config) (model.Client, error)
```

Cases:

- `deepseek`: existing client;
- `openai`: OpenAI client with OpenAI base URL;
- `local`: OpenAI client with `LocalBaseURL`, no required API key;
- `anthropic`: returns clear `anthropic provider not implemented` until Task 5
  adds the concrete Anthropic client.

- [ ] **Step 8: Wire runtime to provider factory**

Replace direct DeepSeek construction in `internal/app/runtime.go` with provider
factory config.

- [ ] **Step 9: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/config ./internal/model/openai ./internal/model/provider ./internal/app ./cmd/codeworld ./...
```

Expected: all tests pass, except Anthropic is not selected unless configured.

- [ ] **Step 10: Commit**

```bash
git add internal/config internal/model/provider internal/model/openai internal/app cmd/codeworld
git commit -m "feat: add provider factory and openai-compatible client"
```

---

### Task 5: Add Anthropic Provider

**Files:**
- Create: `internal/model/anthropic/client.go`
- Create: `internal/model/anthropic/client_test.go`
- Modify: `internal/model/provider/factory.go`
- Modify: `internal/model/provider/factory_test.go`

- [ ] **Step 1: Write failing Anthropic mapper tests**

Use `httptest.Server` and assert:

- request path `/v1/messages`;
- headers include `x-api-key`, `anthropic-version`, and JSON content type;
- system prompt is sent separately from non-system messages;
- tool definitions are mapped into Anthropic `tools`;
- text response maps to `FinalText`;
- tool_use response maps to `model.ToolCall`;
- usage input/output maps to `model.Usage`.

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
mise exec -- go test -count=1 ./internal/model/anthropic ./internal/model/provider
```

Expected: missing package/provider implementation.

- [ ] **Step 3: Implement Anthropic client**

Constructor:

```go
func NewClient(apiKey, modelName string) *Client
```

Defaults:

```go
baseURL = "https://api.anthropic.com"
version = "2023-06-01"
```

Message mapping:

- collect all `RoleSystem` content into request `system`;
- user/assistant/tool messages map to Anthropic content blocks;
- assistant tool calls map to `tool_use` blocks;
- tool results map to user `tool_result` blocks.

Response mapping:

- concatenate text blocks for final text;
- map `tool_use` blocks to `model.ToolCall`.

Logging:

- compact `body_json` only, matching DeepSeek/OpenAI logs.

- [ ] **Step 4: Wire provider factory**

`provider.NewClient` case `anthropic` creates `anthropic.NewClient`.

Missing `ANTHROPIC_API_KEY` returns:

```text
ANTHROPIC_API_KEY is not set
```

- [ ] **Step 5: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/model/anthropic ./internal/model/provider ./...
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/model/anthropic internal/model/provider
git commit -m "feat: add anthropic provider"
```

---

### Task 6: Add Workspace Index And `codeworld index`

**Files:**
- Create: `internal/context/indexer/indexer.go`
- Create: `internal/context/indexer/indexer_test.go`
- Modify: `internal/app/runtime.go`
- Modify: `cmd/codeworld/main.go`
- Modify: `cmd/codeworld/main_test.go`
- Modify: `internal/tools/registry.go`

- [ ] **Step 1: Write failing indexer tests**

Tests:

```go
func TestBuildIndexSkipsGitCodeworldLogsAndLargeFiles(t *testing.T)
func TestSaveLoadAndSummaryAreDeterministic(t *testing.T)
func TestLanguageFromExtension(t *testing.T)
```

Expected index entry:

```go
type Entry struct {
	Path string `json:"path"`
	Size int64 `json:"size"`
	ModTime time.Time `json:"mod_time"`
	Ext string `json:"ext,omitempty"`
	Language string `json:"language,omitempty"`
	Skipped bool `json:"skipped,omitempty"`
	Reason string `json:"reason,omitempty"`
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
mise exec -- go test -count=1 ./internal/context/indexer
```

Expected: missing package.

- [ ] **Step 3: Implement indexer**

Functions:

```go
func Build(root string, maxFileBytes int64) (Index, error)
func Save(path string, idx Index) error
func Load(path string) (Index, error)
func Summary(idx Index, limit int) string
func DefaultPath(root string) string
```

Skip:

- `.git`;
- `.codeworld/logs`;
- files over `maxFileBytes`;
- binary-looking files by scanning first 8 KiB for NUL bytes.

- [ ] **Step 4: Add command dispatch**

`codeworld index`:

- builds index using runtime config;
- saves `.codeworld/index.json`;
- prints `indexed files=<n> skipped=<n>`.

- [ ] **Step 5: Inject index summary into runtime prompt**

If `.codeworld/index.json` exists, append:

```text
Workspace index:
<summary>
```

to system prompt after workspace files.

- [ ] **Step 6: Add `index_workspace` tool**

Add a read-risk tool that refreshes `.codeworld/index.json` and returns summary.

- [ ] **Step 7: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/context/indexer ./internal/app ./cmd/codeworld ./internal/tools ./...
```

Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/context/indexer internal/app cmd/codeworld internal/tools
git commit -m "feat: add workspace indexing"
```

---

### Task 7: Add Conversation Summarization

**Files:**
- Create: `internal/context/summarizer/summarizer.go`
- Create: `internal/context/summarizer/summarizer_test.go`
- Modify: `internal/session/session.go`
- Modify: `internal/session/session_test.go`
- Modify: `internal/app/runtime.go`
- Modify: `internal/repl/repl.go`
- Modify: `internal/run/run.go`

- [ ] **Step 1: Write failing summarizer tests**

Tests:

```go
func TestShouldSummarizeWhenMessageCountExceedsThreshold(t *testing.T)
func TestSummarizeKeepsRecentMessagesAndStoresSummary(t *testing.T)
func TestSummarizeFailurePreservesMessages(t *testing.T)
```

Use a fake `model.Client` that returns `FinalText: "summary text"`.

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
mise exec -- go test -count=1 ./internal/context/summarizer ./internal/session
```

Expected: missing package/session summary field.

- [ ] **Step 3: Add session summary field**

Add:

```go
Summary string `json:"summary,omitempty"`
```

to `session.Session` and tests.

- [ ] **Step 4: Implement summarizer**

API:

```go
type Options struct {
	MaxMessages int
	KeepRecent int
}

func ShouldSummarize(messages []model.Message, usage model.Usage, opts Options) bool
func Summarize(ctx context.Context, client model.Client, summary string, messages []model.Message, opts Options) (string, []model.Message, error)
```

Prompt:

```text
Summarize the older conversation for a coding agent. Preserve user goals,
files changed, commands run, decisions, and unresolved tasks.
```

Keep the last `KeepRecent` messages verbatim.

- [ ] **Step 5: Inject summary into system prompt**

Runtime system prompt includes:

```text
Conversation summary:
<session summary>
```

when non-empty.

- [ ] **Step 6: Trigger summarization after completed turns**

After REPL/run turn saves messages, if `ShouldSummarize` is true:

- call summarizer;
- update session summary;
- update messages;
- save session again.

On summarizer error, print warning to stderr/event stream and keep original messages.

- [ ] **Step 7: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/context/summarizer ./internal/session ./internal/app ./internal/repl ./internal/run ./...
```

Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/context/summarizer internal/session internal/app internal/repl internal/run
git commit -m "feat: add conversation summarization"
```

---

### Task 8: Add Plugin Manifest Loading And Plugin Tools

**Files:**
- Create: `internal/plugin/manifest.go`
- Create: `internal/plugin/manifest_test.go`
- Create: `internal/tools/plugin_tool.go`
- Create: `internal/tools/plugin_tool_test.go`
- Modify: `internal/app/runtime.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write failing manifest tests**

Tests:

```go
func TestLoadManifestsRequiresNamespacedToolNames(t *testing.T)
func TestLoadManifestsRejectsTraversalPluginName(t *testing.T)
func TestLoadManifestsReturnsToolsWhenEnabled(t *testing.T)
```

Use path `.codeworld/plugins/example/plugin.json`.

- [ ] **Step 2: Write failing plugin tool tests**

Tests:

```go
func TestPluginToolRendersJSONFieldTemplates(t *testing.T)
func TestPluginToolRejectsMissingTemplateValue(t *testing.T)
func TestPluginToolPermissionUsesConfiguredRisk(t *testing.T)
```

For argument template rendering:

```json
{"text":"hello"}
```

with args `["{{text}}"]` renders `["hello"]`.

- [ ] **Step 3: Run tests and verify failure**

Run:

```bash
mise exec -- go test -count=1 ./internal/plugin ./internal/tools
```

Expected: missing plugin package/tool.

- [ ] **Step 4: Implement manifest parsing**

Types:

```go
type Manifest struct {
	Name string `json:"name"`
	Tools []Tool `json:"tools"`
}

type Tool struct {
	Name string `json:"name"`
	Description string `json:"description"`
	Command string `json:"command"`
	Args []string `json:"args"`
	InputSchema map[string]any `json:"input_schema"`
	Risk string `json:"risk"`
}
```

Validation:

- manifest name must match directory name;
- tool name must start with `name + "."`;
- command must be non-empty;
- risk must be `read`, `write`, or `execute`.

- [ ] **Step 5: Implement plugin shell tool**

`tools.PluginTool` implements `tools.Tool`.

Execution:

- render command/args;
- execute through existing process helper or shell tool command path;
- return output as `tools.Result`.

Permission:

- `read` maps to `permissions.ActionRead`;
- `write` maps to `permissions.ActionWrite`;
- `execute` maps to `permissions.ActionShell`.

- [ ] **Step 6: Wire runtime**

If `cfg.PluginsEnabled` is true:

- load manifests;
- convert manifest tools into `tools.PluginTool`;
- pass them as `extra` to `tools.NewRegistry`.

If disabled, do not parse plugin manifests.

- [ ] **Step 7: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/plugin ./internal/tools ./internal/app ./...
```

Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/plugin internal/tools internal/app internal/config
git commit -m "feat: add plugin tool manifests"
```

---

### Task 9: Add Optional Bubble Tea TUI

**Files:**
- Create: `internal/tui/tui.go`
- Create: `internal/tui/tui_test.go`
- Modify: `cmd/codeworld/main.go`
- Modify: `cmd/codeworld/main_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add dependencies**

Run:

```bash
mise exec -- go get github.com/charmbracelet/bubbletea github.com/charmbracelet/lipgloss
```

Expected: `go.mod` and `go.sum` updated.

If network is blocked, rerun with escalation.

- [ ] **Step 2: Write failing TUI model tests**

Add `internal/tui/tui_test.go`:

```go
func TestModelRendersStatusConversationAndInput(t *testing.T) {
	m := NewModel(RuntimeView{
		Provider: "deepseek",
		Model: "deepseek-v4-pro",
		Workspace: "workspace",
		Usage: model.Usage{InputTokens: 1, OutputTokens: 2, CacheTokens: 3, TotalTokens: 3},
		Approvals: 1,
	})
	view := m.View()
	for _, want := range []string{"deepseek", "deepseek-v4-pro", "input=1", "workspace"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}
```

- [ ] **Step 3: Run tests and verify failure**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui ./cmd/codeworld
```

Expected: missing TUI package or dependency until implementation.

- [ ] **Step 4: Implement TUI state model**

Create:

```go
type RuntimeView struct {
	Provider string
	Model string
	Workspace string
	Usage model.Usage
	Approvals int
}

type Model struct {
	view RuntimeView
	width int
	height int
	input string
	conversation []string
	events []string
}
```

Implement Bubble Tea `Init`, `Update`, and `View`.

- [ ] **Step 5: Implement TUI runner**

Function:

```go
func Run(ctx context.Context, rt *app.Runtime) error
```

It should:

- check stdin/stdout terminal status in command layer;
- start Bubble Tea program;
- submit input to `agent.RunTurn`;
- append tool events to event stream;
- save session after turns.

- [ ] **Step 6: Wire `codeworld tui`**

`cmd/codeworld` dispatch:

- `tui` calls `tui.Run`;
- if stdout is not a terminal, return `codeworld tui requires a terminal`.

- [ ] **Step 7: Run tests and build**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui ./cmd/codeworld ./...
mise exec -- go build ./cmd/codeworld
```

Expected: all tests pass and build succeeds.

- [ ] **Step 8: Commit**

```bash
git add internal/tui cmd/codeworld go.mod go.sum
git commit -m "feat: add optional tui"
```

---

## Final Verification

After all tasks:

- [ ] Run:

```bash
mise exec -- go test -count=1 ./...
```

- [ ] Run:

```bash
mise exec -- go build -o /private/tmp/codeworld-build ./cmd/codeworld
```

- [ ] Run smoke commands with fake/no-provider-safe paths:

```bash
mise exec -- go run ./cmd/codeworld /help
mise exec -- go run ./cmd/codeworld index
```

- [ ] Check git status:

```bash
git status --short
```

Expected: only intentional untracked local state such as `.codeworld/` or `.idea/`.

## Self-Review

- Spec coverage: Tasks cover single-shot run, session shell approvals, optional
  TUI, OpenAI/Anthropic/local providers, workspace index, summarization, and
  plugin-ready tool registration.
- Scope exclusions: Codex delegate, full MCP, IDE/web UI, and automatic write
  approvals are not included.
- Type consistency: `model.Client`, `model.Usage`, `session.Session`,
  `agent.Runner`, and existing tool interfaces remain the central contracts.
