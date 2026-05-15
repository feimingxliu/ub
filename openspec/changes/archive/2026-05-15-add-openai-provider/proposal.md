## Why

Sprint 1 需要第二个真实 provider，验证 provider 抽象不只适配 Anthropic。OpenAI provider 也是后续 openai-compat、profiles.dev 和本地 vLLM 测试路径的基础。

## What Changes

- 新增 `internal/provider/openai`，使用 OpenAI 官方 Go SDK。
- 支持 `type: openai` provider 工厂注册。
- 支持 `api_key`、`base_url`、`headers`、`timeout`。
- 实现文本消息转换、非流式和流式 ChatCompletion。
- 让 `ub chat --provider openai --model ...` 可用。

## Capabilities

### New Capabilities

- `openai-provider`: OpenAI provider 的配置、消息转换、流式/非流式调用和 CLI 行为。

### Modified Capabilities

- `provider-runtime`: provider 工厂支持 `openai` 类型，`ub chat` 可消费 OpenAI provider 事件流。

## Impact

- 新增 OpenAI 官方 SDK 依赖。
- 新增 `internal/provider/openai/` 包和测试。
- 修改 CLI provider 注册导入。
- 不实现 tool use、Responses API 或 reasoning content。
