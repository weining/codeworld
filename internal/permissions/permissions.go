package permissions

import "context"

type Action string

const (
	ActionRead  Action = "read"
	ActionWrite Action = "write"
	ActionPatch Action = "patch"
	ActionShell Action = "shell"
)

type Risk string

const (
	RiskRead             Risk = "read"
	RiskWrite            Risk = "write"
	RiskExecute          Risk = "execute"
	RiskDestructive      Risk = "destructive"
	RiskNetwork          Risk = "network"
	RiskOutsideWorkspace Risk = "outside_workspace"
)

type Request struct {
	Action  Action
	Target  string
	Risk    Risk
	Reason  string
	Preview string
}

type DecisionKind string

const (
	DecisionAllow DecisionKind = "allow"
	DecisionAsk   DecisionKind = "ask"
	DecisionDeny  DecisionKind = "deny"
)

type Decision struct {
	Kind   DecisionKind
	Reason string
}

type Policy interface {
	Check(ctx context.Context, req Request) (Decision, error)
}

type ConservativePolicy struct{}

func (ConservativePolicy) Check(ctx context.Context, req Request) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if req.Risk == RiskOutsideWorkspace {
		return Decision{Kind: DecisionDeny, Reason: "path is outside the workspace"}, nil
	}
	if req.Action == ActionRead && req.Risk == RiskRead {
		return Decision{Kind: DecisionAllow, Reason: "read-only workspace action"}, nil
	}
	return Decision{Kind: DecisionAsk, Reason: "action requires confirmation"}, nil
}

type AutoPolicy struct{}

func (AutoPolicy) Check(ctx context.Context, req Request) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	switch req.Risk {
	case RiskOutsideWorkspace:
		return Decision{Kind: DecisionDeny, Reason: "path is outside the workspace"}, nil
	case RiskDestructive, RiskNetwork:
		return Decision{Kind: DecisionAsk, Reason: "high-risk action requires confirmation"}, nil
	default:
		return Decision{Kind: DecisionAllow, Reason: "workspace action allowed by auto policy"}, nil
	}
}
