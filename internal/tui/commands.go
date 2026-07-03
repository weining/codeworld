package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/session"
)

var slashCommands = []string{
	"/help",
	"/model",
	"/status",
	"/diff",
	"/permissions",
	"/mcp",
	"/skills",
	"/context",
	"/theme",
	"/resume",
	"/compact",
	"/goal",
	"/plan",
	"/image",
	"/repl",
	"/clear",
	"/exit",
}

// handleSlashCommand 封装局部逻辑，保持调用方流程清晰。
func (m Model) handleSlashCommand(ctx context.Context, line string) (Model, bool) {
	switch {
	case line == "/help":
		m.appendNotice(strings.Join(slashCommands, " "))
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
		mode := m.rt.Config.ApprovalMode
		if mode == "" {
			mode = "auto"
		}
		if len(m.rt.Session.Approvals) == 0 {
			m.appendNotice("mode=" + mode + "\nno session approvals")
			return m, false
		}
		lines := make([]string, 0, len(m.rt.Session.Approvals))
		lines = append(lines, "mode="+mode)
		for _, approval := range m.rt.Session.Approvals {
			if approval.Command == "" {
				lines = append(lines, approval.Kind)
				continue
			}
			lines = append(lines, approval.Kind+" "+approval.Command)
		}
		m.appendNotice(strings.Join(lines, "\n"))
	case strings.HasPrefix(line, "/permissions "):
		mode := strings.TrimSpace(strings.TrimPrefix(line, "/permissions "))
		switch mode {
		case string(permissions.ModeAuto), string(permissions.ModeReadOnly), string(permissions.ModeFullAccess):
			m.rt.Config.ApprovalMode = mode
			m.rt.Runner.Policy = permissions.ModePolicy{Mode: permissions.Mode(mode)}
			m.appendNotice("mode=" + mode)
		default:
			m.appendError("unknown permission mode: " + mode)
		}
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
	case line == "/goal":
		if m.rt.Session.Goal == "" {
			m.appendNotice("goal not set")
			return m, false
		}
		m.appendNotice(m.rt.Session.Goal)
	case strings.HasPrefix(line, "/goal "):
		next := strings.TrimSpace(strings.TrimPrefix(line, "/goal "))
		if next == "clear" {
			m.rt.Session.Goal = ""
			m.appendNotice("goal cleared")
		} else {
			m.rt.Session.Goal = next
			m.appendNotice("goal=" + next)
		}
		_ = m.rt.Store.SaveCurrent(m.rt.Session)
	case line == "/plan":
		m.rt.Session.Mode = "plan"
		m.appendNotice("mode=plan")
		_ = m.rt.Store.SaveCurrent(m.rt.Session)
	case line == "/plan off":
		m.rt.Session.Mode = ""
		m.appendNotice("mode=default")
		_ = m.rt.Store.SaveCurrent(m.rt.Session)
	case line == "/compact":
		message, err := m.rt.Compact(ctx)
		if err != nil {
			m.appendError("compact error: " + err.Error())
			return m, false
		}
		m.appendNotice(message)
	case strings.HasPrefix(line, "/image "):
		path := strings.TrimSpace(strings.TrimPrefix(line, "/image "))
		part, err := model.ImagePartFromFile(path)
		if err != nil {
			m.appendError("image error: " + err.Error())
			return m, false
		}
		m.pendingImages = append(m.pendingImages, part)
		m.appendNotice(fmt.Sprintf("attached image: %s", path))
	case strings.HasPrefix(line, "/resume"):
		m.handleResumeCommand(line)
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

// handleResumeCommand 展示可恢复 session；指定 ID 时提示使用 CLI 重启恢复。
func (m *Model) handleResumeCommand(line string) {
	id := strings.TrimSpace(strings.TrimPrefix(line, "/resume"))
	if id != "" {
		m.appendNotice("restart with: codeworld resume " + id)
		return
	}
	sessions, err := m.rt.Store.List()
	if err != nil {
		m.appendError("resume error: " + err.Error())
		return
	}
	if len(sessions) == 0 {
		m.appendNotice("no sessions")
		return
	}
	lines := make([]string, 0, min(len(sessions), 10))
	for i, sess := range sessions {
		if i >= 10 {
			break
		}
		lines = append(lines, fmt.Sprintf("%s model=%s updated=%s", sess.ID, sess.Model, sess.UpdatedAt.Local().Format("2006-01-02 15:04:05")))
	}
	m.appendNotice(strings.Join(lines, "\n"))
}

// appendNotice 封装局部逻辑，保持调用方流程清晰。
func (m *Model) appendNotice(text string) {
	m.items = append(m.items, TranscriptItem{Kind: ItemNotice, Text: text})
}

// appendError 封装局部逻辑，保持调用方流程清晰。
func (m *Model) appendError(text string) {
	m.items = append(m.items, TranscriptItem{Kind: ItemError, Text: text})
}

// matchingSlashCommands 返回匹配当前输入前缀的 slash 命令，数量保持适合单屏展示。
func matchingSlashCommands(prefix string) []string {
	if prefix == "" {
		return nil
	}
	matches := make([]string, 0, len(slashCommands))
	for _, command := range slashCommands {
		if strings.HasPrefix(command, prefix) {
			matches = append(matches, command)
		}
	}
	if prefix == "/" {
		matches = append([]string(nil), slashCommands...)
	}
	sort.Strings(matches)
	return matches
}
