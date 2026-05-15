## Why

I-05 需要在接入 provider、fake provider、rollout 和 context manager 之前定义一套 SDK 无关的内部消息结构。先把消息模型稳定下来，可以避免后续代码直接耦合 Anthropic、OpenAI 或 Ollama 的请求类型。

## What Changes

- 新增 `internal/message/` 包，定义 `Role`、`Message`、`ContentBlock` 等中性消息类型。
- 支持文本、图片、`tool_use`、`tool_result` 四类 content block，并保留 JSON 序列化形态用于后续 rollout 存储。
- 提供便捷构造与工具方法：`Text()`、`Append(content)`、`Clone()`。
- 通过单元测试覆盖 JSON 往返、omitempty 行为、文本提取、append 语义和深拷贝。
- 不实现任一 provider 的请求/响应转换，不实现 rollout 写入，也不引入真实工具调用语义。

## Capabilities

### New Capabilities

- `message-model`: SDK 无关的内部 message/role/content block 表示、JSON 序列化和消息工具方法。

### Modified Capabilities

- 无。

## Impact

- 新增 `internal/message/` 生产代码与单元测试。
- 后续 I-07+ provider、I-09 rollout、I-24 agent loop、I-29 context compression 将依赖该包。
- 不新增外部依赖，不修改现有 CLI、store、config 行为。
