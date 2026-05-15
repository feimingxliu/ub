## ADDED Requirements

### Requirement: Provider reasoning 事件

Provider runtime SHALL 支持可选的模型 reasoning/thinking 事件类型，用于传递 provider 明确返回且允许展示的思考摘要或推理片段。Provider adapter MUST NOT 为不支持该能力的响应伪造 reasoning 内容；不支持时仅保持现有 text/tool/usage/done 事件行为。fake provider MUST 支持脚本化 reasoning 事件，便于离线测试 Agent 和 TUI 活动流。

#### Scenario: 支持 reasoning 的 provider 返回思考事件

- **GIVEN** provider API 响应中包含可展示的 reasoning/thinking delta
- **WHEN** 调用方读取 provider stream
- **THEN** stream MUST 返回 reasoning 事件，并保留该 delta 文本
- **THEN** 普通 assistant 正文仍 MUST 通过 text_delta 事件返回

#### Scenario: OpenAI-compatible reasoning_content

- **GIVEN** Chat Completions streaming delta 包含 `reasoning_content`、`reasoning` 或 `thinking` 字段
- **WHEN** OpenAI 或 OpenAI-compatible adapter 读取该 delta
- **THEN** stream MUST 返回 reasoning 事件
- **THEN** 该字段 MUST NOT 混入普通 text_delta

#### Scenario: provider 未返回 reasoning 时不伪造

- **GIVEN** provider API 响应只包含普通 assistant 文本
- **WHEN** 调用方读取 provider stream
- **THEN** stream MUST NOT 生成 reasoning 事件
- **THEN** 现有 text_delta、usage、done 行为 MUST 保持不变

#### Scenario: fake provider 脚本化 reasoning

- **GIVEN** fake provider 脚本包含 reasoning 事件和 text_delta 事件
- **WHEN** 测试读取 fake provider stream
- **THEN** stream MUST 按脚本顺序返回 reasoning 事件和 text_delta 事件
