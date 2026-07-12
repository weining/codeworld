package codex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codeworld/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// TestStreamSendsCodexHeadersAndParsesTextUsage 验证 Codex OAuth token 会走 ChatGPT backend Responses SSE。
func TestStreamSendsCodexHeadersAndParsesTextUsage(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/codex/responses" {
			t.Fatalf("path = %q, want /codex/responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		if got := r.Header.Get("chatgpt-account-id"); got != "acct-1" {
			t.Fatalf("chatgpt-account-id = %q, want acct-1", got)
		}
		if got := r.Header.Get("originator"); got != "codeworld" {
			t.Fatalf("originator = %q, want codeworld", got)
		}
		if got := r.Header.Get("OpenAI-Beta"); got != "responses=experimental" {
			t.Fatalf("OpenAI-Beta = %q, want responses=experimental", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"你"}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"好"}`,
			"",
			`data: {"type":"response.completed","response":{"usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10,"input_tokens_details":{"cached_tokens":4}}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n")))
	}))
	defer server.Close()
	client := NewClient(Config{AccessToken: "access-token", AccountID: "acct-1", Model: "gpt-5", BaseURL: server.URL})

	var deltas []string
	err := client.Stream(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
		Tools: []model.ToolDefinition{{
			Name:        "read_file",
			Description: "read",
			InputSchema: map[string]any{"type": "object"},
		}},
	}, func(event model.StreamEvent) error {
		if event.Kind == model.StreamEventTextDelta {
			deltas = append(deltas, event.Delta)
		}
		if event.Kind == model.StreamEventUsage && event.Usage.TotalTokens != 10 {
			t.Fatalf("usage event = %#v, want total 10", event.Usage)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	if strings.Join(deltas, "") != "你好" {
		t.Fatalf("deltas = %#v, want 你好", deltas)
	}
	if requestBody["model"] != "gpt-5" || requestBody["stream"] != true {
		t.Fatalf("request body = %#v", requestBody)
	}
	if _, ok := requestBody["input"]; !ok {
		t.Fatalf("request body missing input: %#v", requestBody)
	}
	if _, ok := requestBody["tools"]; !ok {
		t.Fatalf("request body missing tools: %#v", requestBody)
	}
}

// TestGenerateAggregatesStream 验证非流式接口通过 SSE 聚合最终文本。
func TestGenerateAggregatesStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"))
	}))
	defer server.Close()
	client := NewClient(Config{AccessToken: "access-token", AccountID: "acct-1", Model: "gpt-5", BaseURL: server.URL})

	resp, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.FinalText != "ok" {
		t.Fatalf("FinalText = %q, want ok", resp.FinalText)
	}
}

func TestStreamRetriesOneTransportEOF(t *testing.T) {
	attempts := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, io.EOF
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")),
			Request:    request,
		}, nil
	})}
	client := NewClient(Config{AccessToken: "token", AccountID: "account", Model: "gpt-5", BaseURL: "https://example.test", HTTPClient: httpClient})
	var text string
	err := client.Stream(context.Background(), model.GenerateRequest{Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}}}, func(event model.StreamEvent) error {
		text += event.Delta
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || text != "ok" {
		t.Fatalf("attempts=%d text=%q", attempts, text)
	}
}
