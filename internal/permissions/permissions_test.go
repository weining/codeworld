package permissions

import (
	"context"
	"testing"
)

// TestConservativePolicyAllowsRead 验证对应场景的行为，避免后续改动破坏既有约束。
func TestConservativePolicyAllowsRead(t *testing.T) {
	policy := ConservativePolicy{}
	decision, err := policy.Check(context.Background(), Request{Action: ActionRead, Risk: RiskRead, Target: "README.md"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if decision.Kind != DecisionAllow {
		t.Fatalf("Kind = %q, want allow", decision.Kind)
	}
}

// TestConservativePolicyAsksForPatchWriteAndShell 验证对应场景的行为，避免后续改动破坏既有约束。
func TestConservativePolicyAsksForPatchWriteAndShell(t *testing.T) {
	policy := ConservativePolicy{}
	cases := []Request{
		{Action: ActionPatch, Risk: RiskWrite, Target: "patch"},
		{Action: ActionWrite, Risk: RiskWrite, Target: "main.go"},
		{Action: ActionShell, Risk: RiskExecute, Target: "go test ./..."},
	}

	for _, req := range cases {
		decision, err := policy.Check(context.Background(), req)
		if err != nil {
			t.Fatalf("Check returned error: %v", err)
		}
		if decision.Kind != DecisionAsk {
			t.Fatalf("Kind for %s = %q, want ask", req.Action, decision.Kind)
		}
	}
}

// TestConservativePolicyDeniesOutsideWorkspace 验证对应场景的行为，避免后续改动破坏既有约束。
func TestConservativePolicyDeniesOutsideWorkspace(t *testing.T) {
	policy := ConservativePolicy{}
	decision, err := policy.Check(context.Background(), Request{Action: ActionRead, Risk: RiskOutsideWorkspace, Target: "../secret"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if decision.Kind != DecisionDeny {
		t.Fatalf("Kind = %q, want deny", decision.Kind)
	}
}

// TestAutoPolicyAllowsWorkspaceActionsAndAsksForHighRiskShell 验证对应场景的行为，避免后续改动破坏既有约束。
func TestAutoPolicyAllowsWorkspaceActionsAndAsksForHighRiskShell(t *testing.T) {
	policy := AutoPolicy{}
	allowCases := []Request{
		{Action: ActionPatch, Risk: RiskWrite, Target: "patch"},
		{Action: ActionWrite, Risk: RiskWrite, Target: "main.go"},
	}
	for _, req := range allowCases {
		decision, err := policy.Check(context.Background(), req)
		if err != nil {
			t.Fatalf("Check returned error: %v", err)
		}
		if decision.Kind != DecisionAllow {
			t.Fatalf("Kind for %s/%s = %q, want allow", req.Action, req.Risk, decision.Kind)
		}
	}

	askCases := []Request{
		{Action: ActionShell, Risk: RiskRead, Target: "git status"},
		{Action: ActionShell, Risk: RiskExecute, Target: "go test ./..."},
		{Action: ActionShell, Risk: RiskWrite, Target: "go mod tidy"},
		{Action: ActionShell, Risk: RiskDestructive, Target: "rm -rf ."},
		{Action: ActionShell, Risk: RiskNetwork, Target: "curl https://example.com"},
	}
	for _, req := range askCases {
		decision, err := policy.Check(context.Background(), req)
		if err != nil {
			t.Fatalf("Check returned error: %v", err)
		}
		if decision.Kind != DecisionAsk {
			t.Fatalf("Kind for %s = %q, want ask", req.Target, decision.Kind)
		}
	}
}

// TestModePolicySupportsReadOnlyAndFullAccess 验证权限模式能切换读写边界。
func TestModePolicySupportsReadOnlyAndFullAccess(t *testing.T) {
	readOnly := ModePolicy{Mode: ModeReadOnly}
	decision, err := readOnly.Check(context.Background(), Request{Action: ActionWrite, Risk: RiskWrite, Target: "main.go"})
	if err != nil {
		t.Fatalf("readOnly.Check returned error: %v", err)
	}
	if decision.Kind != DecisionAsk {
		t.Fatalf("read-only write decision = %#v, want ask", decision)
	}

	full := ModePolicy{Mode: ModeFullAccess}
	decision, err = full.Check(context.Background(), Request{Action: ActionShell, Risk: RiskNetwork, Target: "curl https://example.com"})
	if err != nil {
		t.Fatalf("full.Check returned error: %v", err)
	}
	if decision.Kind != DecisionAllow {
		t.Fatalf("full access network decision = %#v, want allow", decision)
	}
}

func TestModePolicySupportsCodexApprovalModes(t *testing.T) {
	requests := map[string]Request{
		"read shell":    {Action: ActionShell, Risk: RiskRead, Target: "git status"},
		"write shell":   {Action: ActionShell, Risk: RiskWrite, Target: "go fmt ./..."},
		"network shell": {Action: ActionShell, Risk: RiskNetwork, Target: "git push"},
		"outside":       {Action: ActionRead, Risk: RiskOutsideWorkspace, Target: "../secret"},
		"requested":     {Action: ActionShell, Risk: RiskExecute, Target: "go test ./...", ApprovalRequested: true},
	}
	cases := []struct {
		mode Mode
		name string
		want DecisionKind
	}{
		{ModeUntrusted, "read shell", DecisionAllow},
		{ModeUntrusted, "write shell", DecisionAsk},
		{ModeOnRequest, "write shell", DecisionAllow},
		{ModeOnRequest, "network shell", DecisionAsk},
		{ModeOnRequest, "requested", DecisionAsk},
		{ModeNever, "network shell", DecisionAllow},
		{ModeNever, "outside", DecisionDeny},
	}
	for _, tc := range cases {
		decision, err := (ModePolicy{Mode: tc.mode}).Check(context.Background(), requests[tc.name])
		if err != nil || decision.Kind != tc.want {
			t.Fatalf("mode=%s request=%s decision=%#v err=%v, want %s", tc.mode, tc.name, decision, err, tc.want)
		}
	}
}

func TestModePolicyCanAllowOnlyExplicitNativeSearch(t *testing.T) {
	policy := ModePolicy{Mode: ModeReadOnly, AllowSearch: true}
	search, err := policy.Check(context.Background(), Request{Action: ActionRead, Risk: RiskNetwork, Target: "current news", Reason: "web_search"})
	if err != nil || search.Kind != DecisionAllow {
		t.Fatalf("search=%#v err=%v", search, err)
	}
	other, err := policy.Check(context.Background(), Request{Action: ActionShell, Risk: RiskNetwork, Target: "curl example.com", Reason: "shell"})
	if err != nil || other.Kind != DecisionAsk {
		t.Fatalf("other network=%#v err=%v", other, err)
	}
}
