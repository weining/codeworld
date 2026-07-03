package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"codeworld/internal/agent"
	"codeworld/internal/app"
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
	running           bool
	quitting          bool
}

// NewModel 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewModel(rt *app.Runtime) Model {
	input := textarea.New()
	input.Placeholder = "Ask codeworld..."
	input.Prompt = "> "
	input.SetHeight(3)
	input.Focus()
	vp := viewport.New(80, 20)
	reporter := NewToolReporter()
	confirmer := NewTUIConfirmer()
	turnEvents := make(chan agent.TurnEvent, 64)
	rt.Runner.Reporter = reporter
	rt.Runner.Confirmer = confirmer
	// TUI 用 channel 接收 agent 事件，避免模型流式输出阻塞 Bubble Tea 的按键和绘制循环。
	m := Model{rt: rt, adapter: NewRunnerAdapter(rt, turnEvents), toolEvents: reporter.Events(), turnEvents: turnEvents, confirmer: confirmer, input: input, viewport: vp, width: 80, height: 24, theme: "system"}
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
		m.input.SetWidth(msg.Width)
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
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text != "" {
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
				return m, func() tea.Msg {
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
	status := RuntimeStatus(m.rt)
	status.Running = m.running
	header := lipgloss.NewStyle().Bold(true).Render("codeworld") + "  " + StatusLine(status)
	body := m.viewport.View()
	composer := m.input.View()
	return fmt.Sprintf("%s\n%s\n%s", header, body, composer)
}

// refreshViewport 封装局部逻辑，保持调用方流程清晰。
func (m *Model) refreshViewport() {
	lines := make([]string, 0, len(m.items)*2)
	for _, item := range m.items {
		lines = append(lines, string(item.Kind))
		lines = append(lines, "  "+item.Text)
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.GotoBottom()
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
