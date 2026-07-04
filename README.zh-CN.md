# Codeworld

Codeworld 是一个用 Go 实现的本地 coding agent。它的目标是作为一个小而可读的基础版本，逐步演进到接近 Codex 或 Claude Code 的终端开发助手。

当前版本已经具备：

- 默认进入接近 Codex CLI 风格的 Bubble Tea TUI；
- 默认使用 DeepSeek，并支持 OpenAI-compatible、Anthropic 与 Codex OAuth provider；
- OpenAI-compatible provider 的流式输出；
- 面向 workspace 的读写、patch、shell、git、context 和原生 web search 工具；
- project skills、plugin tools、Codex plugin bundle 基础兼容、stdio/HTTP MCP tools、hooks 和本地子代理；
- session 持久化、图片输入、token 统计、模型调用日志；
- `AGENTS.md` / `AGENTS.override.md` 项目指令加载；
- skills progressive disclosure：默认只注入 skill 索引，需要完整内容时通过 `skill_open` 工具读取。

## 当前状态

Codeworld 已经可以作为本地 coding agent 使用，但还不是完整 Codex 替代品。当前重点是接近 Codex CLI 风格的本地 TUI、模型调用、workspace 工具、原生 web search、权限确认、skills/plugins/MCP、hooks、本地子代理和确定性的上下文系统。

尚未实现或仍明显简化的 Codex 类能力包括：cloud tasks、IDE 集成、浏览器控制、Computer Use、GitHub PR review、图片生成，以及更完整的 MCP OAuth 体验等。

## 安装

需要 Go 1.26.4 或更新版本。

```bash
go test ./...
go install ./cmd/codeworld
```

确保 Go bin 目录在 `PATH` 中：

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

## 配置

Codeworld 从 workspace root 读取 `.codeworld/config.toml`。API-key provider 从环境变量读取 key；Codex OAuth 使用本地登录文件。

```bash
export DEEPSEEK_API_KEY="sk-..."
```

最小配置示例：

```toml
provider = "deepseek"
model = "deepseek-v4-pro"
max_steps = 20
plugins_enabled = true
summary_max_messages = 40
index_max_file_bytes = 262144
```

配置 stdio MCP server：

```toml
[[mcp_servers]]
name = "demo"
command = "node"
args = ["server.js"]
```

支持的 provider 环境变量：

```bash
DEEPSEEK_API_KEY=...
OPENAI_API_KEY=...
ANTHROPIC_API_KEY=...
CODEWORLD_LOCAL_BASE_URL=http://127.0.0.1:11434/v1
```

Codex OAuth 使用 OpenClaw 风格的 ChatGPT OAuth 流程：

```bash
codeworld auth codex login
```

然后配置：

```toml
provider = "codex"
model = "gpt-5"
```

该登录流程使用 `auth.openai.com`、PKCE、`localhost:1455/auth/callback`，并把凭据以 `0600` 权限保存到 `.codeworld/auth/codex.json`。运行时请求会走 ChatGPT backend 的 Codex Responses endpoint，不是公开 OpenAI API key endpoint。

## 使用

启动 TUI：

```bash
codeworld
```

显式命令：

```bash
codeworld tui
codeworld auth codex login
codeworld resume --last
codeworld resume <session-id>
codeworld repl
codeworld run "inspect the project and explain the entry points"
codeworld run --image screenshot.png "explain this screenshot"
codeworld index
```

命令行为：

- `codeworld`：连接到终端时默认启动 TUI；
- `codeworld tui`：显式启动 TUI；
- `codeworld auth codex login`：使用 ChatGPT/Codex OAuth 登录，供 `provider = "codex"` 使用；
- `codeworld resume --last`：恢复最新的本地归档 session；
- `codeworld resume <session-id>`：恢复指定归档 session；
- `codeworld repl`：启动旧的行式 REPL；
- `codeworld run <task>`：执行一次非交互任务后退出；
- `codeworld run --image <path> <task>`：给非交互任务附加图片；
- `codeworld index`：刷新 `.codeworld/index.json`。

从源码运行：

```bash
go run ./cmd/codeworld
go run ./cmd/codeworld run "list the Go packages"
```

## TUI 命令

TUI 使用接近 Codex CLI 的终端布局：顶部单行状态栏、按角色对齐的 transcript、内联工具和权限事件、带边框的底部输入区以及快捷键提示。

进入 TUI 后可以输入：

```text
/help          查看命令
/status        查看 provider、model、git 状态、消息数、权限、token 使用量
/model         查看当前模型
/model <name>  修改当前 session 的模型名
/diff          查看 git diff
/permissions   查看当前 session 已批准权限
/permissions <auto|read-only|full-access>
               切换当前权限模式
/mcp           查看已配置 MCP servers
/skills        查看已加载 skills
/context       查看上下文系统状态
/agents        查看本地子代理任务
/agents <id>   查看单个子代理任务详情
/theme <mode>  设置 system、dark 或 light 状态
/resume        查看最近可恢复 sessions
/goal <text>   设置或查看当前任务目标
/goal clear    清除当前任务目标
/plan          开启当前 session 的 plan mode
/plan off      回到默认模式
/compact       手动摘要较早的对话历史
/image <path>  给下一条 prompt 附加图片
/repl          显示如何切换到 REPL
/clear         清空 session 消息、权限和 token 计数
/exit          退出
```

输入 `/` 会显示 slash command 建议；composer 中可以用 `Up` / `Down` 恢复已提交草稿。

`/goal` 会把长期目标持久化到当前 session，并注入后续模型上下文。`/plan` 会要求 agent 在偏代码改动的任务前先给出简短计划。`/compact` 会使用当前模型手动压缩较早历史。

滚动 transcript：

```text
PageUp / PageDown
Ctrl+U / Ctrl+D
```

## 架构

核心包结构：

```text
cmd/codeworld
  CLI 入口，分发 TUI、REPL、run、index 模式

internal/app
  Runtime 装配：config、workspace、session、provider、tools、capabilities

internal/agent
  Turn loop：模型调用、流式事件、工具调用、权限确认

internal/model
  provider-neutral 模型类型和 provider client

internal/tools
  workspace、shell、git、patch、web search、plugin、MCP bridge、context 工具

internal/capability
  project skills、Codeworld plugins、Codex plugin bundles、MCP setup

internal/context
  文件索引、context graph、conversation summary

internal/tui
  Bubble Tea 终端界面
```

Runtime 会把基础 agent prompt、workspace summary、文件索引、context graph、`AGENTS.md` 指令、skill index 和 conversation summary 组合成 system prompt。

## 项目指令

Codeworld 会加载：

```text
AGENTS.override.md
AGENTS.md
```

发现流程从 git root 开始，一路走到当前 workspace 目录。同一个目录下 `AGENTS.override.md` 优先于 `AGENTS.md`；越靠近当前目录的指令越晚注入，因此更具体的指令有更高优先级。

适合写入这些文件的内容：

- 测试命令；
- 代码风格；
- review 要求；
- 包或目录级规则；
- agent 每次在该项目中都应该遵守的约定。

## Skills

项目 skills 放在：

```text
.codeworld/skills/<skill-name>/SKILL.md
```

示例：

```markdown
---
name: reviewer
description: Review Go changes carefully.
---

Inspect relevant files before editing. Run focused tests before reporting completion.
```

启动时默认只注入 skill 索引。模型需要完整说明时，可以调用 `skill_open` 读取对应 skill 的完整 `SKILL.md`。

## Plugins

Codeworld 支持两种 plugin 形态。

原生 Codeworld plugin：

```text
.codeworld/plugins/<plugin-name>/plugin.json
```

示例：

```json
{
  "name": "demo",
  "tools": [
    {
      "name": "demo.echo",
      "description": "Echo text",
      "command": "printf",
      "args": ["{{text}}"],
      "input_schema": {
        "type": "object",
        "properties": {
          "text": { "type": "string" }
        },
        "required": ["text"]
      },
      "risk": "read"
    }
  ]
}
```

开启 plugin：

```toml
plugins_enabled = true
```

Codex plugin bundle 基础兼容：

```text
.codeworld/codex-plugins/<plugin-name>/.codex-plugin/plugin.json
.codeworld/codex-plugins/<plugin-name>/skills/<skill-name>/SKILL.md
.codeworld/codex-plugins/<plugin-name>/.mcp.json
```

当前只支持 bundled skills 和 stdio MCP server 声明。尚不支持 marketplace、hooks、apps、assets 或 plugin lifecycle policy。

## MCP

Codeworld 支持 stdio 和 HTTP MCP server：

```toml
[[mcp_servers]]
name = "demo"
command = "node"
args = ["server.js"]
```

HTTP MCP 示例：

```toml
mcp_oauth_callback_port = 5555
mcp_oauth_callback_url = "http://localhost:5555/callback"

[[mcp_servers]]
name = "docs"
url = "https://mcp.example.test/mcp"
bearer_token_env_var = "DOCS_TOKEN"
http_headers = ["X-Test: yes"]
enabled_tools = ["search"]
disabled_tools = ["write"]
```

MCP tools 会注册为：

```text
mcp.<server-name>.<tool-name>
```

## Context Tools

模型可以使用这些上下文工具：

```text
context_refresh   重建 workspace context graph
context_search    搜索路径、文件内容和 Go symbols
context_open      打开 graph 中的文件，并附带 symbol 信息
```

Context graph 是本地、确定性的，不依赖 embedding。它会提取文件元数据、Go symbols、git changed 状态、skills、summary 和其他配置上下文。

## 本地子代理

模型可以通过 `subagent_start` 启动独立的只读调查任务，并通过 `subagent_status` 查看任务状态或结果。TUI 和 REPL 中也可以使用 `/agents` 与 `/agents <id>` 查看同一批任务。

子代理任务以 JSON 文件保存在 `.codeworld/subagents/`。每个任务会记录 prompt、状态、最终结果、时间戳，以及只包含 role/content 文本的精简 transcript。

## Web Search

原生 `web_search` 工具通过 DuckDuckGo Lite 搜索网页，不需要额外的搜索 API key。参数包括必填的 `query` 和可选的 `limit`，返回带标题、URL 和摘要的编号结果。

因为它会访问网络，`web_search` 的权限声明是 read action + network risk。TUI 和 REPL 的权限流程会在请求发出前要求确认。

## 图片输入

OpenAI-compatible 和 Anthropic provider 可以接收图片 content parts。TUI 中先输入 `/image <path>`，下一条普通 prompt 会携带该图片。非交互模式可以使用：

```bash
codeworld run --image screenshot.png "explain this screenshot"
```

DeepSeek 文本模型当前不支持图片输入；附加图片时会返回明确的 unsupported-image 错误。

## 权限

每个工具都会声明权限请求，包括 action、target、risk 和 reason。默认 runtime 使用 auto policy：普通 workspace 操作会自动允许，高风险操作如破坏性 shell、网络 shell 或 web search 会要求确认。

TUI 会内联展示权限请求，并支持：

- 允许一次；
- 拒绝；
- 对类似 shell 命令在当前 session 内批准。

这还不是完整 Codex sandbox。细粒度 filesystem profiles、network policy 和 hook trust 是后续工作。

权限模式：

- `auto`：普通 workspace 操作自动允许，高风险操作询问；
- `read-only`：读操作自动允许，写入、patch、命令执行前询问；
- `full-access`：workspace 和网络操作默认允许。

可以在 `.codeworld/config.toml` 中设置默认模式：

```toml
approval_mode = "auto"
```

## Hooks

Codeworld 可以从 `.codeworld/hooks.json` 读取命令型 hooks，并在 session 启动、用户提交、工具调用、压缩和退出时执行。当前支持的事件包括 `SessionStart`、`UserPromptSubmit`、`PreToolUse`、`PermissionRequest`、`PostToolUse`、`PreCompact`、`PostCompact` 和 `Stop`。

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "shell",
        "hooks": [
          { "type": "command", "command": "printf pre-tool >> .codeworld/hooks.log" }
        ]
      }
    ]
  }
}
```

## 日志和状态

workspace 状态保存在 `.codeworld/`：

```text
.codeworld/current-session.json
.codeworld/sessions/
.codeworld/index.json
.codeworld/logs/model-calls.jsonl
```

模型调用日志只记录 compact `body_json`，并会隐藏 API key。

## 限制

相比 Codex，目前仍缺：

- browser/computer-use；
- subagents 还是本地只读调查任务，不是 cloud tasks；
- MCP OAuth token refresh 和 dynamic registration 仍不完整；
- 图片生成；
- cloud tasks 和 PR review 集成；
- TUI 中的代码块/diff 高亮和 slash popup。

## 开发

运行完整测试：

```bash
go test -count=1 ./...
```

构建：

```bash
go build -o /private/tmp/codeworld-build ./cmd/codeworld
```

非交互 smoke-test TUI：

```bash
printf '/exit\n' | go run ./cmd/codeworld tui
```
