## Why

I-31 只能同步 LSP 文档，模型还不能主动读取语言服务信息。I-32 需要把 diagnostics 和 references 暴露为工具，让 agent 能看到代码错误并按位置查询引用。

## What Changes

- 新增 LSP diagnostics 和 references 查询能力。
- 新增本地工具 `diagnostics(file?)` 与 `references(file,line,col)`。
- 工具执行前先同步目标文件的本地内容，保证查询基于最新编辑。
- 不实现 rename、code action、completion、hover。

## Capabilities

### New Capabilities

- `lsp-tools`: 覆盖 diagnostics/references 工具的输入、同步和输出行为。

### Modified Capabilities

## Impact

- 扩展 `internal/lsp` Manager 查询 API。
- 新增 `internal/tool/lsp` 工具包。
- 调整 CLI/TUI 工具注册路径，在存在 LSP 配置时注册 LSP 工具。
