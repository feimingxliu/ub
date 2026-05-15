## Context

I-07 已提供 provider 抽象、fake provider 和 `ub chat`。I-08 需要接入第一个真实 provider：Anthropic Messages API 的非流式调用。当前已有 HTTP VCR，可用于后续真实 HTTP 交互录制/回放；本迭代先确保 SDK 调用与配置传播可被 httptest 或 VCR 验证。

## Goals / Non-Goals

**Goals:**

- 使用 Anthropic 官方 Go SDK 实现 `internal/provider/anthropic`。
- 支持 `api_key`、`base_url`、`headers`、`timeout` 配置。
- 将内部文本消息转换为 Anthropic Messages 请求，并将响应文本转换为 provider 事件。
- 注册 `anthropic` provider 类型，使 `ub chat --provider anthropic` 可用。

**Non-Goals:**

- 不实现 Anthropic streaming；I-10 负责。
- 不实现 tool use、vision、prompt cache 或 prompt caching headers。
- 不要求测试依赖真实 API key；真实调用只作为手测/record 路径。

## Decisions

- **使用官方 SDK 的可配置 HTTP client。** 通过自定义 `http.Client` 注入 timeout、headers 和可选 VCR transport；`base_url` 使用 SDK 选项覆盖。
- **非流式响应封装为小型 stream。** `Chat` 完成一次 SDK 调用后返回一个内存 stream，依次发出 `text_delta`、`usage`、`done`，保持与后续 streaming provider 相同消费接口。
- **只转换文本 block。** user/assistant/system 的文本 block 参与请求；非文本 block 暂时报错，避免静默丢失工具或图片语义。
- **system message 单独提取。** Anthropic Messages API 的 system 指令不作为普通 message 发送；内部 `RoleSystem` 文本合并到 system 字段。
- **工厂注册通过 blank import。** CLI 导入 provider 包时注册 `anthropic`，保持与 fake provider 一致。

## Risks / Trade-offs

- **SDK API 变化** -> 实现时以当前下载的模块 API 为准，并用编译测试固定适配层。
- **base_url 行为依赖 SDK 选项** -> 增加 httptest 覆盖，断言请求打到配置的 base URL。
- **非文本 block 报错影响未来工具调用** -> 这是有意限制；I-21 接 tool use 时再扩展转换。

## Migration Plan

新增 provider 类型不改变现有 fake provider 行为。若回滚，删除 `internal/provider/anthropic`、依赖和注册导入即可。
