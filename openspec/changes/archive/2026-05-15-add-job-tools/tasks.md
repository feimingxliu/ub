## 0. 抽出 procgroup helper

- [x] 0.1 新建 `internal/tool/procgroup/` 子包：`procgroup.go`（包注释）、`procgroup_unix.go`、`procgroup_windows.go`
- [x] 0.2 在 `_unix.go` 暴露 `Set(cmd *exec.Cmd)` 与 `Kill(pid int, sig syscall.Signal) error`
- [x] 0.3 在 `_windows.go` 提供同名占位返回 not-supported 错误
- [x] 0.4 修改 `internal/tool/shell/bash_unix.go` / `bash_windows.go` / `bash.go`：调用 `procgroup.Set` / `procgroup.Kill`，删除原有 `setProcessGroup` / `killProcessGroup`
- [x] 0.5 重跑 `go test ./internal/tool/shell/...`

## 1. job 包骨架

- [x] 1.1 `go get github.com/google/uuid`（让它进入直接依赖）后 `go mod tidy`
- [x] 1.2 新建 `internal/tool/job/` 目录，加 `doc.go` 描述 Manager、ring buffer、进程组语义、不支持跨重启

## 2. Ring buffer

- [x] 2.1 `ring.go`：实现 `ring` 类型（固定容量、`Write([]byte) (int, error)`、`Snapshot(tail int) []byte`、`Total() int`）
- [x] 2.2 `ring_test.go`：覆盖小写入、覆盖式 overflow、Snapshot 顺序正确（最新数据在末尾）、`tail<=0` 与 `tail>size` 行为

## 3. JobManager 与状态

- [x] 3.1 `manager.go`：定义 `Manager`、`job`、`ringWriter`，`Manager.Start(cwd, command) (*job, error)`、`Manager.Get(id) (*job, bool)`
- [x] 3.2 启动流程：构造 `exec.Command`、`Stdin=os.DevNull`、`Stdout/Stderr = ringWriter`、`procgroup.Set`、`Start()`、后台 `go waitAndFinalize`
- [x] 3.3 `waitAndFinalize`：拿 job 锁更新 `state=exited`、`exitCode`、`finishedAt`，关闭 `done`
- [x] 3.4 `Manager.Kill(j *job) (killed bool, err error)`：状态机：已 exited → 直接返回 (false, nil)；存活 → `procgroup.Kill(SIGTERM)`、2s timer + SIGKILL、等 `<-done`；用 `sync.Once` 保证只走一次

## 4. job_run 工具

- [x] 4.1 `run.go`：定义 `runArgs`、`runTool`，`Risk=tool.RiskExec`
- [x] 4.2 校验 `command` 非空；用 `tool.Resolve` 校验 cwd（默认 `.`）
- [x] 4.3 Execute：调 `Manager.Start`，拼装 `Result.Content` 含 `job_id`、`started_at`
- [x] 4.4 Windows runtime 检测直接返回 `not supported on windows` 错误

## 5. job_output 工具

- [x] 5.1 `output.go`：定义 `outputArgs`、`outputTool`，`Risk=tool.RiskSafe`
- [x] 5.2 校验 `job_id` 非空；`tail<=0` 视作全量
- [x] 5.3 Execute：`Manager.Get` 查 job；拿 job 锁后读 state / exit_code / total / snapshot；拼装 spec 规定的多行 Result.Content

## 6. job_kill 工具

- [x] 6.1 `kill.go`：定义 `killArgs`、`killTool`，`Risk=tool.RiskExec`
- [x] 6.2 校验 `job_id` 非空
- [x] 6.3 Execute：`Manager.Kill`；拼装 `Result.Content` 含 `state=exited`、`killed=true|false`、`exit_code`
- [x] 6.4 Windows runtime 检测直接返回 `not supported on windows` 错误

## 7. Register 入口

- [x] 7.1 `register.go`：实现 `job.Register(reg *tool.Registry, root string) error`
- [x] 7.2 创建一个 `Manager` 实例并传给三个工具；注册任一失败时返回错误

## 8. 单测

- [x] 8.1 `manager_test.go`：起 `echo hi` → 等待 Done → 断言 `state=exited`、`exitCode=0`、stdout ring 包含 `hi`
- [x] 8.2 `manager_test.go`：起 `sleep 30` → 立刻 Kill → 调用 ≤ 5s 返回、`state=exited`、`killed=true`
- [x] 8.3 `manager_test.go`：起一个产出 40000 bytes stdout 的命令 → 等结束 → ring `Total()=40000`、`Snapshot(32*1024)` 长度 ≤ 32*1024 且是末尾
- [x] 8.4 `manager_test.go`：cwd 跳出 root 报错（通过 runTool 路径）
- [x] 8.5 `run_test.go`：通过工具入口的 happy path，断言 `Result.Content` 含 `job_id=` 与 `started_at=`，并 `Get(id)` 命中
- [x] 8.6 `output_test.go`：找不到 job 报错；找到 job 时输出格式符合 spec（state/exit_code/total/分隔行）
- [x] 8.7 `kill_test.go`：第二次 kill 的 `killed=false`、不报错；找不到 job 报错；happy path
- [x] 8.8 `register_test.go`：注册三个工具成功；重复注册任一名字时报错
- [x] 8.9 所有 manager/run/kill 测试都加 `runtime.GOOS == "windows"` skip 检测

## 9. 验证

- [x] 9.1 运行 `go test ./internal/tool/job/...`
- [x] 9.2 运行 `go test ./...`
- [x] 9.3 运行 `make lint`
- [x] 9.4 运行 `make build`
- [x] 9.5 运行 `openspec validate add-job-tools --strict`
