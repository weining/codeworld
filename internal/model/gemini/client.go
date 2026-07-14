package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"codeworld/internal/model"
)

// Config configures the native Google Gemini generateContent API.
type Config struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

// Client implements Gemini generateContent and streamGenerateContent.
type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a native Gemini client.
func NewClient(cfg Config) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{apiKey: cfg.APIKey, model: cfg.Model, baseURL: baseURL, httpClient: httpClient}
}

// Generate performs one non-streaming Gemini request.
func (c *Client) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	body, err := buildRequest(req)
	if err != nil {
		return model.GenerateResponse{}, err
	}
	var response responseBody
	if err := c.request(ctx, req.Model, false, body, func(reader io.Reader) error {
		return json.NewDecoder(reader).Decode(&response)
	}); err != nil {
		return model.GenerateResponse{}, err
	}
	return response.toModel()
}

// Stream performs a Gemini SSE request and emits provider-neutral deltas.
func (c *Client) Stream(ctx context.Context, req model.GenerateRequest, emit func(model.StreamEvent) error) error {
	body, err := buildRequest(req)
	if err != nil {
		return err
	}
	return c.request(ctx, req.Model, true, body, func(reader io.Reader) error {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 4<<20)
		var final model.Message
		var usage model.Usage
		var toolCalls []model.ToolCall
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			var chunk responseBody
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				return fmt.Errorf("decode Gemini stream: %w", err)
			}
			chunkUsage := chunk.modelUsage()
			if chunkUsage != (model.Usage{}) {
				// Gemini reports cumulative usage metadata, normally on the final SSE item.
				usage = chunkUsage
			}
			if len(chunk.Candidates) == 0 {
				continue
			}
			message, calls, chunkUsage, err := chunk.content()
			if err != nil {
				return err
			}
			if message.Content != "" {
				final.Content += message.Content
				if emit != nil {
					if err := emit(model.StreamEvent{Kind: model.StreamEventTextDelta, Delta: message.Content}); err != nil {
						return err
					}
				}
			}
			if len(calls) > 0 {
				for index := range calls {
					calls[index].ID = fmt.Sprintf("gemini-call-%d", len(toolCalls)+index+1)
				}
				toolCalls = append(toolCalls, calls...)
				if emit != nil {
					if err := emit(model.StreamEvent{Kind: model.StreamEventToolCall, ToolCalls: calls}); err != nil {
						return err
					}
				}
			}
			if chunkUsage != (model.Usage{}) {
				usage = chunkUsage
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		final.Role = model.RoleAssistant
		final.ToolCalls = toolCalls
		if emit != nil {
			return emit(model.StreamEvent{Kind: model.StreamEventDone, Message: final, ToolCalls: toolCalls, Usage: usage})
		}
		return nil
	})
}

func (c *Client) request(ctx context.Context, modelName string, stream bool, body requestBody, consume func(io.Reader) error) error {
	if strings.TrimSpace(c.apiKey) == "" {
		return fmt.Errorf("Gemini API key is not set")
	}
	modelName = firstNonEmpty(strings.TrimSpace(modelName), c.model)
	if modelName == "" {
		return fmt.Errorf("Gemini model is not set")
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	method := "generateContent"
	query := ""
	if stream {
		method = "streamGenerateContent"
		query = "?alt=sse"
	}
	endpoint := c.baseURL + "/models/" + url.PathEscape(modelName) + ":" + method + query
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("Gemini status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return consume(resp.Body)
}

type requestBody struct {
	SystemInstruction *content      `json:"systemInstruction,omitempty"`
	Contents          []content     `json:"contents"`
	Tools             []toolWrapper `json:"tools,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *inlineData       `json:"inlineData,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}

type inlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type functionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type functionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type toolWrapper struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations"`
}

type functionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

func buildRequest(req model.GenerateRequest) (requestBody, error) {
	var body requestBody
	callNames := map[string]string{}
	for _, message := range req.Messages {
		for _, call := range message.ToolCalls {
			callNames[call.ID] = call.Name
		}
	}
	for _, message := range req.Messages {
		switch message.Role {
		case model.RoleSystem:
			if strings.TrimSpace(message.Content) == "" {
				continue
			}
			if body.SystemInstruction == nil {
				body.SystemInstruction = &content{Parts: []part{{Text: message.Content}}}
			} else {
				body.SystemInstruction.Parts = append(body.SystemInstruction.Parts, part{Text: message.Content})
			}
		case model.RoleTool:
			name := callNames[message.ToolCallID]
			if name == "" {
				name = "tool"
			}
			body.Contents = append(body.Contents, content{Role: "user", Parts: []part{{FunctionResponse: &functionResponse{Name: name, Response: map[string]any{"result": message.Content}}}}})
		case model.RoleAssistant:
			parts, err := messageParts(message)
			if err != nil {
				return requestBody{}, err
			}
			for _, call := range message.ToolCalls {
				var args map[string]any
				if len(call.Arguments) > 0 {
					if err := json.Unmarshal(call.Arguments, &args); err != nil {
						return requestBody{}, fmt.Errorf("decode tool arguments: %w", err)
					}
				}
				parts = append(parts, part{FunctionCall: &functionCall{Name: call.Name, Args: args}})
			}
			body.Contents = append(body.Contents, content{Role: "model", Parts: parts})
		default:
			parts, err := messageParts(message)
			if err != nil {
				return requestBody{}, err
			}
			body.Contents = append(body.Contents, content{Role: "user", Parts: parts})
		}
	}
	if len(req.Tools) > 0 {
		wrapper := toolWrapper{FunctionDeclarations: make([]functionDeclaration, 0, len(req.Tools))}
		for _, definition := range req.Tools {
			wrapper.FunctionDeclarations = append(wrapper.FunctionDeclarations, functionDeclaration{Name: definition.Name, Description: definition.Description, Parameters: definition.InputSchema})
		}
		body.Tools = []toolWrapper{wrapper}
	}
	return body, nil
}

func messageParts(message model.Message) ([]part, error) {
	if len(message.Parts) == 0 {
		if message.Content == "" {
			return []part{}, nil
		}
		return []part{{Text: message.Content}}, nil
	}
	parts := make([]part, 0, len(message.Parts))
	for _, item := range message.Parts {
		switch item.Type {
		case model.ContentPartText:
			parts = append(parts, part{Text: item.Text})
		case model.ContentPartImage:
			if item.Data == "" {
				return nil, fmt.Errorf("Gemini image input requires inline base64 data")
			}
			parts = append(parts, part{InlineData: &inlineData{MIMEType: item.MediaType, Data: item.Data}})
		}
	}
	return parts, nil
}

type responseBody struct {
	Candidates []struct {
		Content content `json:"content"`
	} `json:"candidates"`
	Usage struct {
		PromptTokens    int `json:"promptTokenCount"`
		CandidateTokens int `json:"candidatesTokenCount"`
		CachedTokens    int `json:"cachedContentTokenCount"`
		TotalTokens     int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func (r responseBody) content() (model.Message, []model.ToolCall, model.Usage, error) {
	usage := r.modelUsage()
	if len(r.Candidates) == 0 {
		return model.Message{}, nil, usage, fmt.Errorf("Gemini returned no candidates")
	}
	message := model.Message{Role: model.RoleAssistant}
	var calls []model.ToolCall
	for index, item := range r.Candidates[0].Content.Parts {
		message.Content += item.Text
		if item.FunctionCall != nil {
			args, err := json.Marshal(item.FunctionCall.Args)
			if err != nil {
				return model.Message{}, nil, usage, err
			}
			calls = append(calls, model.ToolCall{ID: fmt.Sprintf("gemini-call-%d", index+1), Name: item.FunctionCall.Name, Arguments: args})
		}
	}
	message.ToolCalls = calls
	return message, calls, usage, nil
}

func (r responseBody) modelUsage() model.Usage {
	return model.Usage{InputTokens: r.Usage.PromptTokens, OutputTokens: r.Usage.CandidateTokens, CacheTokens: r.Usage.CachedTokens, TotalTokens: r.Usage.TotalTokens}
}

func (r responseBody) toModel() (model.GenerateResponse, error) {
	message, calls, usage, err := r.content()
	if err != nil {
		return model.GenerateResponse{}, err
	}
	finalText := message.Content
	if len(calls) > 0 {
		finalText = ""
	}
	return model.GenerateResponse{Message: message, ToolCalls: calls, FinalText: finalText, Usage: usage}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
