## ADDED Requirements

### Requirement: Stdio MCP server lifecycle

系统 SHALL 能通过配置的命令和参数启动一个 stdio MCP server，并在客户端关闭时终止或释放该子进程。

#### Scenario: 启动并初始化 stdio server

- **WHEN** 调用方用合法命令创建 MCP stdio client 并执行初始化
- **THEN** 系统完成 `initialize` request 和 `notifications/initialized` notification

#### Scenario: 关闭 stdio server

- **WHEN** 调用方关闭 MCP stdio client
- **THEN** 系统释放 stdio pipe 并等待或终止关联子进程

### Requirement: MCP tools list and call over stdio

系统 SHALL 支持通过 stdio MCP client 调用 `tools/list` 和 `tools/call`。

#### Scenario: 列出工具

- **WHEN** MCP server 返回工具定义列表
- **THEN** 系统返回每个工具的 name、description 和 inputSchema

#### Scenario: 调用工具

- **WHEN** 调用方传入工具名和 JSON 参数调用 MCP 工具
- **THEN** 系统发送 `tools/call` request 并返回 MCP call result 的 content 与 isError
