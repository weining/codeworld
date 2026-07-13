package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/execpolicy"
)

func TestExecPolicyCheckOutputsEvaluationWithoutExecuting(t *testing.T) {
	root := t.TempDir()
	rules := filepath.Join(root, "policy.rules")
	marker := filepath.Join(root, "not-created")
	content := strings.Join([]string{
		`prefix_rule(pattern=["touch"], decision="allow")`,
		`prefix_rule(pattern=["touch", "blocked"], decision="forbidden")`,
	}, "\n") + "\n"
	if err := os.WriteFile(rules, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runExecPolicyCommand(&out, root, []string{"check", "--pretty", "--rules=policy.rules", "--", "touch", marker}); err != nil {
		t.Fatal(err)
	}
	var result execpolicy.Evaluation
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision != "allow" || len(result.MatchedRules) != 1 || !strings.Contains(out.String(), "\n  \"matchedRules\"") {
		t.Fatalf("output=%s result=%#v", out.String(), result)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("checked command was executed: %v", err)
	}
}

func TestExecPolicyCheckCombinesRepeatedRuleFiles(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"allow.rules":  `prefix_rule(pattern=["git"], decision="allow")`,
		"prompt.rules": `prefix_rule(pattern=["git", "push"], decision="prompt")`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := runExecPolicyCommand(&out, root, []string{"check", "-c", "model=ignored", "--enable", "example", "-r", "allow.rules", "--rules", "prompt.rules", "git", "push"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"decision":"prompt"`) || !strings.Contains(out.String(), `"matchedRules":[`) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestExecPolicyCheckResolvesAllowedHostExecutable(t *testing.T) {
	root := t.TempDir()
	rules := filepath.Join(root, "host.rules")
	content := strings.Join([]string{
		`prefix_rule(pattern=["git", "status"], decision="allow")`,
		`host_executable(`,
		`    name="git",`,
		`    paths=["/usr/bin/git"],`,
		`)`,
	}, "\n") + "\n"
	if err := os.WriteFile(rules, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runExecPolicyCommand(&out, root, []string{"check", "--resolve-host-executables", "--rules", rules, "/usr/bin/git", "status"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"resolvedProgram":"/usr/bin/git"`) || !strings.Contains(out.String(), `"decision":"allow"`) {
		t.Fatalf("output=%s", out.String())
	}
	out.Reset()
	if err := runExecPolicyCommand(&out, root, []string{"check", "--resolve-host-executables", "--rules", rules, "/opt/git", "status"}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != `{"matchedRules":[]}` {
		t.Fatalf("disallowed output=%s", out.String())
	}
}

func TestExecPolicyCheckValidatesArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"missing"}, {"check"}, {"check", "--rules"}, {"check", "--rules", "x.rules"}} {
		if err := runExecPolicyCommand(&bytes.Buffer{}, t.TempDir(), args); err == nil {
			t.Fatalf("args %#v accepted", args)
		}
	}
}
