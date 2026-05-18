## 1. LSP 查询 API

- [x] 1.1 在 `internal/lsp` Manager 中增加 diagnostics 缓存读取与等待逻辑。
- [x] 1.2 在 `internal/lsp` Manager 中增加 references 查询逻辑和位置转换。

## 2. LSP 工具

- [x] 2.1 新增 `internal/tool/lsp`，实现 `diagnostics(file?)` 工具。
- [x] 2.2 新增 `references(file,line,col)` 工具。
- [x] 2.3 调整 CLI/TUI registry builder，在 LSP server 成功启动时注册 LSP 工具。

## 3. 验证

- [x] 3.1 增加 diagnostics 工具测试，覆盖语法错误和无诊断输出。
- [x] 3.2 增加 references 工具测试，覆盖引用列表和空引用。
- [x] 3.3 运行 `go test ./internal/lsp ./internal/tool/lsp ./internal/cli` 和 `go test ./...`。
