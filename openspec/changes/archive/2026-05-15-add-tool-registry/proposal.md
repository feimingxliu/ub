## Why

Sprint 2 的第一步要把工具基础设施搭起来：后续 fs/grep/bash/job 工具、MCP 适配以及 agent loop 的 tool 调用都依赖一个统一的 `Tool` 接口和注册中心。先单独落地基础设施，把接口形状、Risk 等级、PreviewableTool 协议固定下来，后面每个具体工具都能直接套模板，不用反复改通用代码。

## What Changes

- 新建 `internal/tool/` 包：定义 `Tool` 接口、`Risk`（`safe` / `write` / `exec`）、`Result`、`Preview`、`FileDiff`、可选接口 `PreviewableTool`。
- 新建 `Registry`：本地静态注册（`Register` / `Get` / `All`），重名注册返回错误；提供 `Schemas()` 给 agent loop 把工具 input schema 一次性传给 provider。
- 用 `github.com/invopop/jsonschema` 从 Go 结构体生成 tool input 的 JSON Schema，供 provider 请求体使用。
- 单测覆盖：注册成功 / 查找 / 重名报错 / `Schemas()` JSON 序列化 / `PreviewableTool` 可通过 type assertion 检测。
- 暂不实现 dispatcher、permission 审批、具体工具。

## Capabilities

### New Capabilities

- `tool-registry`：统一的工具接口、Risk 模型、Registry 注册查找、Preview 协议与 JSON Schema 生成。

### Modified Capabilities

无。

## Impact

- 新增 `internal/tool/` 目录与单测文件。
- 不修改 provider、rollout、session、cli 等已有包。
- 不引入新的第三方依赖；`invopop/jsonschema` 已经在 `go.mod` 中（用于 provider 工具 schema 同样的库，避免后续 I-21 再换）。
- 不改 `docs/` 既有内容；本 change 的 spec 文件作为 `tool-registry` capability 的首版 spec。
