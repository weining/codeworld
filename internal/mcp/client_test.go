package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestStdioClientListsAndCallsTools(t *testing.T) {
	if os.Getenv("CODEWORLD_MCP_TEST_SERVER") == "1" {
		runFakeServer()
		return
	}
	ctx := context.Background()
	cmd := exec.Command(os.Args[0], "-test.run=TestStdioClientListsAndCallsTools")
	cmd.Env = append(os.Environ(), "CODEWORLD_MCP_TEST_SERVER=1")

	client, err := StartStdio(ctx, ServerConfig{Name: "demo", Command: cmd.Path, Args: cmd.Args[1:], Env: cmd.Env})
	if err != nil {
		t.Fatalf("StartStdio returned error: %v", err)
	}
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" || tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tools = %#v, want echo tool", tools)
	}
	result, err := client.CallTool(ctx, "echo", json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if result.Text() != "hello" {
		t.Fatalf("result = %#v, want hello text", result)
	}
}

func TestStdioClientListsResourcesAndPrompts(t *testing.T) {
	if os.Getenv("CODEWORLD_MCP_TEST_SERVER") == "1" {
		runFakeServer()
		return
	}
	ctx := context.Background()
	cmd := exec.Command(os.Args[0], "-test.run=TestStdioClientListsResourcesAndPrompts")
	cmd.Env = append(os.Environ(), "CODEWORLD_MCP_TEST_SERVER=1")

	client, err := StartStdio(ctx, ServerConfig{Name: "demo", Command: cmd.Path, Args: cmd.Args[1:], Env: cmd.Env})
	if err != nil {
		t.Fatalf("StartStdio returned error: %v", err)
	}
	defer client.Close()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	resources, err := client.ListResources(ctx)
	if err != nil {
		t.Fatalf("ListResources returned error: %v", err)
	}
	if len(resources) != 1 || resources[0].URI != "file:///demo.md" {
		t.Fatalf("resources = %#v, want demo resource", resources)
	}
	contents, err := client.ReadResource(ctx, "file:///demo.md")
	if err != nil {
		t.Fatalf("ReadResource returned error: %v", err)
	}
	if len(contents) != 1 || contents[0].Text != "resource text" {
		t.Fatalf("contents = %#v, want resource text", contents)
	}
	prompts, err := client.ListPrompts(ctx)
	if err != nil {
		t.Fatalf("ListPrompts returned error: %v", err)
	}
	if len(prompts) != 1 || prompts[0].Name != "review" {
		t.Fatalf("prompts = %#v, want review prompt", prompts)
	}
}

func runFakeServer() {
	reader := bufio.NewReader(os.Stdin)
	for {
		msg, err := readFramed(reader)
		if err != nil {
			if err != io.EOF {
				fmt.Fprintln(os.Stderr, err)
			}
			return
		}
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(msg, &req); err != nil {
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "fake", "version": "test"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "echo", "description": "Echo text", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			var params struct {
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &params)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": params.Arguments["text"]}}}
		case "resources/list":
			result = map[string]any{"resources": []any{map[string]any{"uri": "file:///demo.md", "name": "demo"}}}
		case "resources/read":
			result = map[string]any{"contents": []any{map[string]any{"uri": "file:///demo.md", "text": "resource text"}}}
		case "prompts/list":
			result = map[string]any{"prompts": []any{map[string]any{"name": "review", "description": "Review code"}}}
		default:
			result = map[string]any{}
		}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(data), data)
	}
}

func readFramed(reader *bufio.Reader) ([]byte, error) {
	var length int
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if _, err := fmt.Sscanf(line, "Content-Length: %d", &length); err != nil {
			return nil, err
		}
	}
	if length <= 0 {
		return nil, fmt.Errorf("missing content length")
	}
	data := make([]byte, length)
	_, err := io.ReadFull(reader, data)
	return data, err
}
