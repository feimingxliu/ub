## 1. MCP stdio client

- [x] 1.1 新增 `internal/mcp` 协议类型、client API 和 stdio transport。
- [x] 1.2 实现 `initialize`、`notifications/initialized`、`tools/list`、`tools/call`。
- [x] 1.3 实现 client close，确保 stdio pipe 和子进程生命周期可释放。

## 2. 验证

- [x] 2.1 增加 stdio MCP fixture 测试，覆盖工具列表和工具调用。
- [x] 2.2 运行 `go test ./internal/mcp` 和 `go test ./...`。
