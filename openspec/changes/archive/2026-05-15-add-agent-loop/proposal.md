## Why

Sprint 2 已具备 provider、rollout、tool registry、具体工具和权限管理器，但还没有把它们串成可执行的 agent loop。I-21 需要让 `ub run -p` 能端到端处理模型 tool_use、执行本地工具并把 tool_result 回灌给模型。

## What Changes

- 新增 `internal/agent`，按 session 顺序执行单轮用户请求，最多 25 轮 provider/tool 循环。
- 新增 headless `ub run -p "..."`，支持 `--mode default|plan|agent-approve`，注册本地工具并用 auto-allow asker 跑通 V1。
- 修改 provider runtime，使请求可携带工具 schema，并把 provider tool_call 转换为内部事件。
- 修改 Anthropic 与 OpenAI/OpenAI-compatible provider，支持工具定义、tool_use/tool_result 消息转换和 tool_call 事件。
- 修改 rollout 事件，记录 tool_result 事件以便后续 resume/TUI 展示。
- `ub chat` 保持裸聊天语义，遇到 tool_call 仍返回可读错误。

## Capabilities

### New Capabilities

- `agent-loop`: headless agent loop、tool dispatch、permission 集成、`ub run -p` 行为。

### Modified Capabilities

- `provider-runtime`: provider request 增加 tools，事件流与 fake provider 支持 agent loop 所需 tool_call。
- `anthropic-provider`: Anthropic provider 支持工具定义、tool_use/tool_result 消息和 tool_call 事件。
- `openai-provider`: OpenAI provider 支持工具定义、tool_use/tool_result 消息和 tool_call 事件。
- `compat-provider`: OpenAI-compatible provider 复用 OpenAI tool call 支持。
- `rollout-events`: rollout 增加 tool_result 事件，agent loop 执行工具后必须写入。

## Impact

- 新增 `internal/agent/` 包与相关单测。
- 修改 `internal/provider/`、`internal/provider/fake/`、`internal/provider/anthropic/`、`internal/provider/openai/`、`internal/provider/compat/`。
- 修改 `internal/rollout/` 事件类型与 helper。
- 修改 `internal/cli/` 增加 `run` 真实实现和 `--mode` 参数。
- 验证包含 fake provider agent loop 单测、plan 模式写工具拒绝测试、provider 转换测试、`go test ./...`、`make lint`、`make build`、`openspec validate add-agent-loop --strict`。
