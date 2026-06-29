package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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
