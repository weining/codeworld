package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
)

type globalOptions struct {
	Profile     string
	WorkingDir  string
	Model       string
	Approval    permissions.Mode
	Sandbox     sandbox.Mode
	Network     *bool
	Search      bool
	Config      []string
	Strict      bool
	OSS         bool
	Local       string
	Bypass      bool
	BypassHooks bool
	AddDirs     []string
	Images      []model.ContentPart
	NoAltScreen bool
}

func parseGlobalOptions(args []string) (globalOptions, []string, error) {
	args = expandLongOptionValues(args)
	var opts globalOptions
	for len(args) > 0 {
		switch args[0] {
		case "--profile", "-p":
			if len(args) < 2 || args[1] == "" {
				return globalOptions{}, nil, fmt.Errorf("--profile requires a name")
			}
			if opts.Profile != "" {
				return globalOptions{}, nil, fmt.Errorf("--profile may be specified only once")
			}
			opts.Profile = args[1]
			args = args[2:]
		case "--cd", "-C":
			if len(args) < 2 || args[1] == "" {
				return globalOptions{}, nil, fmt.Errorf("--cd requires a directory")
			}
			if opts.WorkingDir != "" {
				return globalOptions{}, nil, fmt.Errorf("--cd may be specified only once")
			}
			opts.WorkingDir = args[1]
			args = args[2:]
		case "--add-dir":
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				return globalOptions{}, nil, fmt.Errorf("--add-dir requires a directory")
			}
			opts.AddDirs = append(opts.AddDirs, args[1])
			args = args[2:]
		case "--image", "-i":
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				return globalOptions{}, nil, fmt.Errorf("--image requires a file")
			}
			part, err := model.ImagePartFromFile(args[1])
			if err != nil {
				return globalOptions{}, nil, err
			}
			opts.Images = append(opts.Images, part)
			args = args[2:]
		case "--no-alt-screen":
			if opts.NoAltScreen {
				return globalOptions{}, nil, fmt.Errorf("--no-alt-screen may be specified only once")
			}
			opts.NoAltScreen = true
			args = args[1:]
		case "--model", "-m":
			if len(args) < 2 || args[1] == "" {
				return globalOptions{}, nil, fmt.Errorf("--model requires a name")
			}
			if opts.Model != "" {
				return globalOptions{}, nil, fmt.Errorf("--model may be specified only once")
			}
			opts.Model = args[1]
			args = args[2:]
		case "--oss":
			if opts.OSS {
				return globalOptions{}, nil, fmt.Errorf("--oss may be specified only once")
			}
			opts.OSS = true
			args = args[1:]
		case "--local-provider":
			if len(args) < 2 || opts.Local != "" {
				return globalOptions{}, nil, fmt.Errorf("--local-provider requires one of ollama or lmstudio")
			}
			if _, err := localProviderURL(args[1]); err != nil {
				return globalOptions{}, nil, err
			}
			opts.Local, opts.OSS = args[1], true
			args = args[2:]
		case "--approval-mode", "--ask-for-approval", "-a":
			if len(args) < 2 || args[1] == "" {
				return globalOptions{}, nil, fmt.Errorf("--approval-mode requires a mode")
			}
			if opts.Approval != "" {
				return globalOptions{}, nil, fmt.Errorf("--approval-mode may be specified only once")
			}
			var err error
			opts.Approval, err = parseApprovalMode(args[1])
			if err != nil {
				return globalOptions{}, nil, err
			}
			args = args[2:]
		case "--sandbox", "-s":
			if len(args) < 2 || args[1] == "" {
				return globalOptions{}, nil, fmt.Errorf("--sandbox requires a mode")
			}
			if opts.Sandbox != "" {
				return globalOptions{}, nil, fmt.Errorf("--sandbox may be specified only once")
			}
			opts.Sandbox = sandbox.Mode(args[1])
			switch opts.Sandbox {
			case sandbox.ModeReadOnly, sandbox.ModeWorkspaceWrite, sandbox.ModeDangerFullAccess:
			default:
				return globalOptions{}, nil, fmt.Errorf("invalid sandbox mode %q", args[1])
			}
			if opts.Sandbox == sandbox.ModeDangerFullAccess && opts.Network != nil {
				return globalOptions{}, nil, fmt.Errorf("network options cannot be combined with danger-full-access")
			}
			args = args[2:]
		case "--network", "--no-network":
			if opts.Network != nil {
				return globalOptions{}, nil, fmt.Errorf("network option may be specified only once")
			}
			enabled := args[0] == "--network"
			opts.Network = &enabled
			if opts.Sandbox == sandbox.ModeDangerFullAccess {
				return globalOptions{}, nil, fmt.Errorf("network options cannot be combined with danger-full-access")
			}
			args = args[1:]
		case "--search":
			if opts.Network != nil {
				return globalOptions{}, nil, fmt.Errorf("network option may be specified only once")
			}
			enabled := true
			opts.Network = &enabled
			opts.Search = true
			if opts.Sandbox == sandbox.ModeDangerFullAccess {
				return globalOptions{}, nil, fmt.Errorf("network options cannot be combined with danger-full-access")
			}
			args = args[1:]
		case "--dangerously-bypass-approvals-and-sandbox":
			if opts.Bypass {
				return globalOptions{}, nil, fmt.Errorf("--dangerously-bypass-approvals-and-sandbox may be specified only once")
			}
			opts.Bypass = true
			args = args[1:]
		case "--dangerously-bypass-hook-trust":
			if opts.BypassHooks {
				return globalOptions{}, nil, fmt.Errorf("--dangerously-bypass-hook-trust may be specified only once")
			}
			opts.BypassHooks = true
			args = args[1:]
		case "--config", "-c":
			if len(args) < 2 || args[1] == "" {
				return globalOptions{}, nil, fmt.Errorf("--config requires key=value")
			}
			opts.Config = append(opts.Config, args[1])
			args = args[2:]
		case "--enable", "--disable":
			if len(args) < 2 || args[1] == "" {
				return globalOptions{}, nil, fmt.Errorf("%s requires a feature name", args[0])
			}
			key, err := configurableFeatureKey(args[1])
			if err != nil {
				return globalOptions{}, nil, err
			}
			enabled := args[0] == "--enable"
			opts.Config = append(opts.Config, fmt.Sprintf("%s=%t", key, enabled))
			args = args[2:]
		case "--strict-config":
			if opts.Strict {
				return globalOptions{}, nil, fmt.Errorf("--strict-config may be specified only once")
			}
			opts.Strict = true
			args = args[1:]
		case "--":
			return finalizeGlobalOptions(opts, args[1:])
		default:
			return finalizeGlobalOptions(opts, args)
		}
	}
	return finalizeGlobalOptions(opts, args)
}

func expandLongOptionValues(args []string) []string {
	var expanded []string
	for index, arg := range args {
		if arg == "--" {
			if expanded == nil {
				return args
			}
			return append(expanded, args[index:]...)
		}
		name, value, found := strings.Cut(arg, "=")
		if !found || !longOptionTakesValue(name) {
			if expanded != nil {
				expanded = append(expanded, arg)
			}
			continue
		}
		if expanded == nil {
			expanded = append([]string(nil), args[:index]...)
		}
		expanded = append(expanded, name, value)
	}
	if expanded == nil {
		return args
	}
	return expanded
}

func longOptionTakesValue(name string) bool {
	switch name {
	case "--profile", "--cd", "--add-dir", "--image", "--model", "--local-provider",
		"--approval-mode", "--ask-for-approval", "--sandbox", "--config", "--enable", "--disable",
		"--color", "--output-schema", "--output-last-message", "--base", "--commit", "--title",
		"--scopes", "--url", "--bearer-token-env-var", "--header", "--marketplace", "--ref", "--sparse", "--rules":
		return true
	default:
		return false
	}
}

func finalizeGlobalOptions(opts globalOptions, remaining []string) (globalOptions, []string, error) {
	if opts.Sandbox == sandbox.ModeDangerFullAccess && opts.Network != nil {
		return globalOptions{}, nil, fmt.Errorf("network options cannot be combined with danger-full-access")
	}
	if opts.Bypass {
		if opts.Approval != "" && opts.Approval != permissions.ModeFullAccess || opts.Sandbox != "" && opts.Sandbox != sandbox.ModeDangerFullAccess || opts.Network != nil {
			return globalOptions{}, nil, fmt.Errorf("dangerous bypass cannot be combined with approval, sandbox, or network options")
		}
		opts.Approval = permissions.ModeFullAccess
		opts.Sandbox = sandbox.ModeDangerFullAccess
	}
	if opts.OSS {
		opts.Config = append(opts.Config, "provider=local")
		if opts.Local != "" {
			baseURL, _ := localProviderURL(opts.Local)
			opts.Config = append(opts.Config, "local_base_url="+baseURL)
		}
	}
	return opts, remaining, nil
}

func localProviderURL(name string) (string, error) {
	switch name {
	case "ollama":
		return "http://127.0.0.1:11434/v1", nil
	case "lmstudio":
		return "http://127.0.0.1:1234/v1", nil
	default:
		return "", fmt.Errorf("invalid local provider %q; use ollama or lmstudio", name)
	}
}

func configurableFeatureKey(name string) (string, error) {
	switch name {
	case "plugins":
		return "features.plugins", nil
	case "model_call_logging":
		return "features.model_call_logging", nil
	default:
		return "", fmt.Errorf("feature %q is not configurable", name)
	}
}

func parseApprovalMode(value string) (permissions.Mode, error) {
	switch value {
	case string(permissions.ModeAuto):
		return permissions.ModeAuto, nil
	case string(permissions.ModeUntrusted):
		return permissions.ModeUntrusted, nil
	case string(permissions.ModeOnRequest):
		return permissions.ModeOnRequest, nil
	case string(permissions.ModeReadOnly):
		return permissions.ModeReadOnly, nil
	case string(permissions.ModeFullAccess):
		return permissions.ModeFullAccess, nil
	case string(permissions.ModeNever):
		return permissions.ModeNever, nil
	default:
		return "", fmt.Errorf("invalid approval mode %q", value)
	}
}

func resolveGlobalWorkingDir(current, requested string) (string, error) {
	if requested == "" {
		return current, nil
	}
	path := requested
	if !filepath.IsAbs(path) {
		path = filepath.Join(current, path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("resolve --cd %q: %w", requested, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--cd path %q is not a directory", requested)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(canonical), nil
}
