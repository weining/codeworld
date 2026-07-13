package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const maxMessageBytes = 16 * 1024 * 1024

type StartRequest struct {
	Prompt                string         `json:"prompt"`
	ApprovalPolicy        string         `json:"approval-policy,omitempty"`
	BaseInstructions      string         `json:"base-instructions,omitempty"`
	CompactPrompt         string         `json:"compact-prompt,omitempty"`
	Config                map[string]any `json:"config,omitempty"`
	CWD                   string         `json:"cwd,omitempty"`
	DeveloperInstructions string         `json:"developer-instructions,omitempty"`
	Model                 string         `json:"model,omitempty"`
	Sandbox               string         `json:"sandbox,omitempty"`
}

type ReplyRequest struct {
	ConversationID string `json:"conversationId,omitempty"`
	Prompt         string `json:"prompt"`
	ThreadID       string `json:"threadId,omitempty"`
}

type Response struct {
	ThreadID string `json:"threadId"`
	Content  string `json:"content"`
}

type Runner interface {
	Start(context.Context, StartRequest) (Response, error)
	Reply(context.Context, ReplyRequest) (Response, error)
}

type Server struct {
	Runner  Runner
	Version string
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type inboundRequest struct {
	req      request
	parseErr bool
	ready    chan struct{}
}

type cancellationRegistry struct {
	mu     sync.Mutex
	id     string
	cancel context.CancelFunc
}

func (r *cancellationRegistry) set(id string, cancel context.CancelFunc) {
	r.mu.Lock()
	r.id, r.cancel = id, cancel
	r.mu.Unlock()
}

func (r *cancellationRegistry) clear(id string) {
	r.mu.Lock()
	if r.id == id {
		r.id, r.cancel = "", nil
	}
	r.mu.Unlock()
}

func (r *cancellationRegistry) cancelRequest(raw json.RawMessage) {
	var params struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return
	}
	id := normalizedID(params.RequestID)
	r.mu.Lock()
	cancel := r.cancel
	matched := id != "" && id == r.id
	r.mu.Unlock()
	if matched && cancel != nil {
		cancel()
	}
}

// Serve 使用 MCP stdio 的逐行 JSON-RPC transport 提供 Codeworld tools。
func (s Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if s.Runner == nil {
		return fmt.Errorf("MCP server runner is required")
	}
	encoder := json.NewEncoder(out)
	inbound := make(chan inboundRequest)
	scanDone := make(chan error, 1)
	registry := &cancellationRegistry{}
	go scanRequests(in, inbound, scanDone, registry)
	for item := range inbound {
		if err := ctx.Err(); err != nil {
			close(item.ready)
			return err
		}
		if item.parseErr {
			close(item.ready)
			if encodeErr := encoder.Encode(errorResponse(json.RawMessage("null"), -32700, "Parse error")); encodeErr != nil {
				return encodeErr
			}
			continue
		}
		req := item.req
		if len(req.ID) == 0 {
			close(item.ready)
			_, _ = s.handle(ctx, req)
			continue
		}
		requestCtx, cancel := context.WithCancel(ctx)
		id := normalizedID(req.ID)
		registry.set(id, cancel)
		close(item.ready)
		result, rpcErr := s.handle(requestCtx, req)
		cancel()
		registry.clear(id)
		resp := response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	if err := <-scanDone; err != nil {
		return err
	}
	return ctx.Err()
}

func scanRequests(in io.Reader, out chan<- inboundRequest, done chan<- error, registry *cancellationRegistry) {
	defer close(out)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), maxMessageBytes)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			ready := make(chan struct{})
			out <- inboundRequest{parseErr: true, ready: ready}
			<-ready
			continue
		}
		if req.Method == "notifications/cancelled" {
			registry.cancelRequest(req.Params)
			continue
		}
		ready := make(chan struct{})
		out <- inboundRequest{req: req, ready: ready}
		<-ready
	}
	done <- scanner.Err()
}

func normalizedID(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func (s Server) handle(ctx context.Context, req request) (any, *rpcError) {
	if req.JSONRPC != "2.0" || req.Method == "" {
		return nil, &rpcError{Code: -32600, Message: "Invalid Request"}
	}
	switch req.Method {
	case "initialize":
		version := s.Version
		if version == "" {
			version = "dev"
		}
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
			"serverInfo":      map[string]any{"name": "codeworld-mcp-server", "title": "Codeworld", "version": version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefinitions()}, nil
	case "tools/call":
		return s.callTool(ctx, req.Params)
	case "notifications/initialized":
		return nil, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "Method not found"}
	}
}

func (s Server) callTool(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil || params.Name == "" {
		return nil, &rpcError{Code: -32602, Message: "Invalid params"}
	}
	var result Response
	var err error
	switch params.Name {
	case "codex":
		var args StartRequest
		if decodeErr := json.Unmarshal(params.Arguments, &args); decodeErr != nil || args.Prompt == "" {
			return nil, &rpcError{Code: -32602, Message: "codex requires a non-empty prompt"}
		}
		result, err = s.Runner.Start(ctx, args)
	case "codex-reply":
		var args ReplyRequest
		if decodeErr := json.Unmarshal(params.Arguments, &args); decodeErr != nil || args.Prompt == "" || args.ThreadID == "" && args.ConversationID == "" {
			return nil, &rpcError{Code: -32602, Message: "codex-reply requires prompt and threadId"}
		}
		result, err = s.Runner.Reply(ctx, args)
	default:
		return nil, &rpcError{Code: -32602, Message: fmt.Sprintf("unknown tool %q", params.Name)}
	}
	if err != nil {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		}, nil
	}
	structured := map[string]any{"threadId": result.ThreadID, "content": result.Content}
	data, _ := json.Marshal(structured)
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(data)}},
		"structuredContent": structured,
		"isError":           false,
	}, nil
}

func errorResponse(id json.RawMessage, code int, message string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

func toolDefinitions() []map[string]any {
	outputSchema := map[string]any{
		"type": "object", "properties": map[string]any{
			"threadId": map[string]any{"type": "string"},
			"content":  map[string]any{"type": "string"},
		}, "required": []string{"threadId", "content"},
	}
	return []map[string]any{
		{
			"name": "codex", "title": "Codeworld", "description": "Run a Codeworld session. Accepts configuration parameters matching the Codeworld runtime.",
			"inputSchema": map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"approval-policy":        map[string]any{"type": "string", "enum": []string{"untrusted", "on-request", "never"}, "description": "Approval policy for model-generated commands."},
					"base-instructions":      map[string]any{"type": "string", "description": "Instructions to use instead of the default system instructions."},
					"compact-prompt":         map[string]any{"type": "string", "description": "Prompt used when compacting the conversation."},
					"config":                 map[string]any{"type": "object", "additionalProperties": true, "description": "Runtime config overrides."},
					"cwd":                    map[string]any{"type": "string", "description": "Working directory for the session."},
					"developer-instructions": map[string]any{"type": "string", "description": "Additional developer instructions."},
					"model":                  map[string]any{"type": "string", "description": "Optional model override."},
					"prompt":                 map[string]any{"type": "string", "description": "Initial user prompt."},
					"sandbox":                map[string]any{"type": "string", "enum": []string{"read-only", "workspace-write", "danger-full-access"}, "description": "Process sandbox mode."},
				},
				"required": []string{"prompt"},
			},
			"outputSchema": outputSchema,
		},
		{
			"name": "codex-reply", "title": "Codeworld Reply", "description": "Continue a Codeworld conversation by thread id.",
			"inputSchema": map[string]any{
				"type": "object", "properties": map[string]any{
					"conversationId": map[string]any{"type": "string", "description": "Deprecated alias for threadId."},
					"prompt":         map[string]any{"type": "string", "description": "Next user prompt."},
					"threadId":       map[string]any{"type": "string", "description": "Thread id returned by codex."},
				},
				"required": []string{"prompt"},
			},
			"outputSchema": outputSchema,
		},
	}
}
