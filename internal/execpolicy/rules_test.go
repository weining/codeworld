package execpolicy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/permissions"
)

func TestLoadUserAndProjectRules(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	writeRuleFile(t, filepath.Join(home, "rules", "default.rules"), `prefix_rule(pattern=["git", "status"], decision="allow")`)
	writeRuleFile(t, filepath.Join(root, ".codeworld", "rules", "network.rules"), `prefix_rule(pattern=["nc", "-vz", "2402:c480::1]"], decision="forbid")`)

	set, err := Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Rules) != 2 || strings.Join(set.Rules[0].Pattern, " ") != "git status" || set.Rules[1].Pattern[2] != "2402:c480::1]" {
		t.Fatalf("rules = %#v", set.Rules)
	}
	if !strings.Contains(set.Rules[1].Source, "network.rules:1") {
		t.Fatalf("source = %q", set.Rules[1].Source)
	}
}

func TestLoadRejectsInvalidRule(t *testing.T) {
	home := t.TempDir()
	writeRuleFile(t, filepath.Join(home, "rules", "bad.rules"), `prefix_rule(pattern=["git"], decision="maybe")`)
	if _, err := Load(t.TempDir(), home); err == nil || !strings.Contains(err.Error(), "invalid rule decision") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadFilesAndEvaluateMatchesCodexShape(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "allow.rules")
	second := filepath.Join(dir, "strict.rules")
	writeRuleFile(t, first, `prefix_rule(pattern=["git"], decision="allow")`)
	writeRuleFile(t, second, strings.Join([]string{
		`prefix_rule(pattern=["git", "push"], decision="prompt")`,
		`prefix_rule(pattern=["git", "push", "--force"], decision="forbidden")`,
	}, "\n"))
	set, err := LoadFiles([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	result := set.Evaluate([]string{"git", "push", "--force"})
	if result.Decision != "forbidden" || len(result.MatchedRules) != 3 || result.MatchedRules[1].PrefixRuleMatch.Decision != "prompt" || strings.Join(result.MatchedRules[2].PrefixRuleMatch.MatchedPrefix, " ") != "git push --force" {
		t.Fatalf("evaluation = %#v", result)
	}
	unmatched := set.Evaluate([]string{"go", "test", "./..."})
	if unmatched.Decision != "" || len(unmatched.MatchedRules) != 0 {
		t.Fatalf("unmatched = %#v", unmatched)
	}
}

func TestHostExecutableResolutionIsExplicitAndConstrained(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "host.rules")
	writeRuleFile(t, path, strings.Join([]string{
		`prefix_rule(`,
		`    pattern=["git", "status"], # inline comment`,
		`    decision="allow",`,
		`)`,
		`host_executable(`,
		`    name="git",`,
		`    paths=["/usr/bin/git", "/usr/bin/git"],`,
		`)`,
	}, "\n"))
	set, err := LoadFiles([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.HostExecutables["git"]) != 1 {
		t.Fatalf("host executables=%#v", set.HostExecutables)
	}
	if result := set.Evaluate([]string{"/usr/bin/git", "status"}); len(result.MatchedRules) != 0 {
		t.Fatalf("resolution enabled by default: %#v", result)
	}
	result := set.EvaluateWithOptions([]string{"/usr/bin/git", "status"}, MatchOptions{ResolveHostExecutables: true})
	if result.Decision != "allow" || len(result.MatchedRules) != 1 || result.MatchedRules[0].PrefixRuleMatch.ResolvedProgram != "/usr/bin/git" {
		t.Fatalf("resolved result=%#v", result)
	}
	if result := set.EvaluateWithOptions([]string{"/opt/homebrew/bin/git", "status"}, MatchOptions{ResolveHostExecutables: true}); len(result.MatchedRules) != 0 {
		t.Fatalf("unlisted executable resolved: %#v", result)
	}
	policy := Policy{Base: permissions.AutoPolicy{}, Set: set}
	assertDecision(t, policy, "/usr/bin/git status", permissions.DecisionAllow)
	assertDecision(t, policy, "/opt/homebrew/bin/git status", permissions.DecisionAsk)
}

func TestHostExecutableValidation(t *testing.T) {
	for name, statement := range map[string]string{
		"relative": `host_executable(name="git", paths=["bin/git"])`,
		"basename": `host_executable(name="git", paths=["/usr/bin/go"])`,
		"name":     `host_executable(name="bin/git", paths=["/usr/bin/git"])`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.rules")
			writeRuleFile(t, path, statement)
			if _, err := LoadFiles([]string{path}); err == nil {
				t.Fatal("invalid host executable accepted")
			}
		})
	}
}

func TestHostExecutableResolutionPrefersExactRules(t *testing.T) {
	set := Set{
		Rules: []Rule{
			{Pattern: []string{"git"}, Decision: permissions.DecisionAllow},
			{Pattern: []string{"/usr/bin/git"}, Decision: permissions.DecisionDeny},
		},
		HostExecutables: map[string][]string{"git": {"/usr/bin/git"}},
	}
	result := set.EvaluateWithOptions([]string{"/usr/bin/git", "status"}, MatchOptions{ResolveHostExecutables: true})
	if result.Decision != "forbidden" || len(result.MatchedRules) != 1 || result.MatchedRules[0].PrefixRuleMatch.ResolvedProgram != "" {
		t.Fatalf("exact result=%#v", result)
	}

	unconstrained := Set{Rules: []Rule{{Pattern: []string{"go", "test"}, Decision: permissions.DecisionAllow}}}
	result = unconstrained.EvaluateWithOptions([]string{"/usr/bin/go", "test", "./..."}, MatchOptions{ResolveHostExecutables: true})
	if result.Decision != "allow" || result.MatchedRules[0].PrefixRuleMatch.ResolvedProgram != "/usr/bin/go" {
		t.Fatalf("unconstrained result=%#v", result)
	}
}

func TestPolicyAppliesRulePrecedence(t *testing.T) {
	policy := Policy{Base: permissions.AutoPolicy{}, Set: Set{Rules: []Rule{
		{Pattern: []string{"git"}, Decision: permissions.DecisionAllow, Source: "allow.rules:1"},
		{Pattern: []string{"git", "push"}, Decision: permissions.DecisionAsk, Source: "prompt.rules:1"},
		{Pattern: []string{"git", "push", "--force"}, Decision: permissions.DecisionDeny, Source: "deny.rules:1"},
	}}}

	assertDecision(t, policy, "git status", permissions.DecisionAllow)
	assertDecision(t, policy, "git push", permissions.DecisionAsk)
	assertDecision(t, policy, "git push --force", permissions.DecisionDeny)
	assertDecision(t, policy, "git status; git diff", permissions.DecisionAllow)
	assertDecision(t, policy, "git status; go test ./...", permissions.DecisionAsk)
}

func TestPolicyDoesNotAutoAllowDynamicShellSyntax(t *testing.T) {
	policy := Policy{Base: permissions.AutoPolicy{}, Set: Set{Rules: []Rule{{
		Pattern: []string{"git"}, Decision: permissions.DecisionAllow, Source: "allow.rules:1",
	}}}}
	for _, command := range []string{`git status > result`, `git "$SUBCOMMAND"`, "git `pick-command`", `git status \`} {
		assertDecision(t, policy, command, permissions.DecisionAsk)
	}
	assertDecision(t, policy, `git log --format="hello world"`, permissions.DecisionAllow)
}

func TestPolicyDoesNotAutoAllowNestedShellScript(t *testing.T) {
	policy := Policy{Base: permissions.AutoPolicy{}, Set: Set{Rules: []Rule{{
		Pattern: []string{"sh"}, Decision: permissions.DecisionAllow, Source: "allow.rules:1",
	}}}}
	assertDecision(t, policy, `sh -c "git status; rm -rf ."`, permissions.DecisionAsk)
}

func TestPolicyFallsBackForNonShellAndEmptyBase(t *testing.T) {
	policy := Policy{Set: Set{Rules: []Rule{{Pattern: []string{"git"}, Decision: permissions.DecisionAllow}}}}
	decision, err := policy.Check(context.Background(), permissions.Request{Action: permissions.ActionWrite, Risk: permissions.RiskWrite, Target: "main.go"})
	if err != nil || decision.Kind != permissions.DecisionAllow {
		t.Fatalf("decision=%#v err=%v", decision, err)
	}
}

func assertDecision(t *testing.T, policy Policy, command string, want permissions.DecisionKind) {
	t.Helper()
	decision, err := policy.Check(context.Background(), permissions.Request{Action: permissions.ActionShell, Risk: permissions.RiskExecute, Target: command})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != want {
		t.Fatalf("%q decision = %#v, want %q", command, decision, want)
	}
}

func writeRuleFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
