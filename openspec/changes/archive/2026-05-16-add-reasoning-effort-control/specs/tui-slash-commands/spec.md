## ADDED Requirements

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
