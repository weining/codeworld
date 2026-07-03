package tui

import (
	"context"
	"fmt"
	"strings"

	"codeworld/internal/model"
	"codeworld/internal/session"
)

// handleSlashCommand 封装局部逻辑，保持调用方流程清晰。
func (m Model) handleSlashCommand(ctx context.Context, line string) (Model, bool) {
	switch {
	case line == "/help":
		m.appendNotice("/help /model /status /diff /permissions /mcp /skills /context /theme /repl /clear /exit")
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
	case line == "/permissions":
		if len(m.rt.Session.Approvals) == 0 {
			m.appendNotice("no session approvals")
			return m, false
		}
		lines := make([]string, 0, len(m.rt.Session.Approvals))
		for _, approval := range m.rt.Session.Approvals {
			if approval.Command == "" {
				lines = append(lines, approval.Kind)
				continue
			}
			lines = append(lines, approval.Kind+" "+approval.Command)
		}
		m.appendNotice(strings.Join(lines, "\n"))
	case line == "/mcp":
		if len(m.rt.Config.MCPServers) == 0 {
			m.appendNotice("no mcp servers configured")
			return m, false
		}
		lines := make([]string, 0, len(m.rt.Config.MCPServers))
		for _, server := range m.rt.Config.MCPServers {
			lines = append(lines, strings.TrimSpace(server.Name+" "+server.Command+" "+strings.Join(server.Args, " ")))
		}
		m.appendNotice(strings.Join(lines, "\n"))
	case line == "/skills":
		if len(m.rt.Skills) == 0 {
			m.appendNotice("no project skills loaded")
			return m, false
		}
		lines := make([]string, 0, len(m.rt.Skills))
		for _, skill := range m.rt.Skills {
			if skill.Description == "" {
				lines = append(lines, skill.Name)
				continue
			}
			lines = append(lines, skill.Name+" - "+skill.Description)
		}
		m.appendNotice(strings.Join(lines, "\n"))
	case line == "/context":
		m.appendNotice(fmt.Sprintf("messages=%d skills=%d mcp_servers=%d context_tools=3", len(m.rt.Messages), len(m.rt.Skills), len(m.rt.Config.MCPServers)))
	case line == "/repl":
		m.appendNotice("restart with: codeworld repl")
	case strings.HasPrefix(line, "/theme"):
		next := strings.TrimSpace(strings.TrimPrefix(line, "/theme"))
		if next == "" {
			m.appendNotice("theme=" + m.theme)
			return m, false
		}
		switch next {
		case "system", "dark", "light":
			m.theme = next
			m.appendNotice("theme=" + next)
		default:
			m.appendError("unknown theme: " + next)
		}
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

// appendNotice 封装局部逻辑，保持调用方流程清晰。
func (m *Model) appendNotice(text string) {
	m.items = append(m.items, TranscriptItem{Kind: ItemNotice, Text: text})
}

// appendError 封装局部逻辑，保持调用方流程清晰。
func (m *Model) appendError(text string) {
	m.items = append(m.items, TranscriptItem{Kind: ItemError, Text: text})
}
