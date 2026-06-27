package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Provider  string
	Model     string
	MaxSteps  int
	Workspace string
	APIKey    string
}

func Default() Config {
	return Config{
		Provider:  "deepseek",
		Model:     "deepseek-v4-pro",
		MaxSteps:  20,
		Workspace: ".",
	}
}

func Load(root string) (Config, error) {
	cfg := Default()
	path := filepath.Join(root, ".codeworld", "config.toml")

	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return Config{}, err
		}
		cfg.APIKey = os.Getenv("DEEPSEEK_API_KEY")
		return cfg, nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("invalid config line %q", line)
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"")

		switch key {
		case "provider":
			cfg.Provider = value
		case "model":
			cfg.Model = value
		case "max_steps":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid max_steps %q: %w", value, err)
			}
			cfg.MaxSteps = n
		case "workspace":
			cfg.Workspace = value
		default:
			return Config{}, fmt.Errorf("unknown config key %q", key)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}

	cfg.APIKey = os.Getenv("DEEPSEEK_API_KEY")
	return cfg, nil
}
