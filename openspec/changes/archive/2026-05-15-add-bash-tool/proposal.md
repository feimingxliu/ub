## Why

agent 经常要跑一两条 shell 命令完成读取系统信息、跑测试、查 git 状态这类工作，是除了 fs / grep 之外最常用的工具。先把 bash 工具的执行 / 超时 / 截断 / 退出码语义定下来；权限审批和黑名单留给 I-20 单独做，这样 V1 在没有 permission UI 的阶段也能端到端跑通工具调用。

## What Changes

- 新建 `internal/tool/shell/` 子包，实现一个工具：
  - `bash(command, cwd?, timeout_ms?)`：通过 `/bin/sh -c <command>` 执行 shell 命令，`Risk=exec`。
- 工作目录沿用 workspace 沙箱：`cwd` 经 `tool.Resolve(root, cwd)` 校验，必须落在 root 内；不传则默认 root。
- 超时默认 120 秒，可由 `timeout_ms` 覆盖；超时后给整个进程组发 SIGTERM、2 秒后 SIGKILL。
- 输出策略：stdout 与 stderr 各自捕获并独立截断到 32 KB；超过时在尾部加 `... (truncated, N bytes total)` 标记。
- `Result.Content` 为单条文本，含 `exit_code` 和 `duration_ms` 元信息以及两段输出；非零退出 / 超时 / 启动失败 MUST 把 `IsError=true`。
- 暴露 `shell.Register(reg *tool.Registry, root string) error` 单独注册 bash。
- 不实现 `PreviewableTool`（命令是黑盒）；不做权限审批 / 黑名单 / mode gate / 后台 job / 交互式输入 / 环境变量注入。
- 纯标准库实现（`os/exec` + `syscall.SysProcAttr.Setpgid` POSIX 进程组），不引入新依赖。

## Capabilities

### New Capabilities

- `bash-tool`：bash 工具的 input schema、执行语义、超时与杀进程行为、输出截断、错误模型与沙箱规则。

### Modified Capabilities

无（沿用 `tool-registry` 接口与 `tool.Resolve` 沙箱；不修改其它 capability 的 spec）。

## Impact

- 新增 `internal/tool/shell/` 目录与子包文件、单测。
- 不引入新的第三方依赖；纯标准库 + POSIX syscall。
- 不修改 cli / provider / rollout / config / 其它 tool 包；`shell.Register` 由 agent runtime（I-21）接入。
- 显式不在范围：权限审批 / 黑名单（I-20）、后台 job（I-19）、Windows shell、交互式输入。
