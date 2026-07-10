## Context

当前窗口选择分散在 CLI 的 `modelinfo.Resolve`、provider `CapsForModel` 和 Agent 的 `effectiveMaxContextTokens` 中。显式 `max_context_tokens` 能覆盖 provider 默认值，但未知模型通常只能使用 provider 类型的宽泛默认值；Agent 虽然已经识别 context overflow 并回灌 input usage 给 token estimator，却不会把这些运行事实用于修正窗口。

该改动横跨 provider/model 解析、Agent summary 阈值和 XDG 派生状态，需要先定义统一优先级与失败语义。缓存只保存端点标识、模型名和 token 数，不保存 prompt、消息或密钥。

## Goals / Non-Goals

**Goals:**

- 为主 Agent 提供单一、可解释且并发安全的窗口解析入口。
- 保留用户显式配置的最高优先级，同时允许真实 overflow 修正非显式静态值。
- 利用成功 input usage 识别过小的静态候选，并按 provider endpoint/model 隔离持久观察。
- 让自动 summary、manual compact、overflow recovery 和 TUI context ratio 使用同一个动态结果。
- 缓存损坏或不可写时安全回退，不把派生状态升级为运行前置条件。

**Non-Goals:**

- 不实现 S4-08 的 ContextDecision、semantic pruning 或统一 compact reason event。
- 不扩展 `ub prompt inspect`；该命令继续保持不初始化 provider 的纯本地契约。
- 不为 summary/auto-memory/approval 等辅助模型建立完整的独立窗口学习生命周期。
- 不修改 provider 请求参数、output token 策略或配置 schema。

## Decisions

### 1. 使用独立的 `internal/pkg/llm/contextwindow` 包

该包提供 `Resolver`、`Resolution`、`Key`、`Observation` 与可替换 `Store`。它不依赖 CLI、Agent 或 workspace 包；文件存储接收调用方传入的根目录，因此窗口选择规则可以独立单测，CLI 只负责从 `paths.StateRoot()` 创建默认存储。

相比继续扩充 `modelinfo.Info`，resolver 需要处理运行期观察和持久状态，生命周期已经超出静态模型元信息职责。相比把逻辑直接放进 Agent，独立包也便于未来 provider probe、doctor 和 ContextDecision 复用。

### 2. 显式配置绝对优先，真实观察只修正非显式候选

resolver 输入包含显式配置、模型元信息和 provider caps 三类候选，并返回 `max_tokens`、`source` 和 `confidence`：

1. 有效显式配置：`source=config`、`confidence=exact`；历史观察不覆盖它。
2. 能从 overflow 错误提取明确最大值：`source=learned_overflow`、`confidence=high`。
3. 无明确数值但出现 overflow：把请求估算记录为保守上界；仅当它高于已成功 usage 且小于静态候选时使用，`confidence=low`。
4. 否则使用模型元信息，再回退 provider caps。
5. 成功 input usage 大于当前非显式候选时，至少把结果提高到已接受值，标记 `source=learned_usage`、`confidence=low`。
6. 所有候选都未知时返回零值和 `source=unknown`，保持当前“只上报 used tokens、不按阈值 compact”的行为。

明确 overflow 值若小于同一 key 已成功接受的 input usage，视为冲突观察，不用于缩小窗口。这样可以避免错误文本解析或后端同模型热更新把缓存毒化。

### 3. 缓存按规范化 endpoint 与 model 隔离

cache key 包含 provider 名、规范化 base URL 和完整 model ID。endpoint 规范化会去除 userinfo、query、fragment 和尾部 `/`，避免查询参数中的凭据落盘；文件名使用规范化 key 的 SHA-256 摘要。每个 key 使用独立 JSON 文件，写入采用同目录临时文件加 rename，权限为 `0600`。

使用每 key 文件而不是单个全局 JSON，可以降低多进程写入互相覆盖的范围。MVP 不引入跨进程文件锁；文件是可丢弃派生状态，原子 rename 保证读取方不会看到半写内容。

### 4. Resolver 在 Agent 生命周期内共享并即时更新

CLI 在 provider/model 已解析后创建 resolver，并通过 `agent.Options` 注入。Factory 与 child agent 共享同一并发安全实例；TUI 切换 model 后创建的新 Agent 使用新 key 的 resolver。

主 provider 返回 usage 时，Agent 同时校正 token estimator 并记录 accepted input。Chat 创建错误、stream 错误或 close 错误被识别为 context overflow 时，Agent 在执行 recovery compact 前记录 overflow，使本次 recovery 和后续请求立即使用修正后的窗口。

Agent context event 增加窗口 `source` 和 `confidence` 元数据；现有 used/max/ratio 字段保持兼容，TUI 仍按 max/ratio 更新状态。

### 5. 持久化失败不改变请求结果

cache load 失败时 CLI 记录 warning，并创建无存储 resolver；observe 保存失败时 Agent 记录 warning，但保留内存观察并继续当前请求。解析器只接受正整数和明确位于 context/max/limit 语境中的 token 数，避免把 input/output token 数误识别为窗口。

## Risks / Trade-offs

- [无数字 overflow 只能形成估算上界，可能偏保守] → 标记低置信度，且不得低于已成功 usage；后续明确 overflow 可替换它。
- [同 endpoint/model 的后端能力发生变化会留下旧观察] → 显式配置可立即覆盖，cache 可安全删除；TTL/主动 probe 留给后续切片。
- [多进程同时更新同一个 key 可能最后写入者覆盖另一观察] → 每次保存前重新加载并合并单调字段，文件原子替换；MVP 不增加平台相关锁依赖。
- [usage 只证明下界而非真实最大值] → 仅用于抬高已被事实否定的静态候选，并保持低置信度，不声称精确窗口。

## Migration Plan

1. 新包与 Agent 字段均为新增，保留 `MaxContextTokens` 整数回退，现有直接构造 Agent 的调用和测试继续工作。
2. CLI 主路径逐步注入 resolver；没有缓存文件时行为等价于“显式模型配置，否则 provider caps”。
3. 首次观察后自动创建 `$XDG_STATE_HOME/ub/context-windows/`；无需迁移旧数据。
4. 回滚代码后遗留 JSON 不会被旧版本读取，可直接删除。

## Open Questions

- 是否为观察增加默认 TTL，以及是否通过 `ub doctor`/未来 `ub context inspect` 提供清理与诊断，留到 ContextDecision/可观测性切片决定。
- summary provider 的独立窗口 resolver 是否值得在本切片之后补齐，取决于主模型与 summary 模型分离后的真实 overflow 反馈。
