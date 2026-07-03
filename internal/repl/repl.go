package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"codeworld/internal/agent"
	"codeworld/internal/approvals"
	"codeworld/internal/context/summarizer"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/session"
)

type REPL struct {
	In                 io.Reader
	Out                io.Writer
	Runner             agent.Runner
	Store              session.Store
	Session            session.Session
	Messages           []model.Message
	Usage              model.Usage
	SummaryMaxMessages int
	ShowStatusLine     bool
	ShowTerminalTitle  bool
	Diff               func(context.Context) (string, error)
}

// Run 执行主要流程，并把运行结果或错误返回给调用方。
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
		r.writeStatusLine()
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
		r.maybeSummarize(ctx)
	}
}

// bindConfirmerInput 封装局部逻辑，保持调用方流程清晰。
func (r *REPL) bindConfirmerInput(reader *bufio.Reader) {
	switch confirmer := r.Runner.Confirmer.(type) {
	case Confirmer:
		confirmer.In = reader
		confirmer.Approvals = &r.Session.Approvals
		r.Runner.Confirmer = confirmer
	case *Confirmer:
		confirmer.In = reader
		confirmer.Approvals = &r.Session.Approvals
	}
}

// handleCommand 封装局部逻辑，保持调用方流程清晰。
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
		fmt.Fprintf(r.Out, "workspace=%s provider=%s model=%s messages=%d tokens %s approvals=%d\n", r.Session.Workspace, r.Session.Provider, r.Session.Model, len(r.Messages), formatUsage(r.Usage), len(r.Session.Approvals))
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
	case line == "/goal":
		if r.Session.Goal == "" {
			fmt.Fprintln(r.Out, "goal not set")
			return false
		}
		fmt.Fprintln(r.Out, r.Session.Goal)
	case strings.HasPrefix(line, "/goal "):
		next := strings.TrimSpace(strings.TrimPrefix(line, "/goal "))
		if next == "clear" {
			r.Session.Goal = ""
			fmt.Fprintln(r.Out, "goal cleared")
		} else {
			r.Session.Goal = next
			fmt.Fprintf(r.Out, "goal=%s\n", next)
		}
		r.saveSession()
	case line == "/plan":
		r.Session.Mode = "plan"
		fmt.Fprintln(r.Out, "mode=plan")
		r.saveSession()
	case line == "/plan off":
		r.Session.Mode = ""
		fmt.Fprintln(r.Out, "mode=default")
		r.saveSession()
	case line == "/compact":
		r.compact(ctx)
	case line == "/clear":
		r.Messages = nil
		r.Session.Messages = nil
		r.Session.Approvals = nil
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

// compact 手动压缩 REPL 历史，复用自动摘要使用的 summarizer。
func (r *REPL) compact(ctx context.Context) {
	if len(r.Messages) == 0 {
		fmt.Fprintln(r.Out, "nothing to compact")
		return
	}
	if r.Runner.Model == nil {
		fmt.Fprintln(r.Out, "compact unavailable: model is required")
		return
	}
	before := len(r.Messages)
	summary, recent, err := summarizer.Summarize(ctx, r.Runner.Model, r.Session.Summary, r.Messages, summarizer.Options{
		MaxMessages: r.SummaryMaxMessages,
		KeepRecent:  20,
	})
	if err != nil {
		fmt.Fprintf(r.Out, "compact error: %v\n", err)
		return
	}
	r.Session.Summary = summary
	r.Messages = recent
	r.syncSession()
	r.saveSession()
	fmt.Fprintf(r.Out, "compacted messages=%d kept=%d\n", before, len(recent))
}

// syncSession 封装局部逻辑，保持调用方流程清晰。
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

// toSessionToolCalls 在不同层的数据结构之间做显式转换。
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

// saveSession 持久化当前状态，并处理路径、权限或归档细节。
func (r *REPL) saveSession() {
	if err := r.Store.SaveCurrent(r.Session); err != nil {
		fmt.Fprintf(r.Out, "session save error: %v\n", err)
	}
}

// maybeSummarize 封装局部逻辑，保持调用方流程清晰。
func (r *REPL) maybeSummarize(ctx context.Context) {
	if r.Runner.Model == nil || !summarizer.ShouldSummarize(r.Messages, r.Usage, summarizer.Options{MaxMessages: r.SummaryMaxMessages}) {
		return
	}
	summary, recent, err := summarizer.Summarize(ctx, r.Runner.Model, r.Session.Summary, r.Messages, summarizer.Options{
		MaxMessages: r.SummaryMaxMessages,
		KeepRecent:  20,
	})
	if err != nil {
		fmt.Fprintf(r.Out, "summary warning: %v\n", err)
		return
	}
	r.Session.Summary = summary
	r.Messages = recent
	r.syncSession()
	r.saveSession()
}

// restoreUsageFromSession 封装局部逻辑，保持调用方流程清晰。
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

// toSessionUsage 在不同层的数据结构之间做显式转换。
func toSessionUsage(usage model.Usage) session.Usage {
	return session.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CacheTokens:  usage.CacheTokens,
		TotalTokens:  usage.TotalTokens,
	}
}

// writeTerminalTitle 写入输出数据，并保证必要的目录或权限约束。
func (r *REPL) writeTerminalTitle() {
	if !r.ShowTerminalTitle {
		return
	}
	fmt.Fprintf(r.Out, "\x1b]0;%s\x07", terminalTitle(r.currentModelName(), r.Usage))
}

// writeStatusLine 写入输出数据，并保证必要的目录或权限约束。
func (r *REPL) writeStatusLine() {
	if !r.ShowStatusLine {
		return
	}
	fmt.Fprintf(r.Out, "status workspace=%s provider=%s model=%s messages=%d approvals=%d tokens %s\n",
		r.Session.Workspace,
		r.Session.Provider,
		r.currentModelName(),
		len(r.Messages),
		len(r.Session.Approvals),
		formatUsage(r.Usage),
	)
}

// currentModelName 封装局部逻辑，保持调用方流程清晰。
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

// terminalTitle 封装局部逻辑，保持调用方流程清晰。
func terminalTitle(modelName string, usage model.Usage) string {
	return fmt.Sprintf("codeworld | %s | %s", modelName, formatUsage(usage))
}

// formatUsage 将内部数据格式化为面向用户或模型的文本。
func formatUsage(usage model.Usage) string {
	return fmt.Sprintf("input=%d output=%d cache=%d total=%d", usage.InputTokens, usage.OutputTokens, usage.CacheTokens, usage.TotalTokens)
}

// ReportTool 把工具执行状态转换为用户可见的进度事件。
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

// historyMessages 封装局部逻辑，保持调用方流程清晰。
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
	In        io.Reader
	Out       io.Writer
	Approvals *[]session.Approval
}

// Confirm 向用户确认权限请求，并把选择返回给 agent 流程。
func (c Confirmer) Confirm(ctx context.Context, req permissions.Request, decision permissions.Decision) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c.isApproved(req) {
		return true, nil
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
	if req.Action == permissions.ActionShell {
		fmt.Fprint(c.Out, "Allow? [y/N/a=session] ")
	} else {
		fmt.Fprint(c.Out, "Allow? [y/N] ")
	}

	reader, ok := c.In.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(c.In)
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.TrimSpace(line)
	if strings.EqualFold(answer, "a") && req.Action == permissions.ActionShell {
		c.addApproval(req)
		return true, nil
	}
	return strings.EqualFold(answer, "y"), nil
}

// isApproved 判断输入是否满足特定条件，并用于后续分支决策。
func (c Confirmer) isApproved(req permissions.Request) bool {
	if req.Action != permissions.ActionShell || c.Approvals == nil {
		return false
	}
	return approvalSet(*c.Approvals).Allows(req.Target)
}

// addApproval 封装局部逻辑，保持调用方流程清晰。
func (c Confirmer) addApproval(req permissions.Request) {
	if req.Action != permissions.ActionShell || c.Approvals == nil {
		return
	}
	set := approvalSet(*c.Approvals)
	set.Add(req.Target)
	*c.Approvals = approvalsToSession(set)
}

// approvalSet 封装局部逻辑，保持调用方流程清晰。
func approvalSet(items []session.Approval) approvals.Set {
	set := approvals.Set{}
	for _, item := range items {
		if item.Kind == "shell" {
			set.Add(item.Command)
		}
	}
	return set
}

// approvalsToSession 封装局部逻辑，保持调用方流程清晰。
func approvalsToSession(set approvals.Set) []session.Approval {
	items := make([]session.Approval, 0, len(set.Commands))
	for _, command := range set.Commands {
		items = append(items, session.Approval{Kind: "shell", Command: command})
	}
	return items
}
