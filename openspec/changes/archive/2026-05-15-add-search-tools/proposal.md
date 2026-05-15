## Why

`add-tool-registry` 与 `add-fs-tools` 让 agent 能读 / 列 / 写 / 改文件，但还不能在工作区里按正则搜代码。代码搜索是 V1 用户最常用的能力（“找一下哪里调用了 X”），缺了它 agent 就要靠不断 `ls` + `read` 兜圈子。先把 grep 工具落地，性能优先用本机的 ripgrep，但保留一份内置 Go 实现以保证测试与无 rg 的环境也能跑。

## What Changes

- 新建 `internal/tool/search/` 子包，实现一个工具：
  - `grep(pattern, path?, include?)`：在 workspace 内按正则 `pattern` 搜文本，返回 `path:line:match` 列表，`Risk=safe`。
- 双后端：优先调外部 `rg`（PATH 上存在时）；缺失时退回内置 walker + `regexp` + 行扫描；两者输出格式 MUST 完全一致，路径相对 workspace root。
- 沙箱沿用 `add-fs-tools` 的 `resolve(root, path)` 规则；二进制文件按 NUL 字节探测后跳过。
- 暴露 `search.Register(reg *tool.Registry, root string) error` 单独注册 grep，避免与 `fs.Register` 耦合，方便后续按配置 disable。
- 引入一个可注入的后端探测接口，让测试可以强制走内置实现，保持 deterministic；同时单独覆盖一条“rg 可用时调用其 args 与解析输出”的测试路径，使用 fake 探测器避免依赖系统 rg。
- 单测：happy path、`include` glob 过滤、跳出 root 拒绝、二进制文件跳过、内置实现 deterministic、rg 后端通过注入探测器调用并解析输出。

## Capabilities

### New Capabilities

- `search-tools`：grep 工具的 input schema、双后端策略、输出格式、沙箱与过滤规则。

### Modified Capabilities

无（沿用 `tool-registry` 接口与 `fs-tools` 的沙箱设计，不修改两者 spec）。

## Impact

- 新增 `internal/tool/search/` 目录与子包文件、单测。
- 不引入新的第三方依赖；`doublestar` 已经由 `add-fs-tools` 引入，用于 `include` glob 过滤。
- 不修改 cli / provider / rollout / config 等已有包；`search.Register` 由 agent runtime（I-21）接入。
- 显式不在范围：sourcegraph 远程搜索、多行匹配 / 上下文行、bash 工具（I-18）。
