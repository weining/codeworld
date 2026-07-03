package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"codeworld/internal/agent"
	"codeworld/internal/app"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/session"
)

type Model struct {
	rt                *app.Runtime
	adapter           RunnerAdapter
	toolEvents        <-chan TranscriptItem
	turnEvents        <-chan agent.TurnEvent
	confirmer         *TUIConfirmer
	pendingPermission *permissions.Request
	input             textarea.Model
	viewport          viewport.Model
	items             []TranscriptItem
	width             int
	height            int
	theme             string
	promptHistory     []string
	historyIndex      int
	pendingImages     []model.ContentPart
	running           bool
	quitting          bool
}

// NewModel 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewModel(rt *app.Runtime) Model {
	input := textarea.New()
	input.Placeholder = "Ask codeworld..."
	input.Prompt = "> "
	input.ShowLineNumbers = false
	input.EndOfBufferCharacter = ' '
	input.SetHeight(3)
	input.Focus()
	vp := viewport.New(80, 20)
	reporter := NewToolReporter()
	confirmer := NewTUIConfirmer()
	turnEvents := make(chan agent.TurnEvent, 64)
	rt.Runner.Reporter = reporter
	rt.Runner.Confirmer = confirmer
	// TUI 用 channel 接收 agent 事件，避免模型流式输出阻塞 Bubble Tea 的按键和绘制循环。
	m := Model{rt: rt, adapter: NewRunnerAdapter(rt, turnEvents), toolEvents: reporter.Events(), turnEvents: turnEvents, confirmer: confirmer, input: input, viewport: vp, width: 80, height: 24, theme: "system", historyIndex: -1}
	m.refreshViewport()
	return m
}

// Init 返回 TUI 启动时需要并行监听的初始命令。
func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.waitToolEvent(), m.waitPermissionRequest(), m.waitTurnEvent())
}

// Update 处理 TUI 消息并返回下一版模型状态和后续命令。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case turnEventMsg:
		m.applyTurnEvent(msg.event)
		m.refreshViewport()
		return m, m.waitTurnEvent()
	case permissionRequestMsg:
		m.pendingPermission = &msg.request
		m.items = append(m.items, TranscriptItem{Kind: ItemPermission, Text: FormatPermissionRequest(msg.request)})
		m.refreshViewport()
		return m, m.waitPermissionRequest()
	case toolEventMsg:
		m.items = append(m.items, msg.item)
		m.refreshViewport()
		return m, m.waitToolEvent()
	case turnDoneMsg:
		m.running = false
		if msg.err != nil {
			m.items = append(m.items, TranscriptItem{Kind: ItemError, Text: msg.err.Error()})
		}
		m.refreshViewport()
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = max(3, msg.Height-7)
		m.input.SetWidth(max(10, msg.Width-4))
		m.refreshViewport()
		return m, nil
	case tea.KeyMsg:
		if m.pendingPermission != nil {
			switch msg.String() {
			case "y":
				m.confirmer.Decide(PermissionDecision{Allow: true})
				m.items = append(m.items, TranscriptItem{Kind: ItemPermission, Text: "allowed once"})
				m.pendingPermission = nil
			case "a":
				if m.pendingPermission.Action == permissions.ActionShell {
					m.rt.Session.Approvals = append(m.rt.Session.Approvals, session.Approval{Kind: "shell", Command: m.pendingPermission.Target})
				}
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
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "pgup":
			m.viewport.PageUp()
			return m, nil
		case "pgdown":
			m.viewport.PageDown()
			return m, nil
		case "ctrl+u":
			m.viewport.HalfPageUp()
			return m, nil
		case "ctrl+d":
			m.viewport.HalfPageDown()
			return m, nil
		case "up":
			m.restorePromptHistory(-1)
			return m, nil
		case "down":
			m.restorePromptHistory(1)
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text != "" {
				m.recordPrompt(text)
				if strings.HasPrefix(text, "/") {
					m.items = append(m.items, TranscriptItem{Kind: ItemUser, Text: text})
					next, quit := m.handleSlashCommand(context.Background(), text)
					if quit {
						next.quitting = true
						return next, tea.Quit
					}
					next.input.Reset()
					next.refreshViewport()
					return next, nil
				}
				m.items = append(m.items, TranscriptItem{Kind: ItemUser, Text: text})
				m.input.Reset()
				m.refreshViewport()
				m.running = true
				prompt := text
				images := append([]model.ContentPart(nil), m.pendingImages...)
				m.pendingImages = nil
				return m, func() tea.Msg {
					if len(images) > 0 {
						userMessage := model.Message{Role: model.RoleUser, Content: prompt, Parts: append([]model.ContentPart{model.TextPart(prompt)}, images...)}
						return m.adapter.RunTurnMessage(context.Background(), userMessage)
					}
					return m.adapter.RunTurn(context.Background(), prompt)
				}
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View 渲染当前 TUI 状态，包括状态栏、对话区和输入框。
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	header := m.renderHeader()
	body := m.viewport.View()
	composer := m.renderComposer()
	return fmt.Sprintf("%s\n%s\n%s", header, body, composer)
}

// refreshViewport 重新渲染 transcript，并把滚动位置保持在最新消息底部。
func (m *Model) refreshViewport() {
	blocks := make([]string, 0, len(m.items))
	for _, item := range m.items {
		blocks = append(blocks, m.renderTranscriptItem(item))
	}
	m.viewport.SetContent(strings.Join(blocks, "\n\n"))
	m.viewport.GotoBottom()
}

// renderHeader 渲染接近 Codex CLI 的状态栏，窄屏时拆出 token 行。
func (m Model) renderHeader() string {
	status := RuntimeStatus(m.rt)
	status.Running = m.running
	status.Workspace = filepath.Base(status.Workspace)
	title := lipgloss.NewStyle().Bold(true).Render("codeworld")
	full := title + "  " + HeaderStatusLine(status)
	if m.width <= 0 || lipgloss.Width(full) <= m.width {
		return m.fitLine(full)
	}
	summary := fmt.Sprintf("%s  model=%s provider=%s workspace=%s git=%s", title, status.Model, status.Provider, status.Workspace, status.Git)
	metrics := fmt.Sprintf("tokens input=%d output=%d cache=%d total=%d",
		status.Usage.InputTokens,
		status.Usage.OutputTokens,
		status.Usage.CacheTokens,
		status.Usage.TotalTokens,
	)
	withCounts := fmt.Sprintf("%s messages=%d approvals=%d", metrics, status.Messages, status.Approvals)
	if m.width <= 0 || lipgloss.Width(withCounts) <= m.width {
		metrics = withCounts
	}
	if status.Running {
		metrics += " running"
	}
	return m.fitLine(summary) + "\n" + m.fitLine(metrics)
}

// renderComposer 渲染底部输入区和常用快捷键提示。
func (m Model) renderComposer() string {
	width := max(10, m.width)
	inputWidth := max(8, width-4)
	inputView := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Width(inputWidth).
		Render(m.input.View())
	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")).
		Width(width).
		Render("Enter send  Shift+Enter newline  Ctrl+C exit  /help")
	if suggestions := m.renderSlashSuggestions(); suggestions != "" {
		return inputView + "\n" + suggestions + "\n" + help
	}
	return inputView + "\n" + help
}

// renderTranscriptItem 渲染单条消息，使用固定角色列让内容像 Codex CLI 一样对齐。
func (m Model) renderTranscriptItem(item TranscriptItem) string {
	role := string(item.Kind)
	text := strings.TrimRight(item.Text, "\n")
	if text == "" {
		text = " "
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	out = append(out, fmt.Sprintf("%10s  %s", role, lines[0]))
	for _, line := range lines[1:] {
		out = append(out, fmt.Sprintf("%10s  %s", "", line))
	}
	return strings.Join(out, "\n")
}

// fitLine 按终端宽度裁剪单行内容，避免状态栏横向溢出。
func (m Model) fitLine(line string) string {
	if m.width <= 0 {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(line)
}

// renderSlashSuggestions 在输入 slash 前缀时展示可用命令，提供接近 Codex 的发现体验。
func (m Model) renderSlashSuggestions() string {
	value := strings.TrimSpace(m.input.Value())
	if !strings.HasPrefix(value, "/") {
		return ""
	}
	matches := matchingSlashCommands(value)
	if len(matches) == 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")).
		Width(max(10, m.width)).
		Render("commands  " + strings.Join(matches, "  "))
}

// recordPrompt 保存已提交草稿，供 Up/Down 在 composer 中恢复。
func (m *Model) recordPrompt(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if len(m.promptHistory) == 0 || m.promptHistory[len(m.promptHistory)-1] != text {
		m.promptHistory = append(m.promptHistory, text)
	}
	m.historyIndex = -1
}

// restorePromptHistory 根据方向恢复历史草稿；direction 为 -1 表示更早，1 表示更新。
func (m *Model) restorePromptHistory(direction int) {
	if len(m.promptHistory) == 0 {
		return
	}
	if m.historyIndex == -1 {
		if direction < 0 {
			m.historyIndex = len(m.promptHistory) - 1
		} else {
			return
		}
	} else {
		m.historyIndex += direction
		if m.historyIndex < 0 {
			m.historyIndex = 0
		}
		if m.historyIndex >= len(m.promptHistory) {
			m.historyIndex = len(m.promptHistory) - 1
		}
	}
	m.input.SetValue(m.promptHistory[m.historyIndex])
}

// applyTurnEvent 封装局部逻辑，保持调用方流程清晰。
func (m *Model) applyTurnEvent(event agent.TurnEvent) {
	switch event.Kind {
	case agent.TurnEventAssistantDelta:
		// 文本 delta 合并到最后一个 assistant item，保证流式输出不会刷出大量碎片行。
		if len(m.items) == 0 || m.items[len(m.items)-1].Kind != ItemAssistant {
			m.items = append(m.items, TranscriptItem{Kind: ItemAssistant})
		}
		m.items[len(m.items)-1].Text += event.Text
	case agent.TurnEventAssistantDone:
		if len(m.items) == 0 || m.items[len(m.items)-1].Kind != ItemAssistant {
			m.items = append(m.items, TranscriptItem{Kind: ItemAssistant, Text: event.Text})
			return
		}
		if m.items[len(m.items)-1].Text == "" {
			m.items[len(m.items)-1].Text = event.Text
		}
	case agent.TurnEventError:
		m.items = append(m.items, TranscriptItem{Kind: ItemError, Text: event.Text})
	}
}

// waitToolEvent 封装局部逻辑，保持调用方流程清晰。
func (m Model) waitToolEvent() tea.Cmd {
	return func() tea.Msg {
		item, ok := <-m.toolEvents
		if !ok {
			return nil
		}
		return toolEventMsg{item: item}
	}
}

// waitTurnEvent 封装局部逻辑，保持调用方流程清晰。
func (m Model) waitTurnEvent() tea.Cmd {
	return func() tea.Msg {
		event, ok := <-m.turnEvents
		if !ok {
			return nil
		}
		return turnEventMsg{event: event}
	}
}

// waitPermissionRequest 封装局部逻辑，保持调用方流程清晰。
func (m Model) waitPermissionRequest() tea.Cmd {
	return func() tea.Msg {
		req, ok := <-m.confirmer.Requests()
		if !ok {
			return nil
		}
		return permissionRequestMsg{request: req}
	}
}

// max 从候选值中选择满足条件的结果。
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
