package indexer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const binaryProbeBytes = 8192

type Index struct {
	Root      string    `json:"root"`
	Entries   []Entry   `json:"entries"`
	CreatedAt time.Time `json:"created_at"`
}

type Entry struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	Ext      string    `json:"ext,omitempty"`
	Language string    `json:"language,omitempty"`
	Skipped  bool      `json:"skipped,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}

// Build 构建运行所需的数据结构，并在过程中收集必要的上下文。
func Build(root string, maxFileBytes int64) (Index, error) {
	if maxFileBytes <= 0 {
		maxFileBytes = 256 * 1024
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Index{}, err
	}
	var entries []Entry
	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if rel == ".git" || rel == ".codeworld/logs" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := Entry{
			Path:     rel,
			Size:     info.Size(),
			ModTime:  info.ModTime().UTC(),
			Ext:      strings.TrimPrefix(filepath.Ext(rel), "."),
			Language: LanguageForPath(rel),
		}
		switch {
		case info.Size() > maxFileBytes:
			entry.Skipped = true
			entry.Reason = "too_large"
		case isBinary(path):
			entry.Skipped = true
			entry.Reason = "binary"
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return Index{}, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return Index{Root: absRoot, Entries: entries, CreatedAt: time.Now().UTC()}, nil
}

// Save 持久化当前状态，并处理路径、权限或归档细节。
func Save(path string, idx Index) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

// Load 加载外部或项目内配置，并把原始数据转换为内部结构。
func Load(path string) (Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Index{}, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return Index{}, err
	}
	return idx, nil
}

// Summary 提供对外可复用的能力，并隐藏内部实现细节。
func Summary(idx Index, limit int) string {
	if limit <= 0 {
		limit = 100
	}
	var lines []string
	for _, entry := range idx.Entries {
		if len(lines) >= limit {
			lines = append(lines, "[truncated]")
			break
		}
		status := ""
		if entry.Skipped {
			status = " skipped=" + entry.Reason
		}
		lines = append(lines, fmt.Sprintf("%s %s %d%s", entry.Path, entry.Language, entry.Size, status))
	}
	return strings.Join(lines, "\n")
}

// DefaultPath 提供对外可复用的能力，并隐藏内部实现细节。
func DefaultPath(root string) string {
	return filepath.Join(root, ".codeworld", "index.json")
}

// LanguageForPath 提供对外可复用的能力，并隐藏内部实现细节。
func LanguageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".md", ".markdown":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	default:
		return ""
	}
}

// isBinary 判断输入是否满足特定条件，并用于后续分支决策。
func isBinary(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if len(data) > binaryProbeBytes {
		data = data[:binaryProbeBytes]
	}
	return bytes.IndexByte(data, 0) >= 0
}
