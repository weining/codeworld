package main

import (
	"fmt"
	"os"
	"path/filepath"

	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
)

type globalOptions struct {
	Profile    string
	WorkingDir string
	Model      string
	Approval   permissions.Mode
	Sandbox    sandbox.Mode
	Network    *bool
	Config     []string
	Strict     bool
}

func parseGlobalOptions(args []string) (globalOptions, []string, error) {
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
		case "--model", "-m":
			if len(args) < 2 || args[1] == "" {
				return globalOptions{}, nil, fmt.Errorf("--model requires a name")
			}
			if opts.Model != "" {
				return globalOptions{}, nil, fmt.Errorf("--model may be specified only once")
			}
			opts.Model = args[1]
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
		case "--network", "--no-network", "--search":
			if opts.Network != nil {
				return globalOptions{}, nil, fmt.Errorf("network option may be specified only once")
			}
			enabled := args[0] != "--no-network"
			opts.Network = &enabled
			if opts.Sandbox == sandbox.ModeDangerFullAccess {
				return globalOptions{}, nil, fmt.Errorf("network options cannot be combined with danger-full-access")
			}
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
			return opts, args[1:], nil
		default:
			return opts, args, nil
		}
	}
	if opts.Sandbox == sandbox.ModeDangerFullAccess && opts.Network != nil {
		return globalOptions{}, nil, fmt.Errorf("network options cannot be combined with danger-full-access")
	}
	return opts, args, nil
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
	case string(permissions.ModeAuto), "untrusted", "on-request":
		return permissions.ModeAuto, nil
	case string(permissions.ModeReadOnly):
		return permissions.ModeReadOnly, nil
	case string(permissions.ModeFullAccess), "never":
		return permissions.ModeFullAccess, nil
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
