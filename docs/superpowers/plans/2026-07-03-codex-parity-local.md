# Codex Parity Local Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring Codeworld's local CLI/TUI closer to Codex CLI by adding session resume, richer TUI controls, plan/goal/compact context controls, image input, HTTP/OAuth MCP, hooks/sandbox policy, and local subagents.

**Architecture:** Implement this as seven independently testable vertical slices. Each slice extends the existing package boundary instead of replacing the current runtime: `session` owns persisted conversations, `tui` owns interactive controls, `agent` owns turn planning and subagent orchestration, `model` owns multimodal message transport, `mcp` owns transports and auth, and `permissions/hooks` own safety gates.

**Tech Stack:** Go 1.26, Bubble Tea/Bubbles/Lipgloss, local JSON session storage, TOML-like config parser extended only as needed, HTTP/SSE for MCP, provider-specific Chat Completions style message serialization.

---

### Task 1: Session Resume and TUI Controls

**Files:**
- Modify: `internal/session/session.go`
- Modify: `internal/session/session_test.go`
- Modify: `internal/app/runtime.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/tui_test.go`
- Modify: `cmd/codeworld/main.go`
- Modify: `cmd/codeworld/main_test.go`
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [x] Add `Store.List`, `Store.Load`, and `Store.SetCurrent` tests covering recent-first ordering and invalid IDs.
- [x] Add CLI tests for `codeworld resume --last` and `codeworld resume <session-id>`.
- [x] Add TUI tests for `/resume`, prompt history Up/Down, slash popup rendering for `/`, and syntax-highlight-friendly transcript rendering for diff/code blocks.
- [x] Implement session listing/loading without changing the existing `current-session.json` format.
- [x] Add runtime option `SessionID` and `ResumeLast` so CLI/TUI/REPL share the same resume path.
- [x] Add TUI state for draft history and slash suggestions.
- [x] Render command suggestions when the composer starts with `/`.
- [x] Add lightweight transcript formatting for fenced code blocks and git diff lines using existing Lipgloss styles.
- [x] Update README files with resume and TUI control usage.
- [x] Verify with `go test -count=1 ./cmd/codeworld ./internal/session ./internal/app ./internal/tui`.
- [x] Commit as `feat: add session resume and tui controls`.

### Task 2: Plan, Goal, and Compact

**Files:**
- Modify: `internal/session/session.go`
- Modify: `internal/agent/agent.go`
- Modify: `internal/app/runtime.go`
- Modify: `internal/tui/commands.go`
- Modify: `internal/repl/repl.go`
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [x] Add session fields for `Goal`, `Mode`, and compact metadata.
- [x] Add focused tests for `/goal`, `/plan`, and `/compact`.
- [x] Implement goal injection into the runtime system prompt.
- [x] Implement plan mode as a session mode that asks for a plan before edit-heavy work.
- [x] Expose manual compact through TUI and REPL commands using the existing summarizer.
- [x] Verify with `go test -count=1 ./internal/agent ./internal/app ./internal/tui ./internal/repl ./internal/session`.
- [x] Commit as `feat: add goal plan and compact controls`.

### Task 3: Image Input

**Files:**
- Modify: `internal/model/model.go`
- Modify: `internal/model/openai/client.go`
- Modify: `internal/model/anthropic/client.go`
- Modify: `internal/model/deepseek/client.go`
- Modify: `internal/session/session.go`
- Modify: `internal/tui/model.go`
- Modify: `cmd/codeworld/main.go`
- Modify: `internal/run/run.go`
- Modify: provider tests under `internal/model/*`
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [x] Add `model.ContentPart` with `text` and `image` variants while preserving simple text helpers.
- [x] Add serialization tests for OpenAI-compatible image URL/base64 content.
- [x] Add serialization tests for Anthropic image content.
- [x] Add clear unsupported-image errors for providers/models that cannot accept image parts.
- [x] Add CLI `--image <path>` for `run` and TUI attachment syntax for file paths.
- [x] Persist image metadata in sessions without storing large binary blobs in `current-session.json`.
- [x] Verify with `go test -count=1 ./cmd/codeworld ./internal/model/... ./internal/session ./internal/tui ./internal/run`.
- [x] Commit as `feat: add image input support`.

### Task 4: MCP HTTP and OAuth

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/mcp/client.go`
- Create: `internal/mcp/http.go`
- Create: `internal/mcp/oauth.go`
- Modify: `internal/capability/registry.go`
- Modify: `internal/tools/mcp_tool.go`
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [x] Extend MCP config tests for `url`, bearer token env var, static/env headers, enabled tools, disabled tools, and per-tool approval mode.
- [x] Introduce an MCP client interface shared by stdio and HTTP transports.
- [x] Implement streamable HTTP JSON-RPC calls and server instructions capture.
- [x] Implement OAuth callback skeleton with token storage stubbed to local config-safe files.
- [x] Apply allow/deny tool filters before registration.
- [x] Verify with `go test -count=1 ./internal/config ./internal/mcp ./internal/capability ./internal/tools`.
- [x] Commit as `feat: add http mcp transport`.

### Task 5: Hooks and Sandbox Policy

**Files:**
- Create: `internal/hooks/hooks.go`
- Create: `internal/hooks/hooks_test.go`
- Modify: `internal/agent/agent.go`
- Modify: `internal/permissions/permissions.go`
- Modify: `internal/config/config.go`
- Modify: `internal/tui/commands.go`
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [x] Add hook config parsing tests for command hooks.
- [x] Implement hook events: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `PreCompact`, `PostCompact`, and `Stop`.
- [x] Add approval modes: `read-only`, `auto`, and `full-access`.
- [x] Add read/write root and network policy checks to permission policy.
- [x] Add `/permissions` controls for switching modes.
- [x] Verify with `go test -count=1 ./internal/hooks ./internal/agent ./internal/permissions ./internal/config ./internal/tui`.
- [x] Commit as `feat: add hooks and sandbox policy`.

### Task 6: Local Subagents

**Files:**
- Create: `internal/subagent/subagent.go`
- Create: `internal/subagent/subagent_test.go`
- Modify: `internal/agent/agent.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/model.go`
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [ ] Add subagent manager tests for starting a task, recording transcript, and listing status.
- [ ] Register a `subagent_start` tool available only when subagents are enabled.
- [ ] Run subagents with cloned runtime context and isolated session IDs.
- [ ] Add `/agent` command to list and inspect subagent threads.
- [ ] Aggregate usage back into parent session.
- [ ] Verify with `go test -count=1 ./internal/subagent ./internal/agent ./internal/tui ./internal/tools`.
- [ ] Commit as `feat: add local subagents`.

### Task 7: Final Integration and Documentation

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `Agent.md`

- [ ] Run `go test -count=1 ./...`.
- [ ] Run `go build -o /private/tmp/codeworld-build ./cmd/codeworld`.
- [ ] Run `printf '/exit\n' | go run ./cmd/codeworld tui`.
- [ ] Update both README files with all new user-visible behavior.
- [ ] Update `Agent.md` with any new verification commands and durable conventions.
- [ ] Commit as `docs: document codex parity features`.
