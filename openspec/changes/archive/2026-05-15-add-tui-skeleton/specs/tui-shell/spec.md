## ADDED Requirements

### Requirement: TUI 默认入口

系统 SHALL 在用户运行 `ub` 且未指定子命令时启动 Bubble Tea 终端聊天界面；显式子命令 SHALL 保持原有 CLI 行为。

#### Scenario: 无子命令启动 TUI

- **WHEN** 用户在终端运行 `ub`
- **THEN** 系统启动 TUI 主界面，并显示聊天区、输入框和状态栏

#### Scenario: 显式子命令不进入 TUI

- **WHEN** 用户运行 `ub config show`
- **THEN** 系统执行 config 子命令，而不是启动 TUI

### Requirement: 基础聊天布局

TUI SHALL 渲染消息列表、输入框和状态栏三个区域。状态栏 MUST 至少显示当前模型、执行模式和工作目录。

#### Scenario: 初始界面显示基础区域

- **WHEN** TUI model 初始化并渲染
- **THEN** 输出包含输入提示、状态栏信息和空消息列表占位

### Requirement: 输入回显

TUI SHALL 在用户输入非空文本并按 Enter 后，把该文本作为用户消息追加到消息列表，并清空输入框。

#### Scenario: 发送普通文本

- **WHEN** 用户输入 `hello` 并按 Enter
- **THEN** 消息列表包含一条用户消息 `hello`
- **THEN** 输入框被清空

#### Scenario: 空输入不生成消息

- **WHEN** 用户在空输入框按 Enter
- **THEN** 消息列表不新增消息

### Requirement: TUI 退出

TUI SHALL 在收到 Ctrl+C 或 Esc 时退出当前程序。

#### Scenario: Ctrl+C 退出

- **WHEN** TUI 收到 Ctrl+C 按键
- **THEN** model 返回退出命令
