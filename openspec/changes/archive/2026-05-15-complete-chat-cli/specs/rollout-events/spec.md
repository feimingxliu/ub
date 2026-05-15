## MODIFIED Requirements

### Requirement: Chat rollout 写入

`ub chat` SHALL 为新对话创建 session，也 SHALL 支持通过 `--session <id>` 继续已有 session。每轮 MUST 把用户消息、助手消息、usage 和错误写入 rollout。成功对话后 `ub sessions ls` MUST 能看到该 session，继续已有 session 后 session metadata MUST 更新。

#### Scenario: fake chat 写入 rollout

- **GIVEN** 配置中存在 fake provider
- **WHEN** 用户运行 `ub chat --provider fake "hello"`
- **THEN** 默认 SQLite store 中 MUST 有一个 session，且该 session 至少包含 user_message 和 assistant_message 事件

#### Scenario: provider usage 写入 rollout

- **GIVEN** provider stream 返回 usage 事件
- **WHEN** `ub chat` 成功结束
- **THEN** rollout MUST 包含 usage 事件

#### Scenario: provider 错误写入 rollout

- **GIVEN** provider stream 返回 error 事件
- **WHEN** `ub chat` 返回错误
- **THEN** rollout MUST 包含 error 事件

#### Scenario: 继续 session 写入同一 rollout

- **GIVEN** 已存在 session 及其 user/assistant rollout 事件
- **WHEN** 用户运行 `ub chat --session <id> "next"`
- **THEN** 新 user/assistant 事件 MUST 追加到同一 session，turn MUST 大于已有 turn

#### Scenario: 继续缺失 session 报错

- **WHEN** 用户运行 `ub chat --session missing "next"`
- **THEN** 命令 MUST 返回说明 session 不存在的可读错误，且不创建新 session
