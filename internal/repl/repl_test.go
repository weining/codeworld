package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"codeworld/internal/agent"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/session"
	"codeworld/internal/tools"
)

func TestRunSavesTurnsAndDoesNotFeedSystemPromptBackAsHistory(t *testing.T) {
	client := &fakeModelClient{responses: []model.GenerateResponse{
		{FinalText: "first answer"},
		{FinalText: "second answer"},
	}}
	store := session.NewStore(t.TempDir())
	sess := session.New("workspace", "deepseek", "deepseek-v4-pro")
	var out bytes.Buffer
	app := REPL{
		In:  strings.NewReader("first\nsecond\n/exit\n"),
		Out: &out,
		Runner: agent.Runner{
			Model:        client,
			Tools:        tools.NewRegistry(nil, nil),
			Policy:       permissions.ConservativePolicy{},
			MaxSteps:     3,
			ModelName:    "deepseek-v4-pro",
			SystemPrompt: "system prompt",
		},
		Store:   store,
		Session: sess,
	}

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(out.String(), "first answer") || !strings.Contains(out.String(), "second answer") {
		t.Fatalf("output = %q, want both assistant answers", out.String())
	}
	if len(client.requests) != 2 {
		t.Fatalf("model calls = %d, want 2", len(client.requests))
	}
	if got := countRole(client.requests[1].Messages, model.RoleSystem); got != 1 {
		t.Fatalf("second request system messages = %d, want 1", got)
	}
	if got := countRole(app.Messages, model.RoleSystem); got != 0 {
		t.Fatalf("stored repl history system messages = %d, want 0", got)
	}

	saved, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent returned error: %v", err)
	}
	if len(saved.Messages) != 4 {
		t.Fatalf("saved message count = %d, want 4", len(saved.Messages))
	}
	if saved.Messages[0].Role != "user" || saved.Messages[0].Content != "first" {
		t.Fatalf("first saved message = %#v, want user first", saved.Messages[0])
	}
	if saved.Messages[3].Role != "assistant" || saved.Messages[3].Content != "second answer" {
		t.Fatalf("last saved message = %#v, want assistant second answer", saved.Messages[3])
	}
}

func TestRunSavesToolCallsInSessionHistory(t *testing.T) {
	client := &fakeModelClient{responses: []model.GenerateResponse{
		{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"go.mod"}`)}}},
		{FinalText: "read complete"},
	}}
	store := session.NewStore(t.TempDir())
	app := REPL{
		In:  strings.NewReader("read go.mod\n/exit\n"),
		Out: &bytes.Buffer{},
		Runner: agent.Runner{
			Model:     client,
			Tools:     tools.NewRegistry([]tools.Tool{newReplFakeTool("read", permissions.ActionRead, permissions.RiskRead)}, nil),
			Policy:    permissions.ConservativePolicy{},
			MaxSteps:  3,
			ModelName: "deepseek-v4-pro",
		},
		Store:   store,
		Session: session.New("workspace", "deepseek", "deepseek-v4-pro"),
	}

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	saved, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent returned error: %v", err)
	}
	if len(saved.Messages) != 4 {
		t.Fatalf("saved message count = %d, want 4", len(saved.Messages))
	}
	assistantToolCall := saved.Messages[1]
	if assistantToolCall.Role != "assistant" || len(assistantToolCall.ToolCalls) != 1 {
		t.Fatalf("saved assistant tool call message = %#v, want one tool call", assistantToolCall)
	}
	if assistantToolCall.ToolCalls[0].ID != "call-1" || assistantToolCall.ToolCalls[0].Name != "read" {
		t.Fatalf("saved tool call = %#v, want call-1/read", assistantToolCall.ToolCalls[0])
	}
	assertJSONEqual(t, assistantToolCall.ToolCalls[0].Arguments, `{"path":"go.mod"}`)
	if saved.Messages[2].Role != "tool" || saved.Messages[2].ToolCallID != "call-1" {
		t.Fatalf("saved tool result message = %#v, want matching tool_call_id", saved.Messages[2])
	}
}

func TestRunHandlesSlashCommands(t *testing.T) {
	var out bytes.Buffer
	store := session.NewStore(t.TempDir())
	sess := session.New("workspace", "deepseek", "deepseek-v4-pro")
	sess.Messages = []session.Message{{Role: "user", Content: "old"}}
	app := REPL{
		In:  strings.NewReader("/help\n/model deepseek-v4-flash\n/model\n/status\n/diff\n/clear\n/unknown\n/exit\n"),
		Out: &out,
		Runner: agent.Runner{
			ModelName: "deepseek-v4-pro",
		},
		Session:  sess,
		Messages: []model.Message{{Role: model.RoleUser, Content: "old"}},
		Store:    store,
		Diff: func(ctx context.Context) (string, error) {
			return "diff --git a/a b/a", nil
		},
	}

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"codeworld",
		"/help /model /status /diff /clear /exit",
		"model=deepseek-v4-flash",
		"workspace=workspace provider=deepseek model=deepseek-v4-flash messages=1",
		"diff --git a/a b/a",
		"cleared",
		"unknown command: /unknown",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q in:\n%s", want, output)
		}
	}
	if app.Runner.ModelName != "deepseek-v4-flash" {
		t.Fatalf("runner model = %q, want deepseek-v4-flash", app.Runner.ModelName)
	}
	if len(app.Messages) != 0 || len(app.Session.Messages) != 0 {
		t.Fatalf("clear left messages: repl=%#v session=%#v", app.Messages, app.Session.Messages)
	}
	saved, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent returned error: %v", err)
	}
	if saved.Model != "deepseek-v4-flash" {
		t.Fatalf("saved model = %q, want deepseek-v4-flash", saved.Model)
	}
	if len(saved.Messages) != 0 {
		t.Fatalf("saved messages = %#v, want cleared", saved.Messages)
	}
}

func TestRunUsesSingleInputBufferForPermissionConfirmation(t *testing.T) {
	input := strings.NewReader("change\ny\n/exit\n")
	client := &fakeModelClient{responses: []model.GenerateResponse{
		{ToolCalls: []model.ToolCall{{ID: "call-1", Name: "write", Arguments: json.RawMessage(`{}`)}}},
		{FinalText: "changed"},
	}}
	tool := newReplFakeTool("write", permissions.ActionWrite, permissions.RiskWrite)
	var out bytes.Buffer
	confirmer := Confirmer{In: input, Out: &out}
	app := REPL{
		In:  input,
		Out: &out,
		Runner: agent.Runner{
			Model:     client,
			Tools:     tools.NewRegistry([]tools.Tool{tool}, nil),
			Policy:    permissions.ConservativePolicy{},
			Confirmer: confirmer,
			MaxSteps:  3,
			ModelName: "deepseek-v4-pro",
		},
		Store:   session.NewStore(t.TempDir()),
		Session: session.New("workspace", "deepseek", "deepseek-v4-pro"),
	}

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if tool.executeCalls != 1 {
		t.Fatalf("tool execute calls = %d, want 1", tool.executeCalls)
	}
	if !strings.Contains(out.String(), "changed") {
		t.Fatalf("output = %q, want changed final text", out.String())
	}
}

func TestConfirmerPromptsAndAcceptsY(t *testing.T) {
	var out bytes.Buffer
	confirmer := Confirmer{In: strings.NewReader("y\n"), Out: &out}

	allowed, err := confirmer.Confirm(context.Background(), permissions.Request{
		Action:  permissions.ActionShell,
		Target:  "go test ./...",
		Risk:    permissions.RiskExecute,
		Reason:  "verify changes",
		Preview: "go test ./...",
	}, permissions.Decision{Kind: permissions.DecisionAsk})
	if err != nil {
		t.Fatalf("Confirm returned error: %v", err)
	}
	if !allowed {
		t.Fatalf("Confirm returned false, want true")
	}
	output := out.String()
	for _, want := range []string{"Permission required: shell", "Target: go test ./...", "Risk: execute", "Reason: verify changes", "Preview:"} {
		if !strings.Contains(output, want) {
			t.Fatalf("prompt missing %q in:\n%s", want, output)
		}
	}
}

func TestConfirmerDefaultsToDeny(t *testing.T) {
	confirmer := Confirmer{In: strings.NewReader("\n"), Out: &bytes.Buffer{}}

	allowed, err := confirmer.Confirm(context.Background(), permissions.Request{}, permissions.Decision{Kind: permissions.DecisionAsk})
	if err != nil {
		t.Fatalf("Confirm returned error: %v", err)
	}
	if allowed {
		t.Fatalf("Confirm returned true for empty input")
	}
}

type fakeModelClient struct {
	responses []model.GenerateResponse
	requests  []model.GenerateRequest
}

func (f *fakeModelClient) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	if err := ctx.Err(); err != nil {
		return model.GenerateResponse{}, err
	}
	f.requests = append(f.requests, cloneGenerateRequest(req))
	if len(f.requests) > len(f.responses) {
		return model.GenerateResponse{FinalText: "fallback"}, nil
	}
	return f.responses[len(f.requests)-1], nil
}

type replFakeTool struct {
	name         string
	action       permissions.Action
	risk         permissions.Risk
	executeCalls int
}

func newReplFakeTool(name string, action permissions.Action, risk permissions.Risk) *replFakeTool {
	return &replFakeTool{name: name, action: action, risk: risk}
}

func (f *replFakeTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: f.name, Description: f.name, InputSchema: map[string]any{"type": "object"}}
}

func (f *replFakeTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Action: f.action, Risk: f.risk, Target: f.name, Reason: "test"}, nil
}

func (f *replFakeTool) Execute(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	f.executeCalls++
	return tools.Result{Content: "ok"}, nil
}

func cloneGenerateRequest(req model.GenerateRequest) model.GenerateRequest {
	data, _ := json.Marshal(req)
	var clone model.GenerateRequest
	_ = json.Unmarshal(data, &clone)
	return clone
}

func countRole(messages []model.Message, role model.Role) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == role {
			count++
		}
	}
	return count
}

func assertJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("Unmarshal got JSON: %v", err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("Unmarshal want JSON: %v", err)
	}
	if jsonString(gotValue) != jsonString(wantValue) {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

func jsonString(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
