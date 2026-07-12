package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"codeworld/internal/codexauth"
)

func TestCodexAuthStatusDoesNotExposeTokens(t *testing.T) {
	root := t.TempDir()
	store := codexauth.Store{Root: root}
	cred := codexauth.Credentials{
		Access:    "secret-access",
		Refresh:   "secret-refresh",
		Expires:   time.Now().Add(time.Hour).UnixMilli(),
		AccountID: "account-1",
	}
	if err := store.Save(cred); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runAuthCommand(strings.NewReader(""), &out, root, []string{"codex", "status", "--json"}); err != nil {
		t.Fatal(err)
	}
	var status codexAuthStatus
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Authenticated || status.AccountID != "account-1" || status.Expired {
		t.Fatalf("status = %#v", status)
	}
	if strings.Contains(out.String(), cred.Access) || strings.Contains(out.String(), cred.Refresh) {
		t.Fatalf("status exposed credentials: %s", out.String())
	}
}

func TestCodexAuthStatusReportsMissingCredentials(t *testing.T) {
	var out bytes.Buffer
	err := runCodexAuthStatus(&out, t.TempDir(), []string{"--json"})
	if err == nil || !strings.Contains(err.Error(), "auth codex login") {
		t.Fatalf("err = %v", err)
	}
	if strings.TrimSpace(out.String()) != `{"authenticated":false}` {
		t.Fatalf("output = %q", out.String())
	}
}

func TestInspectCodexAuthMarksExpiredCredentials(t *testing.T) {
	root := t.TempDir()
	store := codexauth.Store{Root: root}
	if err := store.Save(codexauth.Credentials{Access: "a", Refresh: "r", Expires: 1000, AccountID: "id"}); err != nil {
		t.Fatal(err)
	}
	status, err := inspectCodexAuth(root, time.UnixMilli(1001))
	if err != nil {
		t.Fatal(err)
	}
	if !status.Authenticated || !status.Expired || status.ExpiresAt != "1970-01-01T00:00:01Z" {
		t.Fatalf("status = %#v", status)
	}
}

func TestCodexAuthLogoutIsIdempotent(t *testing.T) {
	root := t.TempDir()
	store := codexauth.Store{Root: root}
	if err := store.Save(codexauth.Credentials{Access: "a", Refresh: "r", AccountID: "id"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runCodexAuthLogout(&out, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
		t.Fatalf("credential file still exists: %v", err)
	}
	if err := runCodexAuthLogout(&out, root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "removed") || !strings.Contains(out.String(), "already logged out") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestCodexAuthCommandValidatesUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"codex"}, {"codex", "status", "--bad"}, {"codex", "logout", "extra"}} {
		if err := runAuthCommand(strings.NewReader(""), &bytes.Buffer{}, t.TempDir(), args); err == nil {
			t.Fatalf("args %#v accepted", args)
		}
	}
}

func TestTopLevelLoginStatusAndLogoutAliases(t *testing.T) {
	root := t.TempDir()
	store := codexauth.Store{Root: root}
	if err := store.Save(codexauth.Credentials{Access: "a", Refresh: "r", Expires: time.Now().Add(time.Hour).UnixMilli(), AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runWithIO(strings.NewReader(""), &out, &bytes.Buffer{}, []string{"-C", root, "login", "status", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"authenticated":true`) || !strings.Contains(out.String(), `"account_id":"account-1"`) {
		t.Fatalf("status output = %q", out.String())
	}
	out.Reset()
	if err := runWithIO(strings.NewReader(""), &out, &bytes.Buffer{}, []string{"-C", root, "logout"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
		t.Fatalf("credential file still exists: %v", err)
	}
	if !strings.Contains(out.String(), "removed") {
		t.Fatalf("logout output = %q", out.String())
	}
}

func TestTopLevelLoginLogoutHelpAndUsage(t *testing.T) {
	for _, args := range [][]string{{"login", "--help"}, {"login", "status", "--help"}, {"logout", "--help"}} {
		var out bytes.Buffer
		if err := runWithIO(strings.NewReader(""), &out, &bytes.Buffer{}, args); err != nil {
			t.Fatalf("args %#v: %v", args, err)
		}
		if !strings.Contains(out.String(), "usage: codeworld") {
			t.Fatalf("args %#v output = %q", args, out.String())
		}
	}
	if err := runLogoutCommand(&bytes.Buffer{}, t.TempDir(), []string{"extra"}); err == nil {
		t.Fatal("logout argument accepted")
	}
}
