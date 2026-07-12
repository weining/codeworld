package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetUserFeature 更新用户 config.toml 中可切换的 feature，同时保留其他配置和注释。
func SetUserFeature(home, feature string, enabled bool) error {
	key := ""
	switch feature {
	case "plugins":
		key = "plugins_enabled"
	case "model_call_logging":
		key = "model_call_logging"
	default:
		return fmt.Errorf("feature %q is not configurable", feature)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	path := filepath.Join(home, "config.toml")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	replacement := fmt.Sprintf("%s = %t", key, enabled)
	replaced := false
	insertAt := len(lines)
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[[") && insertAt == len(lines) {
			insertAt = index
		}
		if insertAt == len(lines) {
			if current, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(current) == key {
				lines[index], replaced = replacement, true
			}
		}
	}
	if !replaced {
		lines = append(lines, "")
		copy(lines[insertAt+1:], lines[insertAt:])
		lines[insertAt] = replacement
	}
	content := strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
	tmp, err := os.CreateTemp(home, ".config-*.toml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
