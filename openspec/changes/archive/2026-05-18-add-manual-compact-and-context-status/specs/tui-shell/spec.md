## ADDED Requirements

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
