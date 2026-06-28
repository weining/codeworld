package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/plugin"
	"codeworld/internal/workspace"
)

type pluginTool struct {
	workspace workspace.Workspace
	spec      plugin.Tool
}

func NewPluginTool(ws workspace.Workspace, spec plugin.Tool) Tool {
	return pluginTool{workspace: ws, spec: spec}
}

func (t pluginTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        t.spec.Name,
		Description: t.spec.Description,
		InputSchema: t.spec.InputSchema,
	}
}

func (t pluginTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	command, err := t.renderCommand(args)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{
		Action: permissions.ActionShell,
		Target: command,
		Risk:   pluginRisk(t.spec.Risk),
		Reason: "plugin tool " + t.spec.Name,
	}, nil
}

func (t pluginTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	rendered, err := t.renderArgs(args)
	if err != nil {
		return Result{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, defaultShellTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, t.spec.Command, rendered...)
	cmd.Dir = t.workspace.Root
	configureCommandProcessGroup(cmd)
	stdout := &cappedBuffer{limit: defaultReadLimit}
	stderr := &cappedBuffer{limit: defaultReadLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	start := time.Now()
	err = cmd.Run()
	killCommandProcessGroup(cmd)
	content, truncated := combineCommandOutput(stdout, stderr, defaultReadLimit)
	return Result{
		Content: content,
		Metadata: map[string]any{
			"command":          strings.TrimSpace(t.spec.Command + " " + strings.Join(rendered, " ")),
			"exit_code":        exitCode(err),
			"duration_ms":      time.Since(start).Milliseconds(),
			"truncated":        truncated,
			"stdout_truncated": stdout.truncated(),
			"stderr_truncated": stderr.truncated(),
		},
	}, err
}

func (t pluginTool) renderCommand(args json.RawMessage) (string, error) {
	rendered, err := t.renderArgs(args)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(t.spec.Command + " " + strings.Join(rendered, " ")), nil
}

func (t pluginTool) renderArgs(args json.RawMessage) ([]string, error) {
	values := map[string]any{}
	if err := decodeArgs(args, &values); err != nil {
		return nil, err
	}
	rendered := make([]string, 0, len(t.spec.Args))
	for _, arg := range t.spec.Args {
		value, ok := renderTemplate(arg, values)
		if !ok {
			return nil, fmt.Errorf("missing template value for %q", arg)
		}
		rendered = append(rendered, value)
	}
	return rendered, nil
}

func renderTemplate(template string, values map[string]any) (string, bool) {
	if !strings.HasPrefix(template, "{{") || !strings.HasSuffix(template, "}}") {
		return template, true
	}
	key := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(template, "{{"), "}}"))
	value, ok := values[key]
	if !ok {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		return typed, true
	case float64, bool:
		return fmt.Sprint(typed), true
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return "", false
		}
		return string(data), true
	}
}

func pluginRisk(risk string) permissions.Risk {
	switch risk {
	case "read":
		return permissions.RiskRead
	case "write":
		return permissions.RiskWrite
	case "execute":
		return permissions.RiskExecute
	default:
		return permissions.RiskExecute
	}
}
