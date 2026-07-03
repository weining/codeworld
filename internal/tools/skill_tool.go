package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/skill"
)

type skillOpenTool struct {
	skills map[string]skill.Skill
}

type skillOpenArgs struct {
	Name string `json:"name"`
}

// NewSkillOpenTool 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewSkillOpenTool(skills []skill.Skill) Tool {
	index := make(map[string]skill.Skill, len(skills))
	for _, item := range skills {
		index[item.Name] = item
	}
	return skillOpenTool{skills: index}
}

// Definition 返回工具暴露给模型的名称、描述和参数 schema。
func (t skillOpenTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "skill_open",
		Description: "Read the full instructions for an available skill by name.",
		InputSchema: objectSchema(map[string]any{
			"name": map[string]any{"type": "string"},
		}, []string{"name"}),
	}
}

// PermissionRequest 根据工具参数构造权限请求，供策略层在执行前判断。
func (t skillOpenTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseSkillOpenArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{
		Action: permissions.ActionRead,
		Target: "skill " + parsed.Name,
		Risk:   permissions.RiskRead,
		Reason: "read skill instructions",
	}, nil
}

// Execute 执行工具主体逻辑，并返回可序列化的工具结果。
func (t skillOpenTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	parsed, err := parseSkillOpenArgs(args)
	if err != nil {
		return Result{}, err
	}
	item, ok := t.skills[parsed.Name]
	if !ok {
		return Result{}, fmt.Errorf("unknown skill %q", parsed.Name)
	}
	return Result{
		Content: item.Content,
		Metadata: map[string]any{
			"name":        item.Name,
			"description": item.Description,
			"source":      item.Source,
			"path":        item.Path,
		},
	}, nil
}

// parseSkillOpenArgs 解析输入数据，并执行必要的格式校验。
func parseSkillOpenArgs(args json.RawMessage) (skillOpenArgs, error) {
	var parsed skillOpenArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return skillOpenArgs{}, err
	}
	parsed.Name = strings.TrimSpace(parsed.Name)
	if parsed.Name == "" {
		return skillOpenArgs{}, fmt.Errorf("name is required")
	}
	return parsed, nil
}
