## Context

当前代码已经具备 provider 事件流、fake provider、SQLite rollout、tool registry、fs/search/bash/job 工具、execution mode 与 permission manager。`ub chat` 只做裸聊天，遇到 `tool_call` 会报错；`ub run` 仍是占位命令。I-21 需要在不引入 TUI 的前提下打通 headless agent loop，让 fake provider 单测与本地模型验证能覆盖工具调用链路。

## Goals / Non-Goals

**Goals:**

- 新增 `internal/agent`，负责 provider 调用、tool_call 收集、工具顺序执行、tool_result 回灌和 rollout 写入。
- `ub run -p` 使用配置选择 provider/model，注册本地工具，注入 execution mode 与 permission manager。
- provider request 携带工具 schema；fake、Anthropic、OpenAI/OpenAI-compatible 支持 tool_call 事件与工具消息转换。
- plan 模式下 write 工具必须只返回 tool error，不修改文件。

**Non-Goals:**

- 不实现 TUI 权限弹窗；headless V1 使用 auto-allow Asker。
- 不实现并行工具调用、loop detection、summary 或 resume。
- 不要求 Ollama 在本迭代支持工具调用。

## Decisions

1. **Agent 写入完整 message 历史**
   - Agent 维护本轮内存 `[]message.Message`，每次 assistant stream 完成后追加 assistant message；工具执行后追加 user/tool_result message。
   - 原因：现有 provider converter 已以 message model 为中心，tool_use/tool_result block 可直接复用。
   - 备选：只把 tool_result 作为 provider 私有结构传递；会破坏 rollout 和跨 provider 的统一性。

2. **dispatcher 在 agent 包内实现**
   - `Agent.runTool` 直接从 `tool.Registry` 查找工具，先对 `PreviewableTool` 调 Preview，再调用 `permission.Manager.Ask`，allow 后 Execute。
   - 原因：I-21 只需要单处顺序调度；独立 dispatcher 包会提前抽象。

3. **权限拒绝转成 tool_result error**
   - mode gate 或 human/approval 拒绝不会中断 agent loop，而是生成 `tool_result{is_error:true}` 回灌给模型。
   - 原因：模型需要看到拒绝原因并决定下一步；这也方便 plan 模式单测验证文件未改。

4. **真实 provider 只做必要 tool support**
   - OpenAI adapter 支持 Chat Completions tools、assistant tool_calls、tool role messages，并聚合 streaming tool call delta。
   - Anthropic adapter 支持 Messages API tools、tool_use block、tool_result block，并把 `input_json_delta` 聚合成完整 JSON。
   - Ollama 保持 text-only，避免在 Sprint2 末尾扩大协议面。

5. **`ub run` 复用 chat 的 session/store 习惯**
   - 创建新 session，写 user/assistant/usage/error/tool_result 事件，成功后更新 session metadata。
   - 继续 session/resume 放到后续迭代，避免和 I-33 重叠。

## Risks / Trade-offs

- [Risk] SDK streaming tool delta 类型细节容易随版本变化。→ 用 provider 包内单测覆盖转换 helper，集成测试优先走 fake provider。
- [Risk] auto-allow Asker 在 headless 下会执行危险命令。→ 仍经过 I-20 黑名单，黑名单命中时 auto-allow 只能作为显式 headless 策略；TUI human approval 后续替换。
- [Risk] `maxTurns=25` 仍可能消耗较长时间。→ provider/tool 调用均使用 context，单测用短 fake script。
- [Risk] rollout 中 tool_result 与 assistant message 可能重复表达。→ tool_result 使用独立事件类型，assistant 最终文本仍作为 assistant_message 存储，后续 TUI 可区分渲染。
