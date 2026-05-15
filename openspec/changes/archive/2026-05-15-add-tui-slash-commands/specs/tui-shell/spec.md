## MODIFIED Requirements

### Requirement: 输入回显

TUI SHALL 在用户输入非空文本并按 Enter 后，把普通文本作为用户消息追加到消息列表，并清空输入框。若输入以 `/` 开头，TUI MUST 作为 slash command 本地执行，不得把该输入发送给 Agent。

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
