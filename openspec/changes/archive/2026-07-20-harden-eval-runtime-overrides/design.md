## Context

Eval runner 会启动当前 executable 的 `ub --mode full-access run`，子进程仍从用户全局配置读取 provider 凭据和模型定义，同时把 session/state/data 隔离到临时目录。现有 task schema 没有运行时配置字段，runner 设置的 `UB_EVAL=1` 也没有消费者，因此 `compact-continuation` 是否触发压缩完全取决于 provider/model 的原生上下文窗口。

直接复制或重写用户全局配置会把凭据写入临时目录，并受 provider map 整体替换语义影响；让 eval 修改普通 workspace `.ub/config.yaml` 也无法安全地只覆盖模型窗口而保留 provider 定义。

## Goals / Non-Goals

**Goals:**

- 让 task 能声明一组最小、受校验的 context runtime overrides。
- overrides 只影响 eval 启动的 headless 子进程，不修改用户配置或普通 session。
- 在报告中记录实际应用值，使失败样本可复现。
- 让内置 compact task 在常见 provider/model 上确定性进入压缩路径。

**Non-Goals:**

- 不开放任意 config YAML、环境变量或 provider 凭据覆盖。
- 不为普通 `ub run` 增加公开的实验配置接口。
- 不在本 change 中实现批量/model matrix runner。
- 不保证不同 tokenizer 的压缩前后 token 数完全相同。

## Decisions

### 1. 在 task schema v1 中增加可选 `runtime` 对象

首批字段固定为：

- `max_context_tokens`
- `context.trigger_ratio`
- `context.keep_recent_turns`

所有字段省略时保持当前行为。数值必须满足普通 context 配置的有效范围；`runtime` 及其 `context` 子对象中的未知字段直接拒绝，避免拼写错误被静默忽略。

选择继续使用 schema version 1，是因为这是向后兼容的可选扩展；已有 task 无需迁移。相比允许任意 config map，强类型字段能避免 eval 绕过权限、启用 hooks 或改变 provider 安全边界。

### 2. 使用隐藏的 `run` CLI 参数承载隔离覆盖

Eval runner 把 runtime 字段转换成隐藏的 `ub run` 参数。`runAgent` 在加载正常用户配置、完成 provider/model 选择后应用覆盖：context 参数覆盖本次构造使用的 `ContextConfig`，`max_context_tokens` 作为本次模型角色的显式窗口传入 ContextWindowResolver 和 Agent。

该方案不会复制含凭据的配置，也不会改变全局 config merge 规则。隐藏参数仍是显式 argv，便于单测检查，不需要保留含义模糊的 `UB_EVAL` 模式开关。

### 3. 报告回显规范化后的 runtime overrides

`Report` 增加 `runtime` 字段；仅包含 task 实际声明并通过校验的值。文本报告在存在覆盖时显示一行摘要，JSON 始终给出稳定结构。报告描述的是 runner 请求的有效覆盖，不回显 provider 凭据或完整用户配置。

### 4. compact task 使用保守的小窗口和较低阈值

内置 task 显式设置窗口、trigger ratio 和 recent-turn 保留数，使首轮仍可完成读取，第二轮加入历史后超过阈值并压缩较早 turn。测试使用 fake process/rollout 锁定参数传播；真实 pilot 再校准具体窗口，若某 provider 的 system/tool schema 已大于窗口，则该样本应作为 task/runtime 配置错误暴露，而不是静默放宽。

## Risks / Trade-offs

- **[窗口过小导致首轮请求即溢出]** → 先用 prompt/token 估算选择保守值，并在跨模型 pilot 中校准；报告保留 runtime 值和 stderr。
- **[隐藏参数被普通用户直接调用]** → 参数不出现在 help，且只改变当前 `run` 进程，不提供持久化或权限绕过。
- **[task schema v1 增加字段影响旧解析器]** → 字段可选，旧 task 行为不变；当前 ub 是 task 的唯一权威执行器。
- **[不同 tokenizer 导致触发边界差异]** → 使用足够大的阈值裕量，并以 rollout 的 ContextDecision 作为最终前置条件断言。

## Migration Plan

1. 扩展 task 类型、校验和报告结构。
2. 增加隐藏 run 参数并从 eval runner 传递。
3. 更新 compact task 和测试，运行跨模型 pilot 校准值。
4. 若需要回滚，移除 task runtime 字段和隐藏参数即可；旧 task 不受影响。

## Open Questions

无。批量运行、重复次数和矩阵报告结构由 pilot 数据驱动的后续 change 决定。
