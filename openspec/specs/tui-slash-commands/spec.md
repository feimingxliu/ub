# tui-slash-commands Specification

## Purpose

定义 TUI 内置 slash 命令的解析和执行行为。

## Requirements

### Requirement: Slash 命令解析

系统 SHALL 在 TUI 中识别以 `/` 开头的输入，并解析为 slash command。支持的命令 MUST 至少包含 `model`、`mode`、`clear`、`new`、`sessions`、`help`、`quit`、`exit`、`config`、`profile`。未知命令 MUST 返回可读错误，不得发送给 Agent。
当输入框内容以 `/` 开头但尚未提交时，TUI SHOULD 显示匹配命令候选和每条命令的用法说明。候选存在时，上下方向键 SHOULD 移动候选选中项，Tab SHOULD 补全当前选中的命令名，而不是切换执行模式。对于未补全成完整命令的 slash 输入，Enter SHOULD 先选择当前候选，不得直接执行或报未知命令。对于 `/model`、`/approval-model` 和 `/effort` 的参数候选，TUI SHOULD 同样支持方向键移动候选、Tab 补全当前候选、Enter 选择并执行当前候选。

#### Scenario: 解析 model 命令

- **WHEN** 用户输入 `/model openai/gpt-4o-mini`
- **THEN** parser MUST 返回命令名 `model` 和参数 `openai/gpt-4o-mini`

#### Scenario: 未知命令

- **WHEN** 用户输入 `/unknown`
- **THEN** TUI MUST 显示未知命令错误，且不调用 Agent

#### Scenario: slash 候选提示

- **WHEN** 用户在输入框输入 `/m`
- **THEN** TUI SHOULD 显示 `/model [model]` 和 `/mode <work|plan|auto>` 的候选说明

#### Scenario: Tab 补全 slash 命令

- **WHEN** 用户在输入框输入 `/m`
- **WHEN** 用户按 Tab
- **THEN** TUI SHOULD 把输入补全为一个匹配的 slash 命令名

#### Scenario: 方向键选择 slash 候选

- **WHEN** 用户在输入框输入 `/m`
- **WHEN** 用户按下方向键
- **THEN** TUI SHOULD 移动 slash 命令候选选中项
- **WHEN** 用户按 Enter
- **THEN** TUI SHOULD 把输入补全为选中的 slash 命令名

#### Scenario: 参数候选补全并执行

- **GIVEN** 当前模型支持 effort `high`
- **WHEN** 用户输入 `/effort h`
- **WHEN** 用户按 Tab
- **THEN** TUI SHOULD 把输入补全为 `/effort high`
- **WHEN** 用户再次输入 `/effort h` 并按 Enter
- **THEN** TUI SHOULD 选择候选 `high` 并切换当前 effort

### Requirement: 本地命令执行

TUI SHALL 在本地执行 slash command。`/clear` MUST 清空消息列表但保留当前 session；`/new` MUST 创建并切换到一个新的空 session；`/help` MUST 显示支持的命令；`/quit` 和 `/exit` MUST 退出 TUI。

#### Scenario: clear 清空消息

- **GIVEN** 消息列表已有内容
- **WHEN** 用户输入 `/clear`
- **THEN** 消息列表 MUST 被清空
- **THEN** 后续 Agent turn MUST 继续使用当前 session 历史

#### Scenario: new 开启空 session

- **GIVEN** 当前 TUI 已有 session 历史和 context 状态栏
- **WHEN** 用户输入 `/new`
- **THEN** TUI MUST 创建并切换到新的空 session
- **THEN** 消息列表、本地排队输入和 context 状态栏 MUST 被清空
- **THEN** 后续 Agent turn MUST 使用新 session 的空历史

#### Scenario: quit 退出

- **WHEN** 用户输入 `/quit`
- **THEN** TUI MUST 返回退出命令

#### Scenario: exit 退出

- **WHEN** 用户输入 `/exit`
- **THEN** TUI MUST 返回退出命令

### Requirement: 运行时状态命令

`/model <id>`、`/mode <mode>`、`/new` 和 `/sessions` SHALL 更新 TUI 状态栏或会话状态，并在 runner 支持时同步到后续 Agent turn。`/model` 不带参数时 MUST 打开当前 provider 的可选模型选择列表，不得把模型切为空值。显式指定模型时，TUI MUST 校验该模型属于当前 provider 的候选列表；非法模型 MUST 保持当前模型不变并显示错误。`/sessions` 不带参数时 MUST 打开当前 workspace 的 session 选择列表；`/sessions <id>` MUST 切换到指定 session。`/config`、`/profile` SHALL 输出当前状态或明确操作提示。

#### Scenario: model 切换

- **GIVEN** 当前 provider 的候选模型包含 `fake/next`
- **WHEN** 用户输入 `/model fake/next`
- **THEN** 状态栏 MUST 显示 `fake/next`
- **THEN** 后续 Agent turn MUST 使用该 model

#### Scenario: model 无参数打开选择列表

- **WHEN** 用户输入 `/model`
- **THEN** TUI MUST 显示当前 provider 的候选模型选择列表
- **THEN** 当前模型 MUST 保持不变

#### Scenario: model 选择列表生效

- **GIVEN** `/model` 已打开候选模型选择列表
- **WHEN** 用户选择一个候选模型并确认
- **THEN** 状态栏 MUST 显示被选中的 model
- **THEN** 后续 Agent turn MUST 使用该 model

#### Scenario: 非法 model 不生效

- **GIVEN** 当前 provider 的候选模型不包含 `fake/missing`
- **WHEN** 用户输入 `/model fake/missing`
- **THEN** TUI MUST 显示错误
- **THEN** 当前模型 MUST 保持不变

#### Scenario: mode 切换

- **WHEN** 用户输入 `/mode plan`
- **THEN** 状态栏 MUST 显示 `plan`
- **THEN** 后续 Agent turn MUST 使用 plan execution mode

#### Scenario: sessions 选择列表切换

- **GIVEN** 当前 workspace 存在多个 session
- **WHEN** 用户输入 `/sessions`
- **THEN** TUI MUST 显示 session 选择列表
- **WHEN** 用户选择一个 session 并确认
- **THEN** TUI MUST 加载该 session 的历史
- **THEN** 后续 Agent turn MUST 继续该 session

#### Scenario: sessions 指定 ID 切换

- **GIVEN** 当前 workspace 存在 ID 为 `sess_1` 的 session
- **WHEN** 用户输入 `/sessions sess_1`
- **THEN** TUI MUST 加载 `sess_1` 的历史
- **THEN** 后续 Agent turn MUST 继续 `sess_1`

### Requirement: Effort slash 命令

TUI SHALL 支持 `/effort` slash 命令，用于查看和切换当前模型的 reasoning effort。`/effort` 不带参数时 MUST 显示当前模型支持的候选 effort 和当前值；`/effort <value>` MUST 仅在当前模型支持该 value 时更新后续 Agent turn。状态栏 MUST 展示当前 effort；当当前模型不支持 reasoning 时，状态栏 MUST 展示 `effort: none` 或等价空状态。

#### Scenario: effort 无参数列出候选

- **GIVEN** 当前模型支持 `low` 和 `medium`
- **WHEN** 用户输入 `/effort`
- **THEN** TUI MUST 显示候选 effort
- **THEN** 当前 effort MUST 保持不变

#### Scenario: effort 切换生效

- **GIVEN** 当前模型支持 `high`
- **WHEN** 用户输入 `/effort high`
- **THEN** 状态栏 MUST 显示 `high`
- **THEN** 后续 Agent turn MUST 使用 effort `high`

#### Scenario: 非法 effort 不生效

- **GIVEN** 当前模型不支持 `xhigh`
- **WHEN** 用户输入 `/effort xhigh`
- **THEN** TUI MUST 显示错误
- **THEN** 当前 effort MUST 保持不变

#### Scenario: slash 候选包含 effort 命令

- **WHEN** 用户在输入框输入 `/e`
- **THEN** TUI SHOULD 显示 `/effort [effort]` 的候选说明

### Requirement: Compact slash 命令

TUI SHALL 支持 `/compact` slash 命令，用于主动压缩当前 session 上下文。`/compact` MUST 在本地执行，不得作为普通 prompt 发送给 Agent；当 runner 支持 compact 时，TUI MUST 调用 runner 的 compact 能力并展示运行结果；当 runner 不支持 compact 或当前没有可压缩历史时，TUI MUST 显示可读提示。

#### Scenario: compact 命令不发送给 Agent prompt

- **WHEN** 用户输入 `/compact` 并按 Enter
- **THEN** TUI MUST 执行本地 compact 命令
- **THEN** 普通 Agent prompt runner MUST NOT 收到文本 `/compact`

#### Scenario: compact 成功后更新状态栏

- **GIVEN** runner 支持 compact 且当前 session 有可压缩历史
- **WHEN** 用户输入 `/compact` 并按 Enter
- **THEN** TUI MUST 显示 compact 完成提示
- **THEN** TUI 状态栏 MUST 使用 compact 后的上下文用量更新

#### Scenario: compact 不可用

- **GIVEN** runner 不支持 compact
- **WHEN** 用户输入 `/compact` 并按 Enter
- **THEN** TUI MUST 显示 compact 在当前 runner 不可用
