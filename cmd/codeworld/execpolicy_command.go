package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"codeworld/internal/execpolicy"
)

const execPolicyUsage = "usage: codeworld execpolicy check [--pretty] [--resolve-host-executables] --rules <path> [--rules <path>...] [--] <command>..."

func runExecPolicyCommand(out io.Writer, root string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", execPolicyUsage)
	}
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(out, execPolicyUsage)
		return err
	}
	if args[0] != "check" {
		return fmt.Errorf("%s", execPolicyUsage)
	}
	if len(args) == 2 && (args[1] == "help" || args[1] == "--help" || args[1] == "-h") {
		_, err := fmt.Fprintln(out, execPolicyUsage)
		return err
	}
	rules, command, pretty, resolveHostExecutables, err := parseExecPolicyCheckArgs(root, args[1:])
	if err != nil {
		return err
	}
	set, err := execpolicy.LoadFiles(rules)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(set.EvaluateWithOptions(command, execpolicy.MatchOptions{ResolveHostExecutables: resolveHostExecutables}))
}

func parseExecPolicyCheckArgs(root string, args []string) ([]string, []string, bool, bool, error) {
	args = expandLongOptionValues(args)
	var rules, command []string
	pretty, resolveHostExecutables := false, false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--config", "-c", "--enable", "--disable":
			if index+1 >= len(args) || args[index+1] == "" {
				return nil, nil, false, false, fmt.Errorf("%s requires a value\n%s", args[index], execPolicyUsage)
			}
			index++
		case "--rules", "-r":
			if index+1 >= len(args) || args[index+1] == "" {
				return nil, nil, false, false, fmt.Errorf("--rules requires a path\n%s", execPolicyUsage)
			}
			path := args[index+1]
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			rules = append(rules, filepath.Clean(path))
			index++
		case "--pretty":
			pretty = true
		case "--resolve-host-executables":
			resolveHostExecutables = true
		case "--":
			command = append(command, args[index+1:]...)
			index = len(args)
		default:
			command = append(command, args[index:]...)
			index = len(args)
		}
	}
	if len(rules) == 0 || len(command) == 0 {
		return nil, nil, false, false, fmt.Errorf("%s", execPolicyUsage)
	}
	return rules, command, pretty, resolveHostExecutables, nil
}
