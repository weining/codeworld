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
}

func LoadProject(root string) ([]Skill, error) {
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
		if skill.Name == "" {
			continue
		}
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

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

func parse(dirName, path, content string) Skill {
	skill := Skill{Name: dirName, Content: content, Path: path}
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return skill
	}
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
