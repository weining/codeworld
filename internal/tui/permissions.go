package tui

import (
	"context"
	"fmt"

	"codeworld/internal/permissions"
)

type PermissionDecision struct {
	Allow   bool
	Session bool
}

type TUIConfirmer struct {
	requests  chan permissions.Request
	decisions chan PermissionDecision
}

func NewTUIConfirmer() *TUIConfirmer {
	return &TUIConfirmer{
		requests:  make(chan permissions.Request, 1),
		decisions: make(chan PermissionDecision, 1),
	}
}

func (c *TUIConfirmer) Requests() <-chan permissions.Request {
	return c.requests
}

func (c *TUIConfirmer) Decide(decision PermissionDecision) {
	c.decisions <- decision
}

func (c *TUIConfirmer) Confirm(ctx context.Context, req permissions.Request, decision permissions.Decision) (bool, error) {
	select {
	case c.requests <- req:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	select {
	case result := <-c.decisions:
		return result.Allow, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func FormatPermissionRequest(req permissions.Request) string {
	text := fmt.Sprintf("permission required risk=%s\ntarget: %s\nreason: %s", req.Risk, req.Target, req.Reason)
	if req.Preview != "" {
		text += "\npreview:\n" + req.Preview
	}
	if req.Action == permissions.ActionShell {
		text += "\n[y] allow once   [n] deny   [a] allow similar shell command this session"
	} else {
		text += "\n[y] allow once   [n] deny"
	}
	return text
}
