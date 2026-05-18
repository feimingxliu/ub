## Context

本地工具已经通过 `tool.Registry` 进入 agent loop，配置层也已有 `MCPServerConfig`。I-30 在 I-29 client 基础上补齐 HTTP/SSE，并把 MCP 工具包装成本地 `tool.Tool`，避免 agent loop 特判 MCP。

## Goals / Non-Goals

**Goals:**

- `stdio`、`http`、`sse` 三类 MCP server 都能从 `mcp_servers` 配置启动。
- 远端工具以 `mcp__<server>__<tool>` 形式注册，规避本地工具和不同 server 之间的命名冲突。
- adapter 转发 JSON Schema、description、risk 和执行结果。
- 某个 MCP server 失败时，CLI/TUI 继续启动并保留其它工具。

**Non-Goals:**

- 不接 resources/prompts。
- 不实现 MCP server 热重载。
- 不把 MCP 工具做成 PreviewableTool。

## Decisions

- **Registry 注入发生在工具注册阶段。** `ub run` 和 TUI runner 共用同一套配置驱动的 registry builder，agent loop 不感知 MCP 来源。
- **MCP 工具默认 `RiskExec`。** 远端 MCP server 的副作用边界不透明，V1 先按 exec 风险走审批；后续如果配置支持风险声明再细分。
- **失败隔离使用 warning。** 启动或初始化单个 server 失败会写入 warning，不作为整体 registry 构建失败。
- **SSE 按 MCP 经典 endpoint 模式实现。** 先 GET SSE URL 获取 endpoint event，再 POST JSON-RPC 到 endpoint，并从 SSE message 事件读取 response。

## Risks / Trade-offs

- [Risk] 不同 MCP server 对 HTTP/SSE endpoint 细节可能存在差异。实现保留明确错误，后续按真实 server 补兼容。
- [Risk] MCP 工具风险全部按 exec 处理会偏保守。V1 优先安全，牺牲一部分自动放行便利性。
