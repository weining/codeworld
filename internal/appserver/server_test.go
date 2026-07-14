package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"codeworld/internal/agent"
	"codeworld/internal/model"
)

type fakeRuntime struct {
	info      RuntimeInfo
	run       func(context.Context, model.Message, func(agent.TurnEvent) error, agent.ToolReporter) (agent.TurnResult, error)
	mu        sync.Mutex
	closed    bool
	lastInput model.Message
}

func (r *fakeRuntime) Info() RuntimeInfo { return r.info }

func (r *fakeRuntime) Run(ctx context.Context, input model.Message, emit func(agent.TurnEvent) error, reporter agent.ToolReporter) (agent.TurnResult, error) {
	r.mu.Lock()
	r.lastInput = input
	r.mu.Unlock()
	return r.run(ctx, input, emit, reporter)
}

func (r *fakeRuntime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

func TestServerImplementsCoreThreadAndTurnProtocol(t *testing.T) {
	runtime := &fakeRuntime{info: RuntimeInfo{
		ID: "thread-1", CWD: t.TempDir(), Provider: "codex", Model: "gpt-test",
		ApprovalPolicy: "on-request", Sandbox: "workspace-write",
		CreatedAt: time.Unix(100, 0), UpdatedAt: time.Unix(101, 0),
	}}
	runtime.run = func(ctx context.Context, input model.Message, emit func(agent.TurnEvent) error, reporter agent.ToolReporter) (agent.TurnResult, error) {
		reporter.ReportTool(ctx, agent.ToolEvent{Status: agent.ToolEventStart, Name: "read_file", CallID: "call-1"})
		reporter.ReportTool(ctx, agent.ToolEvent{Status: agent.ToolEventSuccess, Name: "read_file", CallID: "call-1"})
		if err := emit(agent.TurnEvent{Kind: agent.TurnEventAssistantDelta, Text: "hel"}); err != nil {
			return agent.TurnResult{}, err
		}
		if err := emit(agent.TurnEvent{Kind: agent.TurnEventAssistantDelta, Text: "lo"}); err != nil {
			return agent.TurnResult{}, err
		}
		if err := emit(agent.TurnEvent{Kind: agent.TurnEventAssistantDone, Text: "hello"}); err != nil {
			return agent.TurnResult{}, err
		}
		return agent.TurnResult{FinalText: "hello"}, nil
	}
	server := Server{
		Home: "/tmp/codeworld-home", Version: "test-version",
		Factory: func(_ context.Context, req ThreadRequest) (Runtime, error) {
			if req.CWD != runtime.info.CWD || req.Model != "gpt-test" {
				t.Fatalf("thread request = %#v", req)
			}
			return runtime, nil
		},
	}
	input := strings.Join([]string{
		`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"test","version":"1"}}}`,
		`{"method":"initialized"}`,
		`{"id":2,"method":"thread/start","params":{"cwd":"` + runtime.info.CWD + `","model":"gpt-test"}}`,
		`{"id":3,"method":"turn/start","params":{"threadId":"thread-1","input":[{"type":"text","text":"hi"}]}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	messages := decodeMessages(t, out.Bytes())
	for _, want := range []string{
		`"id":1`, `"codexHome":"/tmp/codeworld-home"`, `"id":2`,
		`"method":"thread/started"`, `"id":3`, `"method":"turn/started"`,
		`"method":"item/agentMessage/delta"`, `"delta":"hel"`,
		`"method":"item/started"`, `"type":"dynamicToolCall"`,
		`"method":"turn/completed"`, `"status":"completed"`,
	} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("protocol output is missing %s:\n%s", want, out.String())
		}
	}
	if len(messages) < 12 {
		t.Fatalf("got %d messages, want protocol lifecycle output: %s", len(messages), out.String())
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.lastInput.Content != "hi" || len(runtime.lastInput.Parts) != 1 {
		t.Fatalf("input = %#v", runtime.lastInput)
	}
	if !runtime.closed {
		t.Fatal("runtime was not closed when stdio ended")
	}
}

func TestTurnInterruptCancelsActiveRuntime(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	runtime := &fakeRuntime{info: RuntimeInfo{ID: "thread-1", CWD: t.TempDir(), Provider: "codex", Model: "gpt-test"}}
	runtime.run = func(ctx context.Context, _ model.Message, _ func(agent.TurnEvent) error, _ agent.ToolReporter) (agent.TurnResult, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return agent.TurnResult{}, ctx.Err()
	}
	var out bytes.Buffer
	server := Server{
		Factory: func(context.Context, ThreadRequest) (Runtime, error) { return runtime, nil },
		threads: map[string]*threadState{"thread-1": {runtime: runtime}},
		writer:  newMessageWriter(&out),
	}
	startParams := json.RawMessage(`{"threadId":"thread-1","input":[{"type":"text","text":"wait"}]}`)
	if err := server.startTurn(context.Background(), request{ID: json.RawMessage(`1`), Method: "turn/start", Params: startParams}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("runtime did not start")
	}
	server.threads["thread-1"].mu.Lock()
	turnID := server.threads["thread-1"].active.id
	server.threads["thread-1"].mu.Unlock()
	interrupt, err := json.Marshal(map[string]string{"threadId": "thread-1", "turnId": turnID})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.interruptTurn(request{ID: json.RawMessage(`2`), Method: "turn/interrupt", Params: interrupt}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("runtime context was not cancelled")
	}
	server.turns.Wait()
	if !bytes.Contains(out.Bytes(), []byte(`"status":"interrupted"`)) {
		t.Fatalf("missing interrupted completion: %s", out.String())
	}
}

func TestServerReturnsMethodNotFound(t *testing.T) {
	server := Server{Factory: func(context.Context, ThreadRequest) (Runtime, error) { return nil, io.EOF }}
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader("{\"id\":7,\"method\":\"unknown\"}\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"code":-32601`) {
		t.Fatalf("output = %s", out.String())
	}
}

func decodeMessages(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var messages []map[string]any
	for decoder.More() {
		var message map[string]any
		if err := decoder.Decode(&message); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
	return messages
}
