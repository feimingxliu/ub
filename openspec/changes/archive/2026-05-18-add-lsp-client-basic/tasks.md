## 1. LSP client

- [x] 1.1 新增 `internal/lsp` stdio JSON-RPC client 和生命周期方法。
- [x] 1.2 实现文档 URI、languageId、version 管理，以及 didOpen/didChange。
- [x] 1.3 实现 Manager/Notifier，按配置启动 LSP server 并路由文件同步。

## 2. 文件工具集成

- [x] 2.1 为 `internal/tool/fs` 增加可选变更 notifier 注册路径。
- [x] 2.2 在 `write` 和 `edit` 执行成功后通知 LSP，保持无 notifier 路径兼容。

## 3. 验证

- [x] 3.1 增加 LSP fixture 测试，覆盖 initialize、didOpen、didChange 和 shutdown。
- [x] 3.2 增加 fs 工具 notifier 测试。
- [x] 3.3 运行 `go test ./internal/lsp ./internal/tool/fs` 和 `go test ./...`。
