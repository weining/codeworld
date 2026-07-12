package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"codeworld/internal/config"
	"codeworld/internal/session"
	"codeworld/internal/workspace"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type doctorReport struct {
	Checks   []doctorCheck `json:"checks"`
	Passed   int           `json:"passed"`
	Warnings int           `json:"warnings"`
	Failed   int           `json:"failed"`
}

func runDoctorCommand(out io.Writer, root string, global globalOptions, args []string) error {
	jsonOutput := false
	seen := map[string]bool{}
	for _, arg := range args {
		switch arg {
		case "--json", "--summary", "--all", "--no-color", "--ascii":
			if seen[arg] {
				return fmt.Errorf("doctor option %s may be specified only once", arg)
			}
			seen[arg] = true
			jsonOutput = jsonOutput || arg == "--json"
		default:
			return fmt.Errorf("usage: codeworld doctor [--json] [--summary] [--all] [--no-color] [--ascii]")
		}
	}

	report := buildDoctorReport(root, global)
	if err := writeDoctorReport(out, report, jsonOutput); err != nil {
		return err
	}
	if report.Failed > 0 {
		return fmt.Errorf("doctor found %d failed check(s)", report.Failed)
	}
	return nil
}

func buildDoctorReport(root string, global globalOptions) doctorReport {
	var checks []doctorCheck
	add := func(name, status, detail string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Detail: detail})
	}

	cfg, err := config.LoadWithOptions(root, config.LoadOptions{Profile: global.Profile, Overrides: global.Config})
	if err != nil {
		add("config", "failed", err.Error())
		return summarizeDoctorChecks(checks)
	}
	configDetail := fmt.Sprintf("provider=%s model=%s sandbox=%s", cfg.Provider, cfg.Model, cfg.SandboxMode)
	if cfg.Profile != "" {
		configDetail += " profile=" + cfg.Profile
	}
	add("config", "passed", configDetail)

	ws, err := workspace.New(root)
	if err != nil {
		add("workspace", "failed", err.Error())
	} else if ws.IsGitRepo() {
		add("workspace", "passed", ws.Root+" (Git repository)")
	} else {
		add("workspace", "warning", ws.Root+" (not a Git repository)")
	}

	addProviderCheck(&checks, root, cfg)
	addSandboxCheck(&checks, cfg)

	if len(cfg.MCPServers) == 0 {
		add("mcp", "passed", "no servers configured")
	} else {
		add("mcp", "passed", fmt.Sprintf("%d server(s) configured; connectivity not tested", len(cfg.MCPServers)))
	}

	store := session.NewStore(root)
	active, activeErr := store.List()
	archived, archivedErr := store.ListArchived()
	if activeErr != nil {
		add("sessions", "failed", "active sessions: "+activeErr.Error())
	} else if archivedErr != nil {
		add("sessions", "failed", "archived sessions: "+archivedErr.Error())
	} else {
		add("sessions", "passed", fmt.Sprintf("active=%d archived=%d", len(active), len(archived)))
	}

	term := strings.TrimSpace(os.Getenv("TERM"))
	if term == "" || term == "dumb" {
		add("terminal", "warning", fmt.Sprintf("%s/%s TERM=%q", runtime.GOOS, runtime.GOARCH, term))
	} else {
		add("terminal", "passed", fmt.Sprintf("%s/%s TERM=%s", runtime.GOOS, runtime.GOARCH, term))
	}
	return summarizeDoctorChecks(checks)
}

func addProviderCheck(checks *[]doctorCheck, root string, cfg config.Config) {
	check := doctorCheck{Name: "provider", Status: "passed"}
	switch cfg.Provider {
	case "", "deepseek":
		check.Detail = "DeepSeek credentials available"
		if cfg.APIKey == "" {
			check.Status, check.Detail = "failed", "DEEPSEEK_API_KEY is not set"
		}
	case "openai":
		check.Detail = "OpenAI credentials available"
		if cfg.OpenAIAPIKey == "" {
			check.Status, check.Detail = "failed", "OPENAI_API_KEY is not set"
		}
	case "anthropic":
		check.Detail = "Anthropic credentials available"
		if cfg.AnthropicAPIKey == "" {
			check.Status, check.Detail = "failed", "ANTHROPIC_API_KEY is not set"
		}
	case "local":
		check.Detail = "local endpoint configured: " + cfg.LocalBaseURL
	case "codex":
		status, err := inspectCodexAuth(root, time.Now())
		if err != nil {
			check.Status, check.Detail = "failed", "run codeworld auth codex login: "+err.Error()
		} else if status.Expired {
			check.Status, check.Detail = "warning", "Codex OAuth token is expired; it will be refreshed on use"
		} else {
			check.Detail = "Codex OAuth credentials available"
		}
	default:
		check.Status, check.Detail = "failed", fmt.Sprintf("unknown provider %q", cfg.Provider)
	}
	*checks = append(*checks, check)
}

func addSandboxCheck(checks *[]doctorCheck, cfg config.Config) {
	check := doctorCheck{Name: "sandbox", Status: "passed"}
	if cfg.SandboxMode == "danger-full-access" {
		check.Status, check.Detail = "warning", "danger-full-access bypasses the process sandbox"
		*checks = append(*checks, check)
		return
	}
	backend := ""
	switch runtime.GOOS {
	case "darwin":
		backend = "sandbox-exec"
	case "linux":
		backend = "bwrap"
	default:
		check.Status, check.Detail = "failed", "restricted process sandbox is unsupported on "+runtime.GOOS
		*checks = append(*checks, check)
		return
	}
	path, err := exec.LookPath(backend)
	if err != nil {
		check.Status, check.Detail = "failed", backend+" is not installed"
	} else {
		check.Detail = backend + " available at " + path
	}
	*checks = append(*checks, check)
}

func summarizeDoctorChecks(checks []doctorCheck) doctorReport {
	report := doctorReport{Checks: checks}
	for _, check := range checks {
		switch check.Status {
		case "passed":
			report.Passed++
		case "warning":
			report.Warnings++
		case "failed":
			report.Failed++
		}
	}
	return report
}

func writeDoctorReport(out io.Writer, report doctorReport, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(report)
	}
	for _, check := range report.Checks {
		if _, err := fmt.Fprintf(out, "%-8s %-10s %s\n", strings.ToUpper(check.Status), check.Name, check.Detail); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(out, "\nSummary: %d passed, %d warning(s), %d failed\n", report.Passed, report.Warnings, report.Failed)
	return err
}
