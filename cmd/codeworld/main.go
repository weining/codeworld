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
	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-h":
			return printCLIHelp(out)
		case "auth":
			return runAuthCommand(in, out, root, args[1:])
		case "repl":
			app, err := newAppWithProfile(in, out, stderr, root, global.Profile)
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
			rt, err := app.NewRuntime(context.Background(), app.Options{Root: root, Profile: global.Profile, In: in, Out: out, Err: stderr})
			if err != nil {
				return err
			}
			return withRuntime(rt, func(rt *app.Runtime) error {
				if len(images) > 0 {
					return runmode.OnceWithImages(context.Background(), rt, prompt, images)
				}
				return runmode.Once(context.Background(), rt, prompt)
			})
		case "exec":
			return runExecCommand(context.Background(), in, out, stderr, root, global.Profile, args[1:])
		case "review":
			return runReviewCommand(context.Background(), in, out, stderr, root, global.Profile, args[1:])
		case "sessions":
			return runSessionsCommand(out, root, global.Profile, args[1:])
		case "fork":
			return runForkCommand(context.Background(), in, out, stderr, root, global.Profile, args[1:])
		case "archive":
			return runArchiveCommand(out, root, global.Profile, args[1:], false)
		case "unarchive":
			return runArchiveCommand(out, root, global.Profile, args[1:], true)
		case "delete":
			return runDeleteCommand(out, root, global.Profile, args[1:])
		case "index":
			rt, err := app.NewRuntime(context.Background(), app.Options{Root: root, Profile: global.Profile, In: in, Out: out, Err: stderr})
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
			opts := app.Options{Root: root, Profile: global.Profile, In: in, Out: out, Err: stderr}
			if len(args) < 2 || args[1] == "--last" {
				opts.ResumeLast = true
			} else {
				opts.SessionID = args[1]
			}
			rt, err := app.NewRuntime(context.Background(), opts)
			if err != nil {
				return err
			}
			return withRuntime(rt, func(rt *app.Runtime) error {
				return tui.RunWithOptions(context.Background(), rt, tui.Options{TestMode: !shouldShowTerminalTitle(out)})
			})
		case "tui":
			rt, err := app.NewRuntime(context.Background(), app.Options{Root: root, Profile: global.Profile, In: in, Out: out, Err: stderr})
			if err != nil {
				return err
			}
			return withRuntime(rt, func(rt *app.Runtime) error {
				return tui.RunWithOptions(context.Background(), rt, tui.Options{TestMode: !shouldShowTerminalTitle(out)})
			})
		default:
			return fmt.Errorf("unknown command: %s", args[0])
		}
	}
	if shouldShowTerminalTitle(out) {
		rt, err := app.NewRuntime(context.Background(), app.Options{Root: root, Profile: global.Profile, In: in, Out: out, Err: stderr})
		if err != nil {
			return err
		}
		return withRuntime(rt, func(rt *app.Runtime) error { return tui.Run(context.Background(), rt) })
	}
	app, err := newAppWithProfile(in, out, stderr, root, global.Profile)
	if err != nil {
		return err
	}
	return app.Run(context.Background())
}

func printCLIHelp(out io.Writer) error {
	_, err := fmt.Fprint(out, `Codeworld local coding agent

Usage:
  codeworld                         Start the interactive TUI
  codeworld exec [options] <task|-> Run a script-friendly task
  codeworld review [options]        Review a Git change set in read-only mode
  codeworld sessions [--archived]   List saved sessions
  codeworld fork <id|--last>        Fork a session into a new interactive task
  codeworld archive <id|--last>     Archive a saved session
  codeworld unarchive <id|--last>   Restore an archived session
  codeworld delete <id|--last>      Permanently delete a saved session
  codeworld run [--image path] task Run one compatible non-interactive turn
  codeworld resume [--last|id]      Resume an active saved session
  codeworld index                   Refresh the workspace index
  codeworld repl                    Start the line-oriented REPL

Global options:
  --profile, -p <name>   Load $CODEWORLD_HOME/profiles/<name>.toml

Exec options:
  --json                 Emit JSONL events
  --ephemeral            Do not load or save the current session
  --approval-mode <mode> Set auto, read-only, or full-access permissions
  --sandbox <mode>      Set read-only, workspace-write, or danger-full-access
  --network             Allow network access inside the process sandbox
  --no-network          Disable network access inside the process sandbox
                         Full-access is unsandboxed host execution
  --image <path>         Attach an image
  --output-schema <path> Validate the final JSON response
  -o <path>              Also write the final message to a file

Review options:
  --base <branch>        Review changes against a base branch
  --commit <sha>         Review one commit
  --json                 Emit JSONL events
  --output-schema <path> Validate the final JSON response
  -o <path>              Also write the final message to a file
`)
	return err
}

func runAuthCommand(in io.Reader, out io.Writer, root string, args []string) error {
	if len(args) == 2 && args[0] == "codex" && args[1] == "login" {
		_, err := codexauth.Login(context.Background(), codexauth.LoginOptions{
			Root: root,
			In:   in,
			Out:  out,
		})
		return err
	}
	return fmt.Errorf("usage: codeworld auth codex login")
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
	return newAppWithProfile(in, out, stderr, root, "")
}

func newAppWithProfile(in io.Reader, out io.Writer, stderr io.Writer, root, profile string) (repl.REPL, error) {
	rt, err := app.NewRuntime(context.Background(), app.Options{Root: root, Profile: profile, In: in, Out: out, Err: stderr})
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
