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
	"codeworld/internal/sandbox"
)

const execUsage = "usage: codeworld exec [resume <session-id|--last>] [-c <key=value>] [--strict-config] [--json] [--ephemeral] [--approval-mode <auto|read-only|full-access>] [--sandbox <read-only|workspace-write|danger-full-access>] [--network|--no-network] [--image <path>] [--output-schema <path>] [-o <path>] <task|->"
const reviewUsage = "usage: codeworld review [--uncommitted|--base <branch>|--commit <sha>] [--title <title>] [-c <key=value>] [--strict-config] [--json] [--output-schema <path>] [-o <path>] [instructions]"

type automationOptions struct {
	prompt       string
	images       []model.ContentPart
	json         bool
	ephemeral    bool
	outputSchema []byte
	outputPath   string
	approvalMode permissions.Mode
	sandboxMode  sandbox.Mode
	network      *bool
	sessionID    string
	resumeLast   bool
	config       []string
	strict       bool
	modelName    string
	profile      string
	workingDir   string
	ignoreUser   bool
	ignoreRules  bool
	color        string
	stdinRead    bool
}

func runExecCommand(ctx context.Context, in io.Reader, out, stderr io.Writer, root string, global globalOptions, args []string) error {
	opts, err := parseExecArgs(in, args)
	if err != nil {
		return err
	}
	root, global, err = applyAutomationLocation(root, global, opts)
	if err != nil {
		return err
	}
	runtimeOpts, err := runtimeOptionsForAutomation(root, global, in, out, stderr, opts)
	if err != nil {
		return err
	}
	rt, err := app.NewRuntime(ctx, runtimeOpts)
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

func runtimeOptionsForAutomation(root string, global globalOptions, in io.Reader, out, stderr io.Writer, opts automationOptions) (app.Options, error) {
	runtimeOpts := runtimeOptionsFromGlobal(root, global, in, out, stderr)
	runtimeOpts.Ephemeral = opts.ephemeral
	runtimeOpts.SessionID = opts.sessionID
	runtimeOpts.ResumeLast = opts.resumeLast
	if opts.approvalMode != "" {
		runtimeOpts.ApprovalMode = opts.approvalMode
	}
	if opts.sandboxMode != "" {
		runtimeOpts.SandboxMode = opts.sandboxMode
	}
	if opts.network != nil {
		runtimeOpts.SandboxNetwork = opts.network
	}
	runtimeOpts.ConfigOverrides = append(runtimeOpts.ConfigOverrides, opts.config...)
	runtimeOpts.SkipUserConfig = opts.ignoreUser
	if runtimeOpts.SandboxMode == sandbox.ModeDangerFullAccess && runtimeOpts.SandboxNetwork != nil {
		return app.Options{}, fmt.Errorf("network options cannot be combined with danger-full-access")
	}
	return runtimeOpts, nil
}

func parseExecArgs(in io.Reader, args []string) (automationOptions, error) {
	var sessionID string
	resumeLast := false
	if len(args) > 0 && args[0] == "resume" {
		if len(args) < 2 {
			return automationOptions{}, fmt.Errorf("%s", execUsage)
		}
		if args[1] == "--last" {
			resumeLast = true
		} else {
			sessionID = strings.TrimSpace(args[1])
			if sessionID == "" {
				return automationOptions{}, fmt.Errorf("%s", execUsage)
			}
		}
		args = args[2:]
	}
	opts, err := parseAutomationArgs(in, args, execUsage, true, true)
	if err != nil {
		return automationOptions{}, err
	}
	if opts.ephemeral && (resumeLast || sessionID != "") {
		return automationOptions{}, fmt.Errorf("exec resume cannot be combined with --ephemeral")
	}
	opts.sessionID = sessionID
	opts.resumeLast = resumeLast
	return opts, nil
}

func runReviewCommand(ctx context.Context, in io.Reader, out, stderr io.Writer, root string, global globalOptions, args []string) error {
	target, automation, err := parseReviewArgs(in, args)
	if err != nil {
		return err
	}
	if automation.approvalMode != "" || automation.sandboxMode != "" || automation.network != nil {
		return fmt.Errorf("review always runs in read-only permissions and sandboxing")
	}
	root, global, err = applyAutomationLocation(root, global, automation)
	if err != nil {
		return err
	}
	prompt, err := review.BuildPrompt(ctx, root, target, automation.prompt)
	if err != nil {
		return err
	}
	network := false
	runtimeOpts := runtimeOptionsFromGlobal(root, globalOptions{Profile: global.Profile, Model: global.Model}, in, out, stderr)
	runtimeOpts.Ephemeral = true
	runtimeOpts.ApprovalMode = permissions.ModeReadOnly
	runtimeOpts.SandboxMode = sandbox.ModeReadOnly
	runtimeOpts.SandboxNetwork = &network
	runtimeOpts.ConfigOverrides = append(runtimeOpts.ConfigOverrides, automation.config...)
	runtimeOpts.SkipUserConfig = automation.ignoreUser
	rt, err := app.NewRuntime(ctx, runtimeOpts)
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
		case "--config", "-c":
			if i+1 >= len(args) || args[i+1] == "" {
				return automationOptions{}, fmt.Errorf("%s", usage)
			}
			opts.config = append(opts.config, args[i+1])
			i++
		case "--strict-config":
			if opts.strict {
				return automationOptions{}, fmt.Errorf("--strict-config may be specified only once")
			}
			opts.strict = true
		case "--model", "-m":
			if i+1 >= len(args) || args[i+1] == "" || opts.modelName != "" {
				return automationOptions{}, fmt.Errorf("%s", usage)
			}
			opts.modelName = args[i+1]
			i++
		case "--profile", "-p":
			if i+1 >= len(args) || args[i+1] == "" || opts.profile != "" {
				return automationOptions{}, fmt.Errorf("%s", usage)
			}
			opts.profile = args[i+1]
			i++
		case "--cd", "-C":
			if i+1 >= len(args) || args[i+1] == "" || opts.workingDir != "" {
				return automationOptions{}, fmt.Errorf("%s", usage)
			}
			opts.workingDir = args[i+1]
			i++
		case "--ignore-user-config":
			opts.ignoreUser = true
		case "--ignore-rules", "--skip-git-repo-check":
			opts.ignoreRules = true
		case "--color":
			if i+1 >= len(args) || opts.color != "" {
				return automationOptions{}, fmt.Errorf("%s", usage)
			}
			opts.color = args[i+1]
			switch opts.color {
			case "auto", "always", "never":
			default:
				return automationOptions{}, fmt.Errorf("invalid color mode %q", opts.color)
			}
			i++
		case "--ephemeral":
			opts.ephemeral = true
		case "--approval-mode", "--ask-for-approval", "-a":
			if i+1 >= len(args) {
				return automationOptions{}, fmt.Errorf("%s", usage)
			}
			mode, err := parseApprovalMode(args[i+1])
			if err != nil {
				return automationOptions{}, err
			}
			opts.approvalMode = mode
			i++
		case "--sandbox", "-s":
			if i+1 >= len(args) {
				return automationOptions{}, fmt.Errorf("%s", usage)
			}
			opts.sandboxMode = sandbox.Mode(args[i+1])
			switch opts.sandboxMode {
			case sandbox.ModeReadOnly, sandbox.ModeWorkspaceWrite, sandbox.ModeDangerFullAccess:
			default:
				return automationOptions{}, fmt.Errorf("invalid sandbox mode %q", args[i+1])
			}
			i++
		case "--network", "--no-network":
			if opts.network != nil {
				return automationOptions{}, fmt.Errorf("network sandbox option may be specified only once")
			}
			enabled := args[i] == "--network"
			opts.network = &enabled
		case "--image", "-i":
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
			opts.stdinRead = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return automationOptions{}, fmt.Errorf("unknown option %s\n%s", args[i], usage)
			}
			promptParts = append(promptParts, args[i])
		}
	}
	if !opts.stdinRead {
		stdinText, err := readAutomationStdin(in)
		if err != nil {
			return automationOptions{}, err
		}
		if stdinText != "" {
			if len(promptParts) == 0 {
				promptParts = append(promptParts, stdinText)
			} else {
				promptParts = append(promptParts, "<stdin>\n"+stdinText+"\n</stdin>")
			}
		}
	}
	opts.prompt = strings.TrimSpace(strings.Join(promptParts, " "))
	if opts.sandboxMode == sandbox.ModeDangerFullAccess && opts.network != nil {
		return automationOptions{}, fmt.Errorf("--network and --no-network cannot be combined with danger-full-access")
	}
	if requirePrompt && opts.prompt == "" {
		return automationOptions{}, fmt.Errorf("%s", usage)
	}
	return opts, nil
}

func applyAutomationLocation(root string, global globalOptions, opts automationOptions) (string, globalOptions, error) {
	var err error
	if opts.workingDir != "" {
		root, err = resolveGlobalWorkingDir(root, opts.workingDir)
		if err != nil {
			return "", globalOptions{}, err
		}
	}
	if opts.profile != "" {
		global.Profile = opts.profile
	}
	if opts.modelName != "" {
		global.Model = opts.modelName
	}
	return root, global, nil
}

func readAutomationStdin(in io.Reader) (string, error) {
	if in == nil {
		return "", nil
	}
	if file, ok := in.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			return "", nil
		}
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
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
		case "--uncommitted":
			target.Uncommitted = true
		case "--title":
			if i+1 >= len(args) {
				return review.Target{}, automationOptions{}, fmt.Errorf("%s", reviewUsage)
			}
			target.Title = args[i+1]
			i++
		default:
			remaining = append(remaining, args[i])
		}
	}
	selected := 0
	if target.Base != "" {
		selected++
	}
	if target.Commit != "" {
		selected++
	}
	if target.Uncommitted {
		selected++
	}
	if selected > 1 {
		return review.Target{}, automationOptions{}, fmt.Errorf("review accepts only one of --uncommitted, --base, or --commit")
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
