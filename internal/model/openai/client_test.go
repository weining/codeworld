package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codeworld/internal/model"
)

// TestGenerateSendsChatCompletionRequestAndParsesFinalTextUsageAndLog 验证对应场景的行为，避免后续改动破坏既有约束。
func TestGenerateSendsChatCompletionRequestAndParsesFinalTextUsageAndLog(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer openai-key" {
			t.Fatalf("Authorization = %q, want Bearer openai-key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"hello"}}],
			"usage":{
				"prompt_tokens":50,
				"completion_tokens":10,
				"total_tokens":60,
				"prompt_tokens_details":{"cached_tokens":25}
			}
		}`))
	}))
	defer server.Close()

	var log bytes.Buffer
	client := NewClient("openai-key", "gpt-4.1", server.URL)
	client.SetLogger(NewJSONLLogger(&log))

	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
		Tools: []model.ToolDefinition{{
			Name:        "read_file",
			Description: "read",
			InputSchema: map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.FinalText != "hello" {
		t.Fatalf("FinalText = %q, want hello", resp.FinalText)
	}
	wantUsage := model.Usage{InputTokens: 50, OutputTokens: 10, CacheTokens: 25, TotalTokens: 60}
	if resp.Usage != wantUsage {
		t.Fatalf("Usage = %#v, want %#v", resp.Usage, wantUsage)
	}
	if requestBody["model"] != "gpt-4.1" {
		t.Fatalf("request model = %#v, want gpt-4.1", requestBody["model"])
	}
	if _, ok := requestBody["tools"]; !ok {
		t.Fatalf("request tools missing: %#v", requestBody)
	}

	var entry callLogEntry
	if err := json.Unmarshal(bytes.TrimSpace(log.Bytes()), &entry); err != nil {
		t.Fatalf("Unmarshal log entry: %v\n%s", err, log.String())
	}
	if len(entry.Request.BodyJSON) == 0 || entry.Response == nil || len(entry.Response.BodyJSON) == 0 {
		t.Fatalf("log body_json missing: %s", log.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(log.Bytes()), &raw); err != nil {
		t.Fatalf("Unmarshal raw log: %v", err)
	}
	request := raw["request"].(map[string]any)
	for _, key := range []string{"method", "url", "headers", "body"} {
		if _, ok := request[key]; ok {
			t.Fatalf("request log contains %q: %s", key, log.String())
		}
	}
	response := raw["response"].(map[string]any)
	for _, key := range []string{"status_code", "status", "headers", "body"} {
		if _, ok := response[key]; ok {
			t.Fatalf("response log contains %q: %s", key, log.String())
		}
	}
}

// TestGenerateOmitsAuthorizationWhenAPIKeyIsEmpty 验证对应场景的行为，避免后续改动破坏既有约束。
func TestGenerateOmitsAuthorizationWhenAPIKeyIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty for local provider", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"local"}}]}`))
	}))
	defer server.Close()

	client := NewClient("", "local-model", server.URL)
	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.FinalText != "local" {
		t.Fatalf("FinalText = %q, want local", resp.FinalText)
	}
}

// TestStreamSendsStreamingRequestAndEmitsDeltasUsageAndDone 验证对应场景的行为，避免后续改动破坏既有约束。
func TestStreamSendsStreamingRequestAndEmitsDeltasUsageAndDone(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6,\"prompt_tokens_details\":{\"cached_tokens\":1}},\"choices\":[{\"delta\":{}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient("openai-key", "gpt-4.1", server.URL)
	var events []model.StreamEvent
	err := client.Stream(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	}, func(event model.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	if requestBody["stream"] != true {
		t.Fatalf("request stream = %#v, want true", requestBody["stream"])
	}
	if len(events) != 4 {
		t.Fatalf("events = %#v, want two deltas, usage, done", events)
	}
	if events[0].Kind != model.StreamEventTextDelta || events[0].Delta != "你" {
		t.Fatalf("first event = %#v, want delta 你", events[0])
	}
	if events[1].Kind != model.StreamEventTextDelta || events[1].Delta != "好" {
		t.Fatalf("second event = %#v, want delta 好", events[1])
	}
	if events[2].Kind != model.StreamEventUsage || events[2].Usage.TotalTokens != 6 || events[2].Usage.CacheTokens != 1 {
		t.Fatalf("usage event = %#v, want usage", events[2])
	}
	if events[3].Kind != model.StreamEventDone || events[3].Message.Content != "你好" {
		t.Fatalf("done event = %#v, want final message", events[3])
	}
}

// TestStreamParsesToolCallDeltas 验证对应场景的行为，避免后续改动破坏既有约束。
func TestStreamParsesToolCallDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"read_file","arguments":"{\"path\""}}]}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"go.mod\"}"}}]}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient("openai-key", "gpt-4.1", server.URL)
	var events []model.StreamEvent
	err := client.Stream(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "read"}},
	}, func(event model.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	if len(events) != 1 || events[0].Kind != model.StreamEventDone {
		t.Fatalf("events = %#v, want one done event", events)
	}
	calls := events[0].ToolCalls
	if len(calls) != 1 {
		t.Fatalf("tool calls = %#v, want one call", calls)
	}
	if calls[0].ID != "call-1" || calls[0].Name != "read_file" || string(calls[0].Arguments) != `{"path":"go.mod"}` {
		t.Fatalf("tool call = %#v", calls[0])
	}
}

// TestGenerateParsesToolCallAndLeavesFinalTextEmpty 验证对应场景的行为，避免后续改动破坏既有约束。
func TestGenerateParsesToolCallAndLeavesFinalTextEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"I will read it.","tool_calls":[{"id":"call-1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"go.mod\"}"}}]}}]}`))
	}))
	defer server.Close()

	client := NewClient("openai-key", "gpt-4.1", server.URL)
	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "read"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want one call", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Name != "read_file" || string(resp.ToolCalls[0].Arguments) != `{"path":"go.mod"}` {
		t.Fatalf("tool call = %#v", resp.ToolCalls[0])
	}
	if resp.FinalText != "" {
		t.Fatalf("FinalText = %q, want empty with tool calls", resp.FinalText)
	}
}

// TestGenerateReturnsHTTPError 验证对应场景的行为，避免后续改动破坏既有约束。
func TestGenerateReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient("openai-key", "gpt-4.1", server.URL)
	_, err := client.Generate(context.Background(), model.GenerateRequest{})
	if err == nil || !strings.Contains(err.Error(), "openai status 401") {
		t.Fatalf("err = %v, want status 401", err)
	}
}
