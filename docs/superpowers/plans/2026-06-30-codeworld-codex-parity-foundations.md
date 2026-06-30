# Codeworld Codex Parity Foundations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Codex-compatible foundational customization surfaces to Codeworld: `AGENTS.md` instructions, Codex plugin bundled skills/MCP, and progressive skill loading.

**Architecture:** Add focused loaders under `internal/instructions`, `internal/codexplugin`, and `internal/skill`, then compose them in `internal/capability` and `internal/app`. Keep existing `.codeworld` plugin behavior unchanged while adding Codex-compatible inputs.

**Tech Stack:** Go standard library, existing Codeworld config/capability/tool/runtime packages, TDD with `mise exec -- go test`.

---

## Task 1: Instruction Loader

**Files:**
- Create: `internal/instructions/instructions.go`
- Create: `internal/instructions/instructions_test.go`
- Modify: `internal/app/runtime.go`
- Modify: `internal/app/runtime_test.go`

- [ ] Write tests for `AGENTS.override.md` winning over `AGENTS.md` in the same directory.
- [ ] Write tests for parent-to-child ordering.
- [ ] Write tests for 32 KiB budget truncation.
- [ ] Implement `instructions.Load(root, Options)`.
- [ ] Inject loaded instructions into `app.NewRuntime` system prompt.
- [ ] Run `mise exec -- go test -count=1 ./internal/instructions ./internal/app`.
- [ ] Commit as `feat: load agents instructions`.

## Task 2: Codex Plugin Loader

**Files:**
- Create: `internal/codexplugin/plugin.go`
- Create: `internal/codexplugin/plugin_test.go`
- Modify: `internal/capability/registry.go`
- Modify: `internal/capability/registry_test.go`

- [ ] Write tests for loading `.codeworld/codex-plugins/<name>/.codex-plugin/plugin.json`.
- [ ] Write tests for bundled `skills/*/SKILL.md`.
- [ ] Write tests for `.mcp.json` stdio server declarations.
- [ ] Implement the Codex plugin compatibility loader.
- [ ] Compose plugin skills and MCP servers into capability loading.
- [ ] Run `mise exec -- go test -count=1 ./internal/codexplugin ./internal/capability`.
- [ ] Commit as `feat: load codex plugin bundles`.

## Task 3: Skill Progressive Disclosure

**Files:**
- Modify: `internal/skill/skill.go`
- Modify: `internal/skill/skill_test.go`
- Create: `internal/tools/skill_tool.go`
- Create: `internal/tools/skill_tool_test.go`
- Modify: `internal/capability/registry.go`
- Modify: `internal/app/runtime.go`
- Modify: `internal/app/runtime_test.go`

- [ ] Write tests for compact skill index output.
- [ ] Write tests that full skill bodies are not injected into the runtime system prompt.
- [ ] Write tests for `skill_open` returning full skill content by name.
- [ ] Implement `skill.Index`.
- [ ] Register `skill_open` with loaded skills in runtime.
- [ ] Run `mise exec -- go test -count=1 ./internal/skill ./internal/tools ./internal/app ./internal/tui`.
- [ ] Commit as `feat: add progressive skill loading`.

## Task 4: Full Verification And Push

**Files:**
- Review changed files.

- [ ] Run `mise exec -- go test -count=1 ./...`.
- [ ] Run `mise exec -- go build -o /private/tmp/codeworld-build ./cmd/codeworld`.
- [ ] Run `printf '/exit\n' | mise exec -- go run ./cmd/codeworld tui`.
- [ ] Run `git status --short`.
- [ ] Push commits to `main`.
