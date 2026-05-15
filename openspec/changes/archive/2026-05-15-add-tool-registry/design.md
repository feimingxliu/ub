## Context

`docs/design.md §4` 已经把 Tool 系统的接口形状和两阶段调用流程画清楚了，但代码侧还没有任何 `internal/tool` 包，agent loop 又依赖它。Sprint 2 要在两到三个 iteration 内把 fs/grep/bash/job 都铺上来，所以先把基础设施单独落地，避免每个工具 PR 都顺手改一遍通用接口。

I-15 的边界很窄：只交付接口、Registry、Risk、Preview 协议、JSON Schema 生成；不要碰 dispatcher，不要碰 permission。

## Goals / Non-Goals

**Goals:**

- 落地 `internal/tool/tool.go` 中的 `Tool`、`Risk`、`Result`、`Preview`、`FileDiff`、`PreviewableTool`。
- 落地 `Registry`：`Register` / `Get` / `All` / `Schemas`。
- 用 `invopop/jsonschema` 从 Go 结构体生成 tool input schema，并暴露在 `Tool` 接口上。
- 单测覆盖：注册成功、查找命中/未命中、重名错误、`Schemas()` JSON 序列化稳定、`PreviewableTool` type assertion 行为正确。

**Non-Goals:**

- 不实现 dispatcher（mode gate / preview / 审批 / 回灌都是 I-20 / I-21 的事）。
- 不实现任何具体工具（fs/grep/bash 分别在 I-16/I-17/I-18）。
- 不实现 MCP 适配（I-29/I-30）。
- 不在 `provider` 包侧消费 `Schemas()`；agent loop 还没接进来，本 change 只把数据结构和方法准备好。

## Decisions

- **包结构：** 全部放在 `internal/tool/`，单文件 `tool.go` 定义接口与类型，`registry.go` 定义 Registry，`*_test.go` 单测。后续具体工具走 `internal/tool/fs/`、`internal/tool/shell/`、`internal/tool/search/` 等子包。理由：与 design.md 的现有目录草图一致，避免后续大改。
- **JSON Schema 生成走 `invopop/jsonschema`：** 已经是 `go.mod` 的依赖，Anthropic / OpenAI provider 后续也会复用同一份 schema。`Tool.Schema()` 返回 `*jsonschema.Schema` 指针（库的标准类型），而不是包内自定义包装；避免再造一层抽象。
- **`Schemas()` 返回什么：** 返回 `map[string]*jsonschema.Schema`（按工具名分组），让 provider 层根据自己协议把它转成 Anthropic / OpenAI tool 定义。`Tool.Description()` 单独暴露，因为 schema 自己没合适字段放 description。
- **`PreviewableTool` 是可选接口：** 不写成 `Tool` 的方法，因为 read/ls/grep 这些 safe 工具完全用不上 Preview。dispatcher 后面通过 `tool.(PreviewableTool)` 检测；本 change 在测试里 mock 一个实现 Preview 的工具，断言 type assertion 能命中，把契约固定下来。
- **重名注册返回 error：** 不是 panic。原因：MCP 工具是运行时注册的，运行时遇到重名应该可恢复（按 design.md §4 走 `mcp__<server>__<tool>` 前缀），本地静态注册阶段也只暴露 error，由调用方决定终止还是降级。
- **Registry 不内置锁：** 本地工具在程序启动期一次性注册，运行时只读；MCP 工具的并发注册延后到 I-29 再决定加 `sync.RWMutex`。本 change 注释里写清楚“非并发安全”，避免误用。
- **`Result.Files` 字段先保留但不强制填：** design.md 规定执行后的文件改动摘要由 dispatcher 用来更新 TUI，但本 change 还没实现 dispatcher。保留字段即可，避免后续改接口。

## Risks / Trade-offs

- **`invopop/jsonschema` 输出风格不可控** → 后续 provider 层若发现 Anthropic / OpenAI 需要不同 dialect，可在 provider 侧做转换，不动 `Tool` 接口。
- **不加锁的 Registry 后续可能引发 MCP 注册竞态** → 文档化“启动期注册”约束；I-29 再补 `sync.RWMutex`。
- **`Schema()` 直接返回 jsonschema 库类型** → 与该库强耦合；理由是它已经是项目内事实标准依赖，再加一层抽象只是空转。
