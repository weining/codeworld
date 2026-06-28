package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"codeworld/internal/permissions"
	"codeworld/internal/plugin"
	"codeworld/internal/workspace"
)

func TestPluginToolRendersJSONFieldTemplates(t *testing.T) {
	tool := NewPluginTool(workspace.Workspace{Root: t.TempDir()}, plugin.Tool{
		Name:        "example.echo",
		Description: "Echo",
		Command:     "printf",
		Args:        []string{"{{text}}"},
		InputSchema: map[string]any{"type": "object"},
		Risk:        "read",
	})

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Content != "hello" {
		t.Fatalf("Content = %q, want hello", result.Content)
	}
}

func TestPluginToolRejectsMissingTemplateValue(t *testing.T) {
	tool := NewPluginTool(workspace.Workspace{Root: t.TempDir()}, plugin.Tool{
		Name:        "example.echo",
		Description: "Echo",
		Command:     "echo",
		Args:        []string{"{{text}}"},
		InputSchema: map[string]any{"type": "object"},
		Risk:        "read",
	})

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"other":"hello"}`))
	if err == nil || !strings.Contains(err.Error(), "missing template value") {
		t.Fatalf("err = %v, want missing template value", err)
	}
}

func TestPluginToolPermissionUsesConfiguredRisk(t *testing.T) {
	tool := NewPluginTool(workspace.Workspace{Root: t.TempDir()}, plugin.Tool{
		Name:        "example.test",
		Description: "Run tests",
		Command:     "go",
		Args:        []string{"test", "./..."},
		InputSchema: map[string]any{"type": "object"},
		Risk:        "execute",
	})

	req, err := tool.PermissionRequest(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("PermissionRequest returned error: %v", err)
	}
	if req.Action != permissions.ActionShell || req.Risk != permissions.RiskExecute || req.Target != "go test ./..." {
		t.Fatalf("request = %#v, want shell execute go test", req)
	}
}
