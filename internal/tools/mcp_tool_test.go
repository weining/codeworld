package tools

import (
	"context"
	"encoding/json"
	"testing"

	"codeworld/internal/mcp"
	"codeworld/internal/permissions"
)

type fakeMCPCaller struct {
	name string
	args json.RawMessage
}

// CallTool 是测试辅助函数，用于复用测试准备或断言逻辑。
func (f *fakeMCPCaller) CallTool(ctx context.Context, name string, args json.RawMessage) (mcp.CallToolResult, error) {
	f.name = name
	f.args = append(f.args[:0], args...)
	return mcp.CallToolResult{Content: []mcp.Content{{Type: "text", Text: "hello"}}}, nil
}

// TestMCPToolDefinitionIsNamespaced 验证对应场景的行为，避免后续改动破坏既有约束。
func TestMCPToolDefinitionIsNamespaced(t *testing.T) {
	tool := NewMCPTool("demo", mcp.Tool{Name: "echo", Description: "Echo", InputSchema: map[string]any{"type": "object"}}, &fakeMCPCaller{})

	def := tool.Definition()
	if def.Name != "mcp.demo.echo" || def.Description != "Echo" || def.InputSchema["type"] != "object" {
		t.Fatalf("definition = %#v, want namespaced mcp tool", def)
	}
}

// TestMCPToolExecutesThroughClient 验证对应场景的行为，避免后续改动破坏既有约束。
func TestMCPToolExecutesThroughClient(t *testing.T) {
	caller := &fakeMCPCaller{}
	tool := NewMCPTool("demo", mcp.Tool{Name: "echo", Description: "Echo", InputSchema: map[string]any{"type": "object"}}, caller)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if caller.name != "echo" || string(caller.args) != `{"text":"hello"}` {
		t.Fatalf("call = %s %s, want echo with raw args", caller.name, caller.args)
	}
	if result.Content != "hello" || result.Metadata["server"] != "demo" || result.Metadata["tool"] != "echo" {
		t.Fatalf("result = %#v, want MCP metadata and text", result)
	}
}

// TestMCPToolPermissionIsExecuteRisk 验证对应场景的行为，避免后续改动破坏既有约束。
func TestMCPToolPermissionIsExecuteRisk(t *testing.T) {
	tool := NewMCPTool("demo", mcp.Tool{Name: "echo"}, &fakeMCPCaller{})

	req, err := tool.PermissionRequest(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("PermissionRequest returned error: %v", err)
	}
	if req.Action != permissions.ActionShell || req.Risk != permissions.RiskExecute || req.Target != "mcp.demo.echo" {
		t.Fatalf("request = %#v, want execute risk for namespaced MCP tool", req)
	}
}
