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

type Mode string

const (
	ModeAuto       Mode = "auto"
	ModeReadOnly   Mode = "read-only"
	ModeFullAccess Mode = "full-access"
)

type ModePolicy struct {
	Mode Mode
}

// Check 根据当前 approval mode 执行权限判定。
func (p ModePolicy) Check(ctx context.Context, req Request) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	switch p.Mode {
	case ModeReadOnly:
		if req.Action == ActionRead && req.Risk == RiskRead {
			return Decision{Kind: DecisionAllow, Reason: "read-only mode allows reads"}, nil
		}
		if req.Risk == RiskOutsideWorkspace {
			return Decision{Kind: DecisionDeny, Reason: "path is outside the workspace"}, nil
		}
		return Decision{Kind: DecisionAsk, Reason: "read-only mode requires confirmation"}, nil
	case ModeFullAccess:
		if req.Risk == RiskOutsideWorkspace {
			return Decision{Kind: DecisionAsk, Reason: "outside workspace action requires confirmation"}, nil
		}
		return Decision{Kind: DecisionAllow, Reason: "full-access mode allows action"}, nil
	default:
		return AutoPolicy{}.Check(ctx, req)
	}
}

type ConservativePolicy struct{}

// Check 根据权限请求返回允许、拒绝或询问用户的决策。
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

// Check 根据权限请求返回允许、拒绝或询问用户的决策。
func (AutoPolicy) Check(ctx context.Context, req Request) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	switch req.Risk {
	case RiskOutsideWorkspace:
		return Decision{Kind: DecisionDeny, Reason: "path is outside the workspace"}, nil
	case RiskDestructive, RiskNetwork:
		return Decision{Kind: DecisionAsk, Reason: "high-risk action requires confirmation"}, nil
	}
	// Shell and extension tools can execute arbitrary code or read outside the
	// workspace, so heuristic risk classification must not be their security
	// boundary.
	if req.Action == ActionShell {
		return Decision{Kind: DecisionAsk, Reason: "command execution requires confirmation"}, nil
	}
	return Decision{Kind: DecisionAllow, Reason: "workspace action allowed by auto policy"}, nil
}
