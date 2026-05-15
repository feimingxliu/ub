## Why

`add-tool-registry` 只交付了 Tool 接口与注册中心，没有任何具体工具。Sprint 2 要让 agent 能读 / 列 / 改写工作区文件，fs 工具组是最先要落的一批；它们也是 V1 用户最依赖的能力（read/write/edit 几乎每轮对话都会用）。先把 5 个 fs 工具落地，把 `PreviewableTool` 协议在真实场景下跑通，后续 grep/bash 与 agent loop 都能直接消费。

## What Changes

- 新建 `internal/tool/fs/` 子包，实现五个工具：
  - `read(path, offset?, limit?)`：返回带行号的文本，`Risk=safe`。
  - `ls(path)`：返回目录下条目（文件 / 子目录），`Risk=safe`。
  - `glob(pattern)`：返回匹配路径列表，`Risk=safe`，使用 `github.com/bmatcuk/doublestar/v4`。
  - `write(path, content)`：覆盖写，`Risk=write`，实现 `PreviewableTool`，Preview 返回创建 / 修改的 unified diff。
  - `edit(path, old, new, replace_all?)`：精确替换，`Risk=write`，实现 `PreviewableTool`，Preview 使用 `github.com/aymanbagabas/go-udiff` 计算 unified diff。
- 引入 `fs.Register(reg *tool.Registry, root string) error` 一次性把五个工具注册到 Registry，调用方传入 workspace 根。
- 严格的安全约束：所有 `path` MUST clean 后落在 root 内，跳出根目录返回错误；接受绝对路径但仍要求落在 root 内。
- 单测覆盖：每个工具 happy path、`read` 的 offset/limit 边界、`write`/`edit` Preview 的 diff 字符串断言（不改动磁盘）、`edit` 的 old 缺失 / 多匹配未 `replace_all` 错误、路径跳出 root 拒绝。
- 在 `go.mod` 中新增依赖 `github.com/bmatcuk/doublestar/v4` 与 `github.com/aymanbagabas/go-udiff`。

## Capabilities

### New Capabilities

- `fs-tools`：read / ls / glob / write / edit 五个工具的输入 schema、行为、Preview 协议与 workspace 沙箱规则。

### Modified Capabilities

无（Registry 与 PreviewableTool 由 `add-tool-registry` 提供，本 change 只消费，不改动其 spec）。

## Impact

- 新增 `internal/tool/fs/` 目录与子包文件、单测。
- `go.mod` 引入两个新依赖：doublestar v4、go-udiff。
- 不修改 cli / provider / rollout / config 等已有包；`fs.Register` 由 agent runtime（I-21）接入，本 change 暂不在 `ub` 主进程里调用。
- 不引入新的配置字段；workspace 根作为函数参数显式传入，由调用方决定（V1 计划用进程 cwd）。
