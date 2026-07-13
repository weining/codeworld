package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"codeworld/internal/config"
	"codeworld/internal/sandbox"
	"codeworld/internal/workspace"
)

const sandboxUsage = "usage: codeworld sandbox [--sandbox <mode>] [--network|--no-network] [-C <dir>] -- <command> [args...]"

type sandboxCommandOptions struct {
	mode       string
	network    *bool
	workingDir string
	command    []string
}

func runSandboxCommand(ctx context.Context, in io.Reader, out, stderr io.Writer, root string, global globalOptions, args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(out, sandboxUsage)
		return err
	}
	opts, err := parseSandboxCommandArgs(args)
	if err != nil {
		return err
	}
	cfg, err := config.LoadWithOptions(root, config.LoadOptions{Profile: global.Profile, Overrides: global.Config})
	if err != nil {
		return err
	}
	ws, err := workspace.New(root)
	if err != nil {
		return err
	}
	cwd := ws.Root
	if opts.workingDir != "" {
		cwd, err = ws.Resolve(opts.workingDir)
		if err != nil {
			return err
		}
		info, statErr := os.Stat(cwd)
		if statErr != nil {
			return statErr
		}
		if !info.IsDir() {
			return fmt.Errorf("sandbox working directory %q is not a directory", opts.workingDir)
		}
	}
	mode := cfg.SandboxMode
	if global.Sandbox != "" {
		mode = string(global.Sandbox)
	}
	if opts.mode != "" {
		mode = opts.mode
	}
	network := cfg.SandboxNetwork
	if global.Network != nil {
		network = *global.Network
	}
	if opts.network != nil {
		network = *opts.network
	}
	if mode == string(sandbox.ModeDangerFullAccess) {
		if opts.network != nil && !*opts.network {
			return fmt.Errorf("--no-network cannot restrict danger-full-access host execution")
		}
		network = true
	}
	policy := sandbox.Policy{Mode: sandbox.Mode(mode), Workspace: ws.Root, Network: network}
	cmd, err := policy.CommandContext(ctx, cwd, opts.command[0], opts.command[1:]...)
	if err != nil {
		return err
	}
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sandboxed command failed: %w", err)
	}
	return nil
}

func parseSandboxCommandArgs(args []string) (sandboxCommandOptions, error) {
	args = expandLongOptionValues(args)
	var opts sandboxCommandOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--":
			opts.command = append([]string(nil), args[i+1:]...)
			i = len(args)
		case "--sandbox", "-s":
			if i+1 >= len(args) {
				return sandboxCommandOptions{}, fmt.Errorf("%s", sandboxUsage)
			}
			opts.mode = args[i+1]
			i++
		case "--network":
			value := true
			opts.network = &value
		case "--no-network":
			value := false
			opts.network = &value
		case "--cd", "-C":
			if i+1 >= len(args) {
				return sandboxCommandOptions{}, fmt.Errorf("%s", sandboxUsage)
			}
			opts.workingDir = args[i+1]
			i++
		default:
			if arg != "" && arg[0] == '-' {
				return sandboxCommandOptions{}, fmt.Errorf("unknown sandbox option %q", arg)
			}
			opts.command = append([]string(nil), args[i:]...)
			i = len(args)
		}
	}
	if len(opts.command) == 0 {
		return sandboxCommandOptions{}, fmt.Errorf("%s", sandboxUsage)
	}
	switch opts.mode {
	case "", string(sandbox.ModeReadOnly), string(sandbox.ModeWorkspaceWrite), string(sandbox.ModeDangerFullAccess):
	default:
		return sandboxCommandOptions{}, fmt.Errorf("invalid sandbox mode %q", opts.mode)
	}
	return opts, nil
}
