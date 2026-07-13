## Purpose

定义 ub 的 stdio LSP client 生命周期、文档同步和文件工具变更通知能力。

## Requirements

### Requirement: Stdio LSP lifecycle

系统 SHALL 能启动 stdio LSP server，并完成基础生命周期 request 和 notification。

#### Scenario: 初始化 LSP server

- **WHEN** 调用方创建 LSP client 并初始化 workspace root
- **THEN** 系统发送 `initialize` request 和 `initialized` notification

#### Scenario: 关闭 LSP server

- **WHEN** 调用方关闭 LSP client
- **THEN** 系统发送 `shutdown` request 和 `exit` notification，并释放子进程资源

### Requirement: LSP text document synchronization

系统 SHALL 支持对文件发送 `textDocument/didOpen` 和 `textDocument/didChange`。

#### Scenario: 打开 Go 文件

- **WHEN** 调用方同步一个此前未打开的 `.go` 文件
- **THEN** 系统发送包含 URI、languageId、version 和 text 的 `didOpen`

#### Scenario: 文件工具写入后同步

- **WHEN** `write`、`edit`、`multiedit` 或 `apply_patch` 工具成功修改了仍存在的 workspace 文件
- **THEN** 系统通过 LSP notifier 主动同步该文件的最新内容
