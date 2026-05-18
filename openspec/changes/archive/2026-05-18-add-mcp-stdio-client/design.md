## Context

当前仓库已经有本地 Tool 接口和 Registry，但还没有 MCP client。配置类型中已经预留 `mcp_servers`，设计文档也把 `internal/mcp` 作为 Sprint 5 的扩展点。I-29 只落 stdio transport，先把协议、进程生命周期和工具调用模型稳定下来。

## Goals / Non-Goals

**Goals:**

- 通过 `exec.CommandContext` 启动 stdio MCP server。
- 用 JSON-RPC 2.0 + `Content-Length` frame 完成 request/response 和 notification。
- 暴露 Go API：初始化、列出工具、调用工具、关闭客户端。
- 在测试中用受控子进程模拟 MCP server，避免依赖网络或全局 npm 环境。

**Non-Goals:**

- 不实现 HTTP/SSE 传输。
- 不把 MCP 工具注册进 ub 的本地 `tool.Registry`。
- 不实现 resources/prompts。

## Decisions

- **手写最小 JSON-RPC framing。** 目前不引入 MCP SDK，避免额外依赖和版本漂移；frame 读写逻辑保持在 `internal/mcp` 内部。
- **Client API 使用中性结构。** `ToolSpec` 保留 `inputSchema` 的原始 JSON，I-30 adapter 再转换为 `jsonschema.Schema`。
- **测试使用 test binary fixture。** roadmap 提到 filesystem server e2e，但本地环境不保证有 `npx` 和 npm 包；fixture 可以验证同一协议路径，真实 server 手测留给后续集成环境。

## Risks / Trade-offs

- [Risk] MCP server 可能在响应前发送通知或 server-side request。当前客户端会忽略无关通知，V1 不实现反向 request handling。
- [Risk] stdio 子进程 stderr 可能阻塞。实现需要把 stderr 接到 `io.Discard` 或可消费 writer。
