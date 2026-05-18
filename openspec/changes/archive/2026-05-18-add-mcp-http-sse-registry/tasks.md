## 1. MCP transport

- [x] 1.1 在 `internal/mcp` 增加 HTTP JSON-RPC transport。
- [x] 1.2 在 `internal/mcp` 增加 SSE endpoint/message transport。
- [x] 1.3 补充 HTTP/SSE transport 单元测试。

## 2. Registry adapter

- [x] 2.1 新增 `internal/tool/mcp` adapter，把 MCP tool 包装为 `tool.Tool`。
- [x] 2.2 实现按 `config.MCPServers` 启动 server、初始化、列工具并注册到 Registry。
- [x] 2.3 调整 `ub run` 和 TUI runner 的工具注册路径，使单个 MCP server 失败只产生 warning。

## 3. 验证

- [x] 3.1 增加 registry 注入测试，覆盖两个 server 和命名前缀。
- [x] 3.2 运行 `go test ./internal/mcp ./internal/tool/mcp ./internal/cli` 和 `go test ./...`。
