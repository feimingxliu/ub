## Purpose

定义 ub 通过本地工具暴露 LSP diagnostics 和 references 查询能力。

## Requirements

### Requirement: Diagnostics tool

系统 SHALL 提供本地工具 `diagnostics`，用于读取 LSP 发布的代码诊断。

#### Scenario: 查询单文件诊断

- **WHEN** 模型调用 `diagnostics` 并提供 file
- **THEN** 系统先同步该文件，再返回该文件的 LSP diagnostics

#### Scenario: 没有诊断

- **WHEN** LSP 对目标范围没有发布诊断
- **THEN** 工具返回明确的 no diagnostics 文本且 `is_error` 为 false

### Requirement: References tool

系统 SHALL 提供本地工具 `references`，用于按文件、行、列查询符号引用。

#### Scenario: 查询引用位置

- **WHEN** 模型调用 `references` 并提供 file、line、col
- **THEN** 系统先同步该文件，再调用 `textDocument/references` 并返回引用位置列表

#### Scenario: 无引用

- **WHEN** LSP 返回空引用列表
- **THEN** 工具返回明确的 no references 文本且 `is_error` 为 false
