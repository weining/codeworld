package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"codeworld/internal/app"
	"codeworld/internal/config"
	"codeworld/internal/session"
	"codeworld/internal/tui"
	"codeworld/internal/workspace"
)

func runResumeCommand(ctx context.Context, in io.Reader, out, stderr io.Writer, root string, global globalOptions, args []string) error {
	store, err := sessionStore(root, global)
	if err != nil {
		return err
	}
	id, prompt, err := selectSessionArgument(in, out, store, args, "resume")
	if err != nil {
		return err
	}
	runtimeOpts := runtimeOptionsFromGlobal(root, global, in, out, stderr)
	runtimeOpts.SessionID = id
	rt, err := app.NewRuntime(ctx, runtimeOpts)
	if err != nil {
		return err
	}
	return withRuntime(rt, func(rt *app.Runtime) error {
		return tui.RunWithOptions(ctx, rt, tui.Options{TestMode: !shouldShowTerminalTitle(out), InitialPrompt: prompt})
	})
}

func runSessionsCommand(out io.Writer, root string, global globalOptions, args []string) error {
	store, err := sessionStore(root, global)
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "rename" {
		if len(args) < 3 {
			return fmt.Errorf("usage: codeworld sessions rename <session-id|name|--last> <new-name>")
		}
		reference := args[1]
		if reference == "--last" {
			sessions, err := store.List()
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				return fmt.Errorf("no sessions")
			}
			reference = sessions[0].ID
		}
		renamed, err := store.Rename(reference, strings.Join(args[2:], " "))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "renamed session %s to %s\n", renamed.ID, renamed.Name)
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
	_, _ = fmt.Fprintln(w, "ID\tNAME\tUPDATED\tMODEL\tMESSAGES")
	for _, sess := range sessions {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", sess.ID, sess.Name, sess.UpdatedAt.Local().Format("2006-01-02 15:04"), sess.Model, len(sess.Messages))
	}
	return w.Flush()
}

func runForkCommand(ctx context.Context, in io.Reader, out, stderr io.Writer, root string, global globalOptions, args []string) error {
	store, err := sessionStore(root, global)
	if err != nil {
		return err
	}
	id, prompt, err := selectSessionArgument(in, out, store, args, "fork")
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
	runtimeOpts := runtimeOptionsFromGlobal(root, global, in, out, stderr)
	runtimeOpts.SessionID = forked.ID
	rt, err := app.NewRuntime(ctx, runtimeOpts)
	if err != nil {
		return err
	}
	return withRuntime(rt, func(rt *app.Runtime) error {
		return tui.RunWithOptions(ctx, rt, tui.Options{TestMode: !shouldShowTerminalTitle(out), InitialPrompt: prompt})
	})
}

func selectSessionArgument(in io.Reader, out io.Writer, store session.Store, args []string, action string) (string, string, error) {
	sessions, err := store.List()
	if err != nil {
		return "", "", err
	}
	if len(sessions) == 0 {
		return "", "", fmt.Errorf("no sessions")
	}
	if len(args) == 0 {
		id, err := promptForSession(in, out, sessions, action)
		return id, "", err
	}
	if args[0] == "--all" || args[0] == "--include-non-interactive" {
		return selectSessionArgument(in, out, store, args[1:], action)
	}
	if args[0] == "--last" {
		return sessions[0].ID, strings.TrimSpace(strings.Join(args[1:], " ")), nil
	}
	id := strings.TrimSpace(args[0])
	if id == "" {
		return "", "", fmt.Errorf("usage: codeworld %s [--last|session-id] [prompt]", action)
	}
	resolved, err := store.Resolve(id)
	if err != nil {
		return "", "", err
	}
	return resolved.ID, strings.TrimSpace(strings.Join(args[1:], " ")), nil
}

func promptForSession(in io.Reader, out io.Writer, sessions []session.Session, action string) (string, error) {
	if in == nil {
		return "", fmt.Errorf("%s requires a session selection", action)
	}
	if _, err := fmt.Fprintf(out, "Select a session to %s:\n", action); err != nil {
		return "", err
	}
	for index, sess := range sessions {
		label := sess.ID
		if sess.Name != "" {
			label = sess.Name + " (" + sess.ID + ")"
		}
		if _, err := fmt.Fprintf(out, "  %d) %s  %s  %s  %d messages\n", index+1, label, sess.UpdatedAt.Local().Format("2006-01-02 15:04"), sess.Model, len(sess.Messages)); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprint(out, "> "); err != nil {
		return "", err
	}
	line, err := readSelectionLine(in)
	if err != nil {
		return "", err
	}
	selection := strings.TrimSpace(line)
	if number, err := strconv.Atoi(selection); err == nil {
		if number < 1 || number > len(sessions) {
			return "", fmt.Errorf("session selection %d is out of range", number)
		}
		return sessions[number-1].ID, nil
	}
	for _, sess := range sessions {
		if sess.ID == selection || sess.Name == selection {
			return sess.ID, nil
		}
	}
	return "", fmt.Errorf("session %q not found", selection)
}

func readSelectionLine(in io.Reader) (string, error) {
	var builder strings.Builder
	buffer := []byte{0}
	for {
		n, err := in.Read(buffer)
		if n > 0 {
			if buffer[0] == '\n' {
				return builder.String(), nil
			}
			if buffer[0] != '\r' {
				builder.WriteByte(buffer[0])
			}
		}
		if err != nil {
			if err == io.EOF && builder.Len() > 0 {
				return builder.String(), nil
			}
			return "", err
		}
	}
}

func runArchiveCommand(out io.Writer, root string, global globalOptions, args []string, unarchive bool) error {
	store, err := sessionStore(root, global)
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
		sess, err := store.ResolveArchived(strings.TrimSpace(args[0]))
		if err != nil {
			return "", err
		}
		return sess.ID, nil
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

func runDeleteCommand(out io.Writer, root string, global globalOptions, args []string) error {
	store, err := sessionStore(root, global)
	if err != nil {
		return err
	}
	id, err := resolveAnySessionArgument(store, args, "usage: codeworld delete <session-id|name|--last>")
	if err != nil {
		return err
	}
	if err := store.Delete(id); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "deleted session %s\n", id)
	return err
}

func resolveAnySessionArgument(store session.Store, args []string, usage string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s", usage)
	}
	if args[0] == "--last" {
		return resolveSessionArgument(store, args, usage)
	}
	if sess, err := store.Resolve(args[0]); err == nil {
		return sess.ID, nil
	}
	sess, err := store.ResolveArchived(args[0])
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func resolveSessionArgument(store session.Store, args []string, usage string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s", usage)
	}
	if args[0] != "--last" {
		sess, err := store.Resolve(strings.TrimSpace(args[0]))
		if err != nil {
			return "", err
		}
		return sess.ID, nil
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

func sessionStore(root string, global globalOptions) (session.Store, error) {
	cfg, err := config.LoadWithOptions(root, config.LoadOptions{Profile: global.Profile, Overrides: global.Config})
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
