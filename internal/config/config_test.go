package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.Provider != "deepseek" {
		t.Fatalf("Provider = %q, want deepseek", cfg.Provider)
	}
	if cfg.Model != "deepseek-v4-pro" {
		t.Fatalf("Model = %q, want deepseek-v4-pro", cfg.Model)
	}
	if cfg.MaxSteps != 20 {
		t.Fatalf("MaxSteps = %d, want 20", cfg.MaxSteps)
	}
	if cfg.Workspace != "." {
		t.Fatalf("Workspace = %q, want .", cfg.Workspace)
	}
}

func TestLoadProjectConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	writeFile(t, filepath.Join(dir, ".codeworld", "config.toml"), "provider = \"deepseek\"\nmodel = \"deepseek-v4-flash\"\nmax_steps = 7\nworkspace = \".\"\n")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Model != "deepseek-v4-flash" {
		t.Fatalf("Model = %q, want deepseek-v4-flash", cfg.Model)
	}
	if cfg.MaxSteps != 7 {
		t.Fatalf("MaxSteps = %d, want 7", cfg.MaxSteps)
	}
	if cfg.APIKey != "test-key" {
		t.Fatalf("APIKey was not loaded from environment")
	}
}

func TestLoadRejectsUnknownConfigKeys(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".codeworld", "config.toml"), "provider = \"deepseek\"\nextra = \"nope\"\n")

	if _, err := Load(dir); err == nil {
		t.Fatalf("Load accepted an unknown config key")
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
