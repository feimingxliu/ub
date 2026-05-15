## Context

I-15 到 I-19 已经提供本地工具、文件写入、bash 和后台 job。当前缺口是工具 dispatcher 无法统一判断“当前模式是否允许执行”“是否需要询问用户”“哪些命令已被放行”。I-20 先提供纯后端权限模型，I-21 Agent loop 与 I-24 TUI 权限弹窗再接入它。

## Goals / Non-Goals

**Goals:**

- 用 `internal/execution` 表达三种 session 执行模式，并提供对 tool risk 的 mode gate。
- 用 `internal/permission` 表达五种 human decision、session/global allow-rule、黑名单和审批顺序。
- 用 `internal/approval` 定义 approval agent 的最小接口，供 `agent-approve` 模式自动审查 exec 命令。
- 把 global allow-rule 持久化到 `~/.config/ub/permissions.yaml`，并使用临时文件 + rename 原子写。
- 全部逻辑可通过 mock Asker / mock ApprovalAgent 做离线单测。

**Non-Goals:**

- 不实现 TUI 弹窗或 CLI 交互式审批。
- 不把权限模型接入 Agent loop 或具体工具执行；I-21 负责 dispatcher 集成。
- 不实现复杂策略语言、TTL、规则编辑或规则删除。
- 不让 approval agent 执行工具或访问 secret。

## Decisions

- **execution 包只依赖 tool 风险枚举。** `execution.Mode`、`ParseMode`、`Policy`、`Gate(mode, risk)` 独立于 permission 包，避免循环依赖。`plan + RiskWrite` 返回只读错误；其他组合交给 permission manager 判断。
- **permission.Manager 返回结构化 Result。** `Ask(ctx, Request) (Result, error)` 返回 `Decision`、`Allowed`、`Source`、`Reason`。这样 I-21 可以把 source/reason 写入 rollout，同时测试不用解析字符串。
- **审批顺序固定。** 顺序为：mode gate → 黑名单 → global rules → session rules → approval agent（仅 agent-approve + exec）→ human Asker。safe 和允许的 write 默认放行；exec 未命中规则时需要审批。
- **黑名单只强制绕过自动放行。** 命中黑名单时，global/session rule 和 approval agent 都不生效，必须询问 human Asker；human 仍可 Allow once 或 Always，但本次仍记录来源为 human。
- **规则匹配保持简单。** `Rule` 包含 `Tool`、`Command`、`CommandPrefix`。session `AlwaysCmd` 保存 exact command，`AlwaysTool` 保存 tool；global rule 保存 tool 或 command prefix。命令从 `Request.Command` 获取；若为空，则从 JSON args 的 `command` 字段提取。
- **全局规则文件是稳定 YAML。** 格式为 `global: []Rule`。`SaveGlobalRule` 读取现有文件、append、新建父目录、写临时文件、rename。解析空文件或不存在文件返回空规则。
- **approval 包不依赖 permission 包。** `approval.Request` 复制必要字段（tool、args、risk、mode、command、cwd、context summary、matched rule），permission manager 把自己的 Request 转成 approval Request，避免接口循环。

## Risks / Trade-offs

- **规则模型过窄** → V1 只支持命令和工具级放行，避免把权限系统做成策略语言；后续可扩展 Rule 字段。
- **blacklist 正则误判** → 只覆盖少数高危模式，并且只强制人工确认，不是永久拒绝。
- **atomic write 测试不能真实模拟 panic** → 通过测试 rename 前失败不会覆盖目标文件的辅助函数来覆盖关键路径；生产路径使用同一写入函数。
- **approval agent 无真实模型实现** → I-20 只定义接口和 manager 调用顺序；I-21/后续 provider 集成时再提供真实实现。
