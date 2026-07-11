package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"codeworld/internal/app"
	"codeworld/internal/config"
	"codeworld/internal/session"
	"codeworld/internal/tui"
	"codeworld/internal/workspace"
)

func runSessionsCommand(out io.Writer, root, profile string, args []string) error {
	store, err := sessionStore(root, profile)
	if err != nil {
		return err
	}
	archived := false
	if len(args) > 0 {
		if len(args) != 1 || args[0] != "--archived" {
			return fmt.Errorf("usage: codeworld sessions [--archived]")
		}
		archived = true
	}
	var sessions []session.Session
	if archived {
		sessions, err = store.ListArchived()
	} else {
		sessions, err = store.List()
	}
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		_, err = fmt.Fprintln(out, "no sessions")
		return err
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tUPDATED\tMODEL\tMESSAGES")
	for _, sess := range sessions {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", sess.ID, sess.UpdatedAt.Local().Format("2006-01-02 15:04"), sess.Model, len(sess.Messages))
	}
	return w.Flush()
}

func runForkCommand(ctx context.Context, in io.Reader, out, stderr io.Writer, root, profile string, args []string) error {
	store, err := sessionStore(root, profile)
	if err != nil {
		return err
	}
	id, err := resolveSessionArgument(store, args, "usage: codeworld fork <session-id|--last>")
	if err != nil {
		return err
	}
	forked, err := store.Fork(id)
	if err != nil {
		return err
	}
	if stderr != nil {
		if _, err := fmt.Fprintf(stderr, "forked session %s from %s\n", forked.ID, id); err != nil {
			return err
		}
	}
	rt, err := app.NewRuntime(ctx, app.Options{Root: root, Profile: profile, SessionID: forked.ID, In: in, Out: out, Err: stderr})
	if err != nil {
		return err
	}
	return withRuntime(rt, func(rt *app.Runtime) error {
		return tui.RunWithOptions(ctx, rt, tui.Options{TestMode: !shouldShowTerminalTitle(out)})
	})
}

func runArchiveCommand(out io.Writer, root, profile string, args []string, unarchive bool) error {
	store, err := sessionStore(root, profile)
	if err != nil {
		return err
	}
	usage := "usage: codeworld archive <session-id|--last>"
	if unarchive {
		usage = "usage: codeworld unarchive <session-id|--last>"
	}
	var id string
	if unarchive {
		id, err = resolveArchivedSessionArgument(store, args, usage)
	} else {
		id, err = resolveSessionArgument(store, args, usage)
	}
	if err != nil {
		return err
	}
	if unarchive {
		err = store.Unarchive(id)
	} else {
		err = store.Archive(id)
	}
	if err != nil {
		return err
	}
	action := "archived"
	if unarchive {
		action = "unarchived"
	}
	_, err = fmt.Fprintf(out, "%s session %s\n", action, id)
	return err
}

func resolveArchivedSessionArgument(store session.Store, args []string, usage string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s", usage)
	}
	if args[0] != "--last" {
		return strings.TrimSpace(args[0]), nil
	}
	sessions, err := store.ListArchived()
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		return "", fmt.Errorf("no archived sessions")
	}
	return sessions[0].ID, nil
}

func runDeleteCommand(out io.Writer, root, profile string, args []string) error {
	store, err := sessionStore(root, profile)
	if err != nil {
		return err
	}
	id, err := resolveSessionArgument(store, args, "usage: codeworld delete <session-id|--last>")
	if err != nil {
		return err
	}
	if err := store.Delete(id); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "deleted session %s\n", id)
	return err
}

func resolveSessionArgument(store session.Store, args []string, usage string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s", usage)
	}
	if args[0] != "--last" {
		return strings.TrimSpace(args[0]), nil
	}
	sessions, err := store.List()
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		return "", fmt.Errorf("no sessions")
	}
	return sessions[0].ID, nil
}

func sessionStore(root, profile string) (session.Store, error) {
	cfg, err := config.LoadWithOptions(root, config.LoadOptions{Profile: profile})
	if err != nil {
		return session.Store{}, err
	}
	configRoot, err := workspace.New(root)
	if err != nil {
		return session.Store{}, err
	}
	workspaceRoot := configRoot.Root
	if cfg.Workspace != "" && cfg.Workspace != "." {
		workspaceRoot, err = configRoot.Resolve(cfg.Workspace)
		if err != nil {
			return session.Store{}, err
		}
	}
	ws, err := workspace.New(workspaceRoot)
	if err != nil {
		return session.Store{}, err
	}
	return session.NewStore(ws.Root), nil
}
