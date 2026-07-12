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
	ctx               context.Context
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
	turnCancel        context.CancelFunc
	interrupting      bool
	lastTurnError     string
	queuedTurns       []queuedTurn
	quitting          bool
	initialTurn       *queuedTurn
}

type queuedTurn struct {
	text   string
	images []model.ContentPart
}

type initialTurnMsg struct{}

// NewModel 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewModel(rt *app.Runtime) Model {
	input := textarea.New()
	input.Placeholder = "Describe a task or type / for commands"
	input.Prompt = "› "
	input.ShowLineNumbers = false
	input.EndOfBufferCharacter = ' '
	input.SetHeight(2)
	input.SetWidth(76)
	input.Focus()
	vp := viewport.New(80, 13)
	reporter := NewToolReporter()
	confirmer := NewTUIConfirmer()
	turnEvents := make(chan agent.TurnEvent, 64)
	rt.Runner.Reporter = reporter
	rt.Runner.Confirmer = confirmer
	// TUI 用 channel 接收 agent 事件，避免模型流式输出阻塞 Bubble Tea 的按键和绘制循环。
	m := Model{ctx: context.Background(), rt: rt, adapter: NewRunnerAdapter(rt, turnEvents), toolEvents: reporter.Events(), turnEvents: turnEvents, confirmer: confirmer, input: input, viewport: vp, width: 80, height: 24, theme: "system", historyIndex: -1}
	m.refreshViewport()
	return m
}

// Init 返回 TUI 启动时需要并行监听的初始命令。
func (m Model) Init() tea.Cmd {
	commands := []tea.Cmd{textarea.Blink, m.waitToolEvent(), m.waitPermissionRequest(), m.waitTurnEvent()}
	if m.initialTurn != nil {
		commands = append(commands, func() tea.Msg { return initialTurnMsg{} })
	}
	return tea.Batch(commands...)
}

// Update 处理 TUI 消息并返回下一版模型状态和后续命令。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case initialTurnMsg:
		if m.initialTurn == nil {
			return m, nil
		}
		turn := *m.initialTurn
		m.initialTurn = nil
		return m.beginTurn(turn)
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
		if m.turnCancel != nil {
			m.turnCancel()
			m.turnCancel = nil
		}
		m.running = false
		if msg.err != nil {
			if m.interrupting {
				m.items = append(m.items, TranscriptItem{Kind: ItemNotice, Text: "turn interrupted"})
			} else if m.lastTurnError == "" {
				m.items = append(m.items, TranscriptItem{Kind: ItemError, Text: msg.err.Error()})
			}
		}
		m.interrupting = false
		if len(m.queuedTurns) > 0 {
			next := m.queuedTurns[0]
			m.queuedTurns = m.queuedTurns[1:]
			m.items = append(m.items, TranscriptItem{Kind: ItemNotice, Text: "starting queued follow-up"})
			m, cmd := m.beginTurn(next)
			m.refreshViewport()
			return m, cmd
		}
		m.refreshViewport()
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = max(3, msg.Height-11)
		m.input.SetWidth(max(10, msg.Width-4))
		m.refreshViewport()
		return m, nil
	case tea.KeyMsg:
		if m.pendingPermission != nil {
			switch msg.String() {
			case "ctrl+c":
				if m.turnCancel != nil {
					m.turnCancel()
				}
				m.quitting = true
				return m, tea.Quit
			case "ctrl+x":
				if m.turnCancel != nil && !m.interrupting {
					m.interrupting = true
					m.turnCancel()
					m.items = append(m.items, TranscriptItem{Kind: ItemNotice, Text: "interrupt requested"})
				}
				m.pendingPermission = nil
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
			if m.turnCancel != nil {
				m.turnCancel()
			}
			m.quitting = true
			return m, tea.Quit
		case "ctrl+x":
			if m.running && m.turnCancel != nil && !m.interrupting {
				m.interrupting = true
				m.turnCancel()
				m.items = append(m.items, TranscriptItem{Kind: ItemNotice, Text: "interrupt requested"})
				m.refreshViewport()
			}
			return m, nil
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
		case "ctrl+j", "alt+enter":
			m.input.InsertString("\n")
			return m, nil
		case "enter":
			if m.running {
				text := strings.TrimSpace(m.input.Value())
				if text == "/interrupt" {
					if m.turnCancel != nil && !m.interrupting {
						m.interrupting = true
						m.turnCancel()
						m.items = append(m.items, TranscriptItem{Kind: ItemNotice, Text: "interrupt requested"})
						m.input.Reset()
						m.refreshViewport()
					}
					return m, nil
				}
				if text != "" {
					m.recordPrompt(text)
					images := append([]model.ContentPart(nil), m.pendingImages...)
					m.pendingImages = nil
					m.queuedTurns = append(m.queuedTurns, queuedTurn{text: text, images: images})
					m.items = append(m.items, TranscriptItem{Kind: ItemNotice, Text: fmt.Sprintf("queued follow-up #%d: %s", len(m.queuedTurns), text)})
					m.input.Reset()
					m.refreshViewport()
				}
				return m, nil
			}
			text := strings.TrimSpace(m.input.Value())
			if text != "" {
				m.recordPrompt(text)
				if strings.HasPrefix(text, "/") {
					m.items = append(m.items, TranscriptItem{Kind: ItemUser, Text: text})
					next, quit := m.handleSlashCommand(m.ctx, text)
					if quit {
						next.quitting = true
						return next, tea.Quit
					}
					next.input.Reset()
					next.refreshViewport()
					return next, nil
				}
				m.input.Reset()
				images := append([]model.ContentPart(nil), m.pendingImages...)
				m.pendingImages = nil
				m, cmd := m.beginTurn(queuedTurn{text: text, images: images})
				m.refreshViewport()
				return m, cmd
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) beginTurn(turn queuedTurn) (Model, tea.Cmd) {
	m.items = append(m.items, TranscriptItem{Kind: ItemUser, Text: turn.text})
	m.running = true
	m.lastTurnError = ""
	turnCtx, cancel := context.WithCancel(m.ctx)
	m.turnCancel = cancel
	return m, func() tea.Msg {
		if len(turn.images) > 0 {
			userMessage := model.Message{Role: model.RoleUser, Content: turn.text, Parts: append([]model.ContentPart{model.TextPart(turn.text)}, turn.images...)}
			return m.adapter.RunTurnMessage(turnCtx, userMessage)
		}
		return m.adapter.RunTurn(turnCtx, turn.text)
	}
}

// View 渲染当前 TUI 状态，包括状态栏、对话区和输入框。
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	header := m.renderHeader()
	composer := m.renderComposer()
	bodyHeight := max(3, m.height-lipgloss.Height(header)-lipgloss.Height(composer)-2)
	viewport := m.viewport
	viewport.Height = bodyHeight
	body := viewport.View()
	if len(m.items) == 0 {
		body = m.renderWelcome(bodyHeight)
	}
	return fmt.Sprintf("%s\n%s\n%s", header, body, composer)
}

// refreshViewport 重新渲染 transcript，并把滚动位置保持在最新消息底部。
func (m *Model) refreshViewport() {
	if len(m.items) == 0 {
		m.viewport.SetContent("")
		m.viewport.GotoTop()
		return
	}
	blocks := make([]string, 0, len(m.items))
	for _, item := range m.items {
		blocks = append(blocks, m.renderTranscriptItem(item))
	}
	m.viewport.SetContent(strings.Join(blocks, "\n\n"))
	m.viewport.GotoBottom()
}

// renderHeader 渲染紧凑的品牌、运行状态和会话指标。
func (m Model) renderHeader() string {
	status := RuntimeStatus(m.rt)
	status.Running = m.running
	status.Workspace = filepath.Base(status.Workspace)
	p := paletteFor(m.theme)
	width := max(20, m.width)
	brand := styled(p.accent).Bold(true).Render("◆ CODEWORLD")
	stateText := "● ready"
	stateColor := p.success
	if status.Running {
		stateText = "● working"
		stateColor = p.accent
	}
	state := styled(stateColor).Bold(true).Render(stateText)
	contextLine := styled(p.text).Render(status.Model) + styled(p.muted).Render("  ·  "+status.Provider+"  ·  "+status.Workspace)
	gitColor := p.success
	if status.Git == "dirty" {
		gitColor = p.warning
	}
	git := styled(gitColor).Render("git " + status.Git)
	metrics := styled(p.muted).Render(fmt.Sprintf("%s tok  ·  %d msg  ·  %d approvals", compactCount(status.Usage.TotalTokens), status.Messages, status.Approvals))
	line1 := joinEdges(brand, state, width)
	line2 := joinEdges(contextLine+"  "+git, metrics, width)
	rule := styled(p.border).Render(strings.Repeat("─", width))
	return line1 + "\n" + line2 + "\n" + rule
}

func (m Model) renderWelcome(height int) string {
	p := paletteFor(m.theme)
	width := min(max(28, m.viewport.Width-8), 68)
	title := styled(p.accent).Bold(true).Render("Welcome to Codeworld")
	body := styled(p.text).Render("A local coding agent that works with you in this workspace.")
	hints := styled(p.muted).Render("Try  “inspect this project”  ·  type / for commands")
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(p.border)).
		Padding(1, 2).
		Width(width).
		Render(title + "\n\n" + body + "\n" + hints)
	return lipgloss.Place(max(1, m.viewport.Width), max(1, height), lipgloss.Center, lipgloss.Center, card)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// renderComposer 渲染底部输入区和常用快捷键提示。
func (m Model) renderComposer() string {
	width := max(10, m.width)
	inputWidth := max(8, width-4)
	p := paletteFor(m.theme)
	borderColor := p.accent
	status := "ready"
	if m.running {
		borderColor = p.secondary
		status = "working…"
		if len(m.queuedTurns) > 0 {
			status += fmt.Sprintf("  ·  %d queued", len(m.queuedTurns))
		}
	}
	if m.pendingPermission != nil {
		borderColor = p.warning
		status = "permission required"
	}
	if len(m.pendingImages) > 0 {
		status += fmt.Sprintf("  ·  %d image", len(m.pendingImages))
	}
	caption := joinEdges(styled(borderColor).Bold(true).Render("MESSAGE"), styled(p.muted).Render(status), width)
	inputView := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(inputWidth).
		Render(m.input.View())
	helpText := "↵ send   ctrl+j newline   ↑↓ history   / commands   ctrl+c quit"
	if width < 70 {
		helpText = "↵ send   ^J newline   / commands   ^C quit"
	}
	if width < 46 {
		helpText = "↵ send   ^J newline   ^C quit"
	}
	if m.pendingPermission != nil {
		helpText = "y allow once   a allow for session   n deny"
	} else if m.running {
		helpText = "Working on your request…   ↵ queue   ctrl+x interrupt   ctrl+c quit"
	}
	help := styled(p.muted).
		Width(width).
		Render(helpText)
	if suggestions := m.renderSlashSuggestions(); suggestions != "" {
		return caption + "\n" + inputView + "\n" + suggestions + "\n" + help
	}
	return caption + "\n" + inputView + "\n" + help
}

// renderTranscriptItem 渲染单条消息，使用固定角色列让内容像 Codex CLI 一样对齐。
func (m Model) renderTranscriptItem(item TranscriptItem) string {
	p := paletteFor(m.theme)
	text := strings.TrimRight(item.Text, "\n")
	if text == "" {
		text = " "
	}
	label := strings.ToUpper(string(item.Kind))
	color := p.muted
	icon := "·"
	switch item.Kind {
	case ItemUser:
		label, color, icon = "YOU", p.user, "›"
	case ItemAssistant:
		label, color, icon = "CODEWORLD", p.accent, "◆"
	case ItemTool:
		label, color, icon = "TOOL", p.secondary, "⚙"
	case ItemPermission:
		label, color, icon = "PERMISSION", p.warning, "!"
	case ItemError:
		label, color, icon = "ERROR", p.error, "×"
	case ItemNotice:
		label, color, icon = "INFO", p.success, "i"
	case ItemCommand:
		label, color, icon = "COMMAND", p.muted, "/"
	}
	heading := styled(color).Bold(true).Render(icon + " " + label)
	content := lipgloss.NewStyle().
		Foreground(lipgloss.Color(p.text)).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color(color)).
		PaddingLeft(1).
		MaxWidth(max(10, m.viewport.Width-2)).
		Render(text)
	if item.Kind == ItemTool || item.Kind == ItemNotice || item.Kind == ItemCommand {
		content = styled(p.muted).PaddingLeft(2).MaxWidth(max(10, m.viewport.Width-2)).Render(text)
	}
	return heading + "\n" + content
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
	p := paletteFor(m.theme)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(p.muted)).
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color(p.secondary)).
		PaddingLeft(1).
		Width(max(8, m.width-2)).
		Render("COMMANDS  " + strings.Join(matches, "  "))
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
		m.lastTurnError = event.Text
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
