# Codeworld Phase 3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add streaming output, unified skills/MCP/plugin capabilities, advanced TUI behavior, smarter context, and TUI-by-default entry while preserving existing run/repl workflows.

**Architecture:** Introduce a model stream interface and an agent event stream first, then adapt TUI/run mode to consume those events. Add capability and context layers behind runtime construction so plugins, skills, MCP servers, and context graph tools register through one path.

**Tech Stack:** Go standard library, existing Bubble Tea/Bubbles/Lip Gloss TUI, OpenAI-compatible SSE streaming for DeepSeek/OpenAI/local, stdio JSON-RPC for initial MCP support, deterministic local context graph.

---

## Task 1: Model Stream Types

**Files:**
- Modify: `internal/model/model.go`
- Test: `internal/model/model_test.go`

Add `StreamEvent`, `StreamEventKind`, and optional `StreamClient`. Tests verify accumulated text and usage events.

## Task 2: Agent Turn Event Stream

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_test.go`

Add `TurnEvent`, `TurnEventKind`, and `Runner.RunTurnStream`. It emits assistant deltas/done, tool events, permission events, usage, and error. Existing `RunTurn` remains compatible.

## Task 3: OpenAI-Compatible Streaming Parser

**Files:**
- Modify: `internal/model/openai/client.go`
- Modify: `internal/model/openai/client_test.go`
- Modify: `internal/model/deepseek/client.go`
- Modify: `internal/model/deepseek/client_test.go`

Implement SSE parsing for `stream=true` responses. DeepSeek should share the OpenAI-compatible stream parser where practical. Tests use `httptest` streaming chunks.

## Task 4: Provider Stream Fallback

**Files:**
- Modify: `internal/model/provider/factory.go`
- Modify: `internal/agent/agent_test.go`
- Modify: `internal/model/anthropic/client.go`

Ensure Anthropic still works through non-stream fallback. Agent tests prove a non-streaming model produces the same final events with one assistant done event.

## Task 5: TUI Streaming Transcript

**Files:**
- Modify: `internal/tui/adapter.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/types.go`
- Modify: `internal/tui/tui_test.go`

TUI consumes agent turn events. Assistant deltas update the active assistant transcript item in place. Tool and permission events keep their existing transcript behavior.

## Task 6: Default TUI Entry And REPL Command

**Files:**
- Modify: `cmd/codeworld/main.go`
- Modify: `cmd/codeworld/main_test.go`

`codeworld` starts TUI for terminal output, `codeworld repl` starts line REPL, `codeworld tui` remains explicit, and `codeworld run` is unchanged. Non-terminal default falls back to REPL.

## Task 7: Skill Loader

**Files:**
- Create: `internal/skill/skill.go`
- Create: `internal/skill/skill_test.go`
- Modify: `internal/app/runtime.go`
- Modify: `internal/config/config.go`

Load `.codeworld/skills/*/SKILL.md`, parse optional `name` and `description`, and inject loaded skill text into system context. Add `/skills` TUI command to list project skills.

## Task 8: MCP Stdio Client

**Files:**
- Create: `internal/mcp/client.go`
- Create: `internal/mcp/client_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

Add config parsing for `[[mcp_servers]]`. Implement stdio JSON-RPC initialize, tools/list, tools/call, resources/list, resources/read, prompts/list. Tests use a tiny Go helper process or test binary mode as a fake MCP server.

## Task 9: MCP Tool Bridge

**Files:**
- Create: `internal/tools/mcp_tool.go`
- Create: `internal/tools/mcp_tool_test.go`
- Modify: `internal/app/runtime.go`

Expose MCP tools as `mcp.<server>.<tool>` in the existing registry. Tool calls route to `tools/call` and return text/json content as tool output.

## Task 10: Capability Registry

**Files:**
- Create: `internal/capability/registry.go`
- Create: `internal/capability/registry_test.go`
- Modify: `internal/app/runtime.go`

Unify plugin tools, MCP tools, skill context, and future context providers behind one runtime loading path. Existing plugin behavior must remain unchanged.

## Task 11: Context Graph

**Files:**
- Create: `internal/context/graph/graph.go`
- Create: `internal/context/graph/graph_test.go`
- Modify: `internal/app/runtime.go`

Build a graph containing file entries, Go symbols, git changed files, session mentions, summaries, skills, and MCP resource snippets. Initial search is deterministic lexical matching.

## Task 12: Context Tools

**Files:**
- Create: `internal/tools/context_tools.go`
- Create: `internal/tools/context_tools_test.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/app/runtime.go`

Add `context_refresh`, `context_search`, and `context_open`. These use the context graph and workspace-safe reads.

## Task 13: TUI Advanced Commands

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/tui_test.go`

Add `/permissions`, `/mcp`, `/skills`, `/context`, `/theme`, and `/repl` responses. `/repl` should tell users to restart with `codeworld repl` rather than trying to swap UI modes in-process.

## Task 14: TUI Scroll And Status Polish

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/tui.go`
- Modify: `internal/tui/tui_test.go`

Add PageUp/PageDown and Ctrl+U/Ctrl+D viewport scrolling. Compact the status line and include git dirty state when cheaply available.

## Task 15: Full Verification And Push

**Files:**
- Review: all changed files

Run:

```bash
mise exec -- gofmt -w cmd/codeworld/*.go internal/**/*.go
mise exec -- go test -count=1 ./...
mise exec -- go build -o /private/tmp/codeworld-build ./cmd/codeworld
printf '/exit\n' | mise exec -- go run ./cmd/codeworld tui
git status --short
git push
```

Expected: tests and build pass, smoke run exits 0, only pre-existing untracked local files remain.

---

## Notes

- Use TDD for each task: write the failing test first, verify failure, implement, verify pass, commit.
- Keep `Generate` and `RunTurn` compatible throughout.
- Do not add embeddings in this phase.
- Do not make Anthropic streaming a blocker for TUI streaming; fallback is acceptable.
- Do not track `.codeworld/`, `.idea/`, or local `codeworld` binaries.
