package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"codeworld/internal/app"
	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
)

const mcpThreadRecordVersion = 1

type mcpThreadStore struct {
	home string
}

type mcpThreadRecord struct {
	Version               int              `json:"version"`
	ThreadID              string           `json:"thread_id"`
	Root                  string           `json:"root"`
	Profile               string           `json:"profile,omitempty"`
	Model                 string           `json:"model,omitempty"`
	CompactPrompt         string           `json:"compact_prompt,omitempty"`
	AdditionalDirs        []string         `json:"additional_dirs,omitempty"`
	ApprovalMode          permissions.Mode `json:"approval_mode,omitempty"`
	SandboxMode           sandbox.Mode     `json:"sandbox_mode,omitempty"`
	SandboxNetwork        *bool            `json:"sandbox_network,omitempty"`
	NativeSearch          bool             `json:"native_search,omitempty"`
	ConfigOverrides       []string         `json:"config_overrides,omitempty"`
	SkipUserConfig        bool             `json:"skip_user_config,omitempty"`
	IgnoreRules           bool             `json:"ignore_rules,omitempty"`
	BypassHookTrust       bool             `json:"bypass_hook_trust,omitempty"`
	BaseInstructions      string           `json:"base_instructions,omitempty"`
	DeveloperInstructions string           `json:"developer_instructions,omitempty"`
}

func newMCPThreadStore(home string) mcpThreadStore {
	return mcpThreadStore{home: home}
}

func (s mcpThreadStore) Save(threadID string, thread mcpThread) error {
	if s.home == "" {
		return nil
	}
	if threadID == "" {
		return fmt.Errorf("thread id is empty")
	}
	record := mcpThreadRecord{
		Version: mcpThreadRecordVersion, ThreadID: threadID,
		Root: thread.options.Root, Profile: thread.options.Profile, Model: thread.options.Model,
		CompactPrompt: thread.options.CompactPrompt, AdditionalDirs: append([]string(nil), thread.options.AdditionalDirs...),
		ApprovalMode: thread.options.ApprovalMode, SandboxMode: thread.options.SandboxMode,
		SandboxNetwork: cloneBool(thread.options.SandboxNetwork), NativeSearch: thread.options.NativeSearch,
		ConfigOverrides: append([]string(nil), thread.options.ConfigOverrides...),
		SkipUserConfig:  thread.options.SkipUserConfig, IgnoreRules: thread.options.IgnoreRules,
		BypassHookTrust:  thread.options.BypassHookTrust,
		BaseInstructions: thread.baseInstructions, DeveloperInstructions: thread.developerInstructions,
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeMCPThreadFile(s.path(threadID), data)
}

func (s mcpThreadStore) Load(threadID string) (mcpThread, error) {
	if s.home == "" {
		return mcpThread{}, os.ErrNotExist
	}
	data, err := os.ReadFile(s.path(threadID))
	if err != nil {
		return mcpThread{}, err
	}
	var record mcpThreadRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return mcpThread{}, err
	}
	if record.Version != mcpThreadRecordVersion {
		return mcpThread{}, fmt.Errorf("unsupported record version %d", record.Version)
	}
	if record.ThreadID != threadID {
		return mcpThread{}, fmt.Errorf("thread id mismatch")
	}
	if record.Root == "" {
		return mcpThread{}, fmt.Errorf("workspace root is empty")
	}
	return mcpThread{
		options: app.Options{
			Root: record.Root, Profile: record.Profile, Model: record.Model, SessionID: threadID,
			CompactPrompt: record.CompactPrompt, AdditionalDirs: append([]string(nil), record.AdditionalDirs...),
			ApprovalMode: record.ApprovalMode, SandboxMode: record.SandboxMode,
			SandboxNetwork: cloneBool(record.SandboxNetwork), NativeSearch: record.NativeSearch,
			ConfigOverrides: append([]string(nil), record.ConfigOverrides...),
			SkipUserConfig:  record.SkipUserConfig, IgnoreRules: record.IgnoreRules,
			BypassHookTrust: record.BypassHookTrust, Out: io.Discard, Err: io.Discard,
		},
		baseInstructions: record.BaseInstructions, developerInstructions: record.DeveloperInstructions,
	}, nil
}

func (s mcpThreadStore) path(threadID string) string {
	sum := sha256.Sum256([]byte(threadID))
	return filepath.Join(s.home, "mcp-threads", hex.EncodeToString(sum[:])+".json")
}

func writeMCPThreadFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".thread-*")
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
	return os.Rename(tmpPath, path)
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
