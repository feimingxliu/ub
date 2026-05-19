# tui-shell Specification

## Purpose

定义 ub 的 Bubble Tea 终端聊天外壳，包括默认入口、基础布局、输入回显和退出行为。

## Requirements

### Requirement: TUI 默认入口

系统 SHALL 在用户运行 `ub` 且未指定子命令时启动 Bubble Tea 终端聊天界面；显式子命令 SHALL 保持原有 CLI 行为。

#### Scenario: 无子命令启动 TUI

- **WHEN** 用户在终端运行 `ub`
- **THEN** 系统启动 TUI 主界面，并显示聊天区、输入框和状态栏

#### Scenario: 显式子命令不进入 TUI

- **WHEN** 用户运行 `ub config show`
- **THEN** 系统执行 config 子命令，而不是启动 TUI

### Requirement: 基础聊天布局

TUI SHALL 渲染消息列表、输入框和状态栏三个区域。状态栏 MUST 至少显示当前模型、执行模式、工作目录、turn 序号和运行状态。
消息列表和状态栏 MUST 按当前终端宽度渲染，不得让长文本只能在右侧不可见区域查看。消息列表 MUST 在 TUI 内部支持滚动查看历史输出，并 SHOULD 支持 PageUp/PageDown 和鼠标滚轮滚动聊天区。消息列表 SHOULD 使用紧凑前缀区分用户、Agent、工具和系统提示，不得直接显示 `user` 或 `assistant` 作为消息标签。

#### Scenario: 初始界面显示基础区域

- **WHEN** TUI model 初始化并渲染
- **THEN** 输出包含输入提示、状态栏信息和空消息列表占位

#### Scenario: 状态栏显示 turn

- **WHEN** TUI 完成一次用户发送
- **THEN** 状态栏 MUST 显示当前 turn 序号

#### Scenario: 长文本按终端宽度换行

- **GIVEN** 当前终端宽度小于一条消息的显示宽度
- **WHEN** TUI 渲染消息列表
- **THEN** 长消息 MUST 在可见宽度内换行

#### Scenario: 长输出可在 TUI 内滚动

- **GIVEN** 消息列表高度超过当前终端可见区域
- **WHEN** 用户按 PageUp 或向上滚动鼠标滚轮
- **THEN** TUI MUST 在聊天区显示更早的消息内容
- **THEN** 终端宿主 scrollback 不应成为查看历史输出的主要机制

#### Scenario: 消息标签紧凑显示

- **WHEN** TUI 渲染用户和 Agent 消息
- **THEN** 输出 SHOULD 使用紧凑前缀区分来源
- **THEN** 输出 MUST NOT 把消息来源明文渲染为 `user` 或 `assistant`

### Requirement: 输入回显

TUI SHALL 在用户输入非空文本并按 Enter 后，把普通文本作为用户消息追加到消息列表，并清空输入框。若输入以 `/` 开头，TUI MUST 作为 slash command 本地执行，不得把该输入发送给 Agent。默认输入状态下，TUI SHOULD 支持使用上下方向键浏览此前发送的用户消息，并把选中的历史消息填回输入框。
Agent turn 运行中，TUI SHALL 允许用户继续输入普通文本并按 Enter 加入本地 FIFO 队列；排队消息在实际启动前 MUST NOT 写入 rollout 或消息列表。当前 turn 正常结束后，TUI MUST 自动发送下一条排队消息。运行中上下方向键 SHOULD 优先浏览并编辑已排队消息；slash 命令输入 MUST NOT 作为队列消息发送。

#### Scenario: 发送普通文本

- **WHEN** 用户输入 `hello` 并按 Enter
- **THEN** 消息列表包含一条用户消息 `hello`
- **THEN** 输入框被清空

#### Scenario: 空输入不生成消息

- **WHEN** 用户在空输入框按 Enter
- **THEN** 消息列表不新增消息

#### Scenario: slash 输入不发送给 Agent

- **WHEN** 用户输入 `/help` 并按 Enter
- **THEN** TUI MUST 执行 help 命令
- **THEN** Agent runner MUST NOT 被调用

#### Scenario: 上下方向键浏览历史输入

- **GIVEN** 用户已经发送过 `first` 和 `second`
- **WHEN** 用户在默认输入状态按上方向键
- **THEN** 输入框 SHOULD 填入 `second`
- **WHEN** 用户再次按上方向键
- **THEN** 输入框 SHOULD 填入 `first`
- **WHEN** 用户按下方向键
- **THEN** 输入框 SHOULD 回到较新的历史输入

#### Scenario: 运行中输入消息进入队列

- **GIVEN** 当前 Agent turn 正在运行
- **WHEN** 用户输入 `next` 并按 Enter
- **THEN** TUI MUST 将 `next` 加入本地队列
- **THEN** TUI MUST NOT 启动并发 Agent turn

#### Scenario: 当前 turn 结束后发送队首消息

- **GIVEN** 当前 Agent turn 正在运行且本地队列包含 `next`
- **WHEN** 当前 stream 正常关闭
- **THEN** TUI MUST 将 `next` 作为下一轮用户消息发送给 runner
- **THEN** `next` MUST 从队列移除

#### Scenario: 方向键编辑排队消息

- **GIVEN** 当前 Agent turn 正在运行且本地队列包含 `first` 和 `second`
- **WHEN** 用户按上方向键
- **THEN** 输入框 SHOULD 填入 `second`
- **WHEN** 用户修改输入框内容并按上或下方向键切换队列项
- **THEN** TUI SHOULD 保存被编辑的队列项内容

### Requirement: 快捷键切换执行模式

TUI SHALL 在主输入界面支持按 Shift+Tab 在 `work`、`plan`、`auto` 三种执行模式之间循环切换，并同步状态栏和后续 Agent turn。Tab MUST 保留给输入补全交互，不得在普通输入状态下切换模式。

#### Scenario: Shift+Tab 切换模式

- **GIVEN** 当前执行模式为 `work`
- **WHEN** 用户按 Shift+Tab
- **THEN** TUI MUST 切换为 `plan`
- **WHEN** 用户再次按 Shift+Tab
- **THEN** TUI MUST 切换为 `auto`

### Requirement: TUI session 恢复

系统 SHALL 支持在 TUI 启动时恢复已有 session。`ub --resume` MUST 恢复当前 workspace 最近更新的 session；`ub --resume=<id>` 或 `ub --resume <id>` MUST 恢复指定 session。恢复后 TUI MUST 渲染可显示的历史消息，并让下一轮 Agent turn 继续使用该 session 的 rollout 历史。

#### Scenario: 启动恢复最近 session

- **GIVEN** 当前 workspace 已存在历史 session
- **WHEN** 用户运行 `ub --resume`
- **THEN** TUI MUST 加载最近更新的 session 历史

#### Scenario: 启动恢复指定 session

- **GIVEN** 当前 workspace 存在 ID 为 `sess_1` 的 session
- **WHEN** 用户运行 `ub --resume=sess_1` 或 `ub --resume sess_1`
- **THEN** TUI MUST 加载 `sess_1` 的历史和下一轮 turn

### Requirement: TUI 退出

TUI SHALL 在收到 Ctrl+C 或 Esc 时退出当前程序。

#### Scenario: Ctrl+C 退出

- **WHEN** TUI 收到 Ctrl+C 按键
- **THEN** model 返回退出命令

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

### Requirement: TUI 活动流展示

TUI SHALL 在消息列表中展示 Agent activity 事件，包括 provider thinking、工具生命周期、权限审批结果和错误/notice 摘要。活动流 MUST 使用紧凑前缀与用户消息、assistant 正文区分；活动内容 MUST 按终端宽度换行，并随聊天区历史一起滚动。TUI MUST NOT 把 provider 未返回的隐藏推理链展示为 thinking。同一个 tool call 的 queued/running/done/failed 更新 MUST 合并到同一活动行。

#### Scenario: thinking 活动渲染

- **GIVEN** TUI 收到 `thinking` activity
- **WHEN** TUI 渲染消息列表
- **THEN** 输出 MUST 包含一条与 assistant 正文区分的 thinking 消息
- **THEN** 该消息 MUST NOT 被合并进 assistant 最终回复文本

#### Scenario: 工具 lifecycle 活动渲染

- **GIVEN** TUI 收到工具 queued/running/done/failed activity
- **WHEN** TUI 渲染消息列表
- **THEN** 输出 MUST 显示工具名、状态和短摘要
- **THEN** 输出 MUST NOT 直接展示完整 tool input JSON
- **THEN** 同一个 tool call MUST NOT 因状态变化产生多条重复活动消息

#### Scenario: 权限活动渲染

- **GIVEN** TUI 收到 approval agent 或 human approval 的 permission activity
- **WHEN** TUI 渲染消息列表
- **THEN** 输出 MUST 显示审批来源、allow/deny/unsure/error 决策和原因

#### Scenario: 活动流参与滚动与换行

- **GIVEN** 活动消息和普通聊天消息总高度超过当前聊天区
- **WHEN** 用户按 PageUp 或滚动鼠标
- **THEN** TUI MUST 能在聊天区内查看更早的活动消息
- **THEN** 长活动摘要 MUST 在当前宽度内换行或截断，且 secret 值 MUST 保持遮蔽

### Requirement: TUI 权限弹窗

TUI SHALL 在 permission Manager 请求 human approval 时显示阻塞式 modal。modal MUST 显示工具名、风险等级、执行模式、参数摘要；若请求包含 preview，modal MUST 显示 preview summary，并支持按 `d` 展开/折叠由 diffview 渲染的 unified diff。modal MUST 以可选择候选列表展示每个审批决策，并解释每个选项的作用范围。展开 diff 后，modal MUST 支持用左/右方向键切换 diff 文件。

#### Scenario: 显示 exec 审批

- **GIVEN** permission Manager 请求审批 `bash` 工具
- **WHEN** TUI 收到审批请求
- **THEN** modal MUST 显示工具名、风险等级、执行模式和参数摘要

#### Scenario: 展开 preview diff

- **GIVEN** 审批请求包含 preview summary 和 unified diff
- **WHEN** 用户按 `d`
- **THEN** modal MUST 展示 diffview 渲染的 unified diff

#### Scenario: modal 中切换 diff 文件

- **GIVEN** 审批请求包含两个文件的 preview diff 且 modal 已展开
- **WHEN** 用户按右方向键
- **THEN** diffview MUST 切到下一个文件

### Requirement: TUI 权限决策候选列表

权限 modal SHALL 支持五个决策候选：Allow once、Deny、Always allow exact command in this session、Always allow this tool in this session、Always allow this tool globally。用户 MUST 能用上/下方向键移动候选，并用 Enter 确认当前候选。modal SHOULD 保留 `1` 到 `5` 作为快捷键，但这些数字键不得是主要说明方式。确认后 TUI MUST 把对应 `permission.Decision` 返回给等待中的 permission Asker，并关闭 modal。

#### Scenario: Allow once

- **WHEN** 用户在权限 modal 中选择 `Allow once` 并确认
- **THEN** Asker MUST 返回 `permission.DecisionAllow`

#### Scenario: Deny

- **WHEN** 用户在权限 modal 中选择 `Deny` 并确认
- **THEN** Asker MUST 返回 `permission.DecisionDeny`

#### Scenario: Always global

- **WHEN** 用户在权限 modal 中选择 `Always allow this tool globally` 并确认
- **THEN** Asker MUST 返回 `permission.DecisionAlwaysGlobal`

#### Scenario: 数字快捷键仍可用

- **WHEN** 用户在权限 modal 中按 `5`
- **THEN** Asker SHOULD 返回 `permission.DecisionAlwaysGlobal`

### Requirement: TUI 审批上下文提示

权限 modal SHALL 在特殊上下文中显示额外提示：Plan 模式审批 exec 时 MUST 提示命令可能仍有副作用；auto 回退 human 时 MUST 展示 approval agent reason。

#### Scenario: Plan exec 警告

- **GIVEN** 请求 mode 为 `plan` 且 risk 为 `exec`
- **WHEN** modal 渲染
- **THEN** 输出 MUST 包含 `Plan mode: command may still have side effects`

#### Scenario: approval agent reason

- **GIVEN** 请求包含 approval agent 回退原因
- **WHEN** modal 渲染
- **THEN** 输出 MUST 包含该 reason

### Requirement: 状态栏上下文用量

TUI 状态栏 SHALL 展示最近一次 Agent 上报的上下文 token 使用量。若同时存在最大上下文，状态栏 MUST 展示 used/max 和百分比；若最大上下文未知，状态栏 MUST 至少展示 used token 数。该展示 MUST 参与现有状态栏宽度收缩，避免挤出输入框或造成横向不可见内容。

#### Scenario: 状态栏显示 used/max 和百分比

- **GIVEN** TUI 收到 used tokens 为 1200、max tokens 为 8000、ratio 为 0.15 的上下文用量事件
- **WHEN** TUI 渲染状态栏
- **THEN** 状态栏 MUST 显示 context token 使用量
- **THEN** 状态栏 MUST 包含 used/max 和百分比信息

#### Scenario: 状态栏显示未知最大上下文

- **GIVEN** TUI 收到 used tokens 为 1200 且 max tokens 为 0 的上下文用量事件
- **WHEN** TUI 渲染状态栏
- **THEN** 状态栏 MUST 显示 used tokens
- **THEN** 状态栏 MUST 不显示误导性的百分比
