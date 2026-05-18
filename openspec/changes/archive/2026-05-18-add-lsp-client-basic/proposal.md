## Why

Sprint 5 的 LSP 能力需要先建立稳定的 stdio JSON-RPC client，并让文件写入工具能把本地修改同步给语言服务。I-31 先支持 gopls 这类 stdio LSP server 的基础生命周期和 didOpen/didChange。

## What Changes

- 新增 `internal/lsp` client，支持 stdio JSON-RPC lifecycle。
- 实现 `initialize`、`initialized`、`textDocument/didOpen`、`textDocument/didChange`、`shutdown`/`exit`。
- 为 `write` / `edit` 工具增加可选变更通知 hook，执行成功后主动通知 LSP。
- 不实现 completion、hover、diagnostics、references 工具。

## Capabilities

### New Capabilities

- `lsp-client`: 覆盖 stdio LSP server 生命周期、文档同步和文件工具变更通知。

### Modified Capabilities

## Impact

- 新增 `internal/lsp/` 包和测试。
- 扩展 `internal/tool/fs` 的注册方式，保留现有无 LSP 调用路径。
