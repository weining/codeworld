package review

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const maxReviewContext = 512 * 1024

type Target struct {
	Base   string
	Commit string
}

// BuildPrompt 收集指定 Git 变更集，并生成只读审查请求。
func BuildPrompt(ctx context.Context, root string, target Target, instructions string) (string, error) {
	if target.Base != "" && target.Commit != "" {
		return "", fmt.Errorf("review accepts only one of --base or --commit")
	}
	if err := validateRevision(target.Base); err != nil {
		return "", fmt.Errorf("invalid base: %w", err)
	}
	if err := validateRevision(target.Commit); err != nil {
		return "", fmt.Errorf("invalid commit: %w", err)
	}
	if _, err := git(ctx, root, "rev-parse", "--is-inside-work-tree"); err != nil {
		return "", fmt.Errorf("review requires a Git repository: %w", err)
	}

	label := "uncommitted changes (staged and unstaged)"
	var contextText string
	var err error
	switch {
	case target.Base != "":
		label = "changes against base " + target.Base
		contextText, err = git(ctx, root, "diff", "--no-ext-diff", "--find-renames", target.Base+"...HEAD", "--")
	case target.Commit != "":
		label = "commit " + target.Commit
		contextText, err = git(ctx, root, "show", "--no-ext-diff", "--find-renames", "--format=fuller", target.Commit, "--")
	default:
		var status, staged, unstaged string
		status, err = git(ctx, root, "status", "--short")
		if err == nil {
			staged, err = git(ctx, root, "diff", "--cached", "--no-ext-diff", "--find-renames", "--")
		}
		if err == nil {
			unstaged, err = git(ctx, root, "diff", "--no-ext-diff", "--find-renames", "--")
		}
		contextText = "Git status:\n" + status + "\n\nStaged diff:\n" + staged + "\n\nUnstaged diff:\n" + unstaged
	}
	if err != nil {
		return "", fmt.Errorf("collect review target: %w", err)
	}
	contextText = truncate(contextText, maxReviewContext)
	if strings.TrimSpace(contextText) == "" {
		contextText = "No textual diff was found. Inspect the repository to confirm whether there are reviewable changes."
	}

	prompt := "Review " + label + ". Do not modify files. Focus on correctness, regressions, security, races, and missing tests. " +
		"Report only actionable findings, ordered by severity, with precise file and line references. If there are no findings, say so explicitly."
	if instructions = strings.TrimSpace(instructions); instructions != "" {
		prompt += "\n\nAdditional review instructions:\n" + instructions
	}
	return prompt + "\n\nReview context:\n" + contextText, nil
}

func git(ctx context.Context, root string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func validateRevision(revision string) error {
	if revision == "" {
		return nil
	}
	if strings.HasPrefix(revision, "-") || strings.ContainsAny(revision, "\x00\r\n") {
		return fmt.Errorf("unsafe revision %q", revision)
	}
	return nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n\n[review context truncated]"
}
