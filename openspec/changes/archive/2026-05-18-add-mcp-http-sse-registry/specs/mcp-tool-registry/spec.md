## ADDED Requirements

### Requirement: MCP HTTP and SSE transports

系统 SHALL 支持通过配置创建 HTTP 和 SSE MCP client，并执行 MCP 初始化、工具列表和工具调用。

#### Scenario: HTTP server tools list

- **WHEN** 配置了 type 为 `http` 的 MCP server URL
- **THEN** 系统通过 HTTP JSON-RPC 请求完成初始化并取得工具列表

#### Scenario: SSE server tools call

- **WHEN** 配置了 type 为 `sse` 的 MCP server URL 且 server 提供 endpoint event
- **THEN** 系统通过 endpoint POST 发送工具调用，并从 SSE message event 读取调用结果

### Requirement: MCP tools are registered as local tools

系统 SHALL 把已连接 MCP server 暴露的工具注册为本地 `tool.Tool`。

#### Scenario: MCP tool naming

- **WHEN** server `filesystem` 暴露工具 `read_file`
- **THEN** Registry 中出现名为 `mcp__filesystem__read_file` 的工具

#### Scenario: MCP server startup failure isolation

- **WHEN** 多个 MCP server 配置中有一个启动或初始化失败
- **THEN** 系统保留本地工具和其它成功 MCP server 的工具，并只报告失败 server 的 warning
