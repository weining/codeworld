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
	In                io.Reader
	Out               io.Writer
	Runner            agent.Runner
	Store             session.Store
	Session           session.Session
	Messages          []model.Message
	Usage             model.Usage
	ShowTerminalTitle bool
	Diff              func(context.Context) (string, error)
}

func (r *REPL) Run(ctx context.Context) error {
	if r.In == nil {
		return fmt.Errorf("input is nil")
	}
	if r.Out == nil {
		return fmt.Errorf("output is nil")
	}

	reader := bufio.NewReader(r.In)
	r.bindConfirmerInput(reader)
	r.restoreUsageFromSession()
	r.Runner.Reporter = r
	fmt.Fprintln(r.Out, "codeworld")
	fmt.Fprintln(r.Out, "Type /help for commands.")
	r.writeTerminalTitle()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Fprintf(r.Out, "codeworld [%s]> ", formatUsage(r.Usage))
		raw, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && raw == "" {
				return nil
			}
			if err != io.EOF {
				return err
			}
		}
		line := strings.TrimSpace(raw)
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
		r.Usage = r.Usage.Add(result.Usage)
		r.writeTerminalTitle()
		r.Messages = historyMessages(result.Messages)
		fmt.Fprintln(r.Out, result.FinalText)
		r.syncSession()
		r.saveSession()
	}
}

func (r *REPL) bindConfirmerInput(reader *bufio.Reader) {
	switch confirmer := r.Runner.Confirmer.(type) {
	case Confirmer:
		confirmer.In = reader
		r.Runner.Confirmer = confirmer
	case *Confirmer:
		confirmer.In = reader
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
		r.writeTerminalTitle()
		r.saveSession()
	case line == "/status":
		fmt.Fprintf(r.Out, "workspace=%s provider=%s model=%s messages=%d tokens %s\n", r.Session.Workspace, r.Session.Provider, r.Session.Model, len(r.Messages), formatUsage(r.Usage))
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
		r.Usage = model.Usage{}
		r.Session.Usage = session.Usage{}
		r.writeTerminalTitle()
		fmt.Fprintln(r.Out, "cleared")
		r.saveSession()
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
			ToolCalls:  toSessionToolCalls(msg.ToolCalls),
		})
	}
	r.Session.Usage = toSessionUsage(r.Usage)
}

func toSessionToolCalls(calls []model.ToolCall) []session.ToolCall {
	out := make([]session.ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, session.ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: call.Arguments,
		})
	}
	return out
}

func (r *REPL) saveSession() {
	if err := r.Store.SaveCurrent(r.Session); err != nil {
		fmt.Fprintf(r.Out, "session save error: %v\n", err)
	}
}

func (r *REPL) restoreUsageFromSession() {
	if !r.Usage.IsZero() {
		return
	}
	r.Usage = model.Usage{
		InputTokens:  r.Session.Usage.InputTokens,
		OutputTokens: r.Session.Usage.OutputTokens,
		CacheTokens:  r.Session.Usage.CacheTokens,
		TotalTokens:  r.Session.Usage.TotalTokens,
	}
}

func toSessionUsage(usage model.Usage) session.Usage {
	return session.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CacheTokens:  usage.CacheTokens,
		TotalTokens:  usage.TotalTokens,
	}
}

func (r *REPL) writeTerminalTitle() {
	if !r.ShowTerminalTitle {
		return
	}
	fmt.Fprintf(r.Out, "\x1b]0;%s\x07", terminalTitle(r.currentModelName(), r.Usage))
}

func (r *REPL) currentModelName() string {
	switch {
	case r.Session.Model != "":
		return r.Session.Model
	case r.Runner.ModelName != "":
		return r.Runner.ModelName
	default:
		return "unknown"
	}
}

func terminalTitle(modelName string, usage model.Usage) string {
	return fmt.Sprintf("codeworld | %s | %s", modelName, formatUsage(usage))
}

func formatUsage(usage model.Usage) string {
	return fmt.Sprintf("input=%d output=%d cache=%d total=%d", usage.InputTokens, usage.OutputTokens, usage.CacheTokens, usage.TotalTokens)
}

func (r *REPL) ReportTool(ctx context.Context, event agent.ToolEvent) {
	switch event.Status {
	case agent.ToolEventStart:
		fmt.Fprintf(r.Out, "tool> %s target=%s risk=%s\n", event.Name, event.Request.Target, event.Request.Risk)
	case agent.ToolEventSuccess:
		fmt.Fprintf(r.Out, "tool< %s ok\n", event.Name)
	case agent.ToolEventDenied:
		fmt.Fprintf(r.Out, "tool< %s denied: %s\n", event.Name, event.Error)
	case agent.ToolEventError:
		fmt.Fprintf(r.Out, "tool< %s error: %s\n", event.Name, event.Error)
	}
}

func historyMessages(messages []model.Message) []model.Message {
	history := make([]model.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == model.RoleSystem {
			continue
		}
		history = append(history, msg)
	}
	return history
}

type Confirmer struct {
	In  io.Reader
	Out io.Writer
}

func (c Confirmer) Confirm(ctx context.Context, req permissions.Request, decision permissions.Decision) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c.In == nil {
		return false, fmt.Errorf("confirmation input is nil")
	}
	if c.Out == nil {
		return false, fmt.Errorf("confirmation output is nil")
	}

	fmt.Fprintf(c.Out, "\nPermission required: %s\nTarget: %s\nRisk: %s\nReason: %s\n", req.Action, req.Target, req.Risk, req.Reason)
	if req.Preview != "" {
		fmt.Fprintf(c.Out, "Preview:\n%s\n", req.Preview)
	}
	fmt.Fprint(c.Out, "Allow? [y/N] ")

	reader, ok := c.In.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(c.In)
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "y"), nil
}
