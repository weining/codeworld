package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

type fakeRunner struct {
	mu      sync.Mutex
	starts  []StartRequest
	replies []ReplyRequest
	err     error
}

func (f *fakeRunner) Start(_ context.Context, req StartRequest) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, req)
	return Response{ThreadID: "thread-1", Content: "first answer"}, f.err
}

func (f *fakeRunner) Reply(_ context.Context, req ReplyRequest) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, req)
	return Response{ThreadID: req.ThreadID, Content: "next answer"}, f.err
}

type cancellationRunner struct {
	cancelled chan struct{}
}

func (r *cancellationRunner) Start(ctx context.Context, _ StartRequest) (Response, error) {
	<-ctx.Done()
	close(r.cancelled)
	return Response{}, ctx.Err()
}

func (r *cancellationRunner) Reply(context.Context, ReplyRequest) (Response, error) {
	return Response{}, nil
}

func TestServerImplementsInitializeListAndToolCalls(t *testing.T) {
	runner := &fakeRunner{}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"codex","arguments":{"prompt":"inspect","cwd":"."}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"codex-reply","arguments":{"threadId":"thread-1","prompt":"continue"}}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := (Server{Runner: runner, Version: "test"}).Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("responses = %d\n%s", len(lines), out.String())
	}
	var initialized map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &initialized); err != nil {
		t.Fatal(err)
	}
	result := initialized["result"].(map[string]any)
	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("initialize = %#v", initialized)
	}
	var listed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Result.Tools) != 2 || listed.Result.Tools[0].Name != "codex" || listed.Result.Tools[1].Name != "codex-reply" {
		t.Fatalf("tools = %#v", listed.Result.Tools)
	}
	var called struct {
		Result struct {
			Structured Response `json:"structuredContent"`
			IsError    bool     `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &called); err != nil {
		t.Fatal(err)
	}
	if called.Result.IsError || called.Result.Structured.ThreadID != "thread-1" || called.Result.Structured.Content != "first answer" {
		t.Fatalf("call result = %#v", called)
	}
	if len(runner.starts) != 1 || runner.starts[0].Prompt != "inspect" || len(runner.replies) != 1 || runner.replies[0].Prompt != "continue" {
		t.Fatalf("starts=%#v replies=%#v", runner.starts, runner.replies)
	}
}

func TestServerReturnsProtocolAndToolErrors(t *testing.T) {
	runner := &fakeRunner{err: context.DeadlineExceeded}
	input := "not-json\n" +
		`{"jsonrpc":"2.0","id":1,"method":"missing"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"codex","arguments":{"prompt":"run"}}}` + "\n"
	var out bytes.Buffer
	if err := (Server{Runner: runner}).Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], `"code":-32700`) || !strings.Contains(lines[1], `"code":-32601`) || !strings.Contains(lines[2], `"isError":true`) {
		t.Fatalf("responses:\n%s", out.String())
	}
}

func TestServerCancellationInterruptsActiveToolCall(t *testing.T) {
	runner := &cancellationRunner{cancelled: make(chan struct{})}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"codex","arguments":{"prompt":"wait"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"call-1","reason":"client stopped"}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := (Server{Runner: runner}).Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.cancelled:
	default:
		t.Fatal("runner context was not cancelled")
	}
	if !strings.Contains(out.String(), `"isError":true`) || !strings.Contains(out.String(), "context canceled") {
		t.Fatalf("response = %s", out.String())
	}
}
