## Context

I-31 提供 LSP client 和 didOpen/didChange 同步。I-32 在此基础上把 LSP 查询能力包装成本地工具，沿用 agent loop 的 Tool 接口、权限和 rollout 记录，不引入新的 provider 协议。

## Goals / Non-Goals

**Goals:**

- `diagnostics(file?)` 返回全部或单文件诊断，包含路径、行列、严重级别和消息。
- `references(file,line,col)` 调用 `textDocument/references` 并返回位置列表。
- 两个工具执行前都同步目标文件；diagnostics 无 file 时同步已知/可路由文件后返回缓存诊断。
- 工具只在已配置并成功启动 LSP server 时注册。

**Non-Goals:**

- 不实现 rename、code action、completion、hover。
- 不把 LSP 查询做成 provider 内置能力。

## Decisions

- **工具放在 `internal/tool/lsp`。** 它们和其它本地工具一样实现 `tool.Tool`，agent loop 无需新分支。
- **行列输入使用 1-based。** 面向模型和用户更自然；adapter 内部转换为 LSP 的 0-based position。
- **diagnostics 使用 LSP publish 缓存。** LSP 标准诊断由 server 主动 publish，工具读取 Manager 中的最近缓存；同步后短暂等待异步 publish。

## Risks / Trade-offs

- [Risk] diagnostics 的异步等待可能仍拿不到慢 server 的结果。工具会返回当前缓存，并在无结果时给出清晰文本。
- [Risk] references 对未建立索引的 server 可能返回空。工具保留 LSP 返回的错误，方便模型判断后续动作。
