## Why

`roadmap-v2.md` §3-07 把 LSP 工具扩充列为 M 工程量条目。现状只有 `diagnostics` 与 `references`,远不能让 agent 利用 LSP 做"精细化代码操作"。本 change 把另外 5 个常用 LSP 能力补齐:`hover`、`completion`、`document_symbols`、`rename`、`code_action`。

为了控制 risk,`rename` 与 `code_action` 在本 change 里采取 **"返回 LSP 建议的编辑集合 + 提示 agent 用 `multiedit` 自行应用"** 的策略,不直接落盘。这样:

1. agent 仍能利用 LSP 跨文件理解符号 / 决定改名范围
2. 用户审批/diff 走的是熟悉的 `multiedit` 通道
3. 复杂的 `WorkspaceEdit`(rename 一次涉及几十个文件的情形)不会绕过 ub 的 preview 协议

后续若有用例真正需要 LSP-driven 原子写盘,再单独提案。

## What Changes

- 扩展 `internal/lsp/manager.go`,新增 5 个方法:
  - `Hover(ctx, file, line, col) (HoverResult, error)`
  - `Completion(ctx, file, line, col, max int) ([]CompletionItem, error)`
  - `DocumentSymbols(ctx, file) ([]DocumentSymbol, error)`
  - `Rename(ctx, file, line, col, newName) (WorkspaceEdit, error)`
  - `CodeActions(ctx, file, line, col, endLine?, endCol?) ([]CodeAction, error)`
- `internal/lsp/types.go`:新增 `HoverResult`、`CompletionItem`、`DocumentSymbol`、`WorkspaceEdit`、`TextEdit`、`CodeAction`
- `internal/tool/lsp/lsp.go`:扩展 `Manager` 接口与 `Register`,新增 5 个工具:
  - `hover(file, line, col)` → 返回 markdown 内容(LSP MarkupContent / MarkedString 都拍平为 text)
  - `completion(file, line, col, max?)` → 返回前 max 条建议(默认 25)的 `label\tdetail` 表格
  - `document_symbols(file)` → 返回缩进的符号树(name / kind / range)
  - `rename(file, line, col, new_name)` → 返回每个受影响文件的 `path\told_text\tnew_text` 编辑列表,Description 里明确提示 "use multiedit to apply"
  - `code_action(file, line, col, end_line?, end_col?)` → 返回可用 actions 的 `title / kind / has_edit` 列表
- 所有新工具 `Risk = safe`
- 全部走现有 `serverFor(path)` 与 `DidChangeFile` 路径,自动复用 file-type 路由
- 注册顺序保持 `diagnostics / references` 在前,新工具追加在后

## Capabilities

### Modified Capabilities

- `lsp-tools`:在 diagnostics / references 之外新增 5 个工具的规格

## Impact

- 修改 `internal/lsp/{manager.go,types.go}`:新方法 + 新类型
- 修改 `internal/tool/lsp/lsp.go`:扩展接口与注册
- 新增 `internal/tool/lsp/lsp_test.go` 用例(用 fakeManager 覆盖每个新工具)
- 不引入新依赖
- breaking change:`tool/lsp.Manager` 接口从 3 个方法变成 8 个;接口实现者只有 `internal/lsp/manager.go` 自身(以及测试 fake),影响面可控
- 文档:`docs/design.md` 列出新工具
