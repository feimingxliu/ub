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

### Requirement: hover 工具

系统 SHALL 提供 `hover` 工具,`Risk` 为 `RiskSafe`。input MUST 含 `file: string`、`line: int`(1-based)、`col: int`(1-based);任一缺失或非正 MUST 返回错误。Execute MUST 通过 `Manager.Hover` 调用 `textDocument/hover`,把 LSP 返回的 `MarkupContent` / `MarkedString` / `MarkedString[]` 统一拍平为单 string;空响应 MUST 返回 `Result.Content = "no hover"`。

#### Scenario: 返回 hover 内容

- **GIVEN** LSP 在 `foo.go:3:5` 返回 `{kind:"markdown", value:"## func foo()\n..."}`
- **WHEN** 调用 `hover(file="foo.go", line=3, col=5)`
- **THEN** `Result.Content` MUST 包含 `func foo()`

### Requirement: completion 工具

系统 SHALL 提供 `completion` 工具,`Risk` 为 `RiskSafe`。input MUST 含 `file/line/col`,可选 `max: int`(默认 25,上限 100;超过 100 MUST 被钳制到 100)。Execute MUST 通过 `Manager.Completion` 调用 `textDocument/completion`,接受 `CompletionList` 或 `CompletionItem[]` 两种返回形态,截断到 max 条,`Result.Content` 每行 `label\tdetail`(没有 detail 时省略 detail 但保留 tab 前缀)。空结果 MUST 返回 `"no completions"`。

#### Scenario: max 钳制

- **GIVEN** 调用 `completion(file=x, line=1, col=1, max=500)`
- **WHEN** Manager.Completion 收到该请求
- **THEN** Manager 收到的 max 参数 MUST 被钳制到 100;Result.Content 最多 100 行

#### Scenario: 默认 max

- **GIVEN** 调用 `completion(file=x, line=1, col=1)`,不传 max
- **WHEN** 工具运行
- **THEN** Manager 收到的 max 参数 MUST 等于 25

### Requirement: document_symbols 工具

系统 SHALL 提供 `document_symbols` 工具,`Risk` 为 `RiskSafe`。input MUST 含 `file: string`。Execute MUST 通过 `Manager.DocumentSymbols` 调用 `textDocument/documentSymbol`,把返回的 `DocumentSymbol[]` 递归扁平化为缩进文本,每行格式 `<indent><kind> <name> [<start_line>:<start_col>-<end_line>:<end_col>]`,缩进每层 2 个空格。空结果 MUST 返回 `"no symbols"`。

#### Scenario: 嵌套符号

- **GIVEN** LSP 返回一个含两个 method 的 struct `Foo`
- **WHEN** 调用 `document_symbols(file="foo.go")`
- **THEN** `Result.Content` MUST 含 `Struct Foo [...]` 一行 + 缩进 2 空格的两行 method

### Requirement: rename 工具

系统 SHALL 提供 `rename` 工具,`Risk` 为 `RiskSafe`,**不**直接落盘。input MUST 含 `file/line/col/new_name`。Execute MUST 通过 `Manager.Rename` 调用 `textDocument/rename`,把返回的 `WorkspaceEdit` 中 `changes`(`{[uri]: TextEdit[]}`)与 `documentChanges` 两种形态都规范化为 `[]TextEdit`,按 path 字典序输出。`Result.Content` 第一行 MUST 是 `Rename suggested by LSP. Apply via multiedit:`,后续每条边界格式为 `- <path>:<line>:<col>: '<old_text>' → '<new_name>'`(`old_text` 若 LSP 未提供,可省略 `'<old_text>'` 部分)。空结果 MUST 返回 `"no rename edits"`。

#### Scenario: 跨文件 rename 建议

- **GIVEN** LSP 在 `foo.go:1:5` 对 `Bar` 做 rename 时返回 `foo.go` 与 `bar.go` 共 3 处 edit
- **WHEN** 调用 `rename(file="foo.go", line=1, col=5, new_name="Baz")`
- **THEN** `Result.Content` MUST 含开头提示 + 3 行 edit,按文件路径字典序

### Requirement: code_action 工具

系统 SHALL 提供 `code_action` 工具,`Risk` 为 `RiskSafe`,**只返回可用 actions 列表,不直接执行**。input MUST 含 `file/line/col`,可选 `end_line/end_col` 定义范围(缺失时使用与 `line/col` 相同的点位)。Execute MUST 通过 `Manager.CodeActions` 调用 `textDocument/codeAction`,把返回的 `Command` 与 `CodeAction` 两种形态统一为 `CodeAction{Title, Kind, HasEdit}`;`Result.Content` 每行格式 `<title> (<kind>)[ — has_edit]`。空结果 MUST 返回 `"no code actions"`。

#### Scenario: 列出 actions

- **GIVEN** LSP 返回 2 个 actions,其中第 1 个含 edit、第 2 个无 edit
- **WHEN** 调用 `code_action(file=x, line=3, col=5)`
- **THEN** `Result.Content` MUST 含两行,第 1 行以 ` — has_edit` 结尾,第 2 行不含此后缀

#### Scenario: end_line/end_col 缺省默认

- **GIVEN** 调用 `code_action(file=x, line=3, col=5)`(无 end_line/end_col)
- **WHEN** Manager.CodeActions 收到请求
- **THEN** 它收到的 endLine MUST = 3、endCol MUST = 5(以 line/col 兜底)

### Requirement: Manager 接口扩展

系统 SHALL 把 `internal/tool/lsp.Manager` 接口从原 3 个方法扩展为 8 个:在 `Diagnostics`、`References`、`ReferencesBySymbol` 之外增加 `Hover`、`Completion`、`DocumentSymbols`、`Rename`、`CodeActions`。`tool/lsp.Register` 在 `manager != nil` 时 MUST 注册全部 7 个工具(diagnostics + references + 5 个新工具);`manager == nil` 时 MUST 不注册任何工具且 MUST 不返回错误(保持现状)。

#### Scenario: nil manager 不注册

- **GIVEN** 一个空 Registry
- **WHEN** 调用 `lsp.Register(reg, nil)`
- **THEN** `reg.All()` MUST 为空

#### Scenario: 完整注册 7 个工具

- **GIVEN** 一个空 Registry 与一个非 nil Manager
- **WHEN** 调用 `lsp.Register(reg, manager)`
- **THEN** Registry MUST 含 `diagnostics`、`references`、`hover`、`completion`、`document_symbols`、`rename`、`code_action` 共 7 个工具

