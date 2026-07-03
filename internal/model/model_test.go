package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStreamEventsAccumulateTextAndUsage 验证对应场景的行为，避免后续改动破坏既有约束。
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

// TestImagePartFromFileBuildsDataURL 验证图片文件会转换为可发送给 provider 的 base64 数据。
func TestImagePartFromFileBuildsDataURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.png")
	if err := os.WriteFile(path, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	part, err := ImagePartFromFile(path)
	if err != nil {
		t.Fatalf("ImagePartFromFile returned error: %v", err)
	}
	if part.Type != ContentPartImage || part.MediaType != "image/png" || !strings.HasPrefix(part.ImageURL, "data:image/png;base64,") || part.Data == "" {
		t.Fatalf("part = %#v, want png image data", part)
	}
}
