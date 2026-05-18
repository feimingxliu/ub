## Context

仓库已有 `lsp_servers` 配置类型，但没有 client。文件写入由 `internal/tool/fs` 的 write/edit 完成，agent loop 根据工具结果继续推理。I-31 需要把文件变更同步给 LSP，为 I-32 的 diagnostics/references 工具提供新鲜视图。

## Goals / Non-Goals

**Goals:**

- 启动 stdio LSP server，完成 initialize/initialized/shutdown/exit。
- 支持对单个文件 didOpen 和 didChange。
- 提供 Manager/Notifier，使 write/edit 成功后可以同步当前文件内容。
- 测试覆盖基础 lifecycle 和文件工具通知 hook。

**Non-Goals:**

- 不提供 diagnostics/references 工具。
- 不实现 completion、hover、rename、code action。
- 不做真实文件系统 watch；只在 ub 工具执行后主动通知。

## Decisions

- **LSP client 使用独立 JSON-RPC loop。** LSP 需要后台接收 notification 和异步 response，不能复用 I-29 的单请求阻塞模型。
- **文档版本由 Manager 维护。** 每次 didOpen/didChange 对同一 URI 递增版本，避免工具层关心 LSP 细节。
- **fs 工具只依赖最小 notifier 接口。** `internal/tool/fs` 不持有 LSP client 具体类型，保持本地工具在无 LSP 配置时行为不变。

## Risks / Trade-offs

- [Risk] gopls diagnostics 可能是异步延迟。I-31 只保证 didOpen/didChange 发送成功，查询工具在 I-32 处理等待策略。
- [Risk] 不做外部文件 watcher 意味着 ub 之外的编辑不会自动同步。V1 只覆盖 ub 工具写入路径。
