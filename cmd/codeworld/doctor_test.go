package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorReportsRunnableConfigurationAsJSON(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	configText := "provider = \"local\"\nlocal_base_url = \"http://127.0.0.1:11434/v1\"\nsandbox_mode = \"danger-full-access\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEWORLD_HOME", home)
	t.Setenv("TERM", "xterm-256color")
	var out bytes.Buffer
	if err := runDoctorCommand(&out, root, globalOptions{}, []string{"--json"}); err != nil {
		t.Fatalf("runDoctorCommand: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, out.String())
	}
	if report.Failed != 0 || report.Passed == 0 || report.Warnings == 0 {
		t.Fatalf("report = %#v", report)
	}
	if !doctorHasCheck(report, "provider", "passed") || !doctorHasCheck(report, "sandbox", "warning") {
		t.Fatalf("checks = %#v", report.Checks)
	}
}

func TestDoctorFailsWhenProviderCredentialIsMissing(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("sandbox_mode = \"danger-full-access\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEWORLD_HOME", home)
	t.Setenv("DEEPSEEK_API_KEY", "")
	var out bytes.Buffer
	err := runDoctorCommand(&out, root, globalOptions{}, nil)
	if err == nil || !strings.Contains(err.Error(), "failed check") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out.String(), "DEEPSEEK_API_KEY is not set") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestDoctorAcceptsCodexCompatibleDisplayOptions(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("provider = \"local\"\nlocal_base_url = \"http://127.0.0.1:11434/v1\"\nsandbox_mode = \"danger-full-access\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEWORLD_HOME", home)
	var out bytes.Buffer
	if err := runDoctorCommand(&out, root, globalOptions{}, []string{"--summary", "--all", "--no-color", "--ascii"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Summary:") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestCompletionScriptsAndValidation(t *testing.T) {
	tests := map[string]string{
		"bash":       "complete -F",
		"zsh":        "#compdef codeworld",
		"fish":       "complete -c codeworld",
		"powershell": "Register-ArgumentCompleter",
	}
	for shell, marker := range tests {
		t.Run(shell, func(t *testing.T) {
			var out bytes.Buffer
			if err := runCompletionCommand(&out, []string{shell}); err != nil {
				t.Fatal(err)
			}
			hasModelOption := strings.Contains(out.String(), "--model") || strings.Contains(out.String(), "-l model")
			if !strings.Contains(out.String(), marker) || !strings.Contains(out.String(), "doctor") || !hasModelOption {
				t.Fatalf("completion output = %q", out.String())
			}
		})
	}
	if err := runCompletionCommand(&bytes.Buffer{}, []string{"cmd"}); err == nil {
		t.Fatal("unsupported shell accepted")
	}
}

func doctorHasCheck(report doctorReport, name, status string) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
