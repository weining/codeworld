package main

import (
	"context"
	"fmt"
	"io"

	"codeworld/internal/app"
	"codeworld/internal/config"
)

func runDebugCommand(ctx context.Context, out io.Writer, root string, global globalOptions, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: codeworld debug <models|prompt-input>")
	}
	switch args[0] {
	case "models":
		if len(args) > 2 || len(args) == 2 && args[1] != "--bundled" {
			return fmt.Errorf("usage: codeworld debug models [--bundled]")
		}
		cfg, err := config.LoadWithOptions(root, config.LoadOptions{Profile: global.Profile, Overrides: global.Config})
		if err != nil {
			return err
		}
		return writeJSON(out, map[string]any{
			"provider": cfg.Provider,
			"models":   []map[string]any{{"id": cfg.Model, "provider": cfg.Provider, "configured": true}},
			"note":     "Codeworld providers do not expose a remote model catalog; this is the effective configured selection.",
		})
	case "prompt-input":
		prompt, images, err := parseDebugPromptOptions(args[1:])
		if err != nil {
			return err
		}
		messages, err := app.DebugPromptInput(root, global.Profile, global.Config, prompt, images)
		if err != nil {
			return err
		}
		return writeJSON(out, messages)
	default:
		return fmt.Errorf("usage: codeworld debug <models|prompt-input>")
	}
}

func parseDebugPromptOptions(args []string) (string, []string, error) {
	var prompt string
	var images []string
	for index := 0; index < len(args); index++ {
		if args[index] == "--image" || args[index] == "-i" {
			if index+1 >= len(args) {
				return "", nil, fmt.Errorf("%s requires a path", args[index])
			}
			images, index = append(images, args[index+1]), index+1
			continue
		}
		if len(args[index]) > 0 && args[index][0] == '-' {
			return "", nil, fmt.Errorf("unknown debug prompt-input option %s", args[index])
		}
		if prompt != "" {
			return "", nil, fmt.Errorf("prompt may be specified only once")
		}
		prompt = args[index]
	}
	return prompt, images, nil
}
