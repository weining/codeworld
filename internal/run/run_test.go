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

// TestOnceRunsTurnPrintsFinalTextAndSavesSession 验证对应场景的行为，避免后续改动破坏既有约束。
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

// TestOnceRejectsEmptyInput 验证对应场景的行为，避免后续改动破坏既有约束。
func TestOnceRejectsEmptyInput(t *testing.T) {
	err := Once(context.Background(), &app.Runtime{Out: &bytes.Buffer{}}, "")
	if err == nil || !strings.Contains(err.Error(), "run input is empty") {
		t.Fatalf("err = %v, want empty input error", err)
	}
}

// TestOnceWithImagesSendsContentParts 验证非交互 run 可以把图片块传入模型请求。
func TestOnceWithImagesSendsContentParts(t *testing.T) {
	store := session.NewStore(t.TempDir())
	out := &bytes.Buffer{}
	client := &fakeModel{responses: []model.GenerateResponse{{FinalText: "described"}}}
	rt := app.Runtime{
		Out:     out,
		Store:   store,
		Session: session.New("workspace", "openai", "gpt-4.1"),
		Runner: agent.Runner{
			Model:    client,
			Tools:    tools.NewRegistry(nil, nil),
			MaxSteps: 3,
		},
	}

	err := OnceWithImages(context.Background(), &rt, "describe", []model.ContentPart{
		{Type: model.ContentPartImage, ImageURL: "data:image/png;base64,AAAA", MediaType: "image/png"},
	})
	if err != nil {
		t.Fatalf("OnceWithImages returned error: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("model calls = %d, want 1", len(client.requests))
	}
	got := client.requests[0].Messages[len(client.requests[0].Messages)-1]
	if got.Content != "describe" || len(got.Parts) != 2 || got.Parts[1].Type != model.ContentPartImage {
		t.Fatalf("user message = %#v, want text and image parts", got)
	}
	saved, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if len(saved.Messages) == 0 || len(saved.Messages[0].Parts) != 2 {
		t.Fatalf("saved messages = %#v, want content parts", saved.Messages)
	}
	if saved.Messages[0].Parts[1].Data != "" || saved.Messages[0].Parts[1].ImageURL != "" {
		t.Fatalf("saved image part = %#v, want metadata without base64 payload", saved.Messages[0].Parts[1])
	}
}

// TestOnceUsesSessionShellApprovalWithoutPrompt 验证对应场景的行为，避免后续改动破坏既有约束。
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

// TestOnceSummarizesWhenMessageCountExceedsThreshold 验证对应场景的行为，避免后续改动破坏既有约束。
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

func TestOnceSavesPartialTurnWhenAgentFailsAfterTool(t *testing.T) {
	store := session.NewStore(t.TempDir())
	tool := &fakeShellTool{}
	rt := app.Runtime{
		Out:     &bytes.Buffer{},
		Store:   store,
		Session: session.New("workspace", "deepseek", "deepseek-v4-pro"),
		Runner: agent.Runner{
			Model:    &fakeModel{responses: []model.GenerateResponse{{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "shell", Arguments: json.RawMessage(`{}`)}}}}},
			Tools:    tools.NewRegistry([]tools.Tool{tool}, nil),
			Policy:   permissions.ModePolicy{Mode: permissions.ModeFullAccess},
			MaxSteps: 1,
		},
	}

	if err := Once(context.Background(), &rt, "change file"); err == nil {
		t.Fatal("Once succeeded, want max steps error")
	}
	saved, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if len(saved.Messages) != 3 || saved.Messages[0].Content != "change file" || saved.Messages[2].Role != "tool" {
		t.Fatalf("saved partial messages = %#v", saved.Messages)
	}
}

type fakeModel struct {
	responses []model.GenerateResponse
	requests  []model.GenerateRequest
}

type fakeShellTool struct {
	executeCalls int
}

// Definition 是测试辅助函数，用于复用测试准备或断言逻辑。
func (f *fakeShellTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "shell", Description: "shell", InputSchema: map[string]any{"type": "object"}}
}

// PermissionRequest 是测试辅助函数，用于复用测试准备或断言逻辑。
func (f *fakeShellTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Action: permissions.ActionShell, Risk: permissions.RiskExecute, Target: "go test ./...", Reason: "run tests"}, nil
}

// Execute 是测试辅助函数，用于复用测试准备或断言逻辑。
func (f *fakeShellTool) Execute(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	f.executeCalls++
	return tools.Result{Content: "ok"}, nil
}

// Generate 是测试辅助函数，用于复用测试准备或断言逻辑。
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
