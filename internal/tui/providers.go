package tui

import (
	"fmt"
	"strings"

	"codeworld/internal/llmprofile"
)

type providerWizard struct {
	action  string
	step    int
	profile llmprofile.Profile
}

func (m *Model) handleProvidersCommand(line string) {
	fields := strings.Fields(line)
	if len(fields) == 1 || len(fields) == 2 && fields[1] == "list" {
		m.listProviderProfiles()
		return
	}
	if m.rt.ProviderProfiles == nil {
		m.appendError("provider profiles are unavailable")
		return
	}
	switch fields[1] {
	case "add":
		if len(fields) != 2 {
			m.appendError("usage: /providers add")
			return
		}
		m.providerWizard = &providerWizard{action: "add", profile: llmprofile.Profile{APIFormat: llmprofile.FormatOpenAI}}
		m.showProviderWizardPrompt()
	case "edit":
		if len(fields) != 3 {
			m.appendError("usage: /providers edit <id>")
			return
		}
		profile, ok := m.rt.ProviderProfiles.Get(fields[2])
		if !ok {
			m.appendError(fmt.Sprintf("provider profile %q not found", fields[2]))
			return
		}
		m.providerWizard = &providerWizard{action: "edit", step: 1, profile: profile}
		m.showProviderWizardPrompt()
	case "delete":
		if len(fields) != 3 {
			m.appendError("usage: /providers delete <id>")
			return
		}
		profile, ok := m.rt.ProviderProfiles.Get(fields[2])
		if !ok {
			m.appendError(fmt.Sprintf("provider profile %q not found", fields[2]))
			return
		}
		if m.rt.ActiveProfile == profile.ID {
			m.appendError("cannot delete the active provider; use another profile first")
			return
		}
		m.providerWizard = &providerWizard{action: "delete", profile: profile}
		m.appendNotice(fmt.Sprintf("Delete %s? Type DELETE to confirm, or Esc to cancel.", profileLabel(profile)))
	case "use":
		if len(fields) != 3 {
			m.appendError("usage: /providers use <id>")
			return
		}
		if err := m.rt.UseProviderProfile(fields[2]); err != nil {
			m.appendError("provider switch failed: " + err.Error())
			return
		}
		m.saveSession()
		m.appendNotice(fmt.Sprintf("provider=%s model=%s", fields[2], m.rt.Session.Model))
	case "cancel":
		m.cancelProviderWizard()
	default:
		m.appendError("usage: /providers [list|add|edit <id>|delete <id>|use <id>|cancel]")
	}
}

func (m *Model) listProviderProfiles() {
	if m.rt.ProviderProfiles == nil || len(m.rt.ProviderProfiles.List()) == 0 {
		m.appendNotice("no LLM provider profiles configured\nstart with: /providers add")
		return
	}
	lines := []string{"LLM provider profiles:"}
	for _, profile := range m.rt.ProviderProfiles.List() {
		marker := " "
		if profile.ID == m.rt.ActiveProfile {
			marker = "*"
		}
		key := profile.APIKeyEnv
		if key == "" {
			key = "none/OAuth"
		}
		lines = append(lines, fmt.Sprintf("%s %s  format=%s  model=%s  url=%s  key=%s", marker, profileLabel(profile), profile.APIFormat, profile.Model, emptyDash(profile.BaseURL), key))
	}
	lines = append(lines, "commands: /providers add | edit <id> | delete <id> | use <id>")
	m.appendNotice(strings.Join(lines, "\n"))
}

func (m *Model) handleProviderWizardInput(value string) {
	if m.providerWizard == nil {
		return
	}
	value = strings.TrimSpace(value)
	if value == "/cancel" || value == "/providers cancel" {
		m.cancelProviderWizard()
		return
	}
	wizard := m.providerWizard
	if wizard.action == "delete" {
		if value != "DELETE" {
			m.appendError("deletion not confirmed; type DELETE or press Esc")
			return
		}
		if err := m.rt.ProviderProfiles.Delete(wizard.profile.ID); err != nil {
			m.appendError("delete provider: " + err.Error())
			return
		}
		m.providerWizard = nil
		m.input.Placeholder = "Describe a task or type / for commands"
		m.appendNotice("deleted provider profile " + wizard.profile.ID)
		return
	}

	switch wizard.step {
	case 0:
		if value == "" {
			m.appendError("provider ID is required")
			return
		}
		if err := llmprofile.ValidateID(value); err != nil {
			m.appendError("invalid provider ID: " + err.Error())
			return
		}
		if _, exists := m.rt.ProviderProfiles.Get(value); exists {
			m.appendError(fmt.Sprintf("provider profile %q already exists; use /providers edit %s", value, value))
			return
		}
		wizard.profile.ID = value
	case 1:
		if value != "" && value != "-" {
			wizard.profile.Name = value
		} else if wizard.action == "add" {
			wizard.profile.Name = wizard.profile.ID
		} else if value == "-" {
			wizard.profile.Name = wizard.profile.ID
		}
	case 2:
		oldFormat := wizard.profile.APIFormat
		oldBaseURL := wizard.profile.BaseURL
		oldAPIKeyEnv := wizard.profile.APIKeyEnv
		format, ok := parseProviderFormat(value, wizard.profile.APIFormat)
		if !ok {
			m.appendError("format must be openai, anthropic, gemini, or codex")
			return
		}
		wizard.profile.APIFormat = format
		if format != oldFormat || wizard.action == "add" || oldBaseURL == "" || oldBaseURL == llmprofile.DefaultBaseURL(oldFormat) {
			wizard.profile.BaseURL = llmprofile.DefaultBaseURL(format)
		}
		if format != oldFormat || oldAPIKeyEnv == "" || oldAPIKeyEnv == defaultAPIKeyEnv(oldFormat) {
			wizard.profile.APIKeyEnv = defaultAPIKeyEnv(format)
		}
	case 3:
		if value == "-" {
			wizard.profile.BaseURL = llmprofile.DefaultBaseURL(wizard.profile.APIFormat)
		} else if value != "" {
			wizard.profile.BaseURL = value
		}
		if err := llmprofile.ValidateBaseURL(wizard.profile.APIFormat, wizard.profile.BaseURL); err != nil {
			m.appendError("invalid base URL: " + err.Error())
			return
		}
	case 4:
		if value != "" {
			wizard.profile.Model = value
		}
		if wizard.profile.Model == "" {
			m.appendError("model is required")
			return
		}
	case 5:
		if value == "-" {
			wizard.profile.APIKeyEnv = ""
		} else if value != "" {
			wizard.profile.APIKeyEnv = value
		} else if wizard.action == "add" {
			wizard.profile.APIKeyEnv = defaultAPIKeyEnv(wizard.profile.APIFormat)
		}
		if (wizard.profile.APIFormat == llmprofile.FormatAnthropic || wizard.profile.APIFormat == llmprofile.FormatGemini) && wizard.profile.APIKeyEnv == "" {
			m.appendError(fmt.Sprintf("API key environment variable is required for %s", wizard.profile.APIFormat))
			return
		}
		if wizard.profile.APIFormat == llmprofile.FormatCodex && wizard.profile.APIKeyEnv != "" {
			m.appendError("Codex profiles use OAuth; enter '-' for no API key environment variable")
			return
		}
		if err := llmprofile.ValidateAPIKeyEnv(wizard.profile.APIKeyEnv); err != nil {
			m.appendError("invalid API key environment variable: " + err.Error())
			return
		}
	}
	wizard.step++
	if wizard.step <= 5 {
		m.showProviderWizardPrompt()
		return
	}
	wizard.profile = llmprofile.Normalize(wizard.profile)
	if err := m.rt.ProviderProfiles.Upsert(wizard.profile); err != nil {
		m.appendError("save provider: " + err.Error())
		wizard.step = 0
		if wizard.action == "edit" {
			wizard.step = 1
		}
		m.showProviderWizardPrompt()
		return
	}
	m.providerWizard = nil
	m.input.Placeholder = "Describe a task or type / for commands"
	m.appendNotice(fmt.Sprintf("saved provider %s\nuse it now with: /providers use %s", profileLabel(wizard.profile), wizard.profile.ID))
}

func (m *Model) showProviderWizardPrompt() {
	if m.providerWizard == nil {
		return
	}
	wizard := m.providerWizard
	var prompt string
	switch wizard.step {
	case 0:
		prompt = "Step 1/6 · Provider ID (letters, digits, dot, underscore, hyphen)"
	case 1:
		prompt = fmt.Sprintf("Step 2/6 · Display name [%s]", firstNonEmpty(wizard.profile.Name, wizard.profile.ID))
	case 2:
		prompt = fmt.Sprintf("Step 3/6 · API format: openai | anthropic | gemini | codex [%s]", wizard.profile.APIFormat)
	case 3:
		prompt = fmt.Sprintf("Step 4/6 · Base URL; Enter keeps default, '-' resets [%s]", emptyDash(wizard.profile.BaseURL))
	case 4:
		prompt = fmt.Sprintf("Step 5/6 · Model name [%s]", wizard.profile.Model)
	case 5:
		prompt = fmt.Sprintf("Step 6/6 · API key environment variable; '-' means none [%s]", firstNonEmpty(wizard.profile.APIKeyEnv, defaultAPIKeyEnv(wizard.profile.APIFormat), "none"))
	}
	m.input.Placeholder = "Provider wizard input"
	m.appendNotice(prompt + "\nEnter a value, or type /cancel.")
}

func (m *Model) cancelProviderWizard() {
	if m.providerWizard == nil {
		m.appendNotice("no provider wizard is active")
		return
	}
	m.providerWizard = nil
	m.input.Reset()
	m.input.Placeholder = "Describe a task or type / for commands"
	m.appendNotice("provider wizard cancelled")
}

func parseProviderFormat(value string, current llmprofile.APIFormat) (llmprofile.APIFormat, bool) {
	if strings.TrimSpace(value) == "" {
		value = string(current)
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "openai", "openai-compatible":
		return llmprofile.FormatOpenAI, true
	case "2", "anthropic", "claude":
		return llmprofile.FormatAnthropic, true
	case "3", "gemini", "google":
		return llmprofile.FormatGemini, true
	case "4", "codex", "codex-oauth":
		return llmprofile.FormatCodex, true
	default:
		return "", false
	}
}

func defaultAPIKeyEnv(format llmprofile.APIFormat) string {
	switch format {
	case llmprofile.FormatOpenAI:
		return "OPENAI_API_KEY"
	case llmprofile.FormatAnthropic:
		return "ANTHROPIC_API_KEY"
	case llmprofile.FormatGemini:
		return "GEMINI_API_KEY"
	default:
		return ""
	}
}

func profileLabel(profile llmprofile.Profile) string {
	if profile.Name == "" || profile.Name == profile.ID {
		return profile.ID
	}
	return fmt.Sprintf("%s (%s)", profile.Name, profile.ID)
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
