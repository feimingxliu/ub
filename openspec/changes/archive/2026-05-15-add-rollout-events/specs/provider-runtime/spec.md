## ADDED Requirements

### Requirement: Chat rollout 持久化

`ub chat` SHALL 把单轮对话绑定到 SQLite session，并写入 rollout 事件。该持久化 MUST 不改变 stdout 的文本输出行为。

#### Scenario: chat 创建 session

- **GIVEN** 默认 store 为空
- **WHEN** 用户运行一次成功的 `ub chat`
- **THEN** `ub sessions ls` MUST 能列出新 session

#### Scenario: chat stdout 不受 rollout 影响

- **GIVEN** provider 输出文本 `pong`
- **WHEN** 用户运行 `ub chat`
- **THEN** stdout MUST 仍只包含 provider 文本，不包含 rollout metadata
