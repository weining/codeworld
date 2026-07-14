package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"codeworld/internal/app"
	"codeworld/internal/llmprofile"
	"codeworld/internal/session"
	"codeworld/internal/workspace"
)

func newProviderTestModel(t *testing.T) (Model, *llmprofile.Store) {
	t.Helper()
	root := t.TempDir()
	profiles, err := llmprofile.Load(t.TempDir())
	if err != nil {
		t.Fatalf("load provider profiles: %v", err)
	}
	rt := app.Runtime{
		Workspace:        workspace.Workspace{Root: root},
		Store:            session.NewStore(root),
		Session:          session.New(root, "deepseek", "deepseek-v4-pro"),
		ProviderProfiles: profiles,
	}
	return NewModel(&rt), profiles
}

func TestProviderWizardAddsAndEditsProfile(t *testing.T) {
	m, profiles := newProviderTestModel(t)
	m.handleProvidersCommand("/providers add")
	for _, value := range []string{
		"moonshot",
		"Moonshot",
		"openai",
		"https://api.moonshot.cn/v1",
		"moonshot-v1-8k",
		"MOONSHOT_API_KEY",
	} {
		m.handleProviderWizardInput(value)
	}
	if m.providerWizard != nil {
		t.Fatalf("provider wizard remained active after save: %#v", m.providerWizard)
	}
	profile, ok := profiles.Get("moonshot")
	if !ok {
		t.Fatal("saved provider profile not found")
	}
	if profile.APIFormat != llmprofile.FormatOpenAI || profile.Model != "moonshot-v1-8k" || profile.APIKeyEnv != "MOONSHOT_API_KEY" {
		t.Fatalf("saved profile = %#v", profile)
	}

	m.handleProvidersCommand("/providers edit moonshot")
	for _, value := range []string{"", "gemini", "", "gemini-2.5-pro", ""} {
		m.handleProviderWizardInput(value)
	}
	profile, ok = profiles.Get("moonshot")
	if !ok {
		t.Fatal("edited provider profile not found")
	}
	if profile.APIFormat != llmprofile.FormatGemini {
		t.Fatalf("format = %q, want gemini", profile.APIFormat)
	}
	if profile.BaseURL != llmprofile.DefaultBaseURL(llmprofile.FormatGemini) {
		t.Fatalf("base URL = %q, want Gemini default", profile.BaseURL)
	}
	if profile.APIKeyEnv != "GEMINI_API_KEY" || profile.Model != "gemini-2.5-pro" {
		t.Fatalf("edited profile = %#v", profile)
	}
}

func TestProviderWizardValidatesEachStepAndEscCancels(t *testing.T) {
	m, profiles := newProviderTestModel(t)
	m.handleProvidersCommand("/providers add")
	m.input.SetValue("bad id")

	nextModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("provider wizard Enter returned an async command")
	}
	m = nextModel.(Model)
	if m.providerWizard == nil || m.providerWizard.step != 0 {
		t.Fatalf("wizard advanced after invalid ID: %#v", m.providerWizard)
	}
	if !strings.Contains(transcriptText(m.items), "invalid provider ID") {
		t.Fatalf("transcript does not show validation error: %s", transcriptText(m.items))
	}

	nextModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nextModel.(Model)
	if m.providerWizard != nil {
		t.Fatal("Esc did not cancel provider wizard")
	}
	if len(profiles.List()) != 0 {
		t.Fatalf("cancelled wizard persisted profiles: %#v", profiles.List())
	}
}

func TestProviderWizardAddDoesNotOverwriteExistingProfile(t *testing.T) {
	m, profiles := newProviderTestModel(t)
	existing := llmprofile.Profile{
		ID: "existing", APIFormat: llmprofile.FormatOpenAI,
		BaseURL: "http://127.0.0.1:11434/v1", Model: "original",
	}
	if err := profiles.Upsert(existing); err != nil {
		t.Fatal(err)
	}
	m.handleProvidersCommand("/providers add")
	m.handleProviderWizardInput("existing")
	if m.providerWizard == nil || m.providerWizard.step != 0 {
		t.Fatalf("wizard advanced for duplicate ID: %#v", m.providerWizard)
	}
	profile, _ := profiles.Get("existing")
	if profile.Model != "original" {
		t.Fatalf("existing profile changed: %#v", profile)
	}
}

func TestProvidersUseAndDeleteFlow(t *testing.T) {
	m, profiles := newProviderTestModel(t)
	profile := llmprofile.Profile{
		ID:        "local",
		Name:      "Local Ollama",
		APIFormat: llmprofile.FormatOpenAI,
		BaseURL:   "http://127.0.0.1:11434/v1",
		Model:     "qwen3-coder",
	}
	if err := profiles.Upsert(profile); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	m.handleProvidersCommand("/providers use local")
	if m.rt.ActiveProfile != "local" || m.rt.Session.Provider != "profile:local" || m.rt.Session.Model != "qwen3-coder" {
		t.Fatalf("runtime was not switched: active=%q session=%#v", m.rt.ActiveProfile, m.rt.Session)
	}
	if m.rt.Runner.Model == nil {
		t.Fatal("provider switch did not install a model client")
	}

	m.handleProvidersCommand("/providers delete local")
	if m.providerWizard != nil {
		t.Fatal("active provider should not enter delete confirmation")
	}
	if !strings.Contains(transcriptText(m.items), "cannot delete the active provider") {
		t.Fatalf("missing active-delete error: %s", transcriptText(m.items))
	}

	m.rt.ActiveProfile = ""
	m.handleProvidersCommand("/providers delete local")
	m.handleProviderWizardInput("not yet")
	if _, ok := profiles.Get("local"); !ok {
		t.Fatal("profile deleted without DELETE confirmation")
	}
	m.handleProviderWizardInput("DELETE")
	if _, ok := profiles.Get("local"); ok {
		t.Fatal("profile still exists after DELETE confirmation")
	}
}
