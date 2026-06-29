package permissions

import (
	"context"
	"testing"
)

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

func TestAutoPolicyAllowsWorkspaceActionsAndAsksForHighRiskShell(t *testing.T) {
	policy := AutoPolicy{}
	allowCases := []Request{
		{Action: ActionPatch, Risk: RiskWrite, Target: "patch"},
		{Action: ActionWrite, Risk: RiskWrite, Target: "main.go"},
		{Action: ActionShell, Risk: RiskExecute, Target: "go test ./..."},
		{Action: ActionShell, Risk: RiskWrite, Target: "go mod tidy"},
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
