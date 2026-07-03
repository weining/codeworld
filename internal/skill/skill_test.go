package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadProjectSkillsParsesFrontmatter 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadProjectSkillsParsesFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "reviewer", `---
name: reviewer
description: Review Go changes carefully.
---

# Reviewer

Check tests before claiming completion.
`)

	skills, err := LoadProject(root)
	if err != nil {
		t.Fatalf("LoadProject returned error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("skills = %#v, want one skill", skills)
	}
	if skills[0].Name != "reviewer" || skills[0].Description != "Review Go changes carefully." {
		t.Fatalf("metadata = %#v, want parsed name and description", skills[0])
	}
	if !strings.Contains(skills[0].Content, "Check tests before claiming completion.") {
		t.Fatalf("content = %q, want full skill body", skills[0].Content)
	}
}

// TestContextIncludesProjectSkills 验证对应场景的行为，避免后续改动破坏既有约束。
func TestContextIncludesProjectSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "go-style", "# Go Style\n\nPrefer small interfaces.\n")

	skills, err := LoadProject(root)
	if err != nil {
		t.Fatalf("LoadProject returned error: %v", err)
	}
	ctx := Context(skills)
	if !strings.Contains(ctx, "Project skills:") || !strings.Contains(ctx, "go-style") || !strings.Contains(ctx, "Prefer small interfaces.") {
		t.Fatalf("context = %q, want project skill text", ctx)
	}
}

// TestIndexListsSkillsWithoutFullContent 验证对应场景的行为，避免后续改动破坏既有约束。
func TestIndexListsSkillsWithoutFullContent(t *testing.T) {
	skills := []Skill{{
		Name:        "reviewer",
		Description: "Review Go changes.",
		Path:        "/repo/.codeworld/skills/reviewer/SKILL.md",
		Source:      "project",
		Content:     "secret long workflow body",
	}}

	index := Index(skills)
	if !strings.Contains(index, "reviewer") || !strings.Contains(index, "Review Go changes.") || !strings.Contains(index, "project") {
		t.Fatalf("index = %q, want skill metadata", index)
	}
	if strings.Contains(index, "secret long workflow body") {
		t.Fatalf("index = %q, should not include full skill content", index)
	}
}

// TestLoadProjectSkipsMissingSkillsDirectory 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadProjectSkipsMissingSkillsDirectory(t *testing.T) {
	skills, err := LoadProject(t.TempDir())
	if err != nil {
		t.Fatalf("LoadProject returned error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("skills = %#v, want none", skills)
	}
}

// writeSkill 是测试辅助函数，用于复用测试准备或断言逻辑。
func writeSkill(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, ".codeworld", "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
