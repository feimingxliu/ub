# rollout-events Specification

## Purpose

Define rollout event persistence for session conversations, including event shape, SQLite writer/reader behavior, durability, and chat integration.

## Requirements

### Requirement: Rollout 事件模型

系统 SHALL 在 `internal/rollout` 中定义 rollout 事件模型。事件 MUST 包含 `ID`、`SessionID`、`Turn`、`Time`、`Type` 和 JSON `Payload`；I-09 MUST 支持 `user_message`、`assistant_message`、`usage`、`error` 四种事件类型。

#### Scenario: 创建用户消息事件

- **WHEN** 调用 rollout helper 创建用户消息事件
- **THEN** 事件类型 MUST 为 `user_message`，payload MUST 可 JSON 序列化

#### Scenario: 空必要字段拒绝

- **WHEN** 追加事件时缺少 `ID`、`SessionID` 或 `Type`
- **THEN** Writer MUST 返回可读错误

### Requirement: Rollout Writer

系统 SHALL 提供 Writer，把 rollout 事件追加到 SQLite `events` 表。每次 `Append` MUST 执行一条 INSERT 并在返回前 commit；写入时 MUST 保留 turn、time、type 和 payload。

#### Scenario: 写入 100 条事件

- **WHEN** Writer 连续追加 100 条同一 session 的事件
- **THEN** Reader 读取时 MUST 得到 100 条事件

#### Scenario: 写入后新连接可见

- **WHEN** Writer 追加事件并返回成功
- **THEN** 关闭当前 store 后重新打开数据库，Reader MUST 能读取该事件

### Requirement: Rollout Reader

系统 SHALL 提供 Reader，按 session ID 顺序读取事件。读取顺序 MUST 按 `turn`、`time`、`id` 升序稳定返回。

#### Scenario: 按顺序读取

- **GIVEN** 同一 session 下存在不同 turn 与 time 的事件
- **WHEN** Reader 读取该 session
- **THEN** 返回顺序 MUST 为 turn 从小到大，同 turn 内按 time 和 id 排序

#### Scenario: 只读取目标 session

- **GIVEN** 数据库中存在两个 session 的事件
- **WHEN** Reader 读取其中一个 session
- **THEN** 返回结果 MUST 不包含另一个 session 的事件

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
