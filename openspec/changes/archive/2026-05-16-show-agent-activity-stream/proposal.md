## Why

当前 TUI 主要展示最终回复和简短工具状态，用户难以判断模型正在思考、准备调用哪些工具、工具执行是否卡住，以及审批前后的上下文。参考 Claude Code、Codex、opencode 的交互体验，需要把模型活动过程以可扫描、不过度打扰的方式呈现出来。

## What Changes

- 在 Agent 运行事件中补充结构化 activity 事件，覆盖模型思考/推理摘要、工具调用准备、工具输入摘要、工具执行结果摘要、权限审批结果和错误。
- TUI 新增活动流渲染：以紧凑、可折叠、按时间顺序的系统/工具消息展示过程，不直接刷屏完整 JSON 或大段工具输出。
- Provider runtime 支持可选 reasoning/thinking delta：仅展示 provider 明确返回的 reasoning summary / thinking text；不伪造或暴露不可获得的隐藏推理链。
- 工具调用展示从简单 `tool started/finished` 升级为更清晰的生命周期：queued / running / approved / denied / done / failed，并显示安全的参数摘要。
- 保持 headless `ub run` 的输出兼容：默认 stdout 仍只输出最终 assistant 文本，活动流通过事件回调/TUI 展示，不污染脚本输出。

## Capabilities

### New Capabilities

- 无

### Modified Capabilities

- `provider-runtime`：新增可选 reasoning/thinking 事件语义，供支持该能力的 provider 透传可展示的思考摘要。
- `agent-loop`：扩展 Agent runtime event contract，输出结构化活动事件与工具生命周期信息。
- `tui-shell`：新增活动流展示要求，优雅呈现模型思考、工具调用过程、权限结果和错误状态。

## Impact

- 影响 `internal/provider` 事件类型与各 provider adapter 的事件转换逻辑。
- 影响 `internal/agent` 的事件模型、tool dispatch 周期和测试。
- 影响 `internal/tui` 的消息列表、工具状态渲染、滚动和宽度适配测试。
- 更新 OpenSpec 主 specs 与文档，补充 fake provider 脚本事件以便离线测试活动流。
