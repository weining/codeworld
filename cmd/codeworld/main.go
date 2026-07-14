package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"codeworld/internal/app"
	"codeworld/internal/codexauth"
	"codeworld/internal/config"
	"codeworld/internal/context/indexer"
	"codeworld/internal/model"
	"codeworld/internal/repl"
	runmode "codeworld/internal/run"
	"codeworld/internal/tui"
)

func main() {
	if err := runWithIO(os.Stdin, os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "codeworld:", err)
		os.Exit(1)
	}
}

func runWithIO(in io.Reader, out io.Writer, stderr io.Writer, args []string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	global, args, err := parseGlobalOptions(args)
	if err != nil {
		return err
	}
	root, err = resolveGlobalWorkingDir(root, global.WorkingDir)
	if err != nil {
		return err
	}
	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-h":
			return printCLIHelp(out)
		case "version", "--version", "-V":
			if len(args) != 1 {
				return fmt.Errorf("usage: codeworld version")
			}
			_, err := fmt.Fprintln(out, buildVersionString())
			return err
		case "login":
			return runLoginCommand(in, out, root, args[1:])
		case "logout":
			return runLogoutCommand(out, root, args[1:])
		case "auth":
			return runAuthCommand(in, out, root, args[1:])
		case "mcp":
			return runMCPCommand(out, root, global, args[1:])
		case "mcp-server":
			return runMCPServerCommand(context.Background(), in, out, root, global, args[1:])
		case "app-server":
			return runAppServerCommand(context.Background(), in, out, stderr, root, global, args[1:])
		case "plugin":
			return runPluginCommand(out, args[1:])
		case "features":
			return runFeaturesCommand(out, root, global, args[1:])
		case "debug":
			return runDebugCommand(context.Background(), out, root, global, args[1:])
		case "execpolicy":
			return runExecPolicyCommand(out, root, args[1:])
		case "doctor":
			return runDoctorCommand(out, root, global, args[1:])
		case "completion":
			return runCompletionCommand(out, args[1:])
		case "sandbox":
			return runSandboxCommand(context.Background(), in, out, stderr, root, global, args[1:])
		case "repl":
			app, err := newAppWithGlobal(in, out, stderr, root, global)
			if err != nil {
				return err
			}
			return app.Run(context.Background())
		case "run":
			if len(args) < 2 {
				return fmt.Errorf("usage: codeworld run <task>")
			}
			prompt, images, err := parseRunArgs(args[1:])
			if err != nil {
				return err
			}
			images = append(append([]model.ContentPart(nil), global.Images...), images...)
			rt, err := app.NewRuntime(context.Background(), runtimeOptionsFromGlobal(root, global, in, out, stderr))
			if err != nil {
				return err
			}
			return withRuntime(rt, func(rt *app.Runtime) error {
				if len(images) > 0 {
					return runmode.OnceWithImages(context.Background(), rt, prompt, images)
				}
				return runmode.Once(context.Background(), rt, prompt)
			})
		case "exec", "e":
			if len(args) > 1 && args[1] == "review" {
				return runReviewCommand(context.Background(), in, out, stderr, root, global, args[2:])
			}
			return runExecCommand(context.Background(), in, out, stderr, root, global, args[1:])
		case "review":
			return runReviewCommand(context.Background(), in, out, stderr, root, global, args[1:])
		case "sessions":
			return runSessionsCommand(out, root, global, args[1:])
		case "fork":
			return runForkCommand(context.Background(), in, out, stderr, root, global, args[1:])
		case "archive":
			return runArchiveCommand(out, root, global, args[1:], false)
		case "unarchive":
			return runArchiveCommand(out, root, global, args[1:], true)
		case "delete":
			return runDeleteCommand(out, root, global, args[1:])
		case "index":
			rt, err := app.NewRuntime(context.Background(), runtimeOptionsFromGlobal(root, global, in, out, stderr))
			if err != nil {
				return err
			}
			return withRuntime(rt, func(rt *app.Runtime) error {
				idx, err := indexer.Build(rt.Workspace.Root, int64(rt.Config.IndexMaxFileBytes))
				if err != nil {
					return err
				}
				if err := indexer.Save(indexer.DefaultPath(rt.Workspace.Root), idx); err != nil {
					return err
				}
				skipped := 0
				for _, entry := range idx.Entries {
					if entry.Skipped {
						skipped++
					}
				}
				_, err = fmt.Fprintf(out, "indexed files=%d skipped=%d\n", len(idx.Entries), skipped)
				return err
			})
		case "resume":
			return runResumeCommand(context.Background(), in, out, stderr, root, global, args[1:])
		case "tui":
			if len(args) != 1 {
				return fmt.Errorf("usage: codeworld tui")
			}
			rt, err := app.NewRuntime(context.Background(), runtimeOptionsFromGlobal(root, global, in, out, stderr))
			if err != nil {
				return err
			}
			return withRuntime(rt, func(rt *app.Runtime) error {
				return tui.RunWithOptions(context.Background(), rt, tui.Options{TestMode: !shouldShowTerminalTitle(out), InitialImages: global.Images, NoAltScreen: global.NoAltScreen})
			})
		default:
			prompt := strings.TrimSpace(strings.Join(args, " "))
			if prompt == "" {
				return fmt.Errorf("interactive prompt is empty")
			}
			rt, err := app.NewRuntime(context.Background(), runtimeOptionsFromGlobal(root, global, in, out, stderr))
			if err != nil {
				return err
			}
			return withRuntime(rt, func(rt *app.Runtime) error {
				return tui.RunWithOptions(context.Background(), rt, tui.Options{TestMode: !shouldShowTerminalTitle(out), InitialPrompt: prompt, InitialImages: global.Images, NoAltScreen: global.NoAltScreen})
			})
		}
	}
	if shouldShowTerminalTitle(out) {
		rt, err := app.NewRuntime(context.Background(), runtimeOptionsFromGlobal(root, global, in, out, stderr))
		if err != nil {
			return err
		}
		return withRuntime(rt, func(rt *app.Runtime) error {
			return tui.RunWithOptions(context.Background(), rt, tui.Options{InitialImages: global.Images, NoAltScreen: global.NoAltScreen})
		})
	}
	app, err := newAppWithGlobal(in, out, stderr, root, global)
	if err != nil {
		return err
	}
	return app.Run(context.Background())
}

func printCLIHelp(out io.Writer) error {
	_, err := fmt.Fprint(out, `Codeworld local coding agent

Usage:
  codeworld [options] [prompt]      Start the interactive TUI
  codeworld exec [options] <task|-> Run a script-friendly task
  codeworld e [options] <task|->    Alias for exec
  codeworld exec resume <id|--last> <task|->
                                    Continue a saved session non-interactively
  codeworld review [options]        Review a Git change set in read-only mode
  codeworld login [status]          Log in with Codex OAuth or inspect status
  codeworld logout                  Remove user-level Codex OAuth credentials
  codeworld mcp <command>           Manage user-level MCP servers
  codeworld mcp-server              Start Codeworld as a stdio MCP server
  codeworld app-server [options]    Start the Codex-compatible app server (stdio)
  codeworld plugin <command>        Manage Codex-compatible plugins and marketplaces
  codeworld features <command>      Inspect or update supported feature flags
  codeworld debug <command>         Inspect the effective model selection or prompt input
  codeworld execpolicy check ...    Check rule files against a command without executing it
  codeworld auth codex <command>    Log in, inspect status, or log out
  codeworld doctor [--json]         Check local configuration and dependencies
  codeworld completion [shell]      Generate bash, elvish, fish, PowerShell, or zsh completion
  codeworld sandbox [options] -- cmd
                                    Run a command in the configured process sandbox
  codeworld sessions [--archived]   List saved sessions
  codeworld sessions rename <id> <name>
                                    Assign a reusable session name
  codeworld fork <id|--last>        Fork a session into a new interactive task
  codeworld archive <id|--last>     Archive a saved session
  codeworld unarchive <id|--last>   Restore an archived session
  codeworld delete <id|--last>      Permanently delete a saved session
  codeworld run [--image path] task Run one compatible non-interactive turn
  codeworld resume [--last|id] [prompt]
                                    Pick or resume an active saved session
  codeworld index                   Refresh the workspace index
  codeworld repl                    Start the line-oriented REPL

Global options:
  --profile, -p <name>   Load $CODEWORLD_HOME/<name>.config.toml
  --cd, -C <dir>         Use a directory as the workspace root
  --add-dir <dir>        Add an extra readable and writable workspace root (repeatable)
  --image, -i <file>     Attach an image to the initial prompt (repeatable)
  --model, -m <name>     Override the model for this invocation
  --oss                  Use the local OpenAI-compatible provider
  --local-provider <p>   Use ollama or lmstudio and its default local endpoint
  --approval-mode, -a    Set untrusted, on-request, never, or a legacy mode
  --sandbox, -s <mode>   Set read-only, workspace-write, or danger-full-access
  --network              Enable network access in the process sandbox
  --no-network           Disable network access in the process sandbox
  --search               Enable network access and native web search
  --no-alt-screen        Preserve terminal scrollback in interactive mode
  --config, -c <key=val> Override a supported Codeworld config value
  --enable <feature>     Enable a configurable feature for this invocation
  --disable <feature>    Disable a configurable feature for this invocation
  --dangerously-bypass-approvals-and-sandbox
                         Disable approvals and process sandboxing
  --dangerously-bypass-hook-trust
                         Run configured hooks without trust prompts
  --strict-config        Require strict config parsing (the current default)
  --version, -V          Print version information

Exec options:
  --model, -m <name>     Override the model after the exec subcommand
  --profile, -p <name>   Load a profile after the exec subcommand
  --oss                  Use the local OpenAI-compatible provider
  --local-provider <p>   Use ollama or lmstudio
  --cd, -C <dir>         Select the workspace after the exec subcommand
  --add-dir <dir>        Add an extra workspace root (repeatable)
  --config, -c <key=val> Override a supported Codeworld config value
  --enable <feature>     Enable a supported feature for this exec
  --disable <feature>    Disable a supported feature for this exec
  --strict-config        Require strict config parsing (the current default)
  --json                 Emit JSONL events
  --ephemeral            Do not load or save the current session
  --approval-mode <mode> Set untrusted, on-request, never, or a legacy mode
  --sandbox <mode>      Set read-only, workspace-write, or danger-full-access
  --network             Allow network access inside the process sandbox
  --no-network          Disable network access inside the process sandbox
                         Full-access is unsandboxed host execution
  --image <path>         Attach an image
  --color <mode>         Color progress automatically, always, or never
  --ignore-user-config   Skip the user config while retaining project config
  --ignore-rules         Skip user and project exec policy rules
  --skip-git-repo-check  Allow exec outside Git (already the default)
  --dangerously-bypass-approvals-and-sandbox
                         Disable approvals and process sandboxing
  --dangerously-bypass-hook-trust
                         Run configured hooks without trust prompts
  --output-schema <path> Validate the final JSON response
  -o <path>              Also write the final message to a file

Review options:
  --uncommitted          Review staged, unstaged, and untracked changes
  --base <branch>        Review changes against a base branch
  --commit <sha>         Review one commit
  --title <title>        Add a title to the review context
  --enable <feature>     Enable a supported feature for this review
  --disable <feature>    Disable a supported feature for this review
  --json                 Emit JSONL events
  --output-schema <path> Validate the final JSON response
  -o <path>              Also write the final message to a file
`)
	return err
}

func runAuthCommand(in io.Reader, out io.Writer, root string, args []string) error {
	if len(args) < 2 || args[0] != "codex" {
		return fmt.Errorf("usage: codeworld auth codex <login|status|logout>")
	}
	switch args[1] {
	case "login":
		if len(args) != 2 {
			return fmt.Errorf("usage: codeworld auth codex login")
		}
		home, err := config.Home("")
		if err != nil {
			return err
		}
		_, err = codexauth.Login(context.Background(), codexauth.LoginOptions{In: in, Out: out, Store: codexauth.UserStore(home)})
		return err
	case "status":
		return runCodexAuthStatus(out, root, args[2:])
	case "logout":
		if len(args) != 2 {
			return fmt.Errorf("usage: codeworld auth codex logout")
		}
		return runCodexAuthLogout(out, root)
	default:
		return fmt.Errorf("usage: codeworld auth codex <login|status|logout>")
	}
}

func parseRunArgs(args []string) (string, []model.ContentPart, error) {
	var promptParts []string
	var images []model.ContentPart
	for i := 0; i < len(args); i++ {
		if args[i] != "--image" {
			promptParts = append(promptParts, args[i])
			continue
		}
		if i+1 >= len(args) {
			return "", nil, fmt.Errorf("usage: codeworld run [--image <path>] <task>")
		}
		part, err := model.ImagePartFromFile(args[i+1])
		if err != nil {
			return "", nil, err
		}
		images = append(images, part)
		i++
	}
	prompt := strings.TrimSpace(strings.Join(promptParts, " "))
	if prompt == "" {
		return "", nil, fmt.Errorf("run input is empty")
	}
	return prompt, images, nil
}

func newAppWithIO(in io.Reader, out io.Writer, stderr io.Writer, root string) (repl.REPL, error) {
	return newAppWithProfile(in, out, stderr, root, "", "")
}

func newAppWithProfile(in io.Reader, out io.Writer, stderr io.Writer, root, profile, modelName string) (repl.REPL, error) {
	return newAppWithGlobal(in, out, stderr, root, globalOptions{Profile: profile, Model: modelName})
}

func newAppWithGlobal(in io.Reader, out io.Writer, stderr io.Writer, root string, global globalOptions) (repl.REPL, error) {
	rt, err := app.NewRuntime(context.Background(), runtimeOptionsFromGlobal(root, global, in, out, stderr))
	if err != nil {
		return repl.REPL{}, err
	}
	return repl.REPL{
		In:                 in,
		Out:                out,
		Runner:             rt.Runner,
		Store:              rt.Store,
		Session:            rt.Session,
		Messages:           rt.Messages,
		Usage:              rt.Usage,
		Subagents:          rt.Subagents,
		SummaryMaxMessages: rt.Config.SummaryMaxMessages,
		SummaryMaxTokens:   rt.Config.SummaryMaxTokens,
		ShowTerminalTitle:  shouldShowTerminalTitle(out),
		SandboxMode:        rt.Config.SandboxMode,
		SandboxNetwork:     rt.Config.SandboxNetwork,
		Diff:               rt.Diff,
		Close:              rt.Close,
		BuildSystemPrompt:  rt.SystemPromptFor,
		SetChildModel: func(name string) {
			if rt.ChildRunner != nil {
				rt.ChildRunner.ModelName = name
			}
		},
	}, nil
}

func runtimeOptionsFromGlobal(root string, global globalOptions, in io.Reader, out, stderr io.Writer) app.Options {
	return app.Options{
		Root: root, Profile: global.Profile, Model: global.Model,
		ApprovalMode: global.Approval, SandboxMode: global.Sandbox, SandboxNetwork: global.Network,
		NativeSearch:    global.Search,
		ConfigOverrides: append([]string(nil), global.Config...),
		BypassHookTrust: global.BypassHooks,
		AdditionalDirs:  append([]string(nil), global.AddDirs...),
		In:              in, Out: out, Err: stderr,
	}
}

func withRuntime(rt app.Runtime, run func(*app.Runtime) error) (err error) {
	defer func() {
		err = errors.Join(err, rt.Close())
	}()
	return run(&rt)
}

func shouldShowTerminalTitle(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func modelCallLogPath(root string) string {
	return filepath.Join(root, ".codeworld", "logs", "model-calls.jsonl")
}
