package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/model"
)

func TestGenerateSendsAuthorizationRequestBodyAndParsesFinalText(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q, want Bearer test-key", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatalf("path = %q, want /chat/completions suffix", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "deepseek-v4-pro")
	client.baseURL = server.URL

	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
		Tools: []model.ToolDefinition{{
			Name:        "read_file",
			Description: "read",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required":   []string{"path"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.FinalText != "hello" {
		t.Fatalf("FinalText = %q, want hello", resp.FinalText)
	}
	if requestBody["model"] != "deepseek-v4-pro" {
		t.Fatalf("request model = %#v, want deepseek-v4-pro", requestBody["model"])
	}
	messages := requestBody["messages"].([]any)
	if got := messages[0].(map[string]any)["role"]; got != "user" {
		t.Fatalf("message role = %#v, want user", got)
	}
	tools := requestBody["tools"].([]any)
	function := tools[0].(map[string]any)["function"].(map[string]any)
	if function["name"] != "read_file" {
		t.Fatalf("tool function name = %#v, want read_file", function["name"])
	}
	if function["parameters"] == nil {
		t.Fatalf("tool function parameters missing")
	}
}

func TestGenerateUsesRequestModelAndMapsToolMessages(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "deepseek-v4-pro")
	client.baseURL = server.URL

	_, err := client.Generate(context.Background(), model.GenerateRequest{
		Model: "deepseek-v4-flash",
		Messages: []model.Message{
			{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{
					ID:        "call-1",
					Name:      "read_file",
					Arguments: json.RawMessage(`{"path":"go.mod"}`),
				}},
			},
			{Role: model.RoleTool, ToolCallID: "call-1", Content: "module codeworld"},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if requestBody["model"] != "deepseek-v4-flash" {
		t.Fatalf("request model = %#v, want deepseek-v4-flash", requestBody["model"])
	}
	messages := requestBody["messages"].([]any)
	assistant := messages[0].(map[string]any)
	toolCalls := assistant["tool_calls"].([]any)
	call := toolCalls[0].(map[string]any)
	if call["type"] != "function" {
		t.Fatalf("tool call type = %#v, want function", call["type"])
	}
	function := call["function"].(map[string]any)
	if function["arguments"] != `{"path":"go.mod"}` {
		t.Fatalf("tool call arguments = %#v, want raw JSON string", function["arguments"])
	}
	toolMessage := messages[1].(map[string]any)
	if toolMessage["tool_call_id"] != "call-1" || toolMessage["content"] != "module codeworld" {
		t.Fatalf("tool message = %#v, want tool_call_id and content", toolMessage)
	}
}

func TestGenerateParsesToolCallAndLeavesFinalTextEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"I will read it.","tool_calls":[{"id":"call-1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"go.mod\"}"}}]}}]}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "deepseek-v4-pro")
	client.baseURL = server.URL

	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "read"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "read_file" {
		t.Fatalf("tool name = %q, want read_file", resp.ToolCalls[0].Name)
	}
	if string(resp.ToolCalls[0].Arguments) != `{"path":"go.mod"}` {
		t.Fatalf("tool arguments = %s, want path JSON", resp.ToolCalls[0].Arguments)
	}
	if resp.FinalText != "" {
		t.Fatalf("FinalText = %q, want empty while tool calls are present", resp.FinalText)
	}
	if resp.Message.Content != "I will read it." {
		t.Fatalf("message content = %q, want assistant content preserved", resp.Message.Content)
	}
}

func TestGenerateParsesUsageIncludingCachedInputTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"hello"}}],
			"usage":{
				"prompt_tokens":120,
				"completion_tokens":30,
				"total_tokens":150,
				"prompt_cache_hit_tokens":80,
				"prompt_cache_miss_tokens":40
			}
		}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "deepseek-v4-pro")
	client.baseURL = server.URL

	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	want := model.Usage{InputTokens: 120, OutputTokens: 30, CacheTokens: 80, TotalTokens: 150}
	if resp.Usage != want {
		t.Fatalf("usage = %#v, want %#v", resp.Usage, want)
	}
}

func TestGenerateParsesOpenAIStyleCachedInputTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	client := NewClient("test-key", "deepseek-v4-pro")
	client.baseURL = server.URL

	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	want := model.Usage{InputTokens: 50, OutputTokens: 10, CacheTokens: 25, TotalTokens: 60}
	if resp.Usage != want {
		t.Fatalf("usage = %#v, want %#v", resp.Usage, want)
	}
}

func TestGenerateRejectsMissingAPIKey(t *testing.T) {
	client := NewClient("", "deepseek-v4-pro")

	if _, err := client.Generate(context.Background(), model.GenerateRequest{}); err == nil {
		t.Fatalf("Generate accepted missing API key")
	}
}

func TestGenerateReturnsHTTPErrorWithoutLeakingAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key secret-key", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient("secret-key", "deepseek-v4-pro")
	client.baseURL = server.URL

	_, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatalf("Generate returned nil error for HTTP failure")
	}
	if strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("error leaked API key: %v", err)
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("error = %v, want status 401", err)
	}
}

func TestGenerateRejectsEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "deepseek-v4-pro")
	client.baseURL = server.URL

	if _, err := client.Generate(context.Background(), model.GenerateRequest{}); err == nil {
		t.Fatalf("Generate accepted response with no choices")
	}
}

func TestGenerateLogsHTTPRequestAndResponseWithRedactedAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req-1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer server.Close()

	var log bytes.Buffer
	client := NewClient("secret-key", "deepseek-v4-pro")
	client.baseURL = server.URL
	client.logger = NewJSONLLogger(&log)

	_, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	var entry callLogEntry
	if err := json.Unmarshal(bytes.TrimSpace(log.Bytes()), &entry); err != nil {
		t.Fatalf("Unmarshal log entry: %v\n%s", err, log.String())
	}
	if entry.Request.Method != http.MethodPost {
		t.Fatalf("request method = %q, want POST", entry.Request.Method)
	}
	if !strings.HasSuffix(entry.Request.URL, "/chat/completions") {
		t.Fatalf("request URL = %q, want chat completions", entry.Request.URL)
	}
	if got := entry.Request.Headers.Get("Authorization"); got != "Bearer [redacted]" {
		t.Fatalf("logged Authorization = %q, want redacted", got)
	}
	if strings.Contains(log.String(), "secret-key") {
		t.Fatalf("log leaked API key:\n%s", log.String())
	}
	if !strings.Contains(string(entry.Request.Body), `"messages"`) {
		t.Fatalf("request body log = %s, want model request body", entry.Request.Body)
	}
	if len(entry.Request.BodyJSON) == 0 {
		t.Fatalf("request body_json missing")
	}
	var requestBodyJSON map[string]any
	if err := json.Unmarshal(entry.Request.BodyJSON, &requestBodyJSON); err != nil {
		t.Fatalf("Unmarshal request body_json: %v", err)
	}
	if requestBodyJSON["model"] != "deepseek-v4-pro" {
		t.Fatalf("request body_json model = %#v, want deepseek-v4-pro", requestBodyJSON["model"])
	}
	if entry.Response == nil {
		t.Fatalf("response log missing")
	}
	if entry.Response.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want 200", entry.Response.StatusCode)
	}
	if entry.Response.Headers.Get("X-Request-ID") != "req-1" {
		t.Fatalf("response headers = %#v, want X-Request-ID", entry.Response.Headers)
	}
	if !strings.Contains(string(entry.Response.Body), `"hello"`) {
		t.Fatalf("response body log = %s, want hello", entry.Response.Body)
	}
	if len(entry.Response.BodyJSON) == 0 {
		t.Fatalf("response body_json missing")
	}
	var responseBodyJSON map[string]any
	if err := json.Unmarshal(entry.Response.BodyJSON, &responseBodyJSON); err != nil {
		t.Fatalf("Unmarshal response body_json: %v", err)
	}
	if _, ok := responseBodyJSON["choices"]; !ok {
		t.Fatalf("response body_json = %#v, want choices", responseBodyJSON)
	}
}

func TestGenerateLogsHTTPErrorWithRedactedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad secret-key", http.StatusUnauthorized)
	}))
	defer server.Close()

	var log bytes.Buffer
	client := NewClient("secret-key", "deepseek-v4-pro")
	client.baseURL = server.URL
	client.logger = NewJSONLLogger(&log)

	_, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatalf("Generate returned nil error for HTTP failure")
	}
	if strings.Contains(log.String(), "secret-key") {
		t.Fatalf("log leaked API key:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "[redacted]") {
		t.Fatalf("log = %s, want redacted marker", log.String())
	}
	var entry callLogEntry
	if err := json.Unmarshal(bytes.TrimSpace(log.Bytes()), &entry); err != nil {
		t.Fatalf("Unmarshal log entry: %v\n%s", err, log.String())
	}
	if entry.Response != nil && len(entry.Response.BodyJSON) != 0 {
		t.Fatalf("error response body_json = %s, want omitted for non-JSON body", entry.Response.BodyJSON)
	}
}

func TestFileJSONLLoggerCreatesRestrictiveLogFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".codeworld", "logs", "model-calls.jsonl")
	logger := NewFileJSONLLogger(path)

	err := logger.LogCall(context.Background(), callLogEntry{
		Provider: "deepseek",
		Request:  httpRequestLog{Method: http.MethodPost, URL: "https://api.deepseek.com/chat/completions"},
	})
	if err != nil {
		t.Fatalf("LogCall returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile log: %v", err)
	}
	if !strings.Contains(string(data), `"provider":"deepseek"`) {
		t.Fatalf("log data = %s, want provider", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat log: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("log permissions = %o, want 600", mode)
	}
}
