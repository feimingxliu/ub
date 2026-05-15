## ADDED Requirements

### Requirement: Slash 命令解析

系统 SHALL 在 TUI 中识别以 `/` 开头的输入，并解析为 slash command。支持的命令 MUST 至少包含 `model`、`mode`、`clear`、`sessions`、`help`、`quit`、`config`、`profile`。未知命令 MUST 返回可读错误，不得发送给 Agent。

#### Scenario: 解析 model 命令

- **WHEN** 用户输入 `/model openai/gpt-4o-mini`
- **THEN** parser MUST 返回命令名 `model` 和参数 `openai/gpt-4o-mini`

#### Scenario: 未知命令

- **WHEN** 用户输入 `/unknown`
- **THEN** TUI MUST 显示未知命令错误，且不调用 Agent

### Requirement: 本地命令执行

TUI SHALL 在本地执行 slash command。`/clear` MUST 清空消息列表；`/help` MUST 显示支持的命令；`/quit` MUST 退出 TUI。

#### Scenario: clear 清空消息

- **GIVEN** 消息列表已有内容
- **WHEN** 用户输入 `/clear`
- **THEN** 消息列表 MUST 被清空

#### Scenario: quit 退出

- **WHEN** 用户输入 `/quit`
- **THEN** TUI MUST 返回退出命令

### Requirement: 运行时状态命令

`/model <id>` 和 `/mode <mode>` SHALL 更新 TUI 状态栏，并在 runner 支持时同步到后续 Agent turn。`/config`、`/sessions`、`/profile` SHALL 输出当前状态或明确操作提示。

#### Scenario: model 切换

- **WHEN** 用户输入 `/model fake/next`
- **THEN** 状态栏 MUST 显示 `fake/next`
- **THEN** 后续 Agent turn MUST 使用该 model

#### Scenario: mode 切换

- **WHEN** 用户输入 `/mode plan`
- **THEN** 状态栏 MUST 显示 `plan`
- **THEN** 后续 Agent turn MUST 使用 plan execution mode
