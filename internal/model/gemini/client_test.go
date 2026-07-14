package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codeworld/internal/model"
)

func TestGenerateUsesNativeGeminiFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-test:generateContent" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "secret" {
			t.Errorf("api key header = %q", r.Header.Get("x-goog-api-key"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["systemInstruction"] == nil || body["tools"] == nil {
			t.Errorf("body = %#v", body)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}`))
	}))
	defer server.Close()
	client := NewClient(Config{APIKey: "secret", Model: "gemini-test", BaseURL: server.URL + "/v1beta"})
	response, err := client.Generate(context.Background(), model.GenerateRequest{
		Messages: []model.Message{{Role: model.RoleSystem, Content: "system"}, {Role: model.RoleUser, Content: "hi"}},
		Tools:    []model.ToolDefinition{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.FinalText != "hello" || response.Usage.TotalTokens != 3 {
		t.Fatalf("response = %#v", response)
	}
}

func TestStreamEmitsTextAndDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hel\"}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1,\"totalTokenCount\":3}}\n\n"))
	}))
	defer server.Close()
	client := NewClient(Config{APIKey: "secret", Model: "gemini-test", BaseURL: server.URL})
	var text string
	var done bool
	var usage model.Usage
	err := client.Stream(context.Background(), model.GenerateRequest{Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}}}, func(event model.StreamEvent) error {
		if event.Kind == model.StreamEventTextDelta {
			text += event.Delta
		}
		if event.Kind == model.StreamEventDone {
			done = true
			usage = event.Usage
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" || !done || usage.TotalTokens != 3 {
		t.Fatalf("text=%q done=%v usage=%#v", text, done, usage)
	}
}

func TestToolResponseRoundTrip(t *testing.T) {
	body, err := buildRequest(model.GenerateRequest{Messages: []model.Message{
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"q":"x"}`)}}},
		{Role: model.RoleTool, ToolCallID: "call-1", Content: "ok"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(body)
	if !strings.Contains(string(data), `"functionCall"`) || !strings.Contains(string(data), `"functionResponse"`) || !strings.Contains(string(data), `"lookup"`) {
		t.Fatalf("body = %s", data)
	}
}

func TestBuildRequestUsesCanonicalGeminiImageFields(t *testing.T) {
	body, err := buildRequest(model.GenerateRequest{Messages: []model.Message{{
		Role: model.RoleUser,
		Parts: []model.ContentPart{{
			Type: model.ContentPartImage, MediaType: "image/png", Data: "aGVsbG8=",
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(body)
	text := string(data)
	if !strings.Contains(text, `"inlineData"`) || !strings.Contains(text, `"mimeType":"image/png"`) {
		t.Fatalf("body = %s", data)
	}
	if strings.Contains(text, `"inline_data"`) || strings.Contains(text, `"mime_type"`) {
		t.Fatalf("body uses non-canonical field names: %s", data)
	}
}
