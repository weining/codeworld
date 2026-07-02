package instructions

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const DefaultMaxBytes = 32 * 1024

type Options struct {
	MaxBytes int
}

type File struct {
	Path string
	Text string
}

type Loaded struct {
	Files     []File
	Text      string
	Truncated bool
}

func Load(root string, opts Options) (Loaded, error) {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Loaded{}, err
	}
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return Loaded{}, err
	}
	start := gitRoot(absRoot)
	if start == "" {
		start = absRoot
	}
	// 从 git 根目录到当前工作目录逐级加载，让更深层目录可以补充局部规则。
	dirs, err := pathChain(start, absRoot)
	if err != nil {
		return Loaded{}, err
	}
	var loaded Loaded
	for _, dir := range dirs {
		file, ok, err := readFirstInstruction(dir)
		if err != nil {
			return Loaded{}, err
		}
		if !ok {
			continue
		}
		loaded.Files = append(loaded.Files, file)
	}
	loaded.Text, loaded.Truncated = combine(loaded.Files, opts.MaxBytes)
	return loaded, nil
}

func readFirstInstruction(dir string) (File, bool, error) {
	// override 文件优先级高于普通 AGENTS，便于项目在子目录覆盖默认约定。
	for _, name := range []string{"AGENTS.override.md", "AGENTS.md"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return File{}, false, err
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			continue
		}
		return File{Path: path, Text: text}, true, nil
	}
	return File{}, false, nil
}

func combine(files []File, maxBytes int) (string, bool) {
	var b strings.Builder
	truncated := false
	for _, file := range files {
		section := fmt.Sprintf("## %s\n\n%s\n", filepath.ToSlash(file.Path), file.Text)
		// 项目指令会进入 system prompt，必须有硬上限，避免挤掉用户问题和工具结果。
		if b.Len()+len(section) > maxBytes {
			remaining := maxBytes - b.Len()
			if remaining > 0 {
				b.WriteString(section[:remaining])
			}
			truncated = true
			break
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(section)
	}
	text := strings.TrimSpace(b.String())
	if truncated {
		text = strings.TrimSpace(text) + "\n\n[instructions truncated]"
	}
	return text, truncated
}

func gitRoot(root string) string {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return ""
	}
	return filepath.Clean(strings.TrimSpace(stdout.String()))
}

func pathChain(start, end string) ([]string, error) {
	rel, err := filepath.Rel(start, end)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return []string{start}, nil
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return []string{end}, nil
	}
	dirs := []string{start}
	current := start
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		dirs = append(dirs, current)
	}
	return dirs, nil
}
