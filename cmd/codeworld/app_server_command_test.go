package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestAppServerHelpAndArgumentParsing(t *testing.T) {
	listen, help, err := parseAppServerArgs([]string{"--listen", "stdio://"})
	if err != nil {
		t.Fatal(err)
	}
	if listen != "stdio://" || help {
		t.Fatalf("listen=%q help=%v", listen, help)
	}
	if _, _, err := parseAppServerArgs([]string{"--listen"}); err == nil {
		t.Fatal("missing listen URL was accepted")
	}
	if _, _, err := parseAppServerArgs([]string{"--unknown"}); err == nil {
		t.Fatal("unknown option was accepted")
	}

	var out bytes.Buffer
	if err := runWithIO(strings.NewReader(""), &out, &bytes.Buffer{}, []string{"app-server", "--help"}); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"codeworld app-server", "--listen", "--stdio"} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("help is missing %q:\n%s", fragment, out.String())
		}
	}
}

func TestAppServerRejectsUnsupportedTransport(t *testing.T) {
	err := runWithIO(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, []string{"app-server", "--listen", "ws://127.0.0.1:4500"})
	if err == nil || !strings.Contains(err.Error(), "currently supported: stdio://") {
		t.Fatalf("err = %v", err)
	}
}
