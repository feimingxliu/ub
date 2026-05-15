## Why

I-08 需要接入第一个真实 LLM provider，让 `ub chat` 不再只能依赖 fake provider。Anthropic 非流式调用先打通配置、消息转换、VCR 测试和 CLI 手测路径，为后续流式与工具调用扩展打基础。

## What Changes

- 新增 `internal/provider/anthropic`，实现 provider 接口。
- 使用 Anthropic 官方 Go SDK 发起一次性非流式 Messages 调用。
- 支持 `api_key`、`base_url`、`headers`、`timeout` 配置项。
- 实现内部 `message.Message` 与 Anthropic message/content 的基础文本转换。
- 给 `ub chat --provider anthropic ...` 提供可用路径，并使用 VCR/httptest 覆盖 HTTP 请求行为。

## Capabilities

### New Capabilities

- `anthropic-provider`: Anthropic provider 的非流式调用、配置、消息转换和测试行为。

### Modified Capabilities

- `provider-runtime`: provider 工厂需要支持 `anthropic` 类型，并让 `ub chat` 可使用真实 provider。

## Impact

- 新增 Anthropic SDK 依赖。
- 新增 `internal/provider/anthropic/` 包及测试。
- 修改 CLI provider 注册导入，使 Anthropic provider 可由工厂创建。
- 不实现 Anthropic 流式、tool use 或多模态输入；这些保留到后续迭代。
