# Codex-Style TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current status-line `codeworld tui` wrapper with a Bubble Tea full-screen terminal UI that is materially closer to Codex CLI.

**Architecture:** Keep `app.Runtime` as the composition root and build a new `internal/tui` Bubble Tea model around it. The TUI owns rendering, input, slash commands, permissions, and transcript state; `agent.Runner` remains the execution engine through a small adapter.

**Tech Stack:** Go 1.26.4, Bubble Tea, Bubbles viewport/textarea, Lip Gloss, existing `agent`, `app`, `model`, `permissions`, `session`, and `tools` packages.

---

## File Structure

- Modify `go.mod` / `go.sum`: add Charm dependencies.
- Replace `internal/tui/tui.go`: public `Run(ctx, *app.Runtime)` entry point and test-friendly options.
- Create `internal/tui/model.go`: Bubble Tea model, update loop, rendering, key handling.
- Create `internal/tui/types.go`: transcript, status, command message, permission decision types.
- Create `internal/tui/adapter.go`: run one agent turn, update runtime/session/usage, report final result.
- Create `internal/tui/permissions.go`: TUI confirmer bridge for `agent.Confirmer`.
- Create `internal/tui/commands.go`: slash command handling for TUI.
- Modify `cmd/codeworld/main.go`: keep `codeworld tui` dispatch, optionally route default interactive mode later.
- Modify `cmd/codeworld/main_test.go`: assert `tui` command dispatches without requiring a real terminal.
- Replace `internal/tui/tui_test.go`: unit tests for status rendering, model update behavior, slash commands, reporter, and permission decisions.

---

### Task 1: Add TUI Dependencies And Test-Friendly Entry Point

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/tui/tui.go`
- Test: `internal/tui/tui_test.go`

- [ ] **Step 1: Write the failing test**

Replace `internal/tui/tui_test.go` with:

```go
package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"codeworld/internal/app"
	"codeworld/internal/model"
	"codeworld/internal/session"
	"codeworld/internal/workspace"
)

func TestStatusLineShowsTokenBreakdown(t *testing.T) {
	line := StatusLine(Status{
		Workspace: "repo",
		Provider:  "deepseek",
		Model:     "deepseek-v4-pro",
		Messages:  5,
		Approvals: 2,
		Usage:     model.Usage{InputTokens: 10, OutputTokens: 3, CacheTokens: 4, TotalTokens: 13},
	})

	for _, want := range []string{"workspace=repo", "provider=deepseek", "model=deepseek-v4-pro", "messages=5", "approvals=2", "input=10", "output=3", "cache=4", "total=13"} {
		if !strings.Contains(line, want) {
			t.Fatalf("status line missing %q in %q", want, line)
		}
	}
}

func TestRunReturnsWithoutTerminalRendererInTestMode(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
		Usage:     model.Usage{InputTokens: 1, OutputTokens: 2, CacheTokens: 3, TotalTokens: 4},
		In:        strings.NewReader("/exit\n"),
		Out:       &out,
	}

	err := RunWithOptions(context.Background(), &rt, Options{TestMode: true})
	if err != nil {
		t.Fatalf("RunWithOptions returned error: %v", err)
	}
	if !strings.Contains(out.String(), "deepseek-v4-pro") {
		t.Fatalf("output = %q, want model status", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: FAIL because `Status`, `Options`, and `RunWithOptions` are not defined.

- [ ] **Step 3: Add dependencies**

Run:

```bash
mise exec -- go get github.com/charmbracelet/bubbletea github.com/charmbracelet/bubbles github.com/charmbracelet/lipgloss
```

If this fails with a network or sandbox error, rerun the same command with escalation approval.

- [ ] **Step 4: Write minimal implementation**

Replace `internal/tui/tui.go` with:

```go
package tui

import (
	"context"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"codeworld/internal/app"
	"codeworld/internal/model"
)

type Options struct {
	TestMode bool
}

type Status struct {
	Workspace string
	Provider  string
	Model     string
	Messages  int
	Approvals int
	Usage     model.Usage
	Running   bool
}

func RuntimeStatus(rt *app.Runtime) Status {
	return Status{
		Workspace: rt.Workspace.Root,
		Provider:  rt.Session.Provider,
		Model:     rt.Session.Model,
		Messages:  len(rt.Messages),
		Approvals: len(rt.Session.Approvals),
		Usage:     rt.Usage,
	}
}

func StatusLine(status Status) string {
	running := ""
	if status.Running {
		running = " running"
	}
	return fmt.Sprintf("workspace=%s provider=%s model=%s messages=%d approvals=%d tokens input=%d output=%d cache=%d total=%d%s",
		status.Workspace,
		status.Provider,
		status.Model,
		status.Messages,
		status.Approvals,
		status.Usage.InputTokens,
		status.Usage.OutputTokens,
		status.Usage.CacheTokens,
		status.Usage.TotalTokens,
		running,
	)
}

func Run(ctx context.Context, rt *app.Runtime) error {
	return RunWithOptions(ctx, rt, Options{})
}

func RunWithOptions(ctx context.Context, rt *app.Runtime, opts Options) error {
	m := NewModel(rt)
	programOpts := []tea.ProgramOption{tea.WithContext(ctx)}
	if opts.TestMode {
		programOpts = append(programOpts, tea.WithoutRenderer())
		if input, ok := rt.In.(io.Reader); ok {
			programOpts = append(programOpts, tea.WithInput(input))
		}
		if output, ok := rt.Out.(io.Writer); ok {
			programOpts = append(programOpts, tea.WithOutput(output))
		}
	} else {
		programOpts = append(programOpts, tea.WithAltScreen())
	}
	_, err := tea.NewProgram(m, programOpts...).Run()
	return err
}
```

- [ ] **Step 5: Run test to verify it still fails for missing model**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: FAIL because `NewModel` is not defined.

- [ ] **Step 6: Commit**

Do not commit yet. Task 2 provides the minimal model needed for the tests to pass.

---

### Task 2: Build Static Full-Screen Model And View

**Files:**
- Create: `internal/tui/types.go`
- Create: `internal/tui/model.go`
- Modify: `internal/tui/tui.go`
- Test: `internal/tui/tui_test.go`

- [ ] **Step 1: Extend failing tests**

Add this test to `internal/tui/tui_test.go`:

```go
func TestModelViewContainsTranscriptStatusAndComposer(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
		Usage:     model.Usage{InputTokens: 10, OutputTokens: 3, CacheTokens: 4, TotalTokens: 13},
	}
	m := NewModel(&rt)
	m.width = 100
	m.height = 24
	m.items = append(m.items, TranscriptItem{Kind: ItemAssistant, Text: "ready"})

	view := m.View()
	for _, want := range []string{"codeworld", "deepseek-v4-pro", "input=10", "ready", ">"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q in:\n%s", want, view)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: FAIL because `TranscriptItem`, `ItemAssistant`, or `NewModel` is missing.

- [ ] **Step 3: Implement model types**

Create `internal/tui/types.go`:

```go
package tui

type ItemKind string

const (
	ItemUser       ItemKind = "user"
	ItemAssistant  ItemKind = "assistant"
	ItemTool       ItemKind = "tool"
	ItemPermission ItemKind = "permission"
	ItemCommand    ItemKind = "command"
	ItemError      ItemKind = "error"
	ItemNotice     ItemKind = "notice"
)

type TranscriptItem struct {
	Kind ItemKind
	Text string
}

type turnDoneMsg struct {
	text string
	err  error
}
```

- [ ] **Step 4: Implement static Bubble Tea model**

Create `internal/tui/model.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"codeworld/internal/app"
)

type Model struct {
	rt       *app.Runtime
	input    textarea.Model
	viewport viewport.Model
	items    []TranscriptItem
	width    int
	height   int
	running  bool
	quitting bool
}

func NewModel(rt *app.Runtime) Model {
	input := textarea.New()
	input.Placeholder = "Ask codeworld..."
	input.Prompt = "> "
	input.SetHeight(3)
	input.Focus()
	vp := viewport.New(80, 20)
	m := Model{rt: rt, input: input, viewport: vp, width: 80, height: 24}
	m.refreshViewport()
	return m
}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = max(3, msg.Height-7)
		m.input.SetWidth(msg.Width)
		m.refreshViewport()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "/exit" {
				m.quitting = true
				return m, tea.Quit
			}
			if text != "" {
				m.items = append(m.items, TranscriptItem{Kind: ItemUser, Text: text})
				m.input.Reset()
				m.refreshViewport()
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	status := RuntimeStatus(m.rt)
	status.Running = m.running
	header := lipgloss.NewStyle().Bold(true).Render("codeworld") + "  " + StatusLine(status)
	body := m.viewport.View()
	composer := m.input.View()
	return fmt.Sprintf("%s\n%s\n%s", header, body, composer)
}

func (m *Model) refreshViewport() {
	lines := make([]string, 0, len(m.items)*2)
	for _, item := range m.items {
		lines = append(lines, string(item.Kind))
		lines = append(lines, "  "+item.Text)
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.GotoBottom()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

- [ ] **Step 5: Run test to verify it passes**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/tui/tui.go internal/tui/types.go internal/tui/model.go internal/tui/tui_test.go
git commit -m "feat: add bubble tea tui shell"
```

---

### Task 3: Add Runner Adapter And Turn Execution

**Files:**
- Create: `internal/tui/adapter.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/types.go`
- Test: `internal/tui/tui_test.go`

- [ ] **Step 1: Write failing test**

Add a fake model and test to `internal/tui/tui_test.go`:

```go
type fakeModelClient struct {
	resp model.GenerateResponse
}

func (f fakeModelClient) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	return f.resp, nil
}

func TestRunnerAdapterUpdatesRuntimeUsageAndMessages(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
		Runner: agent.Runner{
			Model:     fakeModelClient{resp: model.GenerateResponse{FinalText: "done", Usage: model.Usage{InputTokens: 3, OutputTokens: 2, CacheTokens: 1, TotalTokens: 5}}},
			Tools:     tools.NewRegistry(nil, nil),
			Policy:    permissions.ConservativePolicy{},
			MaxSteps:  1,
			ModelName: "deepseek-v4-pro",
		},
	}

	adapter := NewRunnerAdapter(&rt)
	result := adapter.RunTurn(context.Background(), "hello")
	if result.err != nil {
		t.Fatalf("RunTurn error: %v", result.err)
	}
	if result.text != "done" {
		t.Fatalf("text = %q, want done", result.text)
	}
	if rt.Usage.InputTokens != 3 || rt.Usage.OutputTokens != 2 || rt.Usage.CacheTokens != 1 || rt.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v, want accumulated usage", rt.Usage)
	}
	if len(rt.Messages) == 0 {
		t.Fatalf("runtime messages were not updated")
	}
}
```

Also add imports:

```go
	"codeworld/internal/agent"
	"codeworld/internal/permissions"
	"codeworld/internal/tools"
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: FAIL because `NewRunnerAdapter` is undefined.

- [ ] **Step 3: Implement runner adapter**

Create `internal/tui/adapter.go`:

```go
package tui

import (
	"context"

	"codeworld/internal/app"
)

type RunnerAdapter struct {
	rt *app.Runtime
}

func NewRunnerAdapter(rt *app.Runtime) RunnerAdapter {
	return RunnerAdapter{rt: rt}
}

func (a RunnerAdapter) RunTurn(ctx context.Context, input string) turnDoneMsg {
	result, err := a.rt.Runner.RunTurn(ctx, a.rt.Messages, input)
	if err != nil {
		return turnDoneMsg{err: err}
	}
	if err := a.rt.SaveTurn(result); err != nil {
		return turnDoneMsg{err: err}
	}
	if err := a.rt.MaybeSummarize(ctx); err != nil {
		return turnDoneMsg{text: result.FinalText, err: err}
	}
	return turnDoneMsg{text: result.FinalText}
}
```

- [ ] **Step 4: Wire model Enter key to async turn command**

Modify `internal/tui/model.go`:

```go
// In Model struct:
adapter RunnerAdapter

// In NewModel:
m := Model{rt: rt, adapter: NewRunnerAdapter(rt), input: input, viewport: vp, width: 80, height: 24}

// In Update enter branch after appending user item:
m.running = true
prompt := text
return m, func() tea.Msg {
	return m.adapter.RunTurn(context.Background(), prompt)
}

// Add turnDoneMsg branch:
case turnDoneMsg:
	m.running = false
	if msg.err != nil {
		m.items = append(m.items, TranscriptItem{Kind: ItemError, Text: msg.err.Error()})
	} else {
		m.items = append(m.items, TranscriptItem{Kind: ItemAssistant, Text: msg.text})
	}
	m.refreshViewport()
	return m, nil
```

When applying this step, import `context` in `internal/tui/model.go`.

- [ ] **Step 5: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/adapter.go internal/tui/model.go internal/tui/types.go internal/tui/tui_test.go
git commit -m "feat: run agent turns from tui"
```

---

### Task 4: Add Tool Reporter Transcript Events

**Files:**
- Modify: `internal/tui/adapter.go`
- Modify: `internal/tui/types.go`
- Modify: `internal/tui/model.go`
- Test: `internal/tui/tui_test.go`

- [ ] **Step 1: Write failing test**

Add this test to `internal/tui/tui_test.go`:

```go
func TestToolReporterAppendsToolEvents(t *testing.T) {
	reporter := NewToolReporter()
	reporter.ReportTool(context.Background(), agent.ToolEvent{
		Status: agent.ToolEventStart,
		Name:   "shell",
		Request: permissions.Request{
			Target: "mise exec -- go test ./...",
			Risk:   permissions.RiskExecute,
		},
	})

	item := <-reporter.Events()
	if item.Kind != ItemTool || !strings.Contains(item.Text, "shell start") || !strings.Contains(item.Text, "go test") {
		t.Fatalf("item = %#v, want tool start transcript item", item)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: FAIL because `NewToolReporter` is undefined.

- [ ] **Step 3: Implement reporter**

Add to `internal/tui/adapter.go`:

```go
import (
	"fmt"
	// existing imports
	"codeworld/internal/agent"
)

type ToolReporter struct {
	events chan TranscriptItem
}

func NewToolReporter() *ToolReporter {
	return &ToolReporter{events: make(chan TranscriptItem, 32)}
}

func (r *ToolReporter) Events() <-chan TranscriptItem {
	return r.events
}

func (r *ToolReporter) ReportTool(ctx context.Context, event agent.ToolEvent) {
	text := fmt.Sprintf("%s %s target=%s risk=%s", event.Name, event.Status, event.Request.Target, event.Request.Risk)
	if event.Error != "" {
		text += " error=" + event.Error
	}
	select {
	case r.events <- TranscriptItem{Kind: ItemTool, Text: text}:
	case <-ctx.Done():
	}
}
```

- [ ] **Step 4: Add polling command to model**

Add to `internal/tui/types.go`:

```go
type toolEventMsg struct {
	item TranscriptItem
}
```

Modify `Model` in `internal/tui/model.go`:

```go
toolEvents <-chan TranscriptItem

// In NewModel:
reporter := NewToolReporter()
rt.Runner.Reporter = reporter
m := Model{rt: rt, adapter: NewRunnerAdapter(rt), toolEvents: reporter.Events(), input: input, viewport: vp, width: 80, height: 24}

// Init:
return tea.Batch(textarea.Blink, m.waitToolEvent())

// Update:
case toolEventMsg:
	m.items = append(m.items, msg.item)
	m.refreshViewport()
	return m, m.waitToolEvent()

// Method:
func (m Model) waitToolEvent() tea.Cmd {
	return func() tea.Msg {
		item, ok := <-m.toolEvents
		if !ok {
			return nil
		}
		return toolEventMsg{item: item}
	}
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/adapter.go internal/tui/model.go internal/tui/types.go internal/tui/tui_test.go
git commit -m "feat: show tui tool events"
```

---

### Task 5: Add Permission Confirmation Panel

**Files:**
- Create: `internal/tui/permissions.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/types.go`
- Test: `internal/tui/tui_test.go`

- [ ] **Step 1: Write failing test**

Add this test to `internal/tui/tui_test.go`:

```go
func TestTUIConfirmerAllowsSessionApproval(t *testing.T) {
	confirmer := NewTUIConfirmer()
	done := make(chan bool, 1)
	go func() {
		allowed, err := confirmer.Confirm(context.Background(), permissions.Request{
			Action: permissions.ActionShell,
			Target: "mise exec -- go test ./...",
			Risk:   permissions.RiskExecute,
			Reason: "shell command",
		}, permissions.Decision{Kind: permissions.DecisionAsk})
		if err != nil {
			t.Errorf("Confirm returned error: %v", err)
		}
		done <- allowed
	}()

	req := <-confirmer.Requests()
	if req.Target != "mise exec -- go test ./..." {
		t.Fatalf("request = %#v, want shell target", req)
	}
	confirmer.Decide(PermissionDecision{Allow: true, Session: true})
	if !<-done {
		t.Fatalf("confirm denied, want allowed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: FAIL because `NewTUIConfirmer` and `PermissionDecision` are undefined.

- [ ] **Step 3: Implement confirmer**

Create `internal/tui/permissions.go`:

```go
package tui

import (
	"context"
	"fmt"

	"codeworld/internal/permissions"
)

type PermissionDecision struct {
	Allow   bool
	Session bool
}

type TUIConfirmer struct {
	requests  chan permissions.Request
	decisions chan PermissionDecision
}

func NewTUIConfirmer() *TUIConfirmer {
	return &TUIConfirmer{
		requests:  make(chan permissions.Request, 1),
		decisions: make(chan PermissionDecision, 1),
	}
}

func (c *TUIConfirmer) Requests() <-chan permissions.Request {
	return c.requests
}

func (c *TUIConfirmer) Decide(decision PermissionDecision) {
	c.decisions <- decision
}

func (c *TUIConfirmer) Confirm(ctx context.Context, req permissions.Request, decision permissions.Decision) (bool, error) {
	select {
	case c.requests <- req:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	select {
	case result := <-c.decisions:
		return result.Allow, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func FormatPermissionRequest(req permissions.Request) string {
	text := fmt.Sprintf("permission required risk=%s\ntarget: %s\nreason: %s", req.Risk, req.Target, req.Reason)
	if req.Preview != "" {
		text += "\npreview:\n" + req.Preview
	}
	if req.Action == permissions.ActionShell {
		text += "\n[y] allow once   [n] deny   [a] allow similar shell command this session"
	} else {
		text += "\n[y] allow once   [n] deny"
	}
	return text
}
```

- [ ] **Step 4: Wire permission requests into model**

Add to `internal/tui/types.go`:

```go
type permissionRequestMsg struct {
	request permissions.Request
}
```

Import `codeworld/internal/permissions` in `types.go`.

Modify `Model` in `internal/tui/model.go`:

```go
confirmer *TUIConfirmer
pendingPermission *permissions.Request

// In NewModel:
confirmer := NewTUIConfirmer()
rt.Runner.Confirmer = confirmer
m := Model{rt: rt, adapter: NewRunnerAdapter(rt), toolEvents: reporter.Events(), confirmer: confirmer, input: input, viewport: vp, width: 80, height: 24}

// Init batch:
m.waitPermissionRequest()

// Update:
case permissionRequestMsg:
	m.pendingPermission = &msg.request
	m.items = append(m.items, TranscriptItem{Kind: ItemPermission, Text: FormatPermissionRequest(msg.request)})
	m.refreshViewport()
	return m, m.waitPermissionRequest()

// In tea.KeyMsg before normal input:
if m.pendingPermission != nil {
	switch msg.String() {
	case "y":
		m.confirmer.Decide(PermissionDecision{Allow: true})
		m.items = append(m.items, TranscriptItem{Kind: ItemPermission, Text: "allowed once"})
		m.pendingPermission = nil
	case "a":
		m.confirmer.Decide(PermissionDecision{Allow: true, Session: true})
		m.items = append(m.items, TranscriptItem{Kind: ItemPermission, Text: "allowed for session"})
		m.pendingPermission = nil
	case "n", "esc":
		m.confirmer.Decide(PermissionDecision{Allow: false})
		m.items = append(m.items, TranscriptItem{Kind: ItemPermission, Text: "denied"})
		m.pendingPermission = nil
	}
	m.refreshViewport()
	return m, nil
}

func (m Model) waitPermissionRequest() tea.Cmd {
	return func() tea.Msg {
		req, ok := <-m.confirmer.Requests()
		if !ok {
			return nil
		}
		return permissionRequestMsg{request: req}
	}
}
```

When applying this step, make sure `types.go` imports `permissions` and `model.go` imports it if needed.

- [ ] **Step 5: Persist session approvals for `a`**

Extend `PermissionDecision` with enough information in `model.go` when `a` is pressed:

```go
if m.pendingPermission.Action == permissions.ActionShell {
	m.rt.Session.Approvals = append(m.rt.Session.Approvals, session.Approval{Command: m.pendingPermission.Target})
}
```

Import `codeworld/internal/session` in `model.go`.

- [ ] **Step 6: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/permissions.go internal/tui/model.go internal/tui/types.go internal/tui/tui_test.go
git commit -m "feat: add tui permission prompts"
```

---

### Task 6: Add Slash Command Handling

**Files:**
- Create: `internal/tui/commands.go`
- Modify: `internal/tui/model.go`
- Test: `internal/tui/tui_test.go`

- [ ] **Step 1: Write failing test**

Add this test to `internal/tui/tui_test.go`:

```go
func TestHandleSlashCommandUpdatesModelAndTranscript(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
		Usage:     model.Usage{InputTokens: 1, OutputTokens: 2, CacheTokens: 3, TotalTokens: 4},
	}
	m := NewModel(&rt)

	next, quit := m.handleSlashCommand(context.Background(), "/model deepseek-v4-flash")
	if quit {
		t.Fatalf("model command requested quit")
	}
	if next.rt.Session.Model != "deepseek-v4-flash" || next.rt.Runner.ModelName != "deepseek-v4-flash" {
		t.Fatalf("model not updated: session=%q runner=%q", next.rt.Session.Model, next.rt.Runner.ModelName)
	}
	if len(next.items) == 0 || !strings.Contains(next.items[len(next.items)-1].Text, "deepseek-v4-flash") {
		t.Fatalf("items = %#v, want model notice", next.items)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: FAIL because `handleSlashCommand` is undefined.

- [ ] **Step 3: Implement slash commands**

Create `internal/tui/commands.go`:

```go
package tui

import (
	"context"
	"fmt"
	"strings"

	"codeworld/internal/model"
	"codeworld/internal/session"
)

func (m Model) handleSlashCommand(ctx context.Context, line string) (Model, bool) {
	switch {
	case line == "/help":
		m.appendNotice("/help /model /status /diff /clear /exit")
	case line == "/model":
		m.appendNotice(m.rt.Session.Model)
	case strings.HasPrefix(line, "/model "):
		next := strings.TrimSpace(strings.TrimPrefix(line, "/model "))
		if next == "" {
			m.appendNotice(m.rt.Session.Model)
			return m, false
		}
		m.rt.Session.Model = next
		m.rt.Runner.ModelName = next
		m.appendNotice("model=" + next)
		_ = m.rt.Store.SaveCurrent(m.rt.Session)
	case line == "/status":
		m.appendNotice(StatusLine(RuntimeStatus(m.rt)))
	case line == "/diff":
		if m.rt.Diff == nil {
			m.appendNotice("diff unavailable")
			return m, false
		}
		diff, err := m.rt.Diff(ctx)
		if err != nil {
			m.appendError("diff error: " + err.Error())
			return m, false
		}
		if diff == "" {
			m.appendNotice("no diff")
			return m, false
		}
		m.items = append(m.items, TranscriptItem{Kind: ItemCommand, Text: diff})
	case line == "/clear":
		m.items = nil
		m.rt.Messages = nil
		m.rt.Session.Messages = nil
		m.rt.Session.Approvals = nil
		m.rt.Usage = model.Usage{}
		m.rt.Session.Usage = session.Usage{}
		_ = m.rt.Store.SaveCurrent(m.rt.Session)
		m.appendNotice("cleared")
	case line == "/exit":
		return m, true
	default:
		m.appendError(fmt.Sprintf("unknown command: %s", line))
	}
	m.refreshViewport()
	return m, false
}

func (m *Model) appendNotice(text string) {
	m.items = append(m.items, TranscriptItem{Kind: ItemNotice, Text: text})
}

func (m *Model) appendError(text string) {
	m.items = append(m.items, TranscriptItem{Kind: ItemError, Text: text})
}
```

- [ ] **Step 4: Route slash commands from Enter key**

Modify `internal/tui/model.go` Enter branch:

```go
if strings.HasPrefix(text, "/") {
	next, quit := m.handleSlashCommand(context.Background(), text)
	if quit {
		next.quitting = true
		return next, tea.Quit
	}
	next.input.Reset()
	return next, nil
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./internal/tui
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/commands.go internal/tui/model.go internal/tui/tui_test.go
git commit -m "feat: add tui slash commands"
```

---

### Task 7: CLI Integration And Regression Tests

**Files:**
- Modify: `cmd/codeworld/main.go`
- Modify: `cmd/codeworld/main_test.go`
- Modify: `internal/tui/tui.go`
- Test: `cmd/codeworld/main_test.go`

- [ ] **Step 1: Write failing CLI test for non-terminal mode**

Update `TestTUICommandStartsStatusShell` in `cmd/codeworld/main_test.go` to assert the new full-screen TUI still starts in test mode:

```go
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
	for _, want := range []string{"codeworld", "provider=deepseek", "model=deepseek-v4-pro", "input=0", "output=0", "cache=0", "total=0"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout missing %q in:\n%s", want, output)
		}
	}
}
```

- [ ] **Step 2: Run test**

Run:

```bash
mise exec -- go test -count=1 ./cmd/codeworld
```

Expected: PASS if Task 1 kept non-terminal test mode behavior; otherwise FAIL and fix `tui.RunWithOptions`.

- [ ] **Step 3: Ensure production `tui.Run` uses alt screen**

Verify `internal/tui/tui.go` uses:

```go
programOpts = append(programOpts, tea.WithAltScreen())
```

only when `Options.TestMode` is false.

- [ ] **Step 4: Run tests**

Run:

```bash
mise exec -- go test -count=1 ./cmd/codeworld ./internal/tui
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/codeworld/main.go cmd/codeworld/main_test.go internal/tui/tui.go
git commit -m "feat: wire codex style tui command"
```

---

### Task 8: Full Verification And Cleanup

**Files:**
- Review: all modified files

- [ ] **Step 1: Format all changed Go files**

Run:

```bash
mise exec -- gofmt -w cmd/codeworld/main.go cmd/codeworld/main_test.go internal/tui/*.go
```

Expected: no output.

- [ ] **Step 2: Run full test suite**

Run:

```bash
mise exec -- go test -count=1 ./...
```

Expected: all packages PASS.

- [ ] **Step 3: Run full build**

Run:

```bash
mise exec -- go build -o /private/tmp/codeworld-build ./cmd/codeworld
```

Expected: exit 0.

- [ ] **Step 4: Check worktree**

Run:

```bash
git status --short
```

Expected: only intentional tracked changes, plus any pre-existing untracked `.codeworld/` and `.idea/`.

- [ ] **Step 5: Smoke run in source mode**

Run:

```bash
printf '/exit\n' | mise exec -- go run ./cmd/codeworld tui
```

Expected: exits 0 and prints the TUI status/header in non-terminal mode.

- [ ] **Step 6: Commit final cleanup if needed**

If Task 8 required code changes:

```bash
git add <changed-files>
git commit -m "test: verify codex style tui"
```

If no code changes were needed, do not create an empty commit.

---

## Self-Review

- Spec coverage: Tasks 1-7 cover Bubble Tea full-screen launch, transcript view, composer, status line with token split, permission prompt, slash commands, CLI dispatch, and non-interactive `run` preservation. Streaming output, themes, mouse support, resume picker, and image input remain out of scope as required.
- Completion scan: This plan contains no deferred implementation markers or unspecified implementation steps. Each task includes concrete file paths, test code, implementation code, commands, and expected outcomes.
- Type consistency: `Status`, `TranscriptItem`, `RunnerAdapter`, `ToolReporter`, `TUIConfirmer`, `PermissionDecision`, and Bubble Tea message types are introduced before later tasks reference them.
