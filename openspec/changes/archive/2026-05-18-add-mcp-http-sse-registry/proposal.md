## Why

I-29 只能直接使用 stdio MCP client，模型还无法在 `ub run` 或 TUI 中调用 MCP 工具。I-30 需要补齐远程传输并把 MCP 工具注入本地 Registry，让 MCP server 成为 agent loop 的普通工具来源。

## What Changes

- 在 `internal/mcp` 中新增 HTTP 和 SSE transport。
- 新增 MCP tool adapter，把远端 tool 转换为本地 `tool.Tool`。
- 按配置启动所有 MCP server，使用 `mcp__<server>__<tool>` 命名并注册到 Registry。
- 单个 MCP server 启动失败只产生 warning，不阻止本地工具或其它 MCP server 可用。
- 不集成 resources/prompts。

## Capabilities

### New Capabilities

- `mcp-tool-registry`: 覆盖 HTTP/SSE MCP server 启动、MCP 工具命名、Registry 注入和失败隔离。

### Modified Capabilities

## Impact

- 扩展 `internal/mcp` transport。
- 新增 `internal/tool/mcp` adapter。
- 调整 CLI/TUI 工具注册路径，使其读取 `config.MCPServers`。
