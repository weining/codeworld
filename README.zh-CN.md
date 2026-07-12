# Codeworld

Codeworld 是一个用 Go 实现的本地 coding agent。它的目标是作为一个小而可读的基础版本，逐步演进到接近 Codex 或 Claude Code 的终端开发助手。

当前版本已经具备：

- 默认进入接近 Codex CLI 风格的 Bubble Tea TUI；
- 默认使用 DeepSeek，并支持 OpenAI-compatible、Anthropic 与 Codex OAuth provider；
- OpenAI-compatible provider 的流式输出；
- 面向 workspace 的读写、patch、shell、git、context 和原生 web search 工具；
- project skills、plugin tools、Codex plugin bundle 基础兼容、stdio/HTTP MCP tools、hooks 和本地子代理；
- 面向脚本的 JSONL 执行、结构化 JSON 输出校验和本地 Git review；
- 支持增量输出、stdin、resize 和终止的 PTY 后台命令会话；
- session 持久化、图片输入、token 统计、模型调用日志；
- `AGENTS.md` / `AGENTS.override.md` 项目指令加载；
- skills progressive disclosure：默认只注入 skill 索引，需要完整内容时通过 `skill_open` 工具读取。

## 当前状态

Codeworld 已经可以作为本地 coding agent 使用，但还不是完整 Codex 替代品。当前重点是接近 Codex CLI 风格的本地 TUI、模型调用、workspace 工具、原生 web search、权限确认、skills/plugins/MCP、hooks、本地子代理和确定性的上下文系统。

尚未实现或仍明显简化的 Codex 类能力包括：cloud tasks、IDE 集成、浏览器控制、Computer Use、托管 GitHub PR review 和图片生成等。

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

Codeworld 会依次加载用户配置 `$CODEWORLD_HOME/config.toml`（默认
`~/.codeworld/config.toml`）、项目配置 `.codeworld/config.toml` 和可选的
`$CODEWORLD_HOME/profiles/<name>.toml`。项目配置覆盖用户配置，显式选择的
profile 再覆盖前两者。API-key provider 从环境变量读取 key；Codex OAuth
使用本地登录文件。

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
summary_max_tokens = 65536
index_max_file_bytes = 262144
model_call_logging = false
sandbox_mode = "workspace-write"
sandbox_network = false
subagent_max_concurrent = 4

[[subagent_roles]]
name = "reviewer"
description = "检查修改中的回归"
instructions = "优先检查正确性、安全性和缺失测试。"
```

可以通过全局参数或环境变量选择 profile：

```bash
codeworld --profile fast
codeworld --profile review exec "inspect this repository"
export CODEWORLD_PROFILE=fast
```

项目配置不能启用 approval `full-access`、sandbox `danger-full-access` 或网络访问；
这些信任扩展只能来自用户/profile 配置或显式的 `exec` 参数。

配置 stdio MCP server：

```toml
[[mcp_servers]]
name = "demo"
command = "node"
args = ["server.js"]
```

通过 `codeworld mcp add` 添加的 server 会原子写入 `$CODEWORLD_HOME/mcp.toml`。
这一用户级托管层会覆盖用户 `config.toml` 中的同名项；项目配置和显式选择的
profile 仍可继续覆盖它。

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

Codex provider 会优先使用显式的 `HTTPS_PROXY`/`HTTP_PROXY` 环境变量；macOS
未设置这些变量时，会通过 `scutil --proxy` 读取当前系统 HTTPS 代理，使终端请求
与已启用代理的桌面流量保持一致。

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
codeworld exec --json "summarize the repository"
printf 'inspect this repository' | codeworld exec --ephemeral -
codeworld exec --output-schema schema.json -o result.json "extract project metadata"
codeworld exec --approval-mode read-only "inspect without changing files"
codeworld exec resume --last "继续执行下一项任务"
codeworld review
codeworld review --base main
codeworld review --commit HEAD
codeworld mcp add docs --url https://example.com/mcp
codeworld mcp add local -- node server.js --stdio
codeworld mcp list --json
codeworld mcp login docs --scopes tools.read,tools.write
codeworld mcp logout docs
codeworld mcp remove local
codeworld plugin marketplace add ./my-marketplace
codeworld plugin list --available --json
codeworld plugin add reviewer@my-marketplace
codeworld features list
codeworld features enable plugins
codeworld debug models
codeworld debug prompt-input "检查这个 workspace"
codeworld sessions
codeworld doctor
codeworld doctor --json
codeworld completion zsh > ~/.zfunc/_codeworld
codeworld auth codex status
codeworld auth codex logout
codeworld login status
codeworld logout
codeworld --version
codeworld sandbox -- go test ./...
codeworld sandbox --sandbox read-only --no-network -- git status
codeworld -C ../another-project -m gpt-5 exec "检查这个 workspace"
codeworld -s read-only -a read-only --no-network repl
codeworld --search exec "检查最新的上游文档"
codeworld -c 'model="gpt-5"' -c max_steps=30 e "检查这个 workspace"
codeworld exec review --uncommitted --title "工作区变更"
codeworld fork --last
codeworld archive <session-id>
codeworld unarchive <session-id>
codeworld delete <session-id>
codeworld index
```

命令行为：

- `codeworld`：连接到终端时默认启动 TUI；
- `codeworld tui`：显式启动 TUI；
- `codeworld auth codex login`：使用 ChatGPT/Codex OAuth 登录，供 `provider = "codex"` 使用；
- `codeworld resume --last`：恢复最新的活动 session；
- `codeworld resume <session-id>`：恢复指定活动 session；
- `codeworld repl`：启动旧的行式 REPL；
- `codeworld run <task>`：执行一次非交互任务后退出；
- `codeworld run --image <path> <task>`：给非交互任务附加图片；
- `codeworld exec <task|->`：执行面向脚本的任务；使用 `-` 从 stdin 读取提示词；
- `codeworld exec --json`：以 JSONL 输出生命周期、用量以及分类后的 command/file/MCP/web 事件；
- `codeworld exec --ephemeral`：不加载也不保存当前 session；
- `codeworld exec resume <session-id|--last> <task|->`：以非交互方式继续已保存 session；
- `codeworld exec --approval-mode <auto|read-only|full-access>`：覆盖本次确认策略，与 OS 沙箱相互独立；
- `codeworld exec --sandbox <read-only|workspace-write|danger-full-access>`：覆盖文件系统沙箱；`--network` 会开放受限进程和 `web_search` 的网络访问；
- `codeworld exec --output-schema <path>`：要求并校验结构化 JSON 输出。当前校验器支持 `type`、`properties`、`required`、`items`、`enum` 和 `additionalProperties`；
- `codeworld exec -o <path>`：额外把最终消息写入文件；
- `codeworld review`：以只读模式审查 staged 和 unstaged 修改；
- `codeworld review --base <branch>`、`--commit <sha>`：审查指定 Git 变更集；
- `codeworld mcp list|get|add|remove|login|logout`：管理用户级 stdio 和 HTTP MCP servers，包括带 PKCE 和 refresh token 的 OAuth 登录；
- `codeworld plugin list|add|remove`：从本地或 Git marketplace 安装 Codex-compatible plugins；`plugin marketplace add|list|upgrade|remove` 管理 marketplace 快照；
- `codeworld features list|enable|disable`：查看能力阶段并持久化受支持的用户 feature；全局 `--enable/--disable` 提供仅当前进程生效的覆盖；
- `codeworld debug models|prompt-input`：输出当前模型选择，或在不启动 provider、hooks 和 MCP 的前提下渲染模型实际可见的 prompt input；
- `codeworld sessions [--archived]`：列出活动或归档 session；
- `codeworld doctor [--json]`：以只读方式检查配置、凭据、Git workspace、进程沙箱后端、MCP 声明、sessions 和终端信息，不会连接模型；
- `codeworld completion <bash|zsh|fish|powershell>`：输出对应 shell 的补全脚本；
- `codeworld auth codex status [--json]`：查看 workspace 本地 OAuth 账号和过期时间且不暴露 token；`logout` 会幂等删除凭据；
- 与 Codex 兼容的顶层 `login`、`login status`、`logout` 会复用现有 workspace OAuth 生命周期；`--version`/`-V` 输出构建版本信息；
- `codeworld sandbox [options] -- <command>`：使用 agent 工具相同的 OS 沙箱策略运行命令，支持 `-C`、`--sandbox`、`--network` 和 `--no-network`；
- 全局 `-C`/`--cd` 可在不改变父 shell 目录的情况下选择 workspace；`-m`/`--model` 会为本次调用覆盖配置或已恢复 session 的模型；
- 全局 `-s`/`--sandbox`、`-a`/`--approval-mode` 和网络参数会在 TUI、REPL 或自动化启动前覆盖策略；`--search` 会开启沙箱网络和现有原生 web search。exec 的子命令参数优先，review 始终保持只读；
- 可重复的 `-c`/`--config key=value` 会在配置文件、profile 和环境变量之后覆盖 Codeworld 已支持的标量设置；未知键会明确报错。`--strict-config` 用于兼容 Codex CLI，而 Codeworld 默认已经严格解析配置；
- `e` 是 `exec` 的别名，`exec review` 复用顶层 review；review 除 base/commit 外也支持 `--uncommitted` 和 `--title`；
- exec 支持在子命令后使用 Codex 风格的 `-m/-p/-C/-s/-a/-i/-c`；未提供 prompt 时读取管道 stdin，同时存在时会把 stdin 追加为 `<stdin>` 块。`--ignore-user-config`、`--ignore-rules`、`--skip-git-repo-check` 和 `--color` 也会按明确的 Codeworld 语义接受；
- `codeworld fork <session-id|--last>`：复制会话历史并进入新的交互 session；
- `codeworld resume` 或 `codeworld fork` 不带 session 参数时会显示编号选择器；`--last` 保持非交互，后续文本会作为恢复或 fork 后的首条 prompt 自动提交；
- `codeworld sessions rename <id|--last> <name>` 或 TUI `/rename <name>` 可设置唯一 session 名称；resume、fork、archive、unarchive 和 delete 同时接受名称或 ID；
- `codeworld archive`、`unarchive`、`delete`：管理已保存 session 的生命周期；
- `codeworld index`：刷新 `.codeworld/index.json`。

从源码运行：

```bash
go run ./cmd/codeworld
go run ./cmd/codeworld exec --ephemeral "list the Go packages"
```

## TUI 命令

TUI 使用响应式终端布局：顶部紧凑双行状态栏、空会话欢迎卡片、视觉上明确区分的对话/工具/权限区块，以及会显示运行、附件和授权状态的底部输入区。

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
/agents send <id> <message>
               追加指令并恢复或重启任务
/agents interrupt <id>
               中断运行中任务，之后可以恢复
/agents terminate <id>
               永久终止任务
/theme <mode>  设置 system、dark 或 light 状态
/resume        查看最近可恢复 sessions
/goal <text>   设置或查看当前任务目标
/goal clear    清除当前任务目标
/rename <name> 为当前 session 设置可复用名称
/plan          开启当前 session 的 plan mode
/plan off      回到默认模式
/interrupt     中断当前回合但不退出
/compact       手动摘要较早的对话历史
/image <path>  给下一条 prompt 附加图片
/repl          显示如何切换到 REPL
/clear         清空 session 消息、权限和 token 计数
/exit          退出
```

回合运行期间，Enter 会把输入加入 follow-up 队列，`Ctrl+J`/`Alt+Enter` 仍用于
换行，`Ctrl+X` 会中断当前回合但不退出；队列中的消息会在当前回合结束后自动继续。

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

兼容 loader 支持 bundled skills 和 MCP server 声明，也支持 Codex 风格的 marketplace 生命周期：

```sh
codeworld plugin marketplace add ./my-marketplace
codeworld plugin marketplace add owner/repo --ref main
codeworld plugin list --available --json
codeworld plugin add reviewer@my-marketplace
codeworld plugin remove reviewer@my-marketplace
codeworld plugin marketplace upgrade
```

marketplace 从 `plugins/<name>` 发现 bundle，每个目录必须包含 `.codex-plugin/plugin.json`。hooks、apps 和 UI assets 会保留在缓存中，但 Codeworld 暂不执行或渲染它们。

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

对于支持 OAuth 的 HTTP server，`codeworld mcp login <name>` 会发现 protected-resource 与 authorization-server metadata，动态注册 public client，打开 PKCE 授权流程并以受限权限保存 workspace-local token。运行时会自动刷新即将过期的 token；`codeworld mcp logout <name>` 删除凭据。

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

模型可以通过 `subagent_start` 启动独立的只读调查任务，通过 `subagent_status` 查看状态，并使用 `subagent_send`、`subagent_interrupt` 和 `subagent_terminate` 控制生命周期。启动时可以选择配置角色；manager 会执行 `subagent_max_concurrent` 并发限制。interrupted 任务可以追加消息恢复，terminated 任务不可恢复。

子代理任务以 JSON 文件保存在 `.codeworld/subagents/`。每个任务会记录 prompt、角色、follow-up 消息、状态、最终结果、时间戳，以及只包含 role/content 文本的精简 transcript。

## Web Search

原生 `web_search` 工具通过 DuckDuckGo Lite 搜索网页，不需要额外的搜索 API key。参数包括必填的 `query` 和可选的 `limit`，返回带标题、URL 和摘要的编号结果。

因为它会访问网络，`web_search` 的权限声明是 read action + network risk。TUI 和 REPL 的权限流程会在请求发出前要求确认。

## 图片输入

OpenAI-compatible 和 Anthropic provider 可以接收图片 content parts。TUI 中先输入 `/image <path>`，下一条普通 prompt 会携带该图片。非交互模式可以使用：

```bash
codeworld run --image screenshot.png "explain this screenshot"
```

DeepSeek 文本模型和 Codex OAuth provider 当前不支持图片输入；附加图片时会返回明确的 unsupported-image 错误。恢复 session 时，本地图片会从原始路径重新加载。

## 权限

每个工具都会声明权限请求，包括 action、target、risk 和 reason。默认 runtime 使用 auto policy：结构化的 workspace 读写会自动允许，shell、MCP、plugin 执行以及破坏性或网络操作会要求确认。

TUI 会内联展示权限请求，并支持：

- 允许一次；
- 拒绝；
- 对类似 shell 命令在当前 session 内批准。

权限模式：

- `auto`：普通 workspace 操作自动允许，高风险操作询问；
- `read-only`：读操作自动允许，写入、patch、命令执行前询问；
- `full-access`：仅在当前进程内允许 workspace 和网络操作。

可以在 `.codeworld/config.toml` 中设置默认模式：

```toml
approval_mode = "auto"
```

项目配置只接受 `auto` 和 `read-only`；确实需要时请在交互界面临时选择 `full-access`。

## 进程沙箱

模型触发的普通 shell、后台/PTY、plugin 和 hook 命令默认运行在 OS 沙箱中。
`workspace-write` 将宿主文件系统设为只读，仅允许写 workspace；`read-only`
还会移除结构化 write/patch/index 工具；`danger-full-access` 会显式绕过 OS 沙箱。
网络默认关闭，并决定是否注册原生 `web_search` 工具。

```toml
sandbox_mode = "workspace-write"
sandbox_network = false
```

macOS 使用系统自带的 `sandbox-exec`；Linux 需要安装 Bubblewrap 的 `bwrap`。
后端缺失或宿主禁止嵌套沙箱时，受限模式会明确失败，不会静默降级。
Windows 会明确报告暂不支持。已配置的 MCP server 作为可信扩展，启动时仍单独确认；
provider API 流量不经过命令沙箱。

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

每条唯一的 workspace hook 命令都会在 `SessionStart` 前要求显式批准；选择 session 批准会在当前进程内记住该精确命令。重启后不会把 workspace session 文件中的批准当作可信输入。未配置 timeout 时默认限制为 60 秒。

## 日志和状态

workspace 状态保存在 `.codeworld/`：

```text
.codeworld/current-session.json
.codeworld/sessions/
.codeworld/index.json
.codeworld/logs/model-calls.jsonl
```

模型调用日志默认关闭。设置 `model_call_logging = true` 后会记录 compact
`body_json` 并隐藏 API key，但提示词和工具结果仍可能包含 workspace 敏感信息。

## 限制

相比 Codex，目前仍缺：

- browser/computer-use；
- subagents 还是本地只读调查任务，不是 cloud tasks；
- Unix 平台支持 PTY session；Windows 会明确返回 PTY unsupported，不会静默退回管道；
- 权限策略仍是进程内控制，尚无 OS 级文件系统/网络沙箱；
- 图片生成；
- cloud tasks 和托管 GitHub PR review 集成；
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
