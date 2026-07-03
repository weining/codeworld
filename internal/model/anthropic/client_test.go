package anthropic

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

// TestGenerateSendsMessagesRequestAndParsesFinalTextUsageAndLog 验证对应场景的行为，避免后续改动破坏既有约束。
func TestGenerateSendsMessagesRequestAndParsesFinalTextUsageAndLog(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "anthropic-key" {
			t.Fatalf("x-api-key = %q, want anthropic-key", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Fatalf("anthropic-version header missing")
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"role":"assistant",
			"content":[{"type":"text","text":"hello"}],
			"usage":{"input_tokens":50,"output_tokens":10,"cache_read_input_tokens":25}
		}`))
	}))
	defer server.Close()

	var log bytes.Buffer
	client := NewClient("anthropic-key", "claude-sonnet-4-5")
	client.baseURL = server.URL
	client.SetLogger(NewJSONLLogger(&log))

	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "system instructions"},
			{Role: model.RoleUser, Content: "hi"},
		},
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
	if requestBody["model"] != "claude-sonnet-4-5" || requestBody["system"] != "system instructions" {
		t.Fatalf("request body = %#v", requestBody)
	}
	if _, ok := requestBody["tools"]; !ok {
		t.Fatalf("request tools missing: %#v", requestBody)
	}
	if strings.Contains(log.String(), "anthropic-key") {
		t.Fatalf("log leaked API key: %s", log.String())
	}
	var entry callLogEntry
	if err := json.Unmarshal(bytes.TrimSpace(log.Bytes()), &entry); err != nil {
		t.Fatalf("Unmarshal log entry: %v\n%s", err, log.String())
	}
	if len(entry.Request.BodyJSON) == 0 || entry.Response == nil || len(entry.Response.BodyJSON) == 0 {
		t.Fatalf("log body_json missing: %s", log.String())
	}
}

// TestGenerateParsesToolUseAndLeavesFinalTextEmpty 验证对应场景的行为，避免后续改动破坏既有约束。
func TestGenerateParsesToolUseAndLeavesFinalTextEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"role":"assistant",
			"content":[
				{"type":"text","text":"I will read it."},
				{"type":"tool_use","id":"toolu_1","name":"read_file","input":{"path":"go.mod"}}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient("anthropic-key", "claude-sonnet-4-5")
	client.baseURL = server.URL
	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "read"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want one call", resp.ToolCalls)
	}
	if resp.ToolCalls[0].ID != "toolu_1" || resp.ToolCalls[0].Name != "read_file" {
		t.Fatalf("tool call = %#v", resp.ToolCalls[0])
	}
	assertJSONEqual(t, resp.ToolCalls[0].Arguments, `{"path":"go.mod"}`)
	if resp.FinalText != "" {
		t.Fatalf("FinalText = %q, want empty with tool calls", resp.FinalText)
	}
	if resp.Message.Content != "I will read it." {
		t.Fatalf("message content = %q, want text content preserved", resp.Message.Content)
	}
}

// TestGenerateMapsToolResultsBackToAnthropicMessages 验证对应场景的行为，避免后续改动破坏既有约束。
func TestGenerateMapsToolResultsBackToAnthropicMessages(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client := NewClient("anthropic-key", "claude-sonnet-4-5")
	client.baseURL = server.URL
	_, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{
			{
				Role:    model.RoleAssistant,
				Content: "I will read.",
				ToolCalls: []model.ToolCall{{
					ID:        "toolu_1",
					Name:      "read_file",
					Arguments: json.RawMessage(`{"path":"go.mod"}`),
				}},
			},
			{Role: model.RoleTool, ToolCallID: "toolu_1", Content: "module codeworld"},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	messages := requestBody["messages"].([]any)
	assistant := messages[0].(map[string]any)
	assistantContent := assistant["content"].([]any)
	if assistantContent[1].(map[string]any)["type"] != "tool_use" {
		t.Fatalf("assistant content = %#v, want tool_use block", assistantContent)
	}
	toolResult := messages[1].(map[string]any)
	toolContent := toolResult["content"].([]any)
	block := toolContent[0].(map[string]any)
	if block["type"] != "tool_result" || block["tool_use_id"] != "toolu_1" || block["content"] != "module codeworld" {
		t.Fatalf("tool_result block = %#v", block)
	}
}

// TestGenerateReturnsHTTPError 验证对应场景的行为，避免后续改动破坏既有约束。
func TestGenerateReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient("anthropic-key", "claude-sonnet-4-5")
	client.baseURL = server.URL
	_, err := client.Generate(context.Background(), model.GenerateRequest{})
	if err == nil || !strings.Contains(err.Error(), "anthropic status 401") {
		t.Fatalf("err = %v, want status 401", err)
	}
}

// assertJSONEqual 是测试辅助函数，用于复用测试准备或断言逻辑。
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
	gotData, _ := json.Marshal(gotValue)
	wantData, _ := json.Marshal(wantValue)
	if string(gotData) != string(wantData) {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}
