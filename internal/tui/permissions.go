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

// NewTUIConfirmer 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewTUIConfirmer() *TUIConfirmer {
	return &TUIConfirmer{
		requests:  make(chan permissions.Request, 1),
		decisions: make(chan PermissionDecision, 1),
	}
}

// Requests 提供对外可复用的能力，并隐藏内部实现细节。
func (c *TUIConfirmer) Requests() <-chan permissions.Request {
	return c.requests
}

// Decide 提供对外可复用的能力，并隐藏内部实现细节。
func (c *TUIConfirmer) Decide(decision PermissionDecision) {
	c.decisions <- decision
}

// Confirm 向用户确认权限请求，并把选择返回给 agent 流程。
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

// FormatPermissionRequest 将内部数据格式化为面向用户或模型的文本。
func FormatPermissionRequest(req permissions.Request) string {
	text := fmt.Sprintf("Permission required: %s\nTarget: %s\nRisk: %s\nReason: %s", req.Action, req.Target, req.Risk, req.Reason)
	if req.Preview != "" {
		text += "\nPreview:\n" + req.Preview
	}
	if req.Action == permissions.ActionShell {
		text += "\nAllow? [y/N/a=session]"
	} else {
		text += "\nAllow? [y/N]"
	}
	return text
}
