# job-tools Specification

## Purpose

Define background job tools for starting long-running shell commands, reading bounded output snapshots, terminating process groups, and registering the tools together.

## Requirements

### Requirement: job.Register 入口

系统 SHALL 暴露 `job.Register(reg *tool.Registry, root string) error`，把 `job_run`、`job_output`、`job_kill` 三个工具注册到给定 Registry，并让三者共享同一个 `JobManager` 实例。`Register` 单次调用 MUST 注册全部三个工具；任一注册失败 MUST 立即返回错误。

#### Scenario: 注册三个工具

- **GIVEN** 一个空 Registry 与一个可写的临时 root
- **WHEN** 调用 `job.Register(reg, root)`
- **THEN** 返回 nil，`reg.Get` 对 `job_run`、`job_output`、`job_kill` 三个名字 MUST 均命中

### Requirement: job_run 启动后台进程

系统 SHALL 提供 `job_run` 工具，`Risk` 为 `RiskExec`。input schema MUST 含 `command string`（必填）、`cwd string`（可选，默认 `.`，相对 workspace root）。工具 MUST 通过 `/bin/sh -c <command>` 启动子进程，并让子进程成为一个新进程组的领导者；启动后 `Result.Content` MUST 包含 `job_id=<uuid>` 与 `started_at=<RFC3339 时间戳>` 两行。`job_run` MUST 立即返回，不等待子进程结束。

#### Scenario: 启动 echo 命令

- **GIVEN** 一个新建的 JobManager
- **WHEN** 调用 `job_run(command="echo hi")`
- **THEN** `Result.Content` MUST 含 `job_id=` 与 `started_at=` 两行，且 `job_id` 是一个 36 字符 UUID 字符串

#### Scenario: 空 command 报错

- **WHEN** 调用 `job_run(command="")`
- **THEN** 返回错误，且 Manager 中 MUST NOT 出现新 job

### Requirement: job_run cwd 沙箱

系统 SHALL 让 `job_run` 调用 `tool.Resolve(root, cwd)` 校验 `cwd`。解析失败时 MUST 返回错误并 MUST NOT 启动子进程。`cwd` 为空字符串时 MUST 视作 `.`。

#### Scenario: cwd 跳出 root

- **WHEN** 调用 `job_run(command="pwd", cwd="../")`
- **THEN** 返回错误，且 Manager 中 MUST NOT 出现新 job

### Requirement: job_output 读取输出

系统 SHALL 提供 `job_output` 工具，`Risk` 为 `RiskSafe`。input schema MUST 含 `job_id string`（必填）、`tail int`（可选，字节数；不传或 0 表示全量 ring buffer）。`Result.Content` MUST 按以下顺序输出字面行：`job_id=<id>`、`state=<running|exited>`、`exit_code=<N>`（运行中为 -1）、`stdout_total=<bytes>`、`stderr_total=<bytes>`、分隔 `--- stdout ---`、stdout ring buffer 内容（最多 `tail` 字节）、分隔 `--- stderr ---`、stderr ring buffer 内容（最多 `tail` 字节）。

#### Scenario: 读取存活 job 的输出

- **GIVEN** 一个 `job_run(command="for i in 1 2 3; do echo line$i; done; sleep 30")` 起的 job
- **WHEN** 等待若干毫秒后 `job_output(job_id=<id>)`
- **THEN** `Result.Content` MUST 含 `state=running`、`exit_code=-1`，stdout 段 MUST 含 `line1`、`line2`、`line3`

#### Scenario: 找不到 job

- **WHEN** 调用 `job_output(job_id="nonexistent")`
- **THEN** 返回错误，错误消息 MUST 含 `job not found`

### Requirement: job_output ring buffer 上限

系统 SHALL 让每个 job 的 stdout / stderr 各保留最多 32 KB 的最新字节；超过部分 MUST 被旧字节覆盖。`stdout_total` / `stderr_total` MUST 报告**真实**总产出字节数（不受 32 KB 上限影响）。

#### Scenario: stdout 超过 32 KB

- **GIVEN** 一个产出约 40 KB stdout 的 job
- **WHEN** 在 job 结束后 `job_output(job_id, tail=32768)`
- **THEN** `stdout_total` MUST 报告 ≥ 40000，stdout 段长度 MUST ≤ 32768，且 MUST 是末尾字节（不是开头）

### Requirement: job_kill 关停后台进程

系统 SHALL 提供 `job_kill` 工具，`Risk` 为 `RiskExec`。input schema MUST 含 `job_id string`（必填）。对存活 job MUST 给整个进程组发 SIGTERM，2 秒后仍存活 MUST 再发 SIGKILL；调用 MUST 在子进程结束后再返回。子进程派生的孙进程 MUST 被同一信号路径覆盖。`Result.Content` MUST 含 `job_id=`、`state=exited`、`exit_code=` 三行，并 MUST 含 `killed=true`。

#### Scenario: kill 一个 sleep

- **GIVEN** 一个 `job_run(command="sleep 30")` 起的 job
- **WHEN** 立刻调用 `job_kill(job_id=<id>)`
- **THEN** 调用 MUST 在 5 秒内返回，`Result.Content` MUST 含 `state=exited`、`killed=true`、`exit_code=<N>`

#### Scenario: kill 已经结束的 job 幂等

- **GIVEN** 一个已经自然结束的 job
- **WHEN** 调用 `job_kill(job_id=<id>)`
- **THEN** MUST 返回 nil 错误，`Result.Content` MUST 含 `state=exited` 与 该 job 实际的 `exit_code`，`killed` 字段 MUST 为 `false`

#### Scenario: kill 找不到 job

- **WHEN** 调用 `job_kill(job_id="nonexistent")`
- **THEN** 返回错误，错误消息 MUST 含 `job not found`

### Requirement: 平台限制

系统 SHALL 在 Windows 上让 `job_run` / `job_kill` 直接返回包含 `not supported on windows` 字样的错误；`job_output` 在 Windows 上 MAY 正常运行（因为它不启动新进程），但 V1 不要求。

#### Scenario: Windows 不支持

- **GIVEN** `runtime.GOOS == "windows"`
- **WHEN** 调用 `job_run(command="echo")`
- **THEN** 返回错误，错误消息 MUST 含 `not supported on windows`
