## Why

`bash` 是一次性的同步命令，对“跑 dev server / 长 build / tail 日志”这类长进程不够用。Sprint 2 计划在 agent 工具里加上后台 job 概念：调用一次 `job_run` 拿到 `job_id`，之后用 `job_output` 看输出、用 `job_kill` 关掉。本 change 把这三个工具一次性落地，并把 `shell` 包里已有的进程组工具拆出来共享，避免后续每个用进程组的工具都把同样的代码再写一遍。

## What Changes

- 抽出 `internal/tool/procgroup/` 子包：`SetProcessGroup(cmd)` / `KillProcessGroup(pid, sig)` 与平台占位（`_unix.go` / `_windows.go`）。
- `internal/tool/shell/` 改为使用新的 `procgroup` 包；行为不变，spec 不变。
- 新建 `internal/tool/job/` 子包，实现三个工具：
  - `job_run(command, cwd?)` → `{job_id, started_at}`：以 `/bin/sh -c` 启动后台进程，stdin 关闭，进程组隔离，stdout/stderr 由后台 goroutine 写入两条 32 KB ring buffer。`Risk=exec`。
  - `job_output(job_id, tail?)` → 最近输出 + 状态：`Risk=safe`；`tail` 可选（字节），默认 32 KB（即 ring buffer 全量）；返回 `state=running|exited`、（若已结束）`exit_code` 与两条流的近 N 字节。
  - `job_kill(job_id)` → 关停后台进程：`Risk=exec`；对整个进程组发 SIGTERM，2 秒后 SIGKILL；对已结束 job 是幂等的。
- `JobManager`：一个 per-Register 单例，`map[string]*job` + `sync.Mutex`，存活 job 与已结束 job（保留输出便于事后读 output）；jobs 不跨重启恢复。
- 沿用 `tool.Resolve` 沙箱：`cwd` 必须落在 workspace root 内。
- 暴露 `job.Register(reg *tool.Registry, root string) error` 把三个工具与同一个 `JobManager` 注入 Registry。
- 使用 `github.com/google/uuid` 生成 `job_id`（已经在 `go.sum` 间接依赖里）。
- 单测覆盖：happy path、ring buffer 截断、`job_kill` 关停 sleep、`job_output` / `job_kill` 找不到 job、重复 kill 幂等、cwd 跳出 root 拒绝。

## Capabilities

### New Capabilities

- `job-tools`：`job_run` / `job_output` / `job_kill` 的 input schema、状态机、ring buffer 语义、进程组 kill 行为、JobManager 生命周期与沙箱。

### Modified Capabilities

无（`bash-tool` 的对外行为没有变化，只是内部把进程组 helper 移到 `procgroup` 包）。

## Impact

- 新增 `internal/tool/procgroup/`、`internal/tool/job/` 目录与单测。
- `internal/tool/shell/bash.go`、`internal/tool/shell/bash_unix.go`、`internal/tool/shell/bash_windows.go` 内部 import 改为 `procgroup`；不修改 spec、不改对外行为。
- `go.mod`：把 `github.com/google/uuid` 升级为直接依赖（之前是 sqlite 的间接依赖）。
- 不修改 cli / provider / rollout / config / 其它 tool 包。
- 显式不在范围：跨重启恢复、权限审批 / 黑名单（I-20）、stdin 写入、Windows shell 支持。
