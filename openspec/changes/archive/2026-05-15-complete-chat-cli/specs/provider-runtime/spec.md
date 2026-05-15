## MODIFIED Requirements

### Requirement: 最小 chat 命令

系统 SHALL 提供 `ub chat` 子命令用于 provider 对话。命令 MUST 支持 `ub chat "prompt"`、`ub chat -`、`--provider <name>`、`--model <id>`、`--session <id>` 和 `--new`；文本 delta MUST 流式写到 stdout。`--provider` 与 `--model` MUST 只影响当前调用，不写回配置。

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

#### Scenario: 继续 session

- **GIVEN** 已有 session 中存在 user 与 assistant 历史消息
- **WHEN** 用户运行 `ub chat --session <id> "next"`
- **THEN** provider request MUST 包含历史消息和当前 user 消息，并把新事件追加到同一 session

#### Scenario: 强制新 session

- **WHEN** 用户运行 `ub chat --new "hello"`
- **THEN** 命令 MUST 创建新 session，而不是复用任何已有 session

#### Scenario: session 参数冲突

- **WHEN** 用户同时传入 `--session <id>` 和 `--new`
- **THEN** 命令 MUST 返回可读错误

#### Scenario: provider 不存在

- **WHEN** 用户运行 `ub chat --provider missing "hi"`
- **THEN** 命令 MUST 返回说明 provider 未配置的可读错误
