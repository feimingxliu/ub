## ADDED Requirements

### Requirement: TUI Agent 流式响应

TUI SHALL 在用户发送非空输入后调用 Agent，并把 Agent 文本增量追加到当前 assistant 消息。Agent 完成前，TUI MUST 标记当前 turn 为运行中；完成后 MUST 标记为空闲。

#### Scenario: 流式文本追加

- **GIVEN** TUI runner 依次发送 `DeltaText("he")`、`DeltaText("llo")`、`Done`
- **WHEN** 用户发送一条消息
- **THEN** 消息列表 MUST 包含用户消息和 assistant 消息 `hello`
- **THEN** Done 后状态栏 MUST 显示空闲状态

#### Scenario: 运行中禁止重复发送

- **GIVEN** 当前 Agent turn 尚未完成
- **WHEN** 用户再次按 Enter
- **THEN** TUI MUST NOT 启动第二个 Agent turn

### Requirement: TUI 工具调用状态

TUI SHALL 在收到工具调用开始和结束事件时，把工具状态追加到消息列表，便于用户知道 Agent 正在执行的动作。

#### Scenario: 工具调用状态渲染

- **GIVEN** TUI 收到 `ToolCallStart(read)` 后又收到 `ToolCallEnd(read)`
- **WHEN** TUI 渲染消息列表
- **THEN** 输出 MUST 包含 `tool read started` 和 `tool read finished`

## MODIFIED Requirements

### Requirement: 基础聊天布局

TUI SHALL 渲染消息列表、输入框和状态栏三个区域。状态栏 MUST 至少显示当前模型、执行模式、工作目录、turn 序号和运行状态。

#### Scenario: 初始界面显示基础区域

- **WHEN** TUI model 初始化并渲染
- **THEN** 输出包含输入提示、状态栏信息和空消息列表占位

#### Scenario: 状态栏显示 turn

- **WHEN** TUI 完成一次用户发送
- **THEN** 状态栏 MUST 显示当前 turn 序号
