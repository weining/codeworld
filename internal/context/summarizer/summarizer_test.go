package summarizer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"codeworld/internal/model"
)

func TestShouldSummarizeWhenMessageCountExceedsThreshold(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "1"},
		{Role: model.RoleAssistant, Content: "2"},
		{Role: model.RoleUser, Content: "3"},
	}
	if !ShouldSummarize(messages, model.Usage{}, Options{MaxMessages: 2}) {
		t.Fatalf("ShouldSummarize returned false, want true")
	}
	if ShouldSummarize(messages[:2], model.Usage{}, Options{MaxMessages: 2}) {
		t.Fatalf("ShouldSummarize returned true at threshold")
	}
}

func TestSummarizeKeepsRecentMessagesAndStoresSummary(t *testing.T) {
	client := &fakeClient{response: model.GenerateResponse{FinalText: "summary text"}}
	messages := []model.Message{
		{Role: model.RoleUser, Content: "old user"},
		{Role: model.RoleAssistant, Content: "old assistant"},
		{Role: model.RoleUser, Content: "recent user"},
		{Role: model.RoleAssistant, Content: "recent assistant"},
	}

	summary, recent, err := Summarize(context.Background(), client, "previous summary", messages, Options{KeepRecent: 2})
	if err != nil {
		t.Fatalf("Summarize returned error: %v", err)
	}
	if summary != "summary text" {
		t.Fatalf("summary = %q, want summary text", summary)
	}
	if len(recent) != 2 || recent[0].Content != "recent user" || recent[1].Content != "recent assistant" {
		t.Fatalf("recent = %#v, want last two messages", recent)
	}
	if len(client.requests) != 1 {
		t.Fatalf("model requests = %d, want 1", len(client.requests))
	}
	if !strings.Contains(client.requests[0].Messages[len(client.requests[0].Messages)-1].Content, "previous summary") {
		t.Fatalf("summary prompt missing previous summary: %#v", client.requests[0].Messages)
	}
}

func TestSummarizeFailurePreservesMessages(t *testing.T) {
	client := &fakeClient{err: errors.New("boom")}
	messages := []model.Message{
		{Role: model.RoleUser, Content: "old"},
		{Role: model.RoleAssistant, Content: "recent"},
	}

	summary, recent, err := Summarize(context.Background(), client, "existing", messages, Options{KeepRecent: 1})
	if err == nil {
		t.Fatalf("Summarize returned nil error")
	}
	if summary != "existing" {
		t.Fatalf("summary = %q, want existing", summary)
	}
	if len(recent) != len(messages) {
		t.Fatalf("recent = %#v, want original messages preserved", recent)
	}
}

type fakeClient struct {
	response model.GenerateResponse
	err      error
	requests []model.GenerateRequest
}

func (f *fakeClient) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	if err := ctx.Err(); err != nil {
		return model.GenerateResponse{}, err
	}
	f.requests = append(f.requests, req)
	return f.response, f.err
}
