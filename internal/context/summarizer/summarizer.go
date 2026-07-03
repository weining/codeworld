package summarizer

import (
	"context"
	"strings"

	"codeworld/internal/model"
)

type Options struct {
	MaxMessages int
	KeepRecent  int
}

// ShouldSummarize 提供对外可复用的能力，并隐藏内部实现细节。
func ShouldSummarize(messages []model.Message, usage model.Usage, opts Options) bool {
	maxMessages := opts.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 40
	}
	return len(messages) > maxMessages
}

// Summarize 提供对外可复用的能力，并隐藏内部实现细节。
func Summarize(ctx context.Context, client model.Client, summary string, messages []model.Message, opts Options) (string, []model.Message, error) {
	keepRecent := opts.KeepRecent
	if keepRecent <= 0 {
		keepRecent = 20
	}
	if keepRecent > len(messages) {
		keepRecent = len(messages)
	}
	recent := append([]model.Message(nil), messages[len(messages)-keepRecent:]...)
	older := messages[:len(messages)-keepRecent]

	prompt := buildPrompt(summary, older)
	resp, err := client.Generate(ctx, model.GenerateRequest{
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "You summarize coding-agent conversations accurately and concisely."},
			{Role: model.RoleUser, Content: prompt},
		},
	})
	if err != nil {
		return summary, append([]model.Message(nil), messages...), err
	}
	return resp.FinalText, recent, nil
}

// buildPrompt 构建运行所需的数据结构，并在过程中收集必要的上下文。
func buildPrompt(summary string, messages []model.Message) string {
	var b strings.Builder
	b.WriteString("Summarize the older conversation for a coding agent. Preserve user goals, files changed, commands run, decisions, and unresolved tasks.\n")
	if summary != "" {
		b.WriteString("\nPrevious summary:\n")
		b.WriteString(summary)
		b.WriteString("\n")
	}
	b.WriteString("\nOlder messages:\n")
	for _, msg := range messages {
		b.WriteString(string(msg.Role))
		b.WriteString(": ")
		b.WriteString(msg.Content)
		if len(msg.ToolCalls) > 0 {
			b.WriteString(" [tool calls]")
		}
		b.WriteString("\n")
	}
	return b.String()
}
