package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommands(t *testing.T) {
	original := version
	version = "v1.2.3-test"
	t.Cleanup(func() { version = original })
	for _, args := range [][]string{{"--version"}, {"-V"}, {"version"}} {
		var out bytes.Buffer
		if err := runWithIO(strings.NewReader(""), &out, &bytes.Buffer{}, args); err != nil {
			t.Fatalf("args %#v: %v", args, err)
		}
		if !strings.HasPrefix(out.String(), "codeworld v1.2.3-test") {
			t.Fatalf("args %#v output = %q", args, out.String())
		}
	}
	if err := runWithIO(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, []string{"version", "extra"}); err == nil {
		t.Fatal("version argument accepted")
	}
}
