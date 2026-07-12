package main

import (
	"bytes"
	"strings"
	"testing"

	"codeworld/internal/config"
)

func TestFeaturesCommandsListAndToggle(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("CODEWORLD_HOME", home)
	var out bytes.Buffer
	if err := runFeaturesCommand(&out, root, globalOptions{}, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "plugins") || !strings.Contains(out.String(), "mcp_oauth") {
		t.Fatalf("list = %s", out.String())
	}
	if err := runFeaturesCommand(&out, root, globalOptions{}, []string{"enable", "plugins"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadWithOptions(root, config.LoadOptions{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PluginsEnabled {
		t.Fatal("plugins feature was not enabled")
	}
	if err := runFeaturesCommand(&out, root, globalOptions{}, []string{"disable", "hooks"}); err == nil {
		t.Fatal("immutable feature was disabled")
	}
}

func TestGlobalFeatureOverride(t *testing.T) {
	opts, remaining, err := parseGlobalOptions([]string{"--enable", "plugins", "--disable", "model_call_logging", "features", "list"})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 || len(opts.Config) != 2 || opts.Config[0] != "features.plugins=true" || opts.Config[1] != "features.model_call_logging=false" {
		t.Fatalf("opts=%#v remaining=%#v", opts, remaining)
	}
}
