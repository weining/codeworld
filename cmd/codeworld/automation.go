package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"codeworld/internal/app"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/review"
	runmode "codeworld/internal/run"
)

const execUsage = "usage: codeworld exec [--json] [--ephemeral] [--image <path>] [--output-schema <path>] [-o <path>] <task|->"
const reviewUsage = "usage: codeworld review [--base <branch>|--commit <sha>] [--json] [--output-schema <path>] [-o <path>] [instructions]"

type automationOptions struct {
	prompt       string
	images       []model.ContentPart
	json         bool
	ephemeral    bool
	outputSchema []byte
	outputPath   string
}

func runExecCommand(ctx context.Context, in io.Reader, out, stderr io.Writer, root string, args []string) error {
	opts, err := parseAutomationArgs(in, args, execUsage, true, true)
	if err != nil {
		return err
	}
	rt, err := app.NewRuntime(ctx, app.Options{
		Root: root, In: in, Out: out, Err: stderr, Ephemeral: opts.ephemeral,
	})
	if err != nil {
		return err
	}
	return withRuntime(rt, func(rt *app.Runtime) error {
		result, err := runmode.Execute(ctx, rt, opts.prompt, runmode.Options{
			Images: opts.images, JSON: opts.json, Ephemeral: opts.ephemeral, OutputSchema: opts.outputSchema,
		})
		if err != nil {
			return err
		}
		return writeLastMessage(opts.outputPath, result.FinalText)
	})
}

func runReviewCommand(ctx context.Context, in io.Reader, out, stderr io.Writer, root string, args []string) error {
	target, automation, err := parseReviewArgs(in, args)
	if err != nil {
		return err
	}
	prompt, err := review.BuildPrompt(ctx, root, target, automation.prompt)
	if err != nil {
		return err
	}
	rt, err := app.NewRuntime(ctx, app.Options{Root: root, In: in, Out: out, Err: stderr, Ephemeral: true})
	if err != nil {
		return err
	}
	return withRuntime(rt, func(rt *app.Runtime) error {
		rt.Runner.Policy = permissions.ModePolicy{Mode: permissions.ModeReadOnly}
		rt.Runner.SystemPrompt += "\n\nReview mode: inspect and report only. Do not modify files."
		result, err := runmode.Execute(ctx, rt, prompt, runmode.Options{
			JSON: automation.json, Ephemeral: true, OutputSchema: automation.outputSchema,
		})
		if err != nil {
			return err
		}
		return writeLastMessage(automation.outputPath, result.FinalText)
	})
}

func parseAutomationArgs(in io.Reader, args []string, usage string, allowImages, requirePrompt bool) (automationOptions, error) {
	var opts automationOptions
	var promptParts []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			opts.json = true
		case "--ephemeral":
			opts.ephemeral = true
		case "--image":
			if !allowImages || i+1 >= len(args) {
				return automationOptions{}, fmt.Errorf("%s", usage)
			}
			part, err := model.ImagePartFromFile(args[i+1])
			if err != nil {
				return automationOptions{}, err
			}
			opts.images = append(opts.images, part)
			i++
		case "--output-schema":
			if i+1 >= len(args) {
				return automationOptions{}, fmt.Errorf("%s", usage)
			}
			data, err := os.ReadFile(args[i+1])
			if err != nil {
				return automationOptions{}, fmt.Errorf("read output schema: %w", err)
			}
			opts.outputSchema = data
			i++
		case "-o", "--output-last-message":
			if i+1 >= len(args) {
				return automationOptions{}, fmt.Errorf("%s", usage)
			}
			opts.outputPath = args[i+1]
			i++
		case "-":
			data, err := io.ReadAll(in)
			if err != nil {
				return automationOptions{}, fmt.Errorf("read prompt from stdin: %w", err)
			}
			promptParts = append(promptParts, string(data))
		default:
			if strings.HasPrefix(args[i], "-") {
				return automationOptions{}, fmt.Errorf("unknown option %s\n%s", args[i], usage)
			}
			promptParts = append(promptParts, args[i])
		}
	}
	opts.prompt = strings.TrimSpace(strings.Join(promptParts, " "))
	if requirePrompt && opts.prompt == "" {
		return automationOptions{}, fmt.Errorf("%s", usage)
	}
	return opts, nil
}

func parseReviewArgs(in io.Reader, args []string) (review.Target, automationOptions, error) {
	var target review.Target
	var remaining []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--base":
			if i+1 >= len(args) {
				return review.Target{}, automationOptions{}, fmt.Errorf("%s", reviewUsage)
			}
			target.Base = args[i+1]
			i++
		case "--commit":
			if i+1 >= len(args) {
				return review.Target{}, automationOptions{}, fmt.Errorf("%s", reviewUsage)
			}
			target.Commit = args[i+1]
			i++
		default:
			remaining = append(remaining, args[i])
		}
	}
	if target.Base != "" && target.Commit != "" {
		return review.Target{}, automationOptions{}, fmt.Errorf("review accepts only one of --base or --commit")
	}
	automation, err := parseAutomationArgs(in, remaining, reviewUsage, false, false)
	if err != nil {
		return review.Target{}, automationOptions{}, err
	}
	return target, automation, nil
}

func writeLastMessage(path, text string) error {
	if path == "" {
		return nil
	}
	if err := os.WriteFile(path, []byte(text+"\n"), 0o600); err != nil {
		return fmt.Errorf("write final message: %w", err)
	}
	return nil
}
