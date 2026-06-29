package model

import "testing"

func TestStreamEventsAccumulateTextAndUsage(t *testing.T) {
	events := []StreamEvent{
		{Kind: StreamEventTextDelta, Delta: "你"},
		{Kind: StreamEventTextDelta, Delta: "好"},
		{Kind: StreamEventUsage, Usage: Usage{InputTokens: 3, OutputTokens: 2, CacheTokens: 1, TotalTokens: 5}},
		{Kind: StreamEventDone, Message: Message{Role: RoleAssistant, Content: "你好"}},
	}

	var text string
	var usage Usage
	var final Message
	for _, event := range events {
		switch event.Kind {
		case StreamEventTextDelta:
			text += event.Delta
		case StreamEventUsage:
			usage = usage.Add(event.Usage)
		case StreamEventDone:
			final = event.Message
		}
	}

	if text != "你好" {
		t.Fatalf("text = %q, want 你好", text)
	}
	if usage != (Usage{InputTokens: 3, OutputTokens: 2, CacheTokens: 1, TotalTokens: 5}) {
		t.Fatalf("usage = %#v, want stream usage", usage)
	}
	if final.Content != "你好" {
		t.Fatalf("final = %#v, want assistant message", final)
	}
}
