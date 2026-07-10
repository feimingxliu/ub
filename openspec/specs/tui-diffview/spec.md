# tui-diffview Specification

## Purpose

定义 TUI 中统一 diff 的富渲染、多文件导航和语言高亮行为。

## Requirements

### Requirement: Unified diff 富渲染

系统 SHALL 提供 `internal/tui/diffview` 组件，接收一个或多个 `tool.FileDiff` 并渲染 unified diff。渲染结果 MUST 包含当前文件路径、变更类型和 diff 内容；没有 diff 时 MUST 显示空状态。

#### Scenario: 渲染单文件 diff

- **GIVEN** 一个包含 `main.go` unified diff 的 FileDiff
- **WHEN** diffview 渲染
- **THEN** 输出 MUST 包含 `main.go`、变更类型和 diff 正文

#### Scenario: 空 diff

- **GIVEN** FileDiff 列表为空
- **WHEN** diffview 渲染
- **THEN** 输出 MUST 包含空状态提示

### Requirement: 语言高亮

diffview SHALL 使用 Chroma 根据文件路径选择语言 lexer，并对 diff 内容进行终端高亮。无法识别语言时 MUST fallback 到 plaintext，不得 panic。

#### Scenario: 常见语言不 panic

- **GIVEN** Go、Python 和 TypeScript 文件 diff
- **WHEN** diffview 渲染每个文件
- **THEN** 渲染 MUST 成功且不 panic

### Requirement: 多文件切换

diffview SHALL 支持多文件 diff 导航。用户按右/下 MUST 切到下一个文件，按左/上 MUST 切到上一个文件；切换 MUST 在文件列表范围内循环。

#### Scenario: 切到下一个文件

- **GIVEN** diffview 包含两个文件且当前选中第一个
- **WHEN** 用户触发 next
- **THEN** 当前文件 MUST 切到第二个

#### Scenario: 切换循环

- **GIVEN** diffview 当前选中最后一个文件
- **WHEN** 用户触发 next
- **THEN** 当前文件 MUST 回到第一个
