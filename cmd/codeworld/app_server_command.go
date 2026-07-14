package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"codeworld/internal/app"
	"codeworld/internal/appserver"
	"codeworld/internal/config"
	"codeworld/internal/sandbox"
)

func runAppServerCommand(ctx context.Context, in io.Reader, out, stderr io.Writer, root string, global globalOptions, args []string) error {
	listen, help, err := parseAppServerArgs(args)
	if err != nil {
		return err
	}
	if help {
		_, err := fmt.Fprint(out, appServerHelp())
		return err
	}
	if listen != "stdio://" && listen != "stdio" {
		return fmt.Errorf("unsupported app-server listen URL %q; currently supported: stdio://", listen)
	}
	home, err := config.Home("")
	if err != nil {
		return err
	}
	server := appserver.Server{
		Home: home, Version: version,
		Factory: func(factoryCtx context.Context, req appserver.ThreadRequest) (appserver.Runtime, error) {
			return newAppServerRuntime(factoryCtx, root, global, stderr, req)
		},
	}
	return server.Serve(ctx, in, out)
}

func parseAppServerArgs(args []string) (listen string, help bool, err error) {
	listen = "stdio://"
	for index := 0; index < len(args); index++ {
		switch arg := args[index]; {
		case arg == "--help" || arg == "-h":
			help = true
		case arg == "--stdio":
			listen = "stdio://"
		case arg == "--listen":
			if index+1 >= len(args) {
				return "", false, fmt.Errorf("--listen requires a URL")
			}
			index++
			listen = args[index]
		case strings.HasPrefix(arg, "--listen="):
			listen = strings.TrimPrefix(arg, "--listen=")
		default:
			return "", false, fmt.Errorf("unknown app-server option %s", arg)
		}
	}
	return listen, help, nil
}

func newAppServerRuntime(ctx context.Context, defaultRoot string, global globalOptions, stderr io.Writer, req appserver.ThreadRequest) (appserver.Runtime, error) {
	root := defaultRoot
	if req.CWD != "" {
		var err error
		root, err = resolveGlobalWorkingDir(defaultRoot, req.CWD)
		if err != nil {
			return nil, err
		}
	}
	opts := runtimeOptionsFromGlobal(root, global, nil, io.Discard, stderr)
	opts.NewSession = req.ThreadID == ""
	opts.SessionID = req.ThreadID
	opts.Ephemeral = req.Ephemeral
	if req.Model != "" {
		opts.Model = req.Model
	}
	if req.ApprovalPolicy != "" {
		mode, err := parseApprovalMode(req.ApprovalPolicy)
		if err != nil {
			return nil, err
		}
		opts.ApprovalMode = mode
	}
	if req.Sandbox != "" {
		opts.SandboxMode = sandbox.Mode(req.Sandbox)
		switch opts.SandboxMode {
		case sandbox.ModeReadOnly, sandbox.ModeWorkspaceWrite, sandbox.ModeDangerFullAccess:
		default:
			return nil, fmt.Errorf("invalid sandbox mode %q", req.Sandbox)
		}
		if opts.SandboxMode == sandbox.ModeDangerFullAccess {
			opts.SandboxNetwork = nil
		}
	}
	overrides, err := mcpConfigOverrides(req.Config)
	if err != nil {
		return nil, err
	}
	opts.ConfigOverrides = append(opts.ConfigOverrides, overrides...)
	if len(req.RuntimeWorkspaceRoots) > 0 {
		opts.AdditionalDirs = opts.AdditionalDirs[:0]
		cleanRoot := filepath.Clean(root)
		for _, workspaceRoot := range req.RuntimeWorkspaceRoots {
			if filepath.Clean(workspaceRoot) != cleanRoot {
				opts.AdditionalDirs = append(opts.AdditionalDirs, workspaceRoot)
			}
		}
	}
	rt, err := app.NewRuntime(ctx, opts)
	if err != nil {
		return nil, err
	}
	applyMCPInstructions(&rt, req.BaseInstructions, req.DeveloperInstructions)
	return appserver.NewAppRuntime(&rt, req.Ephemeral), nil
}

func appServerHelp() string {
	return `Run the Codeworld app server

Usage: codeworld app-server [OPTIONS]

Options:
  --listen <URL>  Transport endpoint; currently supported: stdio://
  --stdio         Use JSONL stdio transport (equivalent to --listen stdio://)
  -h, --help      Print help
`
}
