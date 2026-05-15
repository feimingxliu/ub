## Context

`docs/roadmap.md I-19` 已经把目标定下来：长进程后台运行、ring buffer 抓输出、进程组级别的 kill。`docs/design.md §4` 描述基本一致。`bash` 工具（I-18）已经实现了 POSIX 进程组的 SIGTERM → 2s 后 SIGKILL 模型，应该尽量复用。

本 change 的关键设计点都集中在 JobManager 的状态机与 ring buffer 的并发模型，因为 job 与 bash 不同：一次 tool call 启动进程，但后续两个 tool call 才读输出与停掉它，期间进程一直在写 buffer。

## Goals / Non-Goals

**Goals:**

- 三个工具 `job_run` / `job_output` / `job_kill` 一致地操作同一组 jobs。
- ring buffer 控制内存：stdout / stderr 各 32 KB，超过的旧字节自动覆盖，永不无限增长。
- `job_kill` 在已结束 job 上幂等且立即返回；对存活进程的 SIGTERM/2s/SIGKILL 路径与 `bash` 工具完全一致。
- 进程组 kill：进程派生的孙进程也被同一信号路径覆盖。
- 单测不依赖外部工具（除了 `/bin/sh`、`sleep`、`awk`、`cat` 这种 POSIX 默认）。
- `bash` 与 `job` 共享进程组 helper，无重复代码。

**Non-Goals:**

- 不实现跨进程 / 跨重启恢复；`ub` 退出即 jobs 全失效。
- 不实现 stdin 写入 / 交互式协议；stdin 一律 `/dev/null`。
- 不实现 job 标签、命名、列表（V1 用户只在最近创建的 job 上操作；后续 iteration 再加 `job_list`）。
- 不做权限审批 / 黑名单（I-20）。
- 不支持 Windows，与 bash 一致。
- 不做 SSE / 流式回灌给模型；`job_output` 是一次性快照。

## Decisions

- **包结构：**
  - 新建 `internal/tool/procgroup/`，把 `setProcessGroup` / `killProcessGroup`（连同 `_unix.go` / `_windows.go` 构建约束）从 `shell` 移过来，公开为 `procgroup.Set(cmd)` / `procgroup.Kill(pid, sig)`。
  - `internal/tool/shell/` 内部改用 `procgroup`；行为不变，spec 不变。
  - `internal/tool/job/`：`manager.go`（`Manager` + `job` 内部类型）、`ring.go`（ring buffer）、`run.go` / `output.go` / `kill.go`（三个工具）、`register.go`、`doc.go`、单测。
- **JobManager：** 一个实例由 `Register` 创建，三个工具共享同一引用（通过闭包持有指针，不暴露在 schema 里）。`map[string]*job` 受 `sync.Mutex` 保护；每个 `job` 自己再持有 `sync.Mutex` 保护 ring buffer 与状态字段，避免长时间持有 Manager 锁。
- **`job` 状态机：**
  - `state` 取值 `running` / `exited`；启动失败时不会进入 map（直接 `job_run` 返回错误）。
  - `exitCode` 默认 `-1`；进程退出后 goroutine 更新为真实值（`exec.ExitError.ExitCode()`），并把 `state` 设为 `exited`、关闭 `done chan struct{}`。
  - 被 kill 的 job 仍保留在 map 中（保留输出供 `job_output` 读），不做 GC；V1 接受这个简单实现，必要时后续 iteration 加 TTL。
- **Ring buffer（`ring.go`）：** 固定 32 KB 容量、`Write([]byte)` / `Snapshot(tail int) []byte`。`Write` 在容量不足时按字节覆盖最早数据；`Snapshot` 把当前内容按写入顺序拷贝出来。`tail` ≤ 0 视作“全量”。结构内部用 `[]byte` + `head` + `size`，无 `append` 抖动。并发安全由外层 `job` 锁负责，ring 自身不加锁，避免双锁。
- **`job_id` 生成：** `uuid.New().String()`；把 `google/uuid` 升级为直接依赖。`go.sum` 已经间接引入；`go mod tidy` 会把它写进 require 块。
- **进程启动：** `exec.Command("/bin/sh", "-c", command)`，`cmd.Dir=absCwd`、`cmd.Stdin=/dev/null` 文件、`cmd.Stdout=ringWriter{stdoutRing}`、`cmd.Stderr=ringWriter{stderrRing}`、`procgroup.Set(cmd)`。`ringWriter` 是 `job` 内部的 `io.Writer` 适配器，每次 `Write` 拿 job 锁后写 ring，并更新 `bytesWritten` 计数（供 `job_output` 报告“真实总字节数”）。
- **进程 wait goroutine：** `cmd.Start()` 成功后立刻 `go waitAndFinalize(j, cmd)`：调 `cmd.Wait()`，结束后把 `state`、`exitCode`、`finishedAt` 写回 `*job`，关闭 `done`。
- **`job_kill` 实现：**
  - 拿 job 锁、检查 `state`：
    - `exited`：直接返回当前 `exit_code`、`state=exited` 摘要。
    - `running`：`procgroup.Kill(pgid, SIGTERM)`；启动 2s timer，timer 触发再发 SIGKILL；释放锁；等 `<-done`；再次拿锁读 `exit_code`；返回。
  - 用 `sync.Once` 保证 kill 路径只走一次；二次 `job_kill` 直接读 done channel + 返回结果。
- **`job_output` 实现：** 拿 job 锁；调用各 ring 的 `Snapshot(tail)`；构造 `Result.Content`：包含 `job_id`、`state`、`exit_code`（或 `running` 标记）、`stdout_total`、`stderr_total`，以及与 bash 工具同样的 `--- stdout ---` / `--- stderr ---` 分段。`Risk=safe`。
- **`Result.Content` 文本格式（统一）：** 与 bash 工具风格一致，方便 LLM 复用解析：
  ```
  job_id=<uuid>
  state=<running|exited>
  exit_code=<N or -1 if running>
  stdout_total=<bytes>
  stderr_total=<bytes>
  --- stdout ---
  <ring buffer tail>
  --- stderr ---
  <ring buffer tail>
  ```
  `job_run` 不返回输出段，仅返回 `job_id=` + `started_at=`。`job_kill` 返回同上但带 `killed=true`。
- **`tool.Resolve` 复用：** `cwd` 全部走 `tool.Resolve(root, cwd)`；空字符串视作 `.`。
- **生命周期：** Manager 不实现 `Shutdown`；进程 退出时 OS 自动回收子进程（V1 不在意 daemonize 行为）。后续 I-21 agent loop 接入时如果需要 graceful shutdown，再补 `Manager.KillAll()`。

## Risks / Trade-offs

- **`shell` 引用 `procgroup`** → 同一 commit 内重构；`bash-tool` 的 spec 不变（只是内部）。回滚成本低（恢复 unix/windows 文件并恢复 import）。
- **ring buffer 旧字节丢失** → 是预期行为；`stdout_total` 告诉模型“总产出了多少字节”，截断标记隐含在 `total != snapshot_len` 上。要不要在 snapshot 里加截断 footer？V1 不加，因为 ring 的语义就是“tail”，与 bash 的“截断”语义不同；模型只要看 total 即可推断。
- **被 kill 的 job 永远留在 map** → 内存泄漏面是输出 64 KB + meta + uuid，每个 job ~70 KB；用户每个 session 起几个 job 也无所谓。后续 iteration 引入 `job_list` + TTL GC。
- **未做进程组传播单测** → roadmap 验证项里写了“起 echo 循环 → 拿 tail”，但“孙进程被一起 kill” 不易在不依赖 ps 的情况下确定。design.md §4 与 bash-tool 已经把这一行为定下来；本 change 在 spec 中保留要求，但单测只验证 `sleep` 被 SIGTERM 终结的 happy path；进程组传播的端到端测试留给后续 iteration。
- **`uuid` 升级为直接依赖** → 包体积影响可忽略；`go mod tidy` 会更新 `go.mod`。
