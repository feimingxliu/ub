## 1. LSP 类型 + Manager 方法

- [x] 1.1 `internal/lsp/types.go`:新增 HoverResult / CompletionItem / DocumentSymbol / WorkspaceEdit / TextEdit / CodeAction
- [x] 1.2 `internal/lsp/manager.go`:
  - `Hover(ctx, file, line, col)`:`textDocument/hover`,把 MarkupContent / MarkedString / array 拍平为单 string
  - `Completion(ctx, file, line, col, max)`:`textDocument/completion`,接受 CompletionList 或 array,截断到 max 条
  - `DocumentSymbols(ctx, file)`:`textDocument/documentSymbol`,递归构建 DocumentSymbol 树
  - `Rename(ctx, file, line, col, newName)`:`textDocument/rename`,把 WorkspaceEdit.changes(URI→edits)与 documentChanges 都规范化成 `[]TextEdit`
  - `CodeActions(ctx, file, line, col, endLine, endCol)`:`textDocument/codeAction`,接受 Command 或 CodeAction;返回 `[]CodeAction`(only title/kind/has_edit 字段,不返回 raw command)
- [x] 1.3 路径与 URI 处理复用现有 `serverFor` / `DidChangeFile` / `fileURI`;无 server 时统一 `lsp: no language server configured` 错误

## 2. 工具

- [x] 2.1 `internal/tool/lsp/lsp.go`:扩展 Manager 接口 + 注册 5 个新工具
- [x] 2.2 `hover`:输入 `{file, line, col}`,输出 markdown / 文本,空时返回 "no hover"
- [x] 2.3 `completion`:输入 `{file, line, col, max?}`(默认 25,上限 100),输出每行 `label\tdetail`
- [x] 2.4 `document_symbols`:输入 `{file}`,输出按 LSP 顺序的缩进树:`<indent><kind> <name> [start_line:start_col-end_line:end_col]`
- [x] 2.5 `rename`:输入 `{file, line, col, new_name}`,输出"建议改名"的人类可读列表;包含一行明确提示让 agent 自行用 multiedit 应用
- [x] 2.6 `code_action`:输入 `{file, line, col, end_line?, end_col?}`,输出 actions 列表 `title (kind)[, has_edit]`

## 3. 测试

- [x] 3.1 `internal/tool/lsp/lsp_test.go`:为每个新工具写 happy path 测试(用 fakeManager 注入 LSP 响应)
- [x] 3.2 边界:空响应 / line=0 / col=0 拒绝;file 为空拒绝
- [x] 3.3 rename 跨文件:fakeManager 返回 2 个文件的 edits,工具输出按 path 字典序

## 4. 文档

- [x] 4.1 `docs/design.md`:tools 列表补 hover / completion / document_symbols / rename / code_action;rename / code_action 注明 "返回建议,不直接落盘"
- [x] 4.2 `openspec/changes/expand-lsp-tools/specs/lsp-tools/spec.md`:Requirements + Scenarios

## 5. 验证

- [x] 5.1 `go test ./...`
- [x] 5.2 `make lint`
- [x] 5.3 `make build`
