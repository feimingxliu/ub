## MODIFIED Requirements

### Requirement: TUI 权限弹窗

TUI SHALL 在 permission Manager 请求 human approval 时显示阻塞式 modal。modal MUST 显示工具名、风险等级、执行模式、参数摘要；若请求包含 preview，modal MUST 显示 preview summary，并支持按 `d` 展开/折叠由 diffview 渲染的 unified diff。展开后，modal MUST 将左/右/上/下方向键转发给 diffview 用于多文件切换。

#### Scenario: 显示 exec 审批

- **GIVEN** permission Manager 请求审批 `bash` 工具
- **WHEN** TUI 收到审批请求
- **THEN** modal MUST 显示工具名、风险等级、执行模式和参数摘要

#### Scenario: 展开 preview diff

- **GIVEN** 审批请求包含 preview summary 和 unified diff
- **WHEN** 用户按 `d`
- **THEN** modal MUST 展示 diffview 渲染的 unified diff

#### Scenario: modal 中切换 diff 文件

- **GIVEN** 审批请求包含两个文件的 preview diff 且 modal 已展开
- **WHEN** 用户按右方向键
- **THEN** diffview MUST 切到下一个文件
