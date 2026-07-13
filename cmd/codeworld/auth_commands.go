package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"codeworld/internal/codexauth"
	"codeworld/internal/config"
)

type codexAuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	AccountID     string `json:"account_id,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	Expired       bool   `json:"expired,omitempty"`
}

func runLoginCommand(in io.Reader, out io.Writer, root string, args []string) error {
	if len(args) == 0 {
		return runAuthCommand(in, out, root, []string{"codex", "login"})
	}
	if args[0] == "status" {
		if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
			_, err := fmt.Fprintln(out, "usage: codeworld login status [--json]")
			return err
		}
		return runCodexAuthStatus(out, root, args[1:])
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(out, "usage: codeworld login [status [--json]]")
		return err
	}
	return fmt.Errorf("usage: codeworld login [status [--json]]")
}

func runLogoutCommand(out io.Writer, root string, args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(out, "usage: codeworld logout")
		return err
	}
	if len(args) != 0 {
		return fmt.Errorf("usage: codeworld logout")
	}
	return runCodexAuthLogout(out, root)
}

func runCodexAuthStatus(out io.Writer, root string, args []string) error {
	jsonOutput := false
	if len(args) == 1 && args[0] == "--json" {
		jsonOutput = true
	} else if len(args) != 0 {
		return fmt.Errorf("usage: codeworld auth codex status [--json]")
	}

	status, err := inspectCodexAuth(root, time.Now())
	if err != nil {
		if writeErr := writeCodexAuthStatus(out, status, jsonOutput); writeErr != nil {
			return writeErr
		}
		return fmt.Errorf("Codex OAuth credentials not found; run codeworld auth codex login")
	}
	return writeCodexAuthStatus(out, status, jsonOutput)
}

func inspectCodexAuth(root string, now time.Time) (codexAuthStatus, error) {
	home, err := config.Home("")
	if err != nil {
		return codexAuthStatus{}, err
	}
	cred, err := codexauth.UserStore(home).Load()
	if err != nil {
		cred, err = (codexauth.Store{Root: root}).Load()
	}
	if err != nil {
		return codexAuthStatus{}, err
	}
	expiresAt := time.UnixMilli(cred.Expires)
	return codexAuthStatus{
		Authenticated: true,
		AccountID:     cred.AccountID,
		ExpiresAt:     expiresAt.UTC().Format(time.RFC3339),
		Expired:       !expiresAt.After(now),
	}, nil
}

func writeCodexAuthStatus(out io.Writer, status codexAuthStatus, jsonOutput bool) error {
	if jsonOutput {
		return json.NewEncoder(out).Encode(status)
	}
	if !status.Authenticated {
		_, err := fmt.Fprintln(out, "Codex OAuth: not logged in")
		return err
	}
	state := "valid"
	if status.Expired {
		state = "expired; refresh will be attempted on next use"
	}
	_, err := fmt.Fprintf(out, "Codex OAuth: logged in\nAccount: %s\nExpires: %s (%s)\n", status.AccountID, status.ExpiresAt, state)
	return err
}

func runCodexAuthLogout(out io.Writer, root string) error {
	home, err := config.Home("")
	if err != nil {
		return err
	}
	removedUser, userErr := codexauth.UserStore(home).Delete()
	removedWorkspace, workspaceErr := (codexauth.Store{Root: root}).Delete()
	if err := errors.Join(userErr, workspaceErr); err != nil {
		return err
	}
	removed := removedUser || removedWorkspace
	if !removed {
		_, err = fmt.Fprintln(out, "Codex OAuth: already logged out")
		return err
	}
	_, err = fmt.Fprintln(out, "Codex OAuth credentials removed")
	return err
}
