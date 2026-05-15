## Why

Sprint 2 的工具已经具备读写与执行能力，但还没有统一的执行模式、审批回调和 allow-rule 机制。I-20 需要把危险操作的决策边界落到代码里，使后续 Agent loop 和 TUI 可以安全地复用同一套权限模型。

## What Changes

- 新增 `internal/execution/`，定义 `default`、`plan`、`agent-approve` 三种模式及 mode gate。
- 新增 `internal/permission/`，定义审批 `Manager`、`Decision`、`Rule`、`Request`、`Asker` 与全局规则持久化。
- 新增 `internal/approval/`，定义 approval agent 接口与 allow / deny / unsure 决策类型。
- 支持 session 级 always-rule（内存）与 global always-rule（`~/.config/ub/permissions.yaml`）两层规则。
- 加入危险命令黑名单，黑名单命中时必须回退到 human Asker，不能被规则或 approval agent 静默放行。
- `plan` 模式拒绝 write 风险工具；exec 风险工具仍进入审批路径。
- `agent-approve` 模式下 exec 风险工具先走 approval agent，deny / unsure / error 时回退 human Asker。

## Capabilities

### New Capabilities

- `execution-policy`: 三种执行模式、模式解析、mode gate 和 write/exec 风险策略。
- `permission-manager`: 审批决策、human Asker、approval agent 回退、session/global allow-rule、黑名单与全局规则持久化。

### Modified Capabilities

无。

## Impact

- 新增包：`internal/execution/`、`internal/permission/`、`internal/approval/`。
- 复用 `internal/tool` 的 `Risk`、`Preview` 与 `PreviewableTool` 类型。
- 复用配置层已有 `execution_mode`、`approval_agent`、`permissions` 字段；本 change 不改 CLI / TUI。
- 新增/更新单元测试覆盖五种 Decision、三种 mode、approval agent 回退、黑名单优先级与 `permissions.yaml` 原子写入。
