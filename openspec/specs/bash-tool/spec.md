# bash-tool Specification

## Purpose
TBD - created by archiving change add-bash-tool. Update Purpose after archive.
## Requirements
### Requirement: shell.Register 入口

系统 SHALL 暴露 `shell.Register(reg *tool.Registry, root string) error`，把 `bash` 工具注册到给定 Registry。注册后 `reg.Get("bash")` MUST 返回该工具实例。`Register` 不与 `fs.Register` / `search.Register` 合并，调用方可以独立启用或禁用。

#### Scenario: 注册 bash

- **GIVEN** 一个空 Registry 与一个可读的临时 root
- **WHEN** 调用 `shell.Register(reg, root)`
- **THEN** 返回 nil 错误，且 `reg.Get("bash")` MUST 返回非 nil 工具，`Risk()` MUST 等于 `tool.RiskExec`

### Requirement: bash 工具输入

系统 SHALL 提供 `bash` 工具，`Risk` 为 `RiskExec`。input schema MUST 包含 `command string`（必填）、`cwd string`（可选，相对 workspace root，默认 `.`）、`timeout_ms int`（可选，毫秒；不传或 0 表示默认 120000）。`command` 为空字符串时 MUST 返回错误，不开始任何子进程。`timeout_ms` 为负数时 MUST 返回错误。

#### Scenario: 缺省 timeout

- **GIVEN** 有效的 command
- **WHEN** 不传 `timeout_ms`
- **THEN** 系统 MUST 用 120000 ms 作为超时上限

#### Scenario: 空 command

- **WHEN** 调用 `bash(command="")`
- **THEN** 返回错误，不启动子进程

#### Scenario: 负数 timeout

- **WHEN** 调用 `bash(command="echo", timeout_ms=-1)`
- **THEN** 返回错误，不启动子进程

### Requirement: bash cwd 沙箱

系统 SHALL 让 `bash` 调用 `tool.Resolve(root, cwd)` 校验 `cwd`；解析后路径若不在 root 之下，工具 MUST 返回包含 `path is outside workspace root` 字样的错误，且 MUST NOT 启动子进程。`cwd` 为空字符串时 MUST 视作 `.`，子进程 `Dir` MUST 设置为解析后的绝对路径。

#### Scenario: 跳出 root

- **WHEN** 调用 `bash(command="pwd", cwd="../")`
- **THEN** 返回错误，不启动子进程

#### Scenario: 默认 cwd

- **GIVEN** workspace root 为 `/tmp/ws`
- **WHEN** 调用 `bash(command="pwd")`（不传 cwd）
- **THEN** 子进程的 `Dir` MUST 等于 `/tmp/ws`

### Requirement: bash Result 文本格式

系统 SHALL 让 `bash` 的 `Result.Content` 始终包含五个区段，按以下顺序与字面量出现：第一行 `exit_code=<N>`、第二行 `duration_ms=<M>`、然后是分隔行 `--- stdout ---`、捕获的 stdout、分隔行 `--- stderr ---`、捕获的 stderr。即使 stdout 或 stderr 为空，分隔行 MUST 依然存在。

#### Scenario: happy path 输出

- **WHEN** 调用 `bash(command="echo hello")`
- **THEN** `Result.Content` MUST 含 `exit_code=0`、`duration_ms=` 开头的两行，以及 stdout 段内包含 `hello`

### Requirement: bash 退出码与 IsError

系统 SHALL 在以下任一情况把 `Result.IsError` 置为 true：进程非零退出、超时被杀、`cmd.Start` 启动失败。零退出码时 `IsError` MUST 为 false。

#### Scenario: 非零退出

- **WHEN** 调用 `bash(command="exit 7")`
- **THEN** `Result.Content` MUST 含 `exit_code=7`，`Result.IsError` MUST 为 true

#### Scenario: 零退出

- **WHEN** 调用 `bash(command="true")`
- **THEN** `Result.Content` MUST 含 `exit_code=0`，`Result.IsError` MUST 为 false

### Requirement: bash 超时与进程组

系统 SHALL 在 `timeout_ms` 到达后给子进程**整个进程组**发 SIGTERM；2 秒后子进程仍未退出 MUST 再发 SIGKILL。超时时 `Result.IsError` MUST 为 true，`Result.Content` MUST 包含描述超时的错误行，且仍 MUST 返回截至杀进程前已捕获的 stdout / stderr。子进程派生的孙进程 MUST 被同一信号路径覆盖。

#### Scenario: sleep 被超时杀掉

- **WHEN** 调用 `bash(command="sleep 10", timeout_ms=200)`
- **THEN** 命令 MUST 在 ~200 ms + 2 s 容忍内返回；`Result.IsError` MUST 为 true；`Result.Content` MUST 含 timeout 标记

### Requirement: bash 输出截断

系统 SHALL 对 stdout 与 stderr 分别施加 32 KB 上限；任一流超过上限时，`Result.Content` 中该流的内容 MUST 仅含前 32 KB，并在该流尾部追加 `... (truncated, total <N> bytes)`，其中 N 为该流真实产生的总字节数。低于上限时 MUST 不出现截断标记。

#### Scenario: stdout 超出 32 KB

- **WHEN** 调用 `bash` 让命令输出 64 KB 的纯文本到 stdout
- **THEN** `Result.Content` 中 stdout 段 MUST 仅含约 32 KB 内容并以 `... (truncated, total 65536 bytes)` 收尾，stderr 段保持原样不截断

### Requirement: bash 关闭 stdin

系统 SHALL 把子进程的 stdin 重定向到 `os.DevNull`，使任何读 stdin 的命令立即得到 EOF。命令 MUST NOT 从父进程或终端读取数据。

#### Scenario: 命令读 stdin 立即结束

- **WHEN** 调用 `bash(command="cat")`
- **THEN** 命令 MUST 在不挂起的情况下返回，`exit_code` 等于 `cat` 的实际退出码（通常 0）

