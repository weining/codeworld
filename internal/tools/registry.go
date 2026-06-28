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

func (r *Registry) Register(tool Tool) {
	r.tools[tool.Definition().Name] = tool
}

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

func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	tool, ok := r.Get(name)
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", name)
	}
	return tool.Execute(ctx, args)
}
