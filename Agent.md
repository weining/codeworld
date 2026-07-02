# Agent Notes

This file is for agents and developers working in this repository.

## Project Shape

Codeworld is a Go coding agent with these main layers:

- `cmd/codeworld`: CLI dispatch for TUI, REPL, one-shot run, and indexing.
- `internal/app`: runtime assembly for config, workspace, session, model client, tools, capabilities, and context.
- `internal/agent`: turn loop, model calls, tool calls, permission flow, and streaming turn events.
- `internal/model`: provider-neutral model types and provider clients.
- `internal/tools`: workspace tools, git tools, plugin tools, MCP tool bridge, and context tools.
- `internal/tui`: Bubble Tea interactive UI.
- `internal/capability`: project skills, plugins, and MCP loading.
- `internal/context`: file index, conversation summary, and context graph.

## Local Rules

- Prefer `mise exec -- go ...` for Go commands.
- Use `rg` or `rg --files` for search.
- Keep changes narrow and consistent with existing package boundaries.
- 新增或修改代码注释时使用中文；注释应解释复杂流程、边界、安全策略或非直观取舍，避免为显而易见的代码补空泛说明。
- Do not commit local runtime state:
  - `.codeworld/`
  - `.idea/`
  - local `codeworld` binaries
- Do not log plaintext API keys. Model call logs may include headers only with authorization redacted.
- Preserve compatibility for:
  - `codeworld`
  - `codeworld tui`
  - `codeworld repl`
  - `codeworld run <task>`
  - `codeworld index`

## Testing

Use focused tests while editing, then run full verification before reporting completion.

```bash
mise exec -- go test -count=1 ./...
mise exec -- go build -o /private/tmp/codeworld-build ./cmd/codeworld
printf '/exit\n' | mise exec -- go run ./cmd/codeworld tui
```

When a change affects model providers, include the relevant provider package tests:

```bash
mise exec -- go test -count=1 ./internal/model ./internal/model/deepseek ./internal/model/openai ./internal/model/provider
```

When a change affects interaction behavior, include:

```bash
mise exec -- go test -count=1 ./cmd/codeworld ./internal/agent ./internal/app ./internal/tui ./internal/repl
```

## Implementation Notes

- Streaming starts at `model.StreamClient`, is normalized into `agent.TurnEvent`, and is consumed by TUI transcript updates.
- Providers that do not implement streaming should still work through `Generate`.
- Tools must implement `Definition`, `PermissionRequest`, and `Execute`.
- Permission requests should describe the target, risk, and reason clearly.
- MCP tools are namespaced as `mcp.<server>.<tool>`.
- Plugin tools must use their plugin namespace, for example `demo.echo`.
- Skills are loaded from `.codeworld/skills/<name>/SKILL.md`.
- Context graph search is deterministic lexical search, not embeddings.

## Documentation Updates

When changing user-visible behavior, update `README.md`.

When making a major feature iteration, update both `README.md` and `README.zh-CN.md` in the same change.

When changing development workflows, package boundaries, or verification commands, update this file.
