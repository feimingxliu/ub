## Context

现有配置只加载顶层字段，并把 `profiles:` 当未知键容忍。I-13 需要在后续 agent/tool 工作前提供稳定的本地模型开发入口，因此 profile 选择、执行模式默认值和 doctor 诊断需要先落地。

## Goals / Non-Goals

**Goals:**

- 在 `Config` 中正式建模 `profiles`、`execution_mode`、`approval_agent` 和 `tools_disabled`。
- 加载顺序扩展为：默认值、全局、本地、profile 覆盖、CLI mode 覆盖。
- CLI root 支持 `--profile`、`--dev` 和 `--mode`，并让 `chat`、`config show`、`doctor` 使用同一套加载逻辑。
- `ub doctor` 用标准库 HTTP 和 `exec.LookPath` 做 CI 友好的纯文本诊断。

**Non-Goals:**

- 不实现完整 permission manager 或 mode gate；I-13 只负责配置与 CLI 覆盖。
- 不实现 MCP server 启动连通性检查。
- 不自动写入用户配置文件。

## Decisions

- **ProfileConfig 使用 Config 子集。** 避免递归 `profiles`，但保留常用运行时字段；应用 profile 时转换为临时 `Config` 后复用现有 `Merge` 语义。
- **显式 LoadOptions。** 新增 `config.LoadWithOptions`，CLI 传入 `--profile`、`--dev`、`--mode`；无 CLI 覆盖时读取 `UB_PROFILE`。
- **mode 字符串集中校验。** `default`、`plan`、`agent-approve` 在 config 包中统一校验，后续 I-20 可复用。
- **doctor 输出先保持纯文本。** `--plain` 被接受并保持同样输出；后续 TUI/着色依赖进入后再增强。
- **provider 探测按协议分支。** OpenAI/OpenAI-compatible 调 `/models`，Ollama 调 `/api/tags`，缺 API key 的远端 provider 直接报告 `NO_API_KEY` 避免误发请求。

## Risks / Trade-offs

- **bool 字段无法覆盖为 false** -> 沿用现有 `Merge` 的非零覆盖语义；I-20 权限配置会重新审视显式 false。
- **doctor 不能代表真实 Chat 可用性** -> I-13 只做轻量 endpoint 和模型列表检查，真实推理由 provider tests 与后续 run smoke 覆盖。
- **profile 与 local config 的边界** -> profile 总是最后覆盖配置文件，CLI `--mode` 再覆盖 profile，符合 requirements 优先级。
