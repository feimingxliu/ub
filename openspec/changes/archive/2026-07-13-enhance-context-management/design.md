## Context

现有实现已具备请求前 token 估算、`ContextWindowResolver`、按完整 user turn 截取的自动/手动 compact、递归分块摘要、overflow 后 compact-and-retry，以及在 rollout `Summary` 事件中保存实际 provider context。它仍把“是否 compact”简化为一个阈值判断：工具结果无法在摘要前减负，保护理由和压缩边界不可审计，overflow、未完成 turn 等路径也没有统一的决策对象。

本变更只维护发送给 provider 的 `ContextHistory`；完整 `History` 和既有 session replay 保持不变。`.references/oh-my-pi` 证明了“先决策、再裁剪/压缩、最后持久化可重建结果”的路径有效，但其 session tree、图像归档和扩展钩子不适合本次范围。

## Goals / Non-Goals

**Goals:**

- 在一次 provider 请求前，以确定性的 `ContextSnapshot` 和 `ContextDecision` 统一阈值、手动、overflow、incomplete 和 mid-turn 维护路径。
- 在不破坏完整 user turn、tool call/result 配对和 provider message 协议的前提下，先移除明确低价值或被覆盖的工具结果，再决定是否摘要。
- 让摘要稳定保留继续任务所需的目标、约束、计划/todo、关键证据、用户纠正、错误、验证和下一步；摘要模型输入仍受自身窗口预算约束。
- 记录压缩前后 token、切分边界、裁剪/保护的 tool use ID、原因、摘要模型和 retry，且恢复会话时使用记录的 provider context。
- 让 TUI 和本地 `ub prompt inspect` 可解释本次上下文维护的结果，而不泄露原始 prompt 或落盘敏感配置。

**Non-Goals:**

- 不改变 ContextWindowResolver 的优先级、增加远程遥测，或引入第三方依赖。
- 不实现 image/snapcompact、session tree/branch summary、跨 session 的全文检索或模型自动升级。
- 不以启发式删除 user/assistant 文本、当前活跃 turn，或拆开 tool_use 与其 tool_result。

## Decisions

### 1. 用纯数据的 context decision engine 取代分散阈值分支

在 `internal/context` 新增不依赖 Agent/provider 的快照、动作和规划器。`ContextSnapshot` 包含请求估算 token、有效窗口、输出保留预算、触发来源、是否存在可安全 compact 的完整 user-turn 前缀、未完成 tool pairing，以及可维护的 tool-result 清单；窗口来源/置信度继续由既有 `ContextWindowResolver` 的 runtime context event 暴露。`ContextDecision` 返回 `keep`、`prune`、`compact` 或 `compact-and-retry`、稳定 reason（`manual`、`threshold`、`overflow`、`incomplete`、`mid_turn`）、候选与受保护 tool ID 和目标 token 预算；executor 在实际 compact 后把 cut boundary 写入审计。

规划器必须确定性且可单测：未知窗口不触发自动维护；手动和已识别 overflow 可要求维护；未完成或 mid-turn 状态只能返回 `keep` 或延后决策，绝不生成破坏配对的裁剪/压缩。Agent 负责从消息、工具定义和运行时状态构造 snapshot，并执行 decision，因此 context 包不引入 Agent 循环依赖。

替代方案是继续在 `summary.go` 增加更多 boolean 分支。这会使 retry、审计和测试分别演进，不能保证相同输入得到相同操作，因此不采用。

### 2. 先按 tool-result 块安全语义裁剪，再按完整 turn 摘要

规划器只给出可裁剪 ID；Agent 在 `message.BlockToolUse` / `BlockToolResult` 层实际重写 context。一个 ID 只有在其完整 call/result 对不在受保护窗口时才可替换为固定、provider-neutral 的“已裁剪”占位结果，绝不删除 result block 或仅删除 call。占位保留 ID 和裁剪事实，以满足 provider pairing；完整输出仍在 transcript、rollout 和既有 spillover 中。

第一阶段仅采用可证明的规则：明确为空的成功 `read` / `grep` 结果，以及被后续同类、相同输入的 `read` / `grep` 完整覆盖的旧结果可裁剪；错误结果、最近验证结果、包含文件变更的结果、目标/plan/todo 相关结果、当前 user turn 和摘要保留窗口默认受保护。无法证明覆盖关系时保持原样。裁剪后重新估算：已低于预算则只持久化 prune decision，否则用已有完整-turn window 执行 compact。

替代方案是按字符截断工具输出。它易破坏 JSON、错误诊断和 tool 配对，且无法解释信息丢失，故不采用。

### 3. 摘要为结构化、锚定式的多阶段归并

保留现有按 user turn 分块和递归归并的实现，但把 compact prompt 作为命名的无工具 prompt section，并规定固定 Markdown 字段：目标与完成条件、约束/用户纠正、计划和进度、关键文件与决策、验证/错误、当前状态及下一步。已有 summary 必须合并更新，不能堆叠。

`summaryInputBudget` 继续根据实际 summary provider/model 的窗口减去输出 reserve；每个分块和归并前都估算完整 prompt。单个完整 user turn 放不进摘要预算时返回明确错误，不能在消息或 tool pairing 内切碎。summary provider 一律无 tools；工具调用或空摘要均为失败，原 context 保持可重试。

替代方案是把所有旧消息直接交给主模型或小模型。这会在原问题出现时再次超窗，并会使摘要语义和模型选择不可控，故不采用。

### 4. rollout 保存可恢复的决策审计，而不是仅保存摘要文本

扩展 `rollout.SummaryPayload`（以及其 helper）以保存 `decision`、`reason`、tokens before/after、cut boundary、pruned/protected tool IDs、summary model、耗时和 retry；新增字段均为可选以兼容旧 SQLite event。`Messages` 始终保存最终 provider context：prune-only 时保存带占位 tool result 的 context，compact 时保存 summary 加保留 suffix。恢复继续优先使用该 `Messages`，旧 event 仍回退到既有 summary message。

活动事件只显示简短原因和 token 变化；`ub prompt inspect` 的 manifest 展示 decision 元数据，默认不展示摘要正文。决策审计不保存 provider endpoint、原始 prompt、工具完整输出或 API key。

替代方案是只在日志输出原因。日志无法可靠 resume，也无法由测试或本地检查重建某次决定，因此不采用。

### 5. 保持兼容，并把执行集中于现有 prepare/Compact/recovery 入口

`prepareMessages`、`Compact` 和 `recoverContextOverflow` 都调用相同的 snapshot/decision/executor 流程。自动阈值保持 `estimated + reserve` 对有效窗口的已有语义；overflow 只允许一次 `compact-and-retry`，避免无限循环。没有可维护前缀时保留当前可读 no-op 提示；裁剪/摘要失败不篡改输入 context，并把原始 provider 错误返回。

现有配置项继续有效。本切片不新增用户可调的复杂策略阈值：先让保守规则、决策数据和测试稳定，再依据评估数据考虑配置扩展。

## Risks / Trade-offs

- [覆盖规则误判而丢失有用工具结果] → 初版只允许确定性、保守规则；错误/文件变更/最近验证/活跃 turn 默认保护，并用占位保留 ID 和审计信息。
- [provider 对 tool pairing 的约束不同] → 所有裁剪保留同一 `tool_use_id` 和 role/block 形状；为 OpenAI-compatible 与 Anthropic fake request 增加配对测试。
- [摘要模型窗口小于主模型] → 所有摘要与归并请求先预算并按完整 turn 分块；无法容纳的单 turn 明确报错。
- [新 payload 影响旧会话恢复] → 新字段 `omitempty`，恢复继续接受没有 decision/messages 的旧 Summary event。
- [多次维护增加延迟] → 在本地 planner 先 prune，只在仍超预算、手动或 overflow 时调用 summary provider；rollout 记录耗时以便评估。

## Migration Plan

1. 增加纯决策数据结构、保守 planner 和单元测试，不改变现有请求结果。
2. 将 Agent 三个维护入口接入 planner，先启用安全 prune，再复用现有摘要/恢复逻辑。
3. 扩展 summary rollout payload 和恢复测试；旧 session 维持现有回退。
4. 将 compact prompt 注册到 prompt manifest，更新设计、requirements、roadmap-v2 与配置 schema（仅当配置类型变化时）。
5. 运行 focused tests、`make test`、`make lint`、`make check` 和 resume/`/compact` smoke tests。若出现 provider pairing 或恢复回归，可关闭 prune execution（decision 仍保留）并回退到原 summary-only 流程；payload 扩展无需数据迁移。

## Open Questions

- 无阻塞问题。初版把“同类 read/grep 的完整覆盖”限定为可比较的工具输入和明确的空/重复结果；更广泛的语义相似度判断留待积累 rollout/eval 数据后再引入。
