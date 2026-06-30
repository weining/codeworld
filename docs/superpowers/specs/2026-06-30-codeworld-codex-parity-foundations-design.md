# Codeworld Codex Parity Foundations Design

Date: 2026-06-30

## Goal

Move Codeworld closer to Codex by adding the foundational customization surfaces that other Codex-like behavior depends on:

1. `AGENTS.md` and `AGENTS.override.md` instruction discovery;
2. Codex plugin compatibility for bundled skills and MCP server declarations;
3. skill progressive disclosure so large skills do not bloat every model request.

This phase intentionally does not attempt full Codex parity. It creates stable local extension and instruction boundaries that later phases can build on.

## Non-Goals

This phase does not add:

- full permission/sandbox profile parity;
- streamable HTTP MCP or MCP OAuth;
- hook execution;
- subagents;
- TUI slash popup and prompt history;
- GitHub PR review integration;
- IDE, app, cloud, browser, image, or web search surfaces.

Those remain follow-up phases after the core customization layer is compatible enough.

## AGENTS.md Instruction Chain

Codeworld currently has `Agent.md` as documentation, but it is not loaded as runtime instruction context. This phase adds a dedicated instruction package that discovers:

- `AGENTS.override.md`;
- `AGENTS.md`;
- configurable fallback names later, but not in the first implementation.

Discovery starts at the git root when one can be found, then walks to the launch workspace root. In each directory it includes at most one file, preferring `AGENTS.override.md` over `AGENTS.md`. Files closer to the workspace root appear later in the combined prompt so they can override earlier guidance by normal prompt order.

The loader skips empty files and enforces a byte budget. The initial budget is 32 KiB, matching Codex's documented default. Runtime injects the combined text into the system prompt under `Project instructions:`.

## Codex Plugin Compatibility

Codeworld currently supports project plugins only through:

```text
.codeworld/plugins/<name>/plugin.json
```

Codex plugins use a different local package shape:

```text
<plugin>/.codex-plugin/plugin.json
<plugin>/skills/<skill>/SKILL.md
<plugin>/.mcp.json
```

This phase adds a compatibility loader under the existing capability layer. The first implementation supports:

- discovering project-local Codex plugins from `.codeworld/codex-plugins/*`;
- reading `.codex-plugin/plugin.json` for plugin identity;
- loading bundled `skills/*/SKILL.md`;
- reading optional `.mcp.json` with a simple `mcp_servers` array compatible with Codeworld's existing stdio MCP configuration.

It does not implement Codex marketplace files, plugin assets, hooks, apps, policy installation states, or plugin cachebuster behavior yet.

## Skill Progressive Disclosure

Codeworld currently injects full project skill text into every system prompt. That is simple but does not scale. This phase changes skill handling to:

1. inject only a compact skill index into the system prompt by default;
2. expose a `skill_open` tool to read a selected skill's full `SKILL.md`;
3. keep `/skills` in TUI as the human-facing list of loaded skills.

The initial skill index includes each skill name, description, and source path. The agent can call `skill_open` when it needs the full instructions. Project skills and Codex plugin bundled skills share the same `skill.Skill` type.

## Runtime Data Flow

Runtime construction becomes:

```text
config + workspace
  -> instruction loader
  -> capability loader
       -> .codeworld plugins
       -> project skills
       -> Codex plugin skills
       -> configured MCP servers
       -> plugin-provided MCP servers
  -> default tool registry + capability tools
  -> system prompt
```

System prompt sections are ordered as:

1. base agent prompt;
2. workspace file summary;
3. workspace index;
4. workspace context graph;
5. project instructions;
6. skill index;
7. conversation summary.

Conversation summary stays last because it is most specific to the current session.

## Error Handling

- Missing instruction files are ignored.
- Empty instruction files are ignored.
- Instruction budget truncation appends a clear marker.
- Invalid Codex plugin manifests return a capability loading error, because malformed extension packages can silently change behavior.
- Missing optional `.mcp.json` files are ignored.
- Plugin-provided MCP startup failures follow existing MCP behavior and fail runtime construction in this phase. A later phase can add optional/required flags.

## Testing

Use TDD for each subsystem:

- instruction loader tests for override precedence, parent-to-child order, and byte limits;
- runtime tests proving `AGENTS.md` text enters the system prompt;
- Codex plugin loader tests for bundled skills and `.mcp.json` parsing;
- capability tests proving plugin bundled skills appear in the skill list and skill index;
- tool tests for `skill_open`;
- TUI tests proving `/skills` still lists project and plugin skills.

Full verification:

```bash
mise exec -- go test -count=1 ./...
mise exec -- go build -o /private/tmp/codeworld-build ./cmd/codeworld
printf '/exit\n' | mise exec -- go run ./cmd/codeworld tui
```
