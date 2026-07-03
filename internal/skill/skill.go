package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Skill struct {
	Name        string
	Description string
	Content     string
	Path        string
	Source      string
}

// LoadProject 加载外部或项目内配置，并把原始数据转换为内部结构。
func LoadProject(root string) ([]Skill, error) {
	// project skill 使用渐进披露：启动时只进入索引，真正内容由 skill_open 工具按需读取。
	skillsRoot := filepath.Join(root, ".codeworld", "skills")
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var skills []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(skillsRoot, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		skill := parse(entry.Name(), path, string(data))
		skill.Source = "project"
		if skill.Name == "" {
			continue
		}
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// LoadFile 加载外部或项目内配置，并把原始数据转换为内部结构。
func LoadFile(name, path, source string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	loaded := parse(name, path, string(data))
	loaded.Source = source
	return loaded, nil
}

// Context 提供对外可复用的能力，并隐藏内部实现细节。
func Context(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Project skills:\n")
	for _, skill := range skills {
		if skill.Description != "" {
			fmt.Fprintf(&b, "\n## %s\nDescription: %s\n\n%s\n", skill.Name, skill.Description, strings.TrimSpace(skill.Content))
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", skill.Name, strings.TrimSpace(skill.Content))
	}
	return strings.TrimSpace(b.String())
}

// Index 提供对外可复用的能力，并隐藏内部实现细节。
func Index(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available skills:\n")
	for _, skill := range skills {
		source := skill.Source
		if source == "" {
			source = "unknown"
		}
		if skill.Description != "" {
			fmt.Fprintf(&b, "- %s: %s (source=%s path=%s)\n", skill.Name, skill.Description, source, filepath.ToSlash(skill.Path))
			continue
		}
		fmt.Fprintf(&b, "- %s (source=%s path=%s)\n", skill.Name, source, filepath.ToSlash(skill.Path))
	}
	return strings.TrimSpace(b.String())
}

// parse 解析输入数据，并执行必要的格式校验。
func parse(dirName, path, content string) Skill {
	skill := Skill{Name: dirName, Content: content, Path: path}
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return skill
	}
	// 只解析 SKILL.md front matter 中当前需要的字段，避免引入完整 YAML 依赖。
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			return skill
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"")
		switch strings.TrimSpace(key) {
		case "name":
			if value != "" && filepath.Base(value) == value && !strings.Contains(value, "\\") {
				skill.Name = value
			}
		case "description":
			skill.Description = value
		}
	}
	return skill
}
