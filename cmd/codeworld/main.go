package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"codeworld/internal/app"
	"codeworld/internal/context/indexer"
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
	if len(args) > 0 {
		switch args[0] {
		case "repl":
			app, err := newAppWithIO(in, out, stderr, root)
			if err != nil {
				return err
			}
			return app.Run(context.Background())
		case "run":
			if len(args) < 2 {
				return fmt.Errorf("usage: codeworld run <task>")
			}
			rt, err := app.NewRuntime(context.Background(), app.Options{Root: root, In: in, Out: out, Err: stderr})
			if err != nil {
				return err
			}
			return runmode.Once(context.Background(), &rt, strings.Join(args[1:], " "))
		case "index":
			rt, err := app.NewRuntime(context.Background(), app.Options{Root: root, In: in, Out: out, Err: stderr})
			if err != nil {
				return err
			}
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
		case "resume":
			opts := app.Options{Root: root, In: in, Out: out, Err: stderr}
			if len(args) < 2 || args[1] == "--last" {
				opts.ResumeLast = true
			} else {
				opts.SessionID = args[1]
			}
			rt, err := app.NewRuntime(context.Background(), opts)
			if err != nil {
				return err
			}
			return tui.RunWithOptions(context.Background(), &rt, tui.Options{TestMode: !shouldShowTerminalTitle(out)})
		case "tui":
			rt, err := app.NewRuntime(context.Background(), app.Options{Root: root, In: in, Out: out, Err: stderr})
			if err != nil {
				return err
			}
			return tui.RunWithOptions(context.Background(), &rt, tui.Options{TestMode: !shouldShowTerminalTitle(out)})
		default:
			return fmt.Errorf("unknown command: %s", args[0])
		}
	}
	if shouldShowTerminalTitle(out) {
		rt, err := app.NewRuntime(context.Background(), app.Options{Root: root, In: in, Out: out, Err: stderr})
		if err != nil {
			return err
		}
		return tui.Run(context.Background(), &rt)
	}
	app, err := newAppWithIO(in, out, stderr, root)
	if err != nil {
		return err
	}
	return app.Run(context.Background())
}

func newAppWithIO(in io.Reader, out io.Writer, stderr io.Writer, root string) (repl.REPL, error) {
	rt, err := app.NewRuntime(context.Background(), app.Options{Root: root, In: in, Out: out, Err: stderr})
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
		SummaryMaxMessages: rt.Config.SummaryMaxMessages,
		ShowTerminalTitle:  shouldShowTerminalTitle(out),
		Diff:               rt.Diff,
	}, nil
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
