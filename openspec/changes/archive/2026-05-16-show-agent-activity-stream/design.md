## Context

当前 Agent 已能通过事件回调向 TUI 推送文本增量、工具开始/结束、权限结果和错误，但这些事件语义偏粗：工具调用只显示 `started/finished`，缺少参数摘要、审批状态和失败原因；provider 层也没有统一表达模型 reasoning/thinking 的事件。结果是用户看到最终回答，却很难理解中间过程是否正常推进。

这次变更横跨 provider、agent 和 TUI。目标是参考 Claude Code、Codex、opencode 的“活动流”体验，用紧凑、可扫描、可折叠的方式展示模型正在做什么，同时保持脚本模式输出稳定。

## Goals / Non-Goals

**Goals:**

- 展示模型可公开的 thinking/reasoning 摘要、工具调用生命周期、权限审批结果和错误状态。
- 将活动过程结构化为 Agent runtime events，TUI 只负责渲染，不从文本里猜测状态。
- 让工具输入/输出摘要安全、短小、宽度自适应，避免把完整 JSON、密钥或超长输出直接刷到聊天区。
- 保持 `ub run` 默认 stdout 只输出最终 assistant 文本，活动流不破坏脚本兼容。

**Non-Goals:**

- 不伪造或尝试暴露 provider 没有返回的隐藏推理链。
- 不在 V1 引入复杂动画、全屏任务面板或持久化 activity timeline。
- 不改变工具执行顺序、权限模型或 provider 选择策略。

## Decisions

1. **Provider 增加可选 reasoning delta 事件**

   在 `provider.EventType` 中增加 `reasoning_delta`（命名可在实现时微调），只承载 provider 明确返回、允许展示的 reasoning summary / thinking text。Anthropic thinking 与 OpenAI-compatible `reasoning_content` / `reasoning` / `thinking` 字段应透传；当前协议没有可用字段时不产生该事件。fake provider 必须支持脚本化该事件用于测试。

   备选方案是让 adapter 把 reasoning 混入普通 text delta。这个方案会污染 assistant 正文，无法在 TUI 中折叠或降噪，因此不采用。

2. **Agent 统一输出 Activity 事件**

   在 Agent event 层增加结构化 activity payload，覆盖：
   - `thinking`：provider reasoning delta 或 “模型正在思考”状态；
   - `tool_queued` / `tool_running` / `tool_done` / `tool_failed`：工具生命周期；
   - `permission`：approval agent 与 human 的审批结果；
   - `notice`：超时、中断、max turns 等非正文状态。

   保留现有 `DeltaText` 和 `Done`，避免 TUI 和 headless 行为被迫大改。工具参数摘要由 Agent 在收到 tool call 后生成，TUI 不直接解析原始 JSON。

3. **TUI 活动流复用消息列表，但使用独立 role/style**

   新增 activity/system-like 消息渲染，不作为 assistant 正文。参考 opencode 的降噪方式，同一个 tool call 通过 `tool_use_id` 原地更新活动行，而不是把 queued/running/done 都追加成日志。默认显示一行摘要，例如：

   - `◇ thinking: checking repository context`
   - `$ read main.go running`
   - `$ bash approved by approval_agent: read-only command`
   - `$ edit main.go done: 1 file changed`

   详细参数和长输出只显示截断摘要；后续可在已有 diff/permission modal 基础上扩展折叠详情。这样能复用当前滚动、宽度换行和 message list 测试，不引入新布局层。

4. **安全摘要优先**

   工具输入摘要只展示工具名、路径、命令首行、cwd、文件数量等白名单字段。疑似 secret 的字段名和值（如 `api_key`、`token`、`password`、`Authorization`）必须遮蔽。工具输出只显示状态和短摘要，完整内容仍通过 tool_result 回灌模型或 diffview 展示。

5. **保持 headless 兼容**

   `ub run` 默认不打印 activity events。未来可新增 `--verbose` 或 `--trace` 输出到 stderr，但不属于本 change 的必需范围。

## Risks / Trade-offs

- **Risk: “思考过程”被误解为完整隐藏 CoT** → 只展示 provider 显式返回且可展示的 reasoning/thinking delta；没有该数据时显示状态摘要，不编造内容。
- **Risk: 活动流刷屏影响阅读** → 同一工具调用原地更新，默认只显示动作短标题；长文本截断，工具详情后续走折叠面板。
- **Risk: 参数摘要泄露敏感信息** → 使用字段白名单和 secret pattern 遮蔽；测试覆盖常见敏感字段。
- **Risk: provider 支持差异大** → provider reasoning event 是可选能力；无支持时仍展示工具/权限 activity，不影响最终回答。
- **Risk: 事件模型膨胀** → 保留少量通用 event kind，避免为每个工具定制 TUI 类型。
