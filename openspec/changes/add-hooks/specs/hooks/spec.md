# hooks Specification (delta: add-hooks)

## ADDED Requirements

### Requirement: Hooks 配置 schema

系统 SHALL 在顶层 `Config` 之下新增 `hooks` 字段,容纳四类触发点:`pre_tool_call`、`post_tool_call`、`pre_user_turn`、`post_user_turn`。每类下是一个有序 hook 列表;每条 hook MUST 包含:

- `command: []string`:argv;`command[0]` 为可执行文件,其余为参数
- 可选 `tools: []string`:仅当事件携带 `tool_name` 且该列表非空时,只有匹配的 tool 才触发 hook;列表为空时对所有 tool 触发
- 可选 `timeout: duration`:0 时取默认 10 秒;最大值 60 秒,超过 MUST 被钳制
- 可选 `on_failure: string`:取值 `warn`(默认)或 `block`;`block` 仅在 `pre_tool_call` 阶段会真正阻止 tool 调用,其他阶段总是被视为 `warn`
- 可选 `env: []string`:子进程允许从父进程继承的 env key 白名单,未列出的 env MUST 不被传给子进程;白名单为空时默认仅传 `PATH`

### Requirement: pre_tool_call 触发

系统 SHALL 在 `agent.runTool` 接到 tool call 后、permission 检查之前依序运行所有匹配的 `pre_tool_call` hook。子进程 stdin MUST 写入 JSON:

```json
{
  "event": "pre_tool_call",
  "session_id": "<sid>",
  "turn": <n>,
  "tool": {"name": "<n>", "use_id": "<id>", "args": <raw>}
}
```

若某条 hook 退出码非 0 且 `on_failure=block`,agent MUST 跳过 Execute,把 hook 的 stderr(截断到 4KB)作为 `tool.Result{IsError:true, Content:...}` 回灌模型,并 MUST 发一个 `Activity{Kind:hook, Status:blocked, ...}` 事件供 TUI 渲染。

#### Scenario: 命中 tools filter

- **GIVEN** 配置中 `pre_tool_call` hook 的 `tools` 为 `[edit, write]`
- **WHEN** agent 调用 `multiedit` 工具
- **THEN** 该 hook MUST 不被触发

#### Scenario: block 失败阻止 tool

- **GIVEN** `pre_tool_call` hook 配置为 `on_failure: block`,且其命令以 exit 1 退出且 stderr 含 "refused"
- **WHEN** agent 试图调用 `bash`
- **THEN** `tool.bash.Execute` MUST 不被调用,模型见到的 `tool_result` MUST 是 IsError 且 Content 包含 "refused"

### Requirement: post_tool_call 触发

系统 SHALL 在 `agent.runTool` 拿到 result 之后(无论是否 IsError)依序运行 `post_tool_call` hook。stdin JSON MUST 额外包含 `result` 字段(其 `content` 字段是发送给模型的字符串)。`on_failure` 在该阶段一律视为 `warn`:hook 失败 MUST 只发一条 `Activity{Kind:hook, Status:failed}` 事件,不修改 tool result。

#### Scenario: post hook 失败仅警告

- **GIVEN** `post_tool_call` hook 命令 exit 1
- **WHEN** 任何 tool 成功执行
- **THEN** tool result MUST 原样返回给模型,且 hook 失败 MUST 体现在 activity 流中

### Requirement: pre_user_turn / post_user_turn 触发

系统 SHALL 在 `agent.Run` 接到 user prompt 后、首次向 provider 发请求之前运行所有 `pre_user_turn` hook;在 `agent.Run` 返回前(无论成功 / 失败 / 取消)运行 `post_user_turn` hook。两者 stdin JSON 不含 `tool` 字段;`tools` filter 在这两个阶段 MUST 被忽略。

post_user_turn 触发 MUST 在 `agent.Run` 返回前同步完成,以便 TUI 在"agent done"之前看到 hook 输出。

### Requirement: hook 进程隔离

子进程 MUST 通过 `exec.CommandContext` 启动,context 在 hook timeout 到达时取消并发送 SIGKILL。stdin MUST 是上述 JSON 字节;stdout 和 stderr MUST 各自被读取并截断到 4KB(剩余字节丢弃)。子进程 env MUST 仅包含配置中 `env` 白名单列出的 key 对应的父进程值,以及由 hook runner 设置的 `UB_HOOK_EVENT`、`UB_HOOK_SESSION_ID`、`UB_HOOK_TURN`、(对 tool 阶段)`UB_HOOK_TOOL_NAME`、`UB_HOOK_TOOL_USE_ID` 这五个固定变量。

#### Scenario: timeout 强制结束

- **GIVEN** hook 配置 `timeout: 50ms` 但脚本 sleep 5 秒
- **WHEN** hook 触发
- **THEN** runner MUST 在 ≤ 200ms 内返回,且 Outcome.Err 非 nil 含 "timeout" / "killed" 字样
