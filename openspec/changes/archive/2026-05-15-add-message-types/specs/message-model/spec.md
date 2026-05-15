## ADDED Requirements

### Requirement: 内部 Message 类型

系统 SHALL 在 `internal/message` 中提供 SDK 无关的消息类型。`Message` MUST 至少包含 `Role` 和有序的 `ContentBlock` 列表；`Role` MUST 支持 `user`、`assistant`、`system`、`tool`。

#### Scenario: 创建用户文本消息

- **WHEN** 调用消息包提供的构造函数创建用户文本消息 `hello`
- **THEN** 返回的 `Message.Role` 等于 `user`，且 `Content` 只包含一个 `text` block

#### Scenario: 角色 JSON 可读

- **WHEN** 将 role 为 `assistant` 的消息序列化为 JSON
- **THEN** JSON 中 role 字段值为字符串 `assistant`

### Requirement: ContentBlock 类型

系统 SHALL 支持 `text`、`image`、`tool_use`、`tool_result` 四类 content block。每个 block MUST 在 JSON 中包含 `type` 字段，并 MUST 只序列化该 block 实际使用的非零字段。

#### Scenario: 文本 block 序列化

- **WHEN** 将包含文本 block 的消息序列化为 JSON
- **THEN** JSON 包含 `type: "text"` 和 `text` 字段，不包含 tool 字段

#### Scenario: tool_use block 保留原始 input

- **WHEN** 创建 `tool_use` block，`Input` 为原始 JSON `{"path":"README.md"}`
- **THEN** JSON 序列化后 `input` 仍为对象而不是转义字符串

#### Scenario: tool_result 错误标记

- **WHEN** 创建错误的 `tool_result` block
- **THEN** JSON 包含 `is_error: true`，并包含对应 `tool_use_id` 和 `output`

### Requirement: JSON 往返

系统 SHALL 支持 `Message` 和 `ContentBlock` 的 JSON 序列化与反序列化往返。往返后 role、block 顺序、文本、工具调用 ID、工具名、input、output 和错误标记 MUST 保持一致。

#### Scenario: 混合 content 往返

- **WHEN** 将同时包含 `text`、`tool_use`、`tool_result` 的消息 marshal 后再 unmarshal
- **THEN** 解码出的消息与原消息等价，且 content block 顺序不变

### Requirement: Text 方法

`Message.Text()` SHALL 返回消息中所有文本 block 的文本内容。系统 MUST 忽略非文本 block；多个文本 block MUST 按原顺序用换行拼接。

#### Scenario: 单个文本 block

- **WHEN** 消息只包含一个 `text` block
- **THEN** `Text()` 返回该 block 的 `Text`

#### Scenario: 混合 block 文本提取

- **WHEN** 消息内容为 `text("a")`、`tool_use(...)`、`text("b")`
- **THEN** `Text()` 返回 `a\nb`

#### Scenario: 无文本 block

- **WHEN** 消息不包含任何 `text` block
- **THEN** `Text()` 返回空字符串

### Requirement: Append 与 Clone

系统 SHALL 提供追加 content block 和深拷贝消息的工具方法。追加方法 MUST 保留原有 block 顺序并把新 block 放在末尾；`Clone()` MUST 深拷贝 content slice 和 `json.RawMessage` 输入。

#### Scenario: Append 保留顺序

- **WHEN** 对已有一个文本 block 的消息追加一个 `tool_use` block
- **THEN** 返回消息的 content 顺序为原文本 block 后跟追加的 `tool_use` block

#### Scenario: Clone 深拷贝 content

- **WHEN** 调用 `Clone()` 后修改 clone 的第一个 content block
- **THEN** 原消息的第一个 content block 不变

#### Scenario: Clone 深拷贝 RawMessage

- **WHEN** 调用 `Clone()` 后修改 clone 中 `tool_use.Input` 的字节内容
- **THEN** 原消息中的 `tool_use.Input` 字节内容不变
