package llmprofile

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// APIFormat identifies the wire protocol used by an LLM platform.
type APIFormat string

const (
	FormatOpenAI    APIFormat = "openai"
	FormatAnthropic APIFormat = "anthropic"
	FormatGemini    APIFormat = "gemini"
	FormatCodex     APIFormat = "codex"
)

var profileIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Profile is one named, user-managed LLM platform configuration.
type Profile struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	APIFormat APIFormat `json:"api_format"`
	BaseURL   string    `json:"base_url,omitempty"`
	Model     string    `json:"model"`
	APIKeyEnv string    `json:"api_key_env,omitempty"`
}

type fileData struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}

// Store owns the user-level providers.json file.
type Store struct {
	home     string
	profiles []Profile
}

// Load reads the user-level LLM platform store. A missing file is an empty store.
func Load(home string) (*Store, error) {
	store := &Store{home: filepath.Clean(home)}
	data, err := os.ReadFile(store.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	var file fileData
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", store.Path(), err)
	}
	if file.Version != 1 {
		return nil, fmt.Errorf("unsupported providers file version %d", file.Version)
	}
	seen := map[string]bool{}
	for _, profile := range file.Profiles {
		profile = Normalize(profile)
		if err := Validate(profile); err != nil {
			return nil, fmt.Errorf("profile %q: %w", profile.ID, err)
		}
		if seen[profile.ID] {
			return nil, fmt.Errorf("duplicate provider profile %q", profile.ID)
		}
		seen[profile.ID] = true
		store.profiles = append(store.profiles, profile)
	}
	store.sort()
	return store, nil
}

// Path returns the private user-level provider configuration path.
func (s *Store) Path() string {
	return filepath.Join(s.home, "providers.json")
}

// List returns profiles sorted by ID.
func (s *Store) List() []Profile {
	if s == nil {
		return nil
	}
	return append([]Profile(nil), s.profiles...)
}

// Get resolves a profile by ID.
func (s *Store) Get(id string) (Profile, bool) {
	if s == nil {
		return Profile{}, false
	}
	id = strings.TrimSpace(id)
	for _, profile := range s.profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return Profile{}, false
}

// Upsert validates and persists one profile.
func (s *Store) Upsert(profile Profile) error {
	if s == nil {
		return fmt.Errorf("provider store is unavailable")
	}
	profile = Normalize(profile)
	if err := Validate(profile); err != nil {
		return err
	}
	previous := append([]Profile(nil), s.profiles...)
	replaced := false
	for index := range s.profiles {
		if s.profiles[index].ID == profile.ID {
			s.profiles[index] = profile
			replaced = true
			break
		}
	}
	if !replaced {
		s.profiles = append(s.profiles, profile)
	}
	s.sort()
	if err := s.save(); err != nil {
		s.profiles = previous
		return err
	}
	return nil
}

// Delete removes and persists one profile.
func (s *Store) Delete(id string) error {
	if s == nil {
		return fmt.Errorf("provider store is unavailable")
	}
	id = strings.TrimSpace(id)
	for index, profile := range s.profiles {
		if profile.ID != id {
			continue
		}
		previous := append([]Profile(nil), s.profiles...)
		s.profiles = append(s.profiles[:index], s.profiles[index+1:]...)
		if err := s.save(); err != nil {
			s.profiles = previous
			return err
		}
		return nil
	}
	return fmt.Errorf("provider profile %q not found", id)
}

func (s *Store) save() error {
	if strings.TrimSpace(s.home) == "" || s.home == "." {
		return fmt.Errorf("provider store home is empty")
	}
	if err := os.MkdirAll(s.home, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.home, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fileData{Version: 1, Profiles: s.profiles}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(s.home, ".providers-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.Path())
}

func (s *Store) sort() {
	sort.Slice(s.profiles, func(i, j int) bool { return s.profiles[i].ID < s.profiles[j].ID })
}

// Normalize trims fields and supplies the conventional endpoint for a format.
func Normalize(profile Profile) Profile {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.APIFormat = APIFormat(strings.ToLower(strings.TrimSpace(string(profile.APIFormat))))
	profile.BaseURL = strings.TrimRight(strings.TrimSpace(profile.BaseURL), "/")
	profile.Model = strings.TrimSpace(profile.Model)
	profile.APIKeyEnv = strings.TrimSpace(profile.APIKeyEnv)
	if profile.BaseURL == "" {
		profile.BaseURL = DefaultBaseURL(profile.APIFormat)
	}
	return profile
}

// Validate rejects unsafe or incomplete profile definitions.
func Validate(profile Profile) error {
	if err := ValidateID(profile.ID); err != nil {
		return err
	}
	switch profile.APIFormat {
	case FormatOpenAI, FormatAnthropic, FormatGemini, FormatCodex:
	default:
		return fmt.Errorf("unsupported API format %q", profile.APIFormat)
	}
	if profile.Model == "" {
		return fmt.Errorf("model is required")
	}
	if err := ValidateBaseURL(profile.APIFormat, profile.BaseURL); err != nil {
		return err
	}
	if (profile.APIFormat == FormatAnthropic || profile.APIFormat == FormatGemini) && profile.APIKeyEnv == "" {
		return fmt.Errorf("API key environment variable is required for %s", profile.APIFormat)
	}
	if profile.APIFormat == FormatCodex && profile.APIKeyEnv != "" {
		return fmt.Errorf("Codex profiles use OAuth and cannot set an API key environment variable")
	}
	return ValidateAPIKeyEnv(profile.APIKeyEnv)
}

// ValidateID checks a profile identifier before the rest of the wizard is complete.
func ValidateID(id string) error {
	if !profileIDPattern.MatchString(strings.TrimSpace(id)) {
		return fmt.Errorf("id must match %s", profileIDPattern.String())
	}
	return nil
}

// ValidateBaseURL checks the endpoint used by a wire format.
func ValidateBaseURL(format APIFormat, baseURL string) error {
	baseURL = strings.TrimSpace(baseURL)
	if format == FormatCodex && baseURL == "" {
		return nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("base URL must be an absolute HTTP(S) URL")
	}
	return nil
}

// ValidateAPIKeyEnv checks an environment variable name without reading its value.
func ValidateAPIKeyEnv(name string) error {
	name = strings.TrimSpace(name)
	for index, ch := range name {
		if !(ch == '_' || ch >= 'A' && ch <= 'Z' || index > 0 && ch >= '0' && ch <= '9') {
			return fmt.Errorf("API key environment variable must use uppercase letters, digits, and underscores")
		}
	}
	return nil
}

// DefaultBaseURL returns the conventional endpoint root for an API format.
func DefaultBaseURL(format APIFormat) string {
	switch format {
	case FormatOpenAI:
		return "https://api.openai.com/v1"
	case FormatAnthropic:
		return "https://api.anthropic.com"
	case FormatGemini:
		return "https://generativelanguage.googleapis.com/v1beta"
	default:
		return ""
	}
}
