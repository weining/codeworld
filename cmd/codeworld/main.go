package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"codeworld/internal/app"
	"codeworld/internal/repl"
)

func main() {
	if err := runWithIO(os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "codeworld:", err)
		os.Exit(1)
	}
}

func runWithIO(in io.Reader, out io.Writer, stderr io.Writer) error {
	root, err := os.Getwd()
	if err != nil {
		return err
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
		In:                in,
		Out:               out,
		Runner:            rt.Runner,
		Store:             rt.Store,
		Session:           rt.Session,
		Messages:          rt.Messages,
		Usage:             rt.Usage,
		ShowTerminalTitle: shouldShowTerminalTitle(out),
		Diff:              rt.Diff,
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
