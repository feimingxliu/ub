## ADDED Requirements

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
