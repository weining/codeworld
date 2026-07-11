package main

import "fmt"

type globalOptions struct {
	Profile string
}

func parseGlobalOptions(args []string) (globalOptions, []string, error) {
	var opts globalOptions
	for len(args) > 0 {
		switch args[0] {
		case "--profile", "-p":
			if len(args) < 2 || args[1] == "" {
				return globalOptions{}, nil, fmt.Errorf("--profile requires a name")
			}
			if opts.Profile != "" {
				return globalOptions{}, nil, fmt.Errorf("--profile may be specified only once")
			}
			opts.Profile = args[1]
			args = args[2:]
		case "--":
			return opts, args[1:], nil
		default:
			return opts, args, nil
		}
	}
	return opts, args, nil
}
