## Context

现有 provider runtime 已支持 fake 和 Anthropic。OpenAI provider 需要同时支持非流式与流式 ChatCompletion，并沿用统一配置字段。I-11 不引入 tool calling，先只处理文本消息。

## Goals / Non-Goals

**Goals:**

- 使用 OpenAI 官方 Go SDK 创建 ChatCompletion 请求。
- 支持 base_url、headers、timeout 和 api_key。
- 将内部 user/assistant/system 文本消息转换为 OpenAI chat messages。
- 将 streaming delta 转换为 provider `text_delta`，结束时返回 usage 和 done。
- 提供非流式 helper/fallback 以便测试响应转换。

**Non-Goals:**

- 不接 Responses API。
- 不实现 tool calls 或 reasoning content。
- 不处理 image/tool block；非文本 block 返回错误。

## Decisions

- **默认使用 streaming。** 与 I-10 后的 Anthropic 行为一致，CLI 能逐 delta 输出。
- **保留非流式方法用于测试和未来 fallback。** I-11 规格要求支持流式 + 非流式，但 provider `Chat` 主路径使用 streaming。
- **配置通过 SDK option 传入。** `base_url`、HTTP client timeout、headers 和 API key 都在 provider 创建时绑定。
- **系统消息作为 OpenAI system role。** OpenAI 原生支持 system message，因此不需要像 Anthropic 那样单独字段。

## Risks / Trade-offs

- **OpenAI SDK 类型较新** -> 以当前 module API 和编译测试为准。
- **usage 在 stream 中可能缺省** -> 若没有 usage，stream 仍必须返回 done。
- **非文本 block 报错** -> 后续 I-21 tool use 时再扩展。
