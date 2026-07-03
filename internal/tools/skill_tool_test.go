package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"codeworld/internal/permissions"
	"codeworld/internal/skill"
)

// TestSkillOpenToolReturnsFullSkillContent 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSkillOpenToolReturnsFullSkillContent(t *testing.T) {
	tool := NewSkillOpenTool([]skill.Skill{{
		Name:        "reviewer",
		Description: "Review Go changes.",
		Source:      "project",
		Path:        "/repo/.codeworld/skills/reviewer/SKILL.md",
		Content:     "full workflow body",
	}})

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"name":"reviewer"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Content, "full workflow body") || result.Metadata["name"] != "reviewer" {
		t.Fatalf("result = %#v, want full skill content and metadata", result)
	}
}

// TestSkillOpenToolPermissionIsRead 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSkillOpenToolPermissionIsRead(t *testing.T) {
	tool := NewSkillOpenTool([]skill.Skill{{Name: "reviewer"}})

	req, err := tool.PermissionRequest(json.RawMessage(`{"name":"reviewer"}`))
	if err != nil {
		t.Fatalf("PermissionRequest returned error: %v", err)
	}
	if req.Action != permissions.ActionRead || req.Risk != permissions.RiskRead || req.Target != "skill reviewer" {
		t.Fatalf("request = %#v, want read permission for skill", req)
	}
}
