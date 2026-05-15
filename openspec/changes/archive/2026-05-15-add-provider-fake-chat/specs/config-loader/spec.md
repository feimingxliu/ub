## ADDED Requirements

### Requirement: Fake provider 脚本配置

配置系统 SHALL 允许 provider 配置包含 `script` 列表，用于 fake provider 的离线脚本事件。每个脚本事件 MUST 至少包含 `type`，并 MAY 包含 `text`、`tool_use_id`、`tool_name`、`input`、`input_tokens`、`output_tokens` 或 `error` 字段。

#### Scenario: 解析 fake provider script

- **WHEN** 用户配置 `providers.fake.type=fake` 且包含两个 script 事件
- **THEN** `config.Load()` 返回的 provider 配置 MUST 保留两个脚本事件及其字段

#### Scenario: 非 fake provider 忽略 script

- **WHEN** provider 类型不是 `fake` 但配置中包含 `script`
- **THEN** 配置加载 MUST 成功，后续真实 provider MAY 忽略该字段
