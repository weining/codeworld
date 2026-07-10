package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"codeworld/internal/model"
)

type Registry struct {
	tools map[string]Tool
}

// NewRegistry 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewRegistry(items []Tool, extra []Tool) *Registry {
	registry := &Registry{tools: make(map[string]Tool)}
	for _, tool := range items {
		registry.Register(tool)
	}
	for _, tool := range extra {
		registry.Register(tool)
	}
	return registry
}

// Register 提供对外可复用的能力，并隐藏内部实现细节。
func (r *Registry) Register(tool Tool) {
	r.tools[tool.Definition().Name] = tool
}

// RegisterChecked rejects ambiguous tool names instead of silently shadowing
// an existing capability.
func (r *Registry) RegisterChecked(tool Tool) error {
	name := tool.Definition().Name
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("duplicate tool %q", name)
	}
	r.tools[name] = tool
	return nil
}

// Definitions 提供对外可复用的能力，并隐藏内部实现细节。
func (r *Registry) Definitions() []model.ToolDefinition {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	defs := make([]model.ToolDefinition, 0, len(names))
	for _, name := range names {
		defs = append(defs, r.tools[name].Definition())
	}
	return defs
}

// Get 提供对外可复用的能力，并隐藏内部实现细节。
func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// Execute 执行工具主体逻辑，并返回可序列化的工具结果。
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	tool, ok := r.Get(name)
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", name)
	}
	return tool.Execute(ctx, args)
}
