package summarizer

import (
	"context"
	"fmt"
	"strings"

	"codeworld/internal/model"
)

type Options struct {
	MaxMessages int
	MaxTokens   int
	KeepRecent  int
	Prompt      string
}

// ShouldSummarize 提供对外可复用的能力，并隐藏内部实现细节。
func ShouldSummarize(messages []model.Message, opts Options) bool {
	maxMessages := opts.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 40
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 64 * 1024
	}
	return len(messages) > maxMessages || estimateTokens(messages) > maxTokens
}

func estimateTokens(messages []model.Message) int {
	bytes := 0
	for _, msg := range messages {
		bytes += len(msg.Content)
		for _, part := range msg.Parts {
			bytes += len(part.Text) + len(part.Data) + len(part.ImageURL)
		}
		for _, call := range msg.ToolCalls {
			bytes += len(call.Name) + len(call.Arguments)
		}
	}
	// A byte-based approximation is deliberately conservative for source code.
	return (bytes + 3) / 4
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

	prompt := buildPrompt(summary, older, opts.Prompt)
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
func buildPrompt(summary string, messages []model.Message, custom string) string {
	var b strings.Builder
	if strings.TrimSpace(custom) == "" {
		custom = "Summarize the older conversation for a coding agent. Preserve user goals, files changed, commands run, decisions, and unresolved tasks."
	}
	b.WriteString(strings.TrimSpace(custom))
	b.WriteByte('\n')
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
		for _, call := range msg.ToolCalls {
			b.WriteString(fmt.Sprintf(" [tool %s args=%s]", call.Name, call.Arguments))
		}
		for _, part := range msg.Parts {
			if part.Type == model.ContentPartImage {
				b.WriteString(" [image " + part.Path + "]")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
