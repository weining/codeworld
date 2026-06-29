package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/tools"
)

func TestRunTurnBuildsRequestWithHistoryToolsAndReturnsFinalText(t *testing.T) {
	client := &fakeClient{responses: []model.GenerateResponse{{FinalText: "answer"}}}
	registry := tools.NewRegistry([]tools.Tool{newFakeTool("echo", permissions.ActionRead, permissions.RiskRead)}, nil)
	runner := Runner{
		Model:        client,
		Tools:        registry,
		Policy:       permissions.ConservativePolicy{},
		ModelName:    "test-model",
		MaxSteps:     3,
		SystemPrompt: "system instructions",
	}
	history := []model.Message{
		{Role: model.RoleUser, Content: "earlier"},
		{Role: model.RoleAssistant, Content: "previous"},
	}

	result, err := runner.RunTurn(context.Background(), history, "now")
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if result.FinalText != "answer" {
		t.Fatalf("FinalText = %q, want answer", result.FinalText)
	}
	if len(client.requests) != 1 {
		t.Fatalf("model calls = %d, want 1", len(client.requests))
	}
	req := client.requests[0]
	if req.Model != "test-model" {
		t.Fatalf("request model = %q, want test-model", req.Model)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "echo" {
		t.Fatalf("request tools = %#v, want echo definition", req.Tools)
	}
	wantMessages := []model.Message{
		{Role: model.RoleSystem, Content: "system instructions"},
		{Role: model.RoleUser, Content: "earlier"},
		{Role: model.RoleAssistant, Content: "previous"},
		{Role: model.RoleUser, Content: "now"},
	}
	if !messagesEqual(req.Messages, wantMessages) {
		t.Fatalf("request messages = %#v, want %#v", req.Messages, wantMessages)
	}
	if last := result.Messages[len(result.Messages)-1]; last.Role != model.RoleAssistant || last.Content != "answer" {
		t.Fatalf("last result message = %#v, want assistant answer", last)
	}
}

func TestRunTurnExecutesToolAndSendsResultBackToModel(t *testing.T) {
	client := &fakeClient{responses: []model.GenerateResponse{
		{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)}}},
		{FinalText: "done"},
	}}
	tool := newFakeTool("echo", permissions.ActionRead, permissions.RiskRead)
	registry := tools.NewRegistry([]tools.Tool{tool}, nil)
	runner := Runner{
		Model:     client,
		Tools:     registry,
		Policy:    permissions.ConservativePolicy{},
		ModelName: "test-model",
		MaxSteps:  3,
	}

	result, err := runner.RunTurn(context.Background(), nil, "start")
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if result.FinalText != "done" {
		t.Fatalf("FinalText = %q, want done", result.FinalText)
	}
	if len(client.requests) != 2 {
		t.Fatalf("model calls = %d, want 2", len(client.requests))
	}
	if tool.executeCalls != 1 {
		t.Fatalf("tool execute calls = %d, want 1", tool.executeCalls)
	}
	secondMessages := client.requests[1].Messages
	toolMessage := secondMessages[len(secondMessages)-1]
	if toolMessage.Role != model.RoleTool || toolMessage.ToolCallID != "call-1" {
		t.Fatalf("last message = %#v, want tool result for call-1", toolMessage)
	}
	if !strings.Contains(toolMessage.Content, `"content":"hello"`) {
		t.Fatalf("tool message content = %q, want encoded tool result", toolMessage.Content)
	}
}

func TestRunTurnAccumulatesUsageAcrossModelCalls(t *testing.T) {
	client := &fakeClient{responses: []model.GenerateResponse{
		{
			ToolCalls: []model.ToolCall{{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)}},
			Usage:     model.Usage{InputTokens: 10, OutputTokens: 2, CacheTokens: 4, TotalTokens: 12},
		},
		{
			FinalText: "done",
			Usage:     model.Usage{InputTokens: 20, OutputTokens: 5, CacheTokens: 6, TotalTokens: 25},
		},
	}}
	tool := newFakeTool("echo", permissions.ActionRead, permissions.RiskRead)
	runner := Runner{
		Model:     client,
		Tools:     tools.NewRegistry([]tools.Tool{tool}, nil),
		Policy:    permissions.ConservativePolicy{},
		ModelName: "test-model",
		MaxSteps:  3,
	}

	result, err := runner.RunTurn(context.Background(), nil, "start")
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	want := model.Usage{InputTokens: 30, OutputTokens: 7, CacheTokens: 10, TotalTokens: 37}
	if result.Usage != want {
		t.Fatalf("usage = %#v, want %#v", result.Usage, want)
	}
}

func TestRunTurnStreamFallsBackToGenerateAndEmitsEvents(t *testing.T) {
	client := &fakeClient{responses: []model.GenerateResponse{{
		FinalText: "stream fallback",
		Usage:     model.Usage{InputTokens: 4, OutputTokens: 2, CacheTokens: 1, TotalTokens: 6},
	}}}
	runner := Runner{
		Model:     client,
		Tools:     tools.NewRegistry(nil, nil),
		Policy:    permissions.ConservativePolicy{},
		ModelName: "test-model",
		MaxSteps:  1,
	}
	var events []TurnEvent

	result, err := runner.RunTurnStream(context.Background(), nil, "hello", func(event TurnEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("RunTurnStream returned error: %v", err)
	}
	if result.FinalText != "stream fallback" {
		t.Fatalf("FinalText = %q, want stream fallback", result.FinalText)
	}
	if len(events) != 2 {
		t.Fatalf("events = %#v, want assistant done and usage", events)
	}
	if events[0].Kind != TurnEventAssistantDone || events[0].Text != "stream fallback" {
		t.Fatalf("assistant event = %#v, want assistant done", events[0])
	}
	if events[1].Kind != TurnEventUsage || events[1].Usage.TotalTokens != 6 {
		t.Fatalf("usage event = %#v, want usage", events[1])
	}
}

func TestRunTurnStreamUsesStreamingClient(t *testing.T) {
	client := &fakeStreamingClient{events: []model.StreamEvent{
		{Kind: model.StreamEventTextDelta, Delta: "你"},
		{Kind: model.StreamEventTextDelta, Delta: "好"},
		{Kind: model.StreamEventUsage, Usage: model.Usage{InputTokens: 4, OutputTokens: 2, TotalTokens: 6}},
		{Kind: model.StreamEventDone, Message: model.Message{Role: model.RoleAssistant, Content: "你好"}},
	}}
	runner := Runner{
		Model:     client,
		Tools:     tools.NewRegistry(nil, nil),
		Policy:    permissions.ConservativePolicy{},
		ModelName: "test-model",
		MaxSteps:  1,
	}
	var events []TurnEvent

	result, err := runner.RunTurnStream(context.Background(), nil, "hello", func(event TurnEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("RunTurnStream returned error: %v", err)
	}
	if result.FinalText != "你好" {
		t.Fatalf("FinalText = %q, want 你好", result.FinalText)
	}
	if len(events) != 4 {
		t.Fatalf("events = %#v, want two deltas, usage, done", events)
	}
	if events[0].Kind != TurnEventAssistantDelta || events[0].Text != "你" {
		t.Fatalf("first event = %#v, want delta 你", events[0])
	}
	if events[1].Kind != TurnEventAssistantDelta || events[1].Text != "好" {
		t.Fatalf("second event = %#v, want delta 好", events[1])
	}
	if events[2].Kind != TurnEventUsage || events[2].Usage.TotalTokens != 6 {
		t.Fatalf("usage event = %#v, want usage", events[2])
	}
	if events[3].Kind != TurnEventAssistantDone || events[3].Text != "你好" {
		t.Fatalf("done event = %#v, want done", events[3])
	}
}

func TestRunTurnReportsToolProgress(t *testing.T) {
	client := &fakeClient{responses: []model.GenerateResponse{
		{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)}}},
		{FinalText: "done"},
	}}
	reporter := &fakeToolReporter{}
	runner := Runner{
		Model:     client,
		Tools:     tools.NewRegistry([]tools.Tool{newFakeTool("echo", permissions.ActionRead, permissions.RiskRead)}, nil),
		Policy:    permissions.ConservativePolicy{},
		Reporter:  reporter,
		ModelName: "test-model",
		MaxSteps:  3,
	}

	_, err := runner.RunTurn(context.Background(), nil, "start")
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if len(reporter.events) != 2 {
		t.Fatalf("reported events = %#v, want start and success", reporter.events)
	}
	if reporter.events[0].Status != ToolEventStart || reporter.events[0].Name != "echo" || reporter.events[0].Request.Target != "echo" {
		t.Fatalf("start event = %#v, want echo start", reporter.events[0])
	}
	if reporter.events[1].Status != ToolEventSuccess || reporter.events[1].Name != "echo" {
		t.Fatalf("success event = %#v, want echo success", reporter.events[1])
	}
}

func TestRunTurnExecutesToolWhenMessageHasContentAndToolCalls(t *testing.T) {
	client := &fakeClient{responses: []model.GenerateResponse{
		{
			Message: model.Message{
				Content:   "I need to inspect first.",
				ToolCalls: []model.ToolCall{{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)}},
			},
		},
		{FinalText: "done"},
	}}
	tool := newFakeTool("echo", permissions.ActionRead, permissions.RiskRead)
	runner := Runner{
		Model:     client,
		Tools:     tools.NewRegistry([]tools.Tool{tool}, nil),
		Policy:    permissions.ConservativePolicy{},
		ModelName: "test-model",
		MaxSteps:  3,
	}

	result, err := runner.RunTurn(context.Background(), nil, "start")
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if result.FinalText != "done" {
		t.Fatalf("FinalText = %q, want done", result.FinalText)
	}
	if tool.executeCalls != 1 {
		t.Fatalf("tool execute calls = %d, want 1", tool.executeCalls)
	}
}

func TestRunTurnReturnsPermissionDenialToModel(t *testing.T) {
	client := &fakeClient{responses: []model.GenerateResponse{
		{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "write", Arguments: json.RawMessage(`{"path":"notes.txt"}`)}}},
		{FinalText: "not changed"},
	}}
	tool := newFakeTool("write", permissions.ActionWrite, permissions.RiskWrite)
	confirmer := &fakeConfirmer{allowed: false}
	runner := Runner{
		Model:     client,
		Tools:     tools.NewRegistry([]tools.Tool{tool}, nil),
		Policy:    permissions.ConservativePolicy{},
		Confirmer: confirmer,
		ModelName: "test-model",
		MaxSteps:  3,
	}

	result, err := runner.RunTurn(context.Background(), nil, "change file")
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if result.FinalText != "not changed" {
		t.Fatalf("FinalText = %q, want not changed", result.FinalText)
	}
	if confirmer.calls != 1 {
		t.Fatalf("confirm calls = %d, want 1", confirmer.calls)
	}
	if tool.executeCalls != 0 {
		t.Fatalf("tool execute calls = %d, want 0", tool.executeCalls)
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	if !strings.Contains(last.Content, "permission denied by user") {
		t.Fatalf("tool denial content = %q, want permission denied by user", last.Content)
	}
}

func TestRunTurnRejectsUnknownPermissionDecision(t *testing.T) {
	client := &fakeClient{responses: []model.GenerateResponse{
		{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "write", Arguments: json.RawMessage(`{"path":"notes.txt"}`)}}},
		{FinalText: "not changed"},
	}}
	tool := newFakeTool("write", permissions.ActionWrite, permissions.RiskWrite)
	runner := Runner{
		Model:     client,
		Tools:     tools.NewRegistry([]tools.Tool{tool}, nil),
		Policy:    fakePolicy{decision: permissions.Decision{}},
		ModelName: "test-model",
		MaxSteps:  3,
	}

	result, err := runner.RunTurn(context.Background(), nil, "change file")
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if result.FinalText != "not changed" {
		t.Fatalf("FinalText = %q, want not changed", result.FinalText)
	}
	if tool.executeCalls != 0 {
		t.Fatalf("tool execute calls = %d, want 0", tool.executeCalls)
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	if !strings.Contains(last.Content, "unknown permission decision") {
		t.Fatalf("tool denial content = %q, want unknown permission decision", last.Content)
	}
}

func TestRunTurnReturnsToolExecutionErrorToModel(t *testing.T) {
	client := &fakeClient{responses: []model.GenerateResponse{
		{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "fail", Arguments: json.RawMessage(`{}`)}}},
		{FinalText: "recovered"},
	}}
	tool := newFakeTool("fail", permissions.ActionRead, permissions.RiskRead)
	tool.executeErr = errors.New("boom")
	runner := Runner{
		Model:     client,
		Tools:     tools.NewRegistry([]tools.Tool{tool}, nil),
		Policy:    permissions.ConservativePolicy{},
		ModelName: "test-model",
		MaxSteps:  3,
	}

	result, err := runner.RunTurn(context.Background(), nil, "try tool")
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if result.FinalText != "recovered" {
		t.Fatalf("FinalText = %q, want recovered", result.FinalText)
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	if !strings.Contains(last.Content, "tool error: boom") {
		t.Fatalf("tool error content = %q, want boom error", last.Content)
	}
}

func TestRunTurnStopsAtMaxSteps(t *testing.T) {
	client := &fakeClient{responses: []model.GenerateResponse{
		{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"again"}`)}}},
		{ToolCalls: []model.ToolCall{{ID: "call-2", Name: "echo", Arguments: json.RawMessage(`{"text":"again"}`)}}},
	}}
	runner := Runner{
		Model:     client,
		Tools:     tools.NewRegistry([]tools.Tool{newFakeTool("echo", permissions.ActionRead, permissions.RiskRead)}, nil),
		Policy:    permissions.ConservativePolicy{},
		ModelName: "test-model",
		MaxSteps:  1,
	}

	_, err := runner.RunTurn(context.Background(), nil, "start")
	if err == nil {
		t.Fatalf("RunTurn succeeded after max steps")
	}
	if !strings.Contains(err.Error(), "max steps") {
		t.Fatalf("RunTurn error = %v, want max steps", err)
	}
}

type fakeClient struct {
	responses []model.GenerateResponse
	requests  []model.GenerateRequest
}

func (f *fakeClient) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	if err := ctx.Err(); err != nil {
		return model.GenerateResponse{}, err
	}
	f.requests = append(f.requests, req)
	if len(f.requests) > len(f.responses) {
		return model.GenerateResponse{FinalText: "fallback"}, nil
	}
	return f.responses[len(f.requests)-1], nil
}

type fakeStreamingClient struct {
	events   []model.StreamEvent
	requests []model.GenerateRequest
}

func (f *fakeStreamingClient) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	return model.GenerateResponse{FinalText: "generate fallback"}, nil
}

func (f *fakeStreamingClient) Stream(ctx context.Context, req model.GenerateRequest, emit func(model.StreamEvent) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.requests = append(f.requests, req)
	for _, event := range f.events {
		if err := emit(event); err != nil {
			return err
		}
	}
	return nil
}

type fakeTool struct {
	name         string
	action       permissions.Action
	risk         permissions.Risk
	executeErr   error
	executeCalls int
}

func newFakeTool(name string, action permissions.Action, risk permissions.Risk) *fakeTool {
	return &fakeTool{name: name, action: action, risk: risk}
}

func (f *fakeTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        f.name,
		Description: f.name,
		InputSchema: map[string]any{"type": "object"},
	}
}

func (f *fakeTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Action: f.action, Risk: f.risk, Target: f.name, Reason: "test"}, nil
}

func (f *fakeTool) Execute(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	f.executeCalls++
	var parsed struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(args, &parsed)
	return tools.Result{Content: parsed.Text}, f.executeErr
}

type fakeConfirmer struct {
	allowed bool
	calls   int
}

func (f *fakeConfirmer) Confirm(ctx context.Context, req permissions.Request, decision permissions.Decision) (bool, error) {
	f.calls++
	return f.allowed, nil
}

type fakePolicy struct {
	decision permissions.Decision
	err      error
}

func (f fakePolicy) Check(ctx context.Context, req permissions.Request) (permissions.Decision, error) {
	if err := ctx.Err(); err != nil {
		return permissions.Decision{}, err
	}
	return f.decision, f.err
}

type fakeToolReporter struct {
	events []ToolEvent
}

func (f *fakeToolReporter) ReportTool(ctx context.Context, event ToolEvent) {
	f.events = append(f.events, event)
}

func messagesEqual(a, b []model.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Content != b[i].Content || a[i].ToolCallID != b[i].ToolCallID {
			return false
		}
	}
	return true
}
