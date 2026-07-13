package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"codeworld/internal/codexplugin"
	"codeworld/internal/config"
)

const pluginUsage = "usage: codeworld plugin <add|list|remove|marketplace>"

func runPluginCommand(out io.Writer, args []string) error {
	args = expandLongOptionValues(args)
	if len(args) == 0 {
		return fmt.Errorf("%s", pluginUsage)
	}
	home, err := config.Home("")
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		return listPlugins(out, home, args[1:])
	case "add":
		return addPlugin(out, home, args[1:])
	case "remove":
		return removePlugin(out, home, args[1:])
	case "marketplace":
		return runMarketplaceCommand(out, home, args[1:])
	default:
		return fmt.Errorf("%s", pluginUsage)
	}
}

func listPlugins(out io.Writer, home string, args []string) error {
	marketplace, jsonOutput, availableOutput, err := parsePluginListOptions(args)
	if err != nil {
		return err
	}
	plugins, err := codexplugin.ListAvailable(home, marketplace)
	if err != nil {
		return err
	}
	installed := make([]codexplugin.AvailablePlugin, 0)
	available := make([]codexplugin.AvailablePlugin, 0)
	for _, plugin := range plugins {
		if plugin.Installed {
			installed = append(installed, plugin)
		} else if availableOutput {
			available = append(available, plugin)
		}
	}
	if jsonOutput {
		return writeJSON(out, map[string]any{"installed": installed, "available": available})
	}
	if len(installed) == 0 {
		_, err = fmt.Fprintln(out, "no plugins installed")
		return err
	}
	for _, plugin := range installed {
		status := "disabled"
		if plugin.Enabled {
			status = "enabled"
		}
		if _, err := fmt.Fprintf(out, "%s %s %s\n", plugin.PluginID, plugin.Version, status); err != nil {
			return err
		}
	}
	return nil
}

func addPlugin(out io.Writer, home string, args []string) error {
	selector, marketplace, jsonOutput, err := parsePluginMutationOptions("add", args)
	if err != nil {
		return err
	}
	installed, err := codexplugin.InstallPlugin(home, selector, marketplace)
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJSON(out, installed)
	}
	_, err = fmt.Fprintf(out, "installed plugin %s\n", installed.PluginID)
	return err
}

func removePlugin(out io.Writer, home string, args []string) error {
	selector, marketplace, jsonOutput, err := parsePluginMutationOptions("remove", args)
	if err != nil {
		return err
	}
	removed, err := codexplugin.RemovePlugin(home, selector, marketplace)
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJSON(out, removed)
	}
	_, err = fmt.Fprintf(out, "removed plugin %s\n", removed.PluginID)
	return err
}

func runMarketplaceCommand(out io.Writer, home string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: codeworld plugin marketplace <add|list|upgrade|remove>")
	}
	switch args[0] {
	case "list":
		jsonOutput, err := parseJSONFlag(args[1:])
		if err != nil {
			return err
		}
		state, err := codexplugin.LoadState(home)
		if err != nil {
			return err
		}
		sort.Slice(state.Marketplaces, func(i, j int) bool { return state.Marketplaces[i].Name < state.Marketplaces[j].Name })
		if jsonOutput {
			return writeJSON(out, map[string]any{"marketplaces": state.Marketplaces})
		}
		if len(state.Marketplaces) == 0 {
			_, err = fmt.Fprintln(out, "no plugin marketplaces configured")
			return err
		}
		for _, marketplace := range state.Marketplaces {
			if _, err := fmt.Fprintf(out, "%s %s %s\n", marketplace.Name, marketplace.SourceType, marketplace.Root); err != nil {
				return err
			}
		}
		return nil
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: codeworld plugin marketplace add <source> [--ref ref] [--sparse path] [--json]")
		}
		ref, sparse, jsonOutput, err := parseMarketplaceAddOptions(args[2:])
		if err != nil {
			return err
		}
		marketplace, err := codexplugin.AddMarketplace(home, args[1], ref, sparse)
		if err != nil {
			return err
		}
		if jsonOutput {
			return writeJSON(out, marketplace)
		}
		_, err = fmt.Fprintf(out, "added marketplace %s\n", marketplace.Name)
		return err
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: codeworld plugin marketplace remove <name> [--json]")
		}
		jsonOutput, err := parseJSONFlag(args[2:])
		if err != nil {
			return err
		}
		if err := codexplugin.RemoveMarketplace(home, args[1]); err != nil {
			return err
		}
		if jsonOutput {
			return writeJSON(out, map[string]any{"name": args[1], "removed": true})
		}
		_, err = fmt.Fprintf(out, "removed marketplace %s\n", args[1])
		return err
	case "upgrade":
		name, jsonOutput, err := parseMarketplaceUpgradeOptions(args[1:])
		if err != nil {
			return err
		}
		upgraded, err := codexplugin.UpgradeMarketplaces(home, name)
		if err != nil {
			return err
		}
		if jsonOutput {
			return writeJSON(out, map[string]any{"upgraded": upgraded})
		}
		if len(upgraded) == 0 {
			_, err = fmt.Fprintln(out, "no Git marketplaces configured")
			return err
		}
		for _, marketplace := range upgraded {
			if _, err := fmt.Fprintf(out, "upgraded marketplace %s\n", marketplace.Name); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("usage: codeworld plugin marketplace <add|list|upgrade|remove>")
	}
}

func parseMarketplaceAddOptions(args []string) (string, []string, bool, error) {
	var ref string
	var sparse []string
	var jsonOutput bool
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--ref":
			if index+1 >= len(args) {
				return "", nil, false, fmt.Errorf("--ref requires a value")
			}
			ref, index = args[index+1], index+1
		case "--sparse":
			if index+1 >= len(args) {
				return "", nil, false, fmt.Errorf("--sparse requires a value")
			}
			sparse, index = append(sparse, args[index+1]), index+1
		case "--json":
			jsonOutput = true
		default:
			return "", nil, false, fmt.Errorf("unknown marketplace add option %s", args[index])
		}
	}
	return ref, sparse, jsonOutput, nil
}

func parseMarketplaceUpgradeOptions(args []string) (string, bool, error) {
	var name string
	var jsonOutput bool
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
			continue
		}
		if strings.HasPrefix(arg, "-") || name != "" {
			return "", false, fmt.Errorf("unknown marketplace upgrade option %s", arg)
		}
		name = arg
	}
	return name, jsonOutput, nil
}

func parsePluginListOptions(args []string) (string, bool, bool, error) {
	var marketplace string
	var jsonOutput, available bool
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--marketplace", "-m":
			if index+1 >= len(args) {
				return "", false, false, fmt.Errorf("%s requires a value", args[index])
			}
			marketplace, index = args[index+1], index+1
		case "--json":
			jsonOutput = true
		case "--available":
			available = true
		default:
			return "", false, false, fmt.Errorf("unknown plugin list option %s", args[index])
		}
	}
	if available && !jsonOutput {
		return "", false, false, fmt.Errorf("--available requires --json")
	}
	return marketplace, jsonOutput, available, nil
}

func parsePluginMutationOptions(command string, args []string) (string, string, bool, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", "", false, fmt.Errorf("usage: codeworld plugin %s <plugin[@marketplace]>", command)
	}
	selector := args[0]
	var marketplace string
	var jsonOutput bool
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--marketplace", "-m":
			if index+1 >= len(args) {
				return "", "", false, fmt.Errorf("%s requires a value", args[index])
			}
			marketplace, index = args[index+1], index+1
		case "--json":
			jsonOutput = true
		default:
			return "", "", false, fmt.Errorf("unknown plugin %s option %s", command, args[index])
		}
	}
	return selector, marketplace, jsonOutput, nil
}
