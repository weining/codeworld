package main

import (
	"fmt"
	"io"

	"codeworld/internal/config"
)

type featureInfo struct {
	Name         string `json:"name"`
	Stage        string `json:"stage"`
	Enabled      bool   `json:"enabled"`
	Configurable bool   `json:"configurable"`
}

func runFeaturesCommand(out io.Writer, root string, global globalOptions, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: codeworld features <list|enable|disable>")
	}
	switch args[0] {
	case "list":
		jsonOutput, err := parseJSONFlag(args[1:])
		if err != nil {
			return err
		}
		cfg, err := config.LoadWithOptions(root, config.LoadOptions{Profile: global.Profile, Overrides: global.Config})
		if err != nil {
			return err
		}
		features := currentFeatures(cfg)
		if jsonOutput {
			return writeJSON(out, features)
		}
		for _, feature := range features {
			if _, err := fmt.Fprintf(out, "%-24s %-18s %t\n", feature.Name, feature.Stage, feature.Enabled); err != nil {
				return err
			}
		}
		return nil
	case "enable", "disable":
		if len(args) != 2 {
			return fmt.Errorf("usage: codeworld features %s <feature>", args[0])
		}
		home, err := config.Home("")
		if err != nil {
			return err
		}
		enabled := args[0] == "enable"
		if err := config.SetUserFeature(home, args[1], enabled); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s feature %s\n", map[bool]string{true: "enabled", false: "disabled"}[enabled], args[1])
		return err
	default:
		return fmt.Errorf("usage: codeworld features <list|enable|disable>")
	}
}

func currentFeatures(cfg config.Config) []featureInfo {
	return []featureInfo{
		{Name: "hooks", Stage: "stable", Enabled: true},
		{Name: "mcp_oauth", Stage: "stable", Enabled: true},
		{Name: "model_call_logging", Stage: "experimental", Enabled: cfg.ModelCallLogging, Configurable: true},
		{Name: "multi_agent", Stage: "stable", Enabled: true},
		{Name: "plugins", Stage: "stable", Enabled: cfg.PluginsEnabled, Configurable: true},
		{Name: "session_names", Stage: "stable", Enabled: true},
		{Name: "unified_exec", Stage: "stable", Enabled: true},
		{Name: "web_search", Stage: "stable", Enabled: cfg.SandboxNetwork},
	}
}
