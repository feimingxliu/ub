## Why

Sprint 5 需要把 ub 的工具体系扩展到 MCP。第一步先支持 stdio 传输，确保 ub 能启动一个本地 MCP server，完成初始化，并列出/调用 server 暴露的工具。

## What Changes

- 新增 `internal/mcp` 客户端包，支持 JSON-RPC 2.0 的 `Content-Length` stdio frame。
- 实现 MCP `initialize`、`notifications/initialized`、`tools/list`、`tools/call`。
- 增加可用测试覆盖：用子进程 MCP fixture 完成工具列表和文件读取调用。
- 不引入 HTTP/SSE 传输，不接入本地 Tool Registry。

## Capabilities

### New Capabilities

- `mcp-stdio-client`: 覆盖 ub 作为 MCP stdio client 的初始化、工具发现和工具调用行为。

### Modified Capabilities

## Impact

- 新增 `internal/mcp/` 包和测试。
- 为 I-30 的 MCP tool adapter 和 registry 注入提供客户端基础。
