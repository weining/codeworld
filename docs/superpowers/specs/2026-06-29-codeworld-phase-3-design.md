# Codeworld Phase 3 Design

Date: 2026-06-29

## Goal

Phase 3 moves Codeworld from a functional local coding agent toward a Codex/Claude Code style daily driver. The requested scope is:

1. streaming model output;
2. support for skills, MCP, and plugins under a unified extension layer;
3. richer TUI experience;
4. a smarter context system;
5. make the TUI the default entry point.

The phase should preserve working Phase 2 behavior: `codeworld run <task>`, provider selection, session persistence, plugin manifests, token accounting, model-call logs, indexing, summarization, and permission safety.

## Non-Goals

This phase does not attempt exact feature parity with Codex or Claude Code. It does not add cloud delegation, IDE extensions, browser UI, vector embeddings, remote MCP over HTTP, image inputs, or full markdown syntax highlighting. Those should remain later phases.

## Architecture

The main architectural change is a unified event stream. Today `agent.Runner.RunTurn` returns only a final `TurnResult`; the TUI can only update after the model finishes. Phase 3 introduces event-producing execution:

```text
model stream events -> agent turn events -> TUI transcript/status
                                  -> run mode progress
                                  -> session/usage persistence
```

The existing blocking APIs stay as compatibility wrappers:

- `model.Client.Generate` remains valid.
- `agent.Runner.RunTurn` remains valid.
- TUI and run mode move to `RunTurnStream` when available.

## Streaming Model Output

Add `model.StreamClient` as an optional interface:

```go
type StreamClient interface {
    Stream(ctx context.Context, req GenerateRequest, emit func(StreamEvent) error) error
}
```

`StreamEvent` includes:

- assistant text delta;
- final assistant message;
- tool call deltas or final tool calls;
- usage;
- provider raw error.

The first implementation supports streaming for OpenAI-compatible providers:

- DeepSeek;
- OpenAI;
- local OpenAI-compatible endpoints.

Anthropic can initially fall back to non-streaming. A later task can map Anthropic Messages streaming into the same `StreamEvent`.

Provider logs should continue to write compact `body_json` request/response records without logging API keys. Streaming logs may record the original request `body_json` and a compact reconstructed final response body.

## Agent Event Stream

Add agent turn events:

- `assistant_delta`;
- `assistant_done`;
- `tool_start`;
- `tool_done`;
- `tool_error`;
- `permission_request`;
- `permission_result`;
- `usage`;
- `error`.

`RunTurnStream(ctx, history, input, emit)` should:

1. build the same system/history/user messages as `RunTurn`;
2. call `Stream` when the model supports it;
3. fall back to `Generate` otherwise;
4. execute tool calls exactly as today;
5. emit tool and permission events;
6. return the final `TurnResult`.

The agent should preserve deterministic message history. Streaming deltas are display events, not separate stored messages.

## Skills, MCP, And Plugins

Phase 2 plugin manifests remain supported. Phase 3 adds a unified capability layer:

```text
internal/capability
  loads plugin tools
  loads skill instructions
  loads MCP stdio tools/resources/prompts
  returns tool registrations and context injections
```

### Skills

Skills are local instruction bundles:

```text
.codeworld/skills/<skill-name>/SKILL.md
```

The initial skill loader:

- parses frontmatter-ish `name` and `description` when present;
- loads the full `SKILL.md`;
- exposes `/skills` in TUI to list available skills;
- injects selected or configured skill text into system context.

The first version can support auto-loading project skills only. Global skill discovery can be added after the project path works.

### MCP

The initial MCP implementation is a stdio JSON-RPC client. It supports:

- initialize;
- tools/list;
- tools/call;
- resources/list;
- resources/read;
- prompts/list.

Configuration lives in `.codeworld/config.toml` with a simple shape:

```toml
[[mcp_servers]]
name = "example"
command = "node"
args = ["server.js"]
```

MCP tools are namespaced as `mcp.<server>.<tool>`. MCP resources and prompts become context sources, not model tools in the first version.

### Plugins

Existing plugin manifests continue to work. The capability layer should call the existing plugin loader rather than replacing manifest semantics.

## TUI Advanced Experience

The TUI becomes the primary interaction surface.

Required improvements:

- stream assistant text deltas into the current assistant transcript item;
- show tool events as structured blocks;
- show permission prompts inline and keep keyboard choices `y`, `n`, `a`;
- add scroll controls;
- compact status bar with provider/model/tokens/workspace/git state;
- add slash commands:
  - `/repl`;
  - `/permissions`;
  - `/mcp`;
  - `/skills`;
  - `/context`;
  - `/theme` minimal light/dark/system selection;
  - keep `/help`, `/model`, `/status`, `/diff`, `/clear`, `/exit`.

The first advanced TUI should favor stability over decoration. Code and diff highlighting can be lightweight plain formatting.

## Smarter Context System

Replace the current flat index summary with a context graph package:

```text
internal/context/graph
```

The graph stores:

- files and metadata;
- symbols extracted from Go files;
- git changed files;
- recent session mentions;
- summaries;
- selected skill context;
- MCP resource snippets.

Add tools:

- `context_refresh`;
- `context_search`;
- `context_open`.

The first version uses deterministic lexical search and simple Go symbol extraction. It does not use embeddings. This keeps behavior inspectable and testable.

## Default Entry Point

Change command behavior:

- `codeworld` starts TUI when stdin/stdout are terminals;
- `codeworld repl` starts the old line REPL;
- `codeworld tui` remains explicit;
- `codeworld run <task>` remains non-interactive;
- non-terminal `codeworld` falls back to REPL or returns a clear error if it cannot read input.

This preserves script compatibility through `run` and gives humans the richer default.

## Error Handling

- Streaming failure should emit an error event and return the turn error.
- If a provider does not support streaming, fallback to non-streaming without failing.
- MCP server startup failure should be reported in `/mcp` and not prevent the core agent from starting unless configured as required later.
- Skill parse failure should produce a warning and skip that skill.
- Context graph refresh failure should not discard the previous graph.

## Testing

Use TDD for each subsystem:

- model stream parser tests for SSE chunks;
- agent stream fallback tests;
- TUI update tests for assistant deltas and permission events;
- skill loader tests with temp project skills;
- MCP stdio client tests with a fake in-process JSON-RPC server command;
- capability registry tests for plugin + skill + MCP composition;
- context graph tests for file metadata, Go symbols, git changed files, and lexical search;
- CLI dispatch tests for default TUI and `repl` fallback.

Full verification remains:

```bash
mise exec -- go test -count=1 ./...
mise exec -- go build -o /private/tmp/codeworld-build ./cmd/codeworld
```
