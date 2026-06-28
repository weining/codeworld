package run

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"codeworld/internal/agent"
	"codeworld/internal/app"
	"codeworld/internal/config"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
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

func TestOnceUsesSessionShellApprovalWithoutPrompt(t *testing.T) {
	store := session.NewStore(t.TempDir())
	tool := &fakeShellTool{}
	rt := app.Runtime{
		Out:   &bytes.Buffer{},
		Store: store,
		Session: session.Session{
			ID:        "session-test",
			Workspace: "workspace",
			Provider:  "deepseek",
			Model:     "deepseek-v4-pro",
			Approvals: []session.Approval{{Kind: "shell", Command: "go test ./..."}},
		},
		Runner: agent.Runner{
			Model: &fakeModel{responses: []model.GenerateResponse{
				{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "shell", Arguments: json.RawMessage(`{}`)}}},
				{FinalText: "tested"},
			}},
			Tools:    tools.NewRegistry([]tools.Tool{tool}, nil),
			Policy:   permissions.ConservativePolicy{},
			MaxSteps: 3,
		},
	}

	err := Once(context.Background(), &rt, "run tests")
	if err != nil {
		t.Fatalf("Once returned error: %v", err)
	}
	if tool.executeCalls != 1 {
		t.Fatalf("execute calls = %d, want approved shell execution", tool.executeCalls)
	}
}

func TestOnceSummarizesWhenMessageCountExceedsThreshold(t *testing.T) {
	store := session.NewStore(t.TempDir())
	out := &bytes.Buffer{}
	rt := app.Runtime{
		Out:     out,
		Store:   store,
		Session: session.New("workspace", "deepseek", "deepseek-v4-pro"),
		Config:  config.Config{SummaryMaxMessages: 1},
		Runner: agent.Runner{
			Model: &fakeModel{responses: []model.GenerateResponse{
				{FinalText: "done"},
				{FinalText: "summary text"},
			}},
			Tools:    tools.NewRegistry(nil, nil),
			MaxSteps: 3,
		},
	}

	err := Once(context.Background(), &rt, "inspect")
	if err != nil {
		t.Fatalf("Once returned error: %v", err)
	}
	saved, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if saved.Summary != "summary text" {
		t.Fatalf("summary = %q, want summary text", saved.Summary)
	}
	if len(saved.Messages) != 2 {
		t.Fatalf("saved messages = %#v, want recent messages retained", saved.Messages)
	}
}

type fakeModel struct {
	responses []model.GenerateResponse
	requests  []model.GenerateRequest
}

type fakeShellTool struct {
	executeCalls int
}

func (f *fakeShellTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "shell", Description: "shell", InputSchema: map[string]any{"type": "object"}}
}

func (f *fakeShellTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Action: permissions.ActionShell, Risk: permissions.RiskExecute, Target: " go   test ./... ", Reason: "run tests"}, nil
}

func (f *fakeShellTool) Execute(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	f.executeCalls++
	return tools.Result{Content: "ok"}, nil
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
