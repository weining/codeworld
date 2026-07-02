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

func NewSkillOpenTool(skills []skill.Skill) Tool {
	index := make(map[string]skill.Skill, len(skills))
	for _, item := range skills {
		index[item.Name] = item
	}
	return skillOpenTool{skills: index}
}

func (t skillOpenTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "skill_open",
		Description: "Read the full instructions for an available skill by name.",
		InputSchema: objectSchema(map[string]any{
			"name": map[string]any{"type": "string"},
		}, []string{"name"}),
	}
}

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
