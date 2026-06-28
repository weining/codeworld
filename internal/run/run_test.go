package run

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"codeworld/internal/agent"
	"codeworld/internal/app"
	"codeworld/internal/model"
	"codeworld/internal/session"
	"codeworld/internal/tools"
)

func TestOnceRunsTurnPrintsFinalTextAndSavesSession(t *testing.T) {
	store := session.NewStore(t.TempDir())
	out := &bytes.Buffer{}
	rt := app.Runtime{
		Out:     out,
		Store:   store,
		Session: session.New("workspace", "deepseek", "deepseek-v4-pro"),
		Runner: agent.Runner{
			Model: &fakeModel{responses: []model.GenerateResponse{
				{FinalText: "done", Usage: model.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}},
			}},
			Tools:    tools.NewRegistry(nil, nil),
			MaxSteps: 3,
		},
	}

	err := Once(context.Background(), &rt, "inspect")
	if err != nil {
		t.Fatalf("Once returned error: %v", err)
	}
	if !strings.Contains(out.String(), "done\n") {
		t.Fatalf("output = %q, want final text", out.String())
	}
	saved, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if len(saved.Messages) != 2 {
		t.Fatalf("saved messages = %#v, want user and assistant", saved.Messages)
	}
	if saved.Messages[0].Role != "user" || saved.Messages[0].Content != "inspect" {
		t.Fatalf("first saved message = %#v, want user inspect", saved.Messages[0])
	}
	if saved.Messages[1].Role != "assistant" || saved.Messages[1].Content != "done" {
		t.Fatalf("second saved message = %#v, want assistant done", saved.Messages[1])
	}
	if saved.Usage.TotalTokens != 5 || saved.Usage.InputTokens != 3 || saved.Usage.OutputTokens != 2 {
		t.Fatalf("saved usage = %#v, want model usage", saved.Usage)
	}
}

func TestOnceRejectsEmptyInput(t *testing.T) {
	err := Once(context.Background(), &app.Runtime{Out: &bytes.Buffer{}}, "")
	if err == nil || !strings.Contains(err.Error(), "run input is empty") {
		t.Fatalf("err = %v, want empty input error", err)
	}
}

type fakeModel struct {
	responses []model.GenerateResponse
	requests  []model.GenerateRequest
}

func (f *fakeModel) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	if err := ctx.Err(); err != nil {
		return model.GenerateResponse{}, err
	}
	f.requests = append(f.requests, req)
	if len(f.requests) > len(f.responses) {
		return model.GenerateResponse{FinalText: "fallback"}, nil
	}
	return f.responses[len(f.requests)-1], nil
}
