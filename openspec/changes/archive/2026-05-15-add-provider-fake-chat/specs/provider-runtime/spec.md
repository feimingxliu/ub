## ADDED Requirements

### Requirement: Provider 接口与事件流

系统 SHALL 在 `internal/provider` 中提供 SDK 无关的 provider 抽象。Provider MUST 暴露名称、能力描述和 `Chat(ctx, Request) (Stream, error)`；Stream MUST 通过 `Next(ctx)` 顺序返回事件，并支持 `Close()`。

#### Scenario: Provider 返回顺序事件

- **WHEN** 调用 provider 的 `Chat` 并持续调用 `Stream.Next`
- **THEN** 系统按 provider 生成顺序返回事件，直到 `done` 事件或流结束

#### Scenario: Context 取消

- **WHEN** 调用方在读取 stream 时取消 context
- **THEN** `Next` MUST 返回 context 取消错误，且 `Close` MUST 可安全调用

### Requirement: Provider 工厂

系统 SHALL 根据配置中的 provider `type` 创建 provider 实例。工厂 MUST 支持 `fake` 类型；未知类型 MUST 返回可读错误。

#### Scenario: 创建 fake provider

- **GIVEN** 配置项 `providers.test.type=fake`
- **WHEN** 调用 provider 工厂创建 `test`
- **THEN** 返回名称为 `test` 且能力可查询的 fake provider

#### Scenario: 未知 provider 类型

- **GIVEN** 配置项 `providers.bad.type=unknown`
- **WHEN** 调用 provider 工厂创建 `bad`
- **THEN** 返回包含未知类型和 provider 名称的 error

### Requirement: Fake provider 脚本

系统 SHALL 提供 `internal/provider/fake`，可按预设脚本顺序产生 `text_delta`、`tool_call`、`usage`、`done` 和 `error` 事件。fake provider MUST 支持通过 Go 代码直接构造，也 MUST 支持从配置脚本构造。

#### Scenario: Go 代码构造脚本

- **WHEN** 测试代码用 `fake.New(fake.Script{fake.TextDelta("hi"), fake.Done()})` 构造 provider
- **THEN** 读取 stream 时依次得到文本 delta 和 done 事件

#### Scenario: 配置脚本构造

- **GIVEN** YAML 配置中 fake provider 含 `script` 列表
- **WHEN** provider 工厂创建该 provider
- **THEN** fake provider MUST 按配置列表顺序产生事件

#### Scenario: tool_call 保留输入

- **WHEN** fake 脚本包含 tool_call 事件，输入为 JSON 对象
- **THEN** provider 事件 MUST 保留工具名和原始 JSON input

### Requirement: 最小 chat 命令

系统 SHALL 提供 `ub chat` 子命令用于单轮 provider 对话。命令 MUST 支持 `ub chat "prompt"`、`ub chat -`、`--provider <name>` 和 `--model <id>`；文本 delta MUST 流式写到 stdout。

#### Scenario: 参数 prompt

- **GIVEN** 配置中存在 fake provider，脚本输出文本 `pong`
- **WHEN** 用户运行 `ub chat --provider fake "ping"`
- **THEN** stdout 包含 `pong`，命令返回成功

#### Scenario: stdin prompt

- **GIVEN** 配置中存在 fake provider
- **WHEN** 用户运行 `ub chat --provider fake -` 并从 stdin 提供 prompt
- **THEN** 命令使用 stdin 内容作为用户消息并输出 provider 文本

#### Scenario: provider 覆盖

- **GIVEN** 配置中有多个 provider
- **WHEN** 用户运行 `ub chat --provider test "hi"`
- **THEN** 命令 MUST 使用名为 `test` 的 provider，而不是默认模型推导出的 provider

#### Scenario: tool_call 暂不执行

- **GIVEN** fake provider 返回 tool_call 事件
- **WHEN** 用户运行 `ub chat`
- **THEN** 命令 MUST 返回可读错误，说明裸 chat 暂不执行工具调用
