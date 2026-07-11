package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"codeworld/internal/agent"
	"codeworld/internal/app"
	"codeworld/internal/config"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/session"
	"codeworld/internal/skill"
	"codeworld/internal/tools"
	"codeworld/internal/workspace"
)

// TestStatusLineShowsTokenBreakdown 验证对应场景的行为，避免后续改动破坏既有约束。
func TestStatusLineShowsTokenBreakdown(t *testing.T) {
	line := StatusLine(Status{
		Workspace: "repo",
		Provider:  "deepseek",
		Model:     "deepseek-v4-pro",
		Messages:  5,
		Approvals: 2,
		Usage:     model.Usage{InputTokens: 10, OutputTokens: 3, CacheTokens: 4, TotalTokens: 13},
		Git:       "dirty",
	})

	for _, want := range []string{"workspace=repo", "provider=deepseek", "model=deepseek-v4-pro", "git=dirty", "messages=5", "approvals=2", "input=10", "output=3", "cache=4", "total=13"} {
		if !strings.Contains(line, want) {
			t.Fatalf("status line missing %q in %q", want, line)
		}
	}
}

// TestRunReturnsWithoutTerminalRendererInTestMode 验证对应场景的行为，避免后续改动破坏既有约束。
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

// TestModelViewContainsTranscriptStatusAndComposer 验证对应场景的行为，避免后续改动破坏既有约束。
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
	m.refreshViewport()

	view := m.View()
	for _, want := range []string{"CODEWORLD", "deepseek-v4-pro", "13 tok", "ready", "›"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q in:\n%s", want, view)
		}
	}
}

// TestModelViewUsesCodexLikeChrome 验证 TUI 采用接近 Codex CLI 的顶部状态、消息区和底部输入提示。
func TestModelViewUsesCodexLikeChrome(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
		Usage:     model.Usage{InputTokens: 10, OutputTokens: 3, CacheTokens: 4, TotalTokens: 13},
	}
	m := NewModel(&rt)
	m.width = 100
	m.height = 28
	m.viewport.Width = 100
	m.viewport.Height = 20
	m.input.SetWidth(100)
	m.items = append(m.items,
		TranscriptItem{Kind: ItemUser, Text: "改成 Codex 风格"},
		TranscriptItem{Kind: ItemAssistant, Text: "我会先调整布局。"},
		TranscriptItem{Kind: ItemTool, Text: "shell start target=go test ./... risk=execute"},
		TranscriptItem{Kind: ItemPermission, Text: FormatPermissionRequest(permissions.Request{
			Action: permissions.ActionShell,
			Target: "go test ./...",
			Risk:   permissions.RiskExecute,
			Reason: "shell",
		})},
	)
	m.refreshViewport()

	view := m.View()
	for _, want := range []string{
		"◆ CODEWORLD",
		"deepseek-v4-pro",
		"YOU",
		"CODEWORLD",
		"TOOL",
		"Permission required: shell",
		"Allow? [y/N/a=session]",
		"↵ send",
		"ctrl+j newline",
		"/ commands",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q in:\n%s", want, view)
		}
	}
}

func TestModelViewShowsWelcomeStateAndWorkingComposer(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{Workspace: workspace.Workspace{Root: root}, Session: session.New(root, "deepseek", "deepseek-v4-pro")}
	m := NewModel(&rt)
	view := m.View()
	for _, want := range []string{"Welcome to Codeworld", "inspect this project", "● ready", "MESSAGE"} {
		if !strings.Contains(view, want) {
			t.Fatalf("welcome view missing %q in:\n%s", want, view)
		}
	}
	m.running = true
	view = m.View()
	for _, want := range []string{"● working", "working…", "Working on your request"} {
		if !strings.Contains(view, want) {
			t.Fatalf("working view missing %q in:\n%s", want, view)
		}
	}
}

func TestThemePalettesAreDistinct(t *testing.T) {
	dark := paletteFor("dark")
	light := paletteFor("light")
	if dark.accent == light.accent || dark.text == light.text || dark.border == light.border {
		t.Fatalf("theme palettes are not distinct: dark=%#v light=%#v", dark, light)
	}
}

func TestModelViewFitsNarrowTerminal(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{Workspace: workspace.Workspace{Root: root}, Session: session.New(root, "deepseek", "deepseek-v4-pro")}
	m := NewModel(&rt)
	m.width = 42
	m.height = 20
	m.viewport.Width = 42
	m.input.SetWidth(38)
	m.refreshViewport()

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("view height = %d, want %d:\n%s", len(lines), m.height, view)
	}
	for i, line := range lines {
		if width := lipgloss.Width(line); width > m.width {
			t.Fatalf("line %d width = %d, want <= %d: %q", i+1, width, m.width, line)
		}
	}
}

// TestFormatPermissionRequestMatchesCodexPrompt 验证权限提示采用接近 Codex CLI 的确认文案。
func TestFormatPermissionRequestMatchesCodexPrompt(t *testing.T) {
	text := FormatPermissionRequest(permissions.Request{
		Action: permissions.ActionShell,
		Target: "go test ./...",
		Risk:   permissions.RiskExecute,
		Reason: "shell",
	})

	for _, want := range []string{"Permission required: shell", "Target: go test ./...", "Risk: execute", "Reason: shell", "Allow? [y/N/a=session]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("permission text missing %q in:\n%s", want, text)
		}
	}
}

type fakeModelClient struct {
	resp model.GenerateResponse
}

// Generate 是测试辅助函数，用于复用测试准备或断言逻辑。
func (f fakeModelClient) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	return f.resp, nil
}

// TestRunnerAdapterUpdatesRuntimeUsageAndMessages 验证对应场景的行为，避免后续改动破坏既有约束。
func TestRunnerAdapterUpdatesRuntimeUsageAndMessages(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Store:     session.NewStore(root),
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
		Runner: agent.Runner{
			Model:     fakeModelClient{resp: model.GenerateResponse{FinalText: "done", Usage: model.Usage{InputTokens: 3, OutputTokens: 2, CacheTokens: 1, TotalTokens: 5}}},
			Tools:     tools.NewRegistry(nil, nil),
			Policy:    permissions.ConservativePolicy{},
			MaxSteps:  1,
			ModelName: "deepseek-v4-pro",
		},
	}

	adapter := NewRunnerAdapter(&rt, nil)
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

// TestToolReporterAppendsToolEvents 验证对应场景的行为，避免后续改动破坏既有约束。
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

// TestTUIConfirmerAllowsSessionApproval 验证对应场景的行为，避免后续改动破坏既有约束。
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

// TestHandleSlashCommandUpdatesModelAndTranscript 验证对应场景的行为，避免后续改动破坏既有约束。
func TestHandleSlashCommandUpdatesModelAndTranscript(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Store:     session.NewStore(root),
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

// TestHandleSlashCommandListsSkills 验证对应场景的行为，避免后续改动破坏既有约束。
func TestHandleSlashCommandListsSkills(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
		Skills:    []skill.Skill{{Name: "reviewer", Description: "Review Go changes."}},
	}
	m := NewModel(&rt)

	next, quit := m.handleSlashCommand(context.Background(), "/skills")
	if quit {
		t.Fatalf("skills command requested quit")
	}
	if len(next.items) == 0 || !strings.Contains(next.items[len(next.items)-1].Text, "reviewer") || !strings.Contains(next.items[len(next.items)-1].Text, "Review Go changes.") {
		t.Fatalf("items = %#v, want skills list", next.items)
	}
}

// TestHandleSlashCommandShowsEmptySkillsAndMCPNotices 验证对应场景的行为，避免后续改动破坏既有约束。
func TestHandleSlashCommandShowsEmptySkillsAndMCPNotices(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
	}
	m := NewModel(&rt)

	var quit bool
	m, quit = mustHandleCommand(t, m, "/skills")
	if quit {
		t.Fatalf("skills command requested quit")
	}
	m, quit = mustHandleCommand(t, m, "/mcp")
	if quit {
		t.Fatalf("mcp command requested quit")
	}
	all := transcriptText(m.items)
	for _, want := range []string{"no project skills loaded", "no mcp servers configured"} {
		if !strings.Contains(all, want) {
			t.Fatalf("transcript missing %q in:\n%s", want, all)
		}
	}
}

// TestUpdateShowsSlashCommandAndResultImmediately 验证对应场景的行为，避免后续改动破坏既有约束。
func TestUpdateShowsSlashCommandAndResultImmediately(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
	}
	m := NewModel(&rt)
	m.input.SetValue("/skills")

	nextModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("slash command returned async command")
	}
	next := nextModel.(Model)
	all := transcriptText(next.items)
	for _, want := range []string{"/skills", "no project skills loaded"} {
		if !strings.Contains(all, want) {
			t.Fatalf("transcript missing %q in:\n%s", want, all)
		}
	}
	if strings.TrimSpace(next.input.Value()) != "" {
		t.Fatalf("input = %q, want cleared", next.input.Value())
	}
	if !strings.Contains(next.View(), "no project skills loaded") {
		t.Fatalf("view did not render slash command result:\n%s", next.View())
	}
}

// TestModelViewShowsSlashSuggestions 验证输入 slash 前缀时 TUI 显示命令建议。
func TestModelViewShowsSlashSuggestions(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
	}
	m := NewModel(&rt)
	m.width = 100
	m.input.SetValue("/")

	view := m.View()
	for _, want := range []string{"/help", "/resume", "/compact", "/goal", "/plan"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing slash suggestion %q in:\n%s", want, view)
		}
	}
}

// TestModelPromptHistoryRestoresSubmittedDrafts 验证 Up/Down 可以恢复已提交草稿。
func TestModelPromptHistoryRestoresSubmittedDrafts(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
	}
	m := NewModel(&rt)
	m.input.SetValue("/status")
	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nextModel.(Model)
	m.input.SetValue("/model")
	nextModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nextModel.(Model)

	nextModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = nextModel.(Model)
	if strings.TrimSpace(m.input.Value()) != "/model" {
		t.Fatalf("input after Up = %q, want /model", m.input.Value())
	}
	nextModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = nextModel.(Model)
	if strings.TrimSpace(m.input.Value()) != "/status" {
		t.Fatalf("input after second Up = %q, want /status", m.input.Value())
	}
	nextModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nextModel.(Model)
	if strings.TrimSpace(m.input.Value()) != "/model" {
		t.Fatalf("input after Down = %q, want /model", m.input.Value())
	}
}

func TestCtrlJInsertsNewlineWithoutSubmitting(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{Workspace: workspace.Workspace{Root: root}, Session: session.New(root, "deepseek", "deepseek-v4-pro")}
	m := NewModel(&rt)
	m.input.SetValue("first line")

	nextModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if cmd != nil {
		t.Fatal("Ctrl+J returned a command, want local newline insertion")
	}
	next := nextModel.(Model)
	if next.input.Value() != "first line\n" {
		t.Fatalf("input = %q, want trailing newline", next.input.Value())
	}
	if next.running || len(next.items) != 0 {
		t.Fatalf("Ctrl+J submitted input: running=%v items=%#v", next.running, next.items)
	}
}

// TestHandleResumeCommandListsRecentSessions 验证 /resume 展示可恢复 session。
func TestHandleResumeCommandListsRecentSessions(t *testing.T) {
	root := t.TempDir()
	store := session.NewStore(root)
	sess := session.New(root, "deepseek", "deepseek-v4-pro")
	sess.ID = "resume-target"
	sess.Messages = []session.Message{{Role: "user", Content: "hello"}}
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Store:     store,
		Session:   sess,
	}
	m := NewModel(&rt)

	next, quit := mustHandleCommand(t, m, "/resume")
	if quit {
		t.Fatalf("resume command requested quit")
	}
	all := transcriptText(next.items)
	for _, want := range []string{"resume-target", "deepseek-v4-pro"} {
		if !strings.Contains(all, want) {
			t.Fatalf("transcript missing %q in:\n%s", want, all)
		}
	}
}

// TestHandleAdvancedSlashCommands 验证对应场景的行为，避免后续改动破坏既有约束。
func TestHandleAdvancedSlashCommands(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Config:    config.Config{MCPServers: []config.MCPServer{{Name: "demo", Command: "node", Args: []string{"server.js"}}}},
		Workspace: workspace.Workspace{Root: root},
		Session: session.Session{
			Provider:  "deepseek",
			Model:     "deepseek-v4-pro",
			Approvals: []session.Approval{{Kind: "shell", Command: "mise exec -- go test ./..."}},
		},
		Skills: []skill.Skill{{Name: "reviewer", Description: "Review Go changes."}},
	}
	m := NewModel(&rt)

	for _, cmd := range []string{"/permissions", "/mcp", "/context", "/theme dark", "/repl"} {
		var quit bool
		m, quit = mustHandleCommand(t, m, cmd)
		if quit {
			t.Fatalf("%s requested quit", cmd)
		}
	}
	all := transcriptText(m.items)
	for _, want := range []string{"mise exec -- go test", "demo node server.js", "skills=1", "theme=dark", "restart with: codeworld repl"} {
		if !strings.Contains(all, want) {
			t.Fatalf("transcript missing %q in:\n%s", want, all)
		}
	}
	if m.theme != "dark" {
		t.Fatalf("theme = %q, want dark", m.theme)
	}
}

// TestHandleGoalPlanAndCompactCommands 验证 TUI 能设置目标、切换 plan mode 并手动 compact。
func TestHandleGoalPlanAndCompactCommands(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Store:     session.NewStore(root),
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
	}
	m := NewModel(&rt)

	var quit bool
	m, quit = mustHandleCommand(t, m, "/goal ship the feature")
	if quit {
		t.Fatalf("goal command requested quit")
	}
	if m.rt.Session.Goal != "ship the feature" {
		t.Fatalf("goal = %q, want ship the feature", m.rt.Session.Goal)
	}
	if !strings.Contains(m.rt.Runner.SystemPrompt, "Current goal:\nship the feature") {
		t.Fatalf("system prompt did not refresh goal: %q", m.rt.Runner.SystemPrompt)
	}
	m, quit = mustHandleCommand(t, m, "/plan")
	if quit {
		t.Fatalf("plan command requested quit")
	}
	if m.rt.Session.Mode != "plan" {
		t.Fatalf("mode = %q, want plan", m.rt.Session.Mode)
	}
	if !strings.Contains(m.rt.Runner.SystemPrompt, "Plan mode:") {
		t.Fatalf("system prompt did not refresh plan mode: %q", m.rt.Runner.SystemPrompt)
	}
	m, quit = mustHandleCommand(t, m, "/compact")
	if quit {
		t.Fatalf("compact command requested quit")
	}
	all := transcriptText(m.items)
	for _, want := range []string{"goal=ship the feature", "mode=plan", "nothing to compact"} {
		if !strings.Contains(all, want) {
			t.Fatalf("transcript missing %q in:\n%s", want, all)
		}
	}
}

func TestHandlePermissionsPersistsSessionMode(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{Store: session.NewStore(root), Session: session.New(root, "deepseek", "deepseek-v4-pro")}
	m, quit := mustHandleCommand(t, NewModel(&rt), "/permissions read-only")
	if quit {
		t.Fatal("permissions command requested quit")
	}
	saved, err := m.rt.Store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if saved.ApprovalMode != "read-only" {
		t.Fatalf("approval mode = %q, want read-only", saved.ApprovalMode)
	}
}

// TestHandleImageCommandAttachesPendingImage 验证 /image 会把图片附加到下一条 prompt。
func TestHandleImageCommandAttachesPendingImage(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "sample.png")
	if err := os.WriteFile(imagePath, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Store:     session.NewStore(root),
		Session:   session.New(root, "openai", "gpt-4.1"),
	}
	m := NewModel(&rt)

	next, quit := mustHandleCommand(t, m, "/image "+imagePath)
	if quit {
		t.Fatalf("image command requested quit")
	}
	if len(next.pendingImages) != 1 || next.pendingImages[0].Type != model.ContentPartImage {
		t.Fatalf("pending images = %#v, want one image", next.pendingImages)
	}
	if !strings.Contains(transcriptText(next.items), "attached image") {
		t.Fatalf("items = %#v, want attached notice", next.items)
	}
}

// TestModelScrollKeysMoveViewport 验证对应场景的行为，避免后续改动破坏既有约束。
func TestModelScrollKeysMoveViewport(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
	}
	m := NewModel(&rt)
	m.viewport.Height = 4
	for i := 0; i < 20; i++ {
		m.items = append(m.items, TranscriptItem{Kind: ItemNotice, Text: "line"})
	}
	m.refreshViewport()
	bottom := m.viewport.YOffset
	if bottom == 0 {
		t.Fatalf("viewport did not scroll to bottom")
	}

	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	next := nextModel.(Model)
	if next.viewport.YOffset >= bottom {
		t.Fatalf("PageUp YOffset = %d, want less than %d", next.viewport.YOffset, bottom)
	}
	afterUp := next.viewport.YOffset
	nextModel, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	next = nextModel.(Model)
	if next.viewport.YOffset <= afterUp {
		t.Fatalf("Ctrl+D YOffset = %d, want greater than %d", next.viewport.YOffset, afterUp)
	}
}

// TestModelAppliesAssistantDeltaToActiveTranscriptItem 验证对应场景的行为，避免后续改动破坏既有约束。
func TestModelAppliesAssistantDeltaToActiveTranscriptItem(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
	}
	m := NewModel(&rt)

	nextModel, _ := m.Update(turnEventMsg{event: agent.TurnEvent{Kind: agent.TurnEventAssistantDelta, Text: "你"}})
	next := nextModel.(Model)
	nextModel, _ = next.Update(turnEventMsg{event: agent.TurnEvent{Kind: agent.TurnEventAssistantDelta, Text: "好"}})
	next = nextModel.(Model)

	if len(next.items) != 1 {
		t.Fatalf("items = %#v, want one assistant item", next.items)
	}
	if next.items[0].Kind != ItemAssistant || next.items[0].Text != "你好" {
		t.Fatalf("assistant item = %#v, want merged streaming text", next.items[0])
	}
}

// mustHandleCommand 是测试辅助函数，用于复用测试准备或断言逻辑。
func mustHandleCommand(t *testing.T, m Model, cmd string) (Model, bool) {
	t.Helper()
	next, quit := m.handleSlashCommand(context.Background(), cmd)
	return next, quit
}

// transcriptText 是测试辅助函数，用于复用测试准备或断言逻辑。
func transcriptText(items []TranscriptItem) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, item.Text)
	}
	return strings.Join(lines, "\n")
}
