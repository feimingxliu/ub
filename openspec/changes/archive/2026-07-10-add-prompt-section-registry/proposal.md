## Why

ub 已经分别构造 coding-agent 指令、运行环境、workspace instructions、Git 快照、执行模式和 memory，但这些内容最终只表现为一组匿名 system messages，缺少稳定标识、顺序契约和可检查元数据。S4-08 ContextDecision、S4-07 CachePlan 与后续 eval 都依赖一个能保持现有请求语义、同时解释 prompt 组成的公共边界，因此应先完成 S4-06 的最小垂直切片。

## What Changes

- 引入 prompt section registry，为现有 prompt section 提供稳定 ID、顺序、启用状态、稳定性、来源、字符/token 估算和截断信息。
- 让主 Agent、只读 provider 请求和 no-tool 请求通过同一 builder 生成 provider-facing prompt，保持各自现有内容与可用 section 语义。
- 新增 `ub prompt inspect` 与 `ub prompt inspect --json`，在不调用 provider、不创建 session 的情况下展示当前 workspace 的 prompt manifest。
- inspect 默认只展示元数据；仅在用户显式请求时展示 section 内容，避免意外暴露 memory 或 workspace instructions。
- 增加 golden、行为和 CLI 测试，锁定默认、plan、no-tool、无 Git、无 memory 以及 section disabled 等边界。
- 更新用户文档与 roadmap 状态，但不在本 change 中实现 prompt cache、ContextDecision、语义裁剪或 eventbus。

## Capabilities

### New Capabilities

- `prompt-sections`: 定义 prompt section registry、统一 prompt builder、可检查 manifest 与 `ub prompt inspect` 的行为契约。

### Modified Capabilities

无。

## Impact

- 主要影响 `internal/app/ub/agent` 的 prompt/runtime context 构造路径和 `internal/app/ub/cli` 的命令注册。
- `internal/pkg/core/config` 的现有 prompt 配置继续复用；本 change 不新增配置字段，因此不需要更新配置 schema。
- provider-facing message 内容与顺序应保持兼容；任何差异都必须由 golden 与 fake provider 行为测试显式确认。
- 新增的 inspect 命令只读取本地配置、workspace instructions、Git 状态和 memory，不访问网络、不写 session/rollout。
