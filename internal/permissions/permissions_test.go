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
