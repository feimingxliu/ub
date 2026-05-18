## Why

当前上下文窗口大小只来自 provider 级硬编码，无法表达同一个 OpenAI-compatible provider 下不同模型的实际窗口。例如用户的 `openai/glm-5.1` 实际上下文为 200k，但状态栏和自动 summary 仍按 provider 默认值计算。

## What Changes

- 在 provider model 配置中增加 `max_context_tokens`，用于覆盖该模型的最大上下文窗口。
- 让模型能力解析结果携带 `MaxContextTokens`，并在 Agent 自动 summary、手动 compact 后 context status、TUI 状态栏中优先使用模型级上下文窗口。
- 保留 provider `Caps().MaxContextTokens` 作为未配置模型级窗口时的回退。
- 更新 JSON Schema、OpenSpec 和文档，并把用户本地 `openai/glm-5.1` 配置为 `max_context_tokens: 200000`。

## Capabilities

### New Capabilities

- 无

### Modified Capabilities

- `config-loader`：provider model 配置支持 `max_context_tokens` 并体现在 schema 中。
- `context-management`：自动 summary 和上下文用量上报优先使用模型级上下文窗口。

## Impact

- 影响 `internal/config`、`internal/modelinfo`、`internal/agent` 和 CLI/TUI Agent 构造路径。
- 更新 `schema/config.schema.json`。
- 更新用户本地配置文件 `/home/lfm/.config/ub/config.yaml`。
