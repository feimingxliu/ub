## Why

`roadmap-v2.md` §3-01 把 hooks 列为 V2 的核心能力扩展之一。灵感是
Claude Code 的 `hooks` 配置 —— 让用户用 shell 命令在 agent 关键节点
(tool call 前后、user turn 前后)挂自定义脚本,典型场景:

1. 每次 `edit`/`write` 后跑 `gofmt`/`prettier`
2. 每次 `bash` 前打 audit log
3. 每个 user turn 之前快照 git working tree
4. 每个 user turn 之后跑 lint 并把结果加到下一轮上下文(本次先不做最后这条)

本次只引入"运行 hook + 报告状态"的最小可用版本。

## What Changes

- 新建 `internal/hook/` 包:
  - `Event` 描述一次触发(kind、session、turn、tool 调用信息、可选 result)
  - `Runner` 接口:`Pre(ctx, Event) Decision` / `Post(ctx, Event)`
  - `shellRunner` 实现:解析配置、过滤 tools、用 `os/exec` 启动子进程,
    通过 stdin JSON + 白名单 env 传入上下文,超时强制 kill,stdout/stderr
    各自截断到 4KB 后写入 `Decision.Output`
- 4 个触发点:
  - `pre_tool_call`:`agent.runTool` 入口,在 permission 之前调用;
    `Decision.Block = true` 时跳过 tool 调用并把 hook stderr 当 IsError result 回灌
  - `post_tool_call`:`agent.runTool` 末尾,在 emit 完 done 后异步(非阻塞)调用
  - `pre_user_turn`:`agent.Run` 收到 user prompt 之后、首次 provider 请求之前
  - `post_user_turn`:`agent.Run` 整个 loop 结束(无论成功/失败)前
- 配置 schema(新增到 `config.Config.Hooks`):
  ```yaml
  hooks:
    pre_tool_call:
      - command: ["gofmt", "-w"]
        tools: ["edit", "write", "multiedit"]   # 空 = 所有 tool
        timeout: 5s                              # 默认 10s,上限 60s
        on_failure: warn                         # warn | block
        env: ["HOME", "PATH", "LANG"]            # 白名单,默认空
    post_tool_call: [...]
    pre_user_turn: [...]
    post_user_turn: [...]
  ```
- workspace 级 `.ub/hooks.yaml` 通过现有 `<cwd>/.ub/config.yaml` layering 合并即可,**不**新增独立文件
- agent rollout 记一条 `EventActivity{kind=hook, status, summary, content}`
  样式的事件,让用户在 TUI / `ub sessions show` 中看见 hook 跑过
- 新增 `ub doctor` 一行:列出当前 hooks 配置数量 + 是否包含 `block` 策略
  (本次可选,先放到 docs 提醒)

## Capabilities

### New Capabilities

- `hooks`:四个触发点的语义、配置 schema、隔离与失败策略、上下文注入约定

## Impact

- 新增 `internal/hook/` 包及 unit test
- 修改 `internal/config/types.go`:增加 `HooksConfig` 类型与 `Config.Hooks`
  字段;`Defaults()` 不预填任何 hook
- 修改 `internal/agent/agent.go`:`Options.Hooks` 字段 + 4 个触发点调用
- 修改 `internal/cli/root.go`:从 cfg 构造 `hook.Runner` 并注入 agent
- 不引入新依赖(用标准库 `os/exec`、`context`、`encoding/json`)
- breaking change:无;hooks 完全可选
- schema/config.schema.json 由 `make schema` 重新生成
