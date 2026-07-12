package main

import (
	"runtime/debug"
	"strings"
)

// version 可在发布构建时通过 -ldflags "-X main.version=vX.Y.Z" 注入。
var version = "dev"

func buildVersionString() string {
	resolved := version
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "codeworld " + resolved
	}
	if resolved == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		resolved = info.Main.Version
	}
	revision := ""
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "codeworld " + resolved
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		revision += "-dirty"
	}
	return strings.TrimSpace("codeworld " + resolved + " (" + revision + ")")
}
