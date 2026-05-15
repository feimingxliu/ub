## 1. 包骨架

- [x] 1.1 新建 `internal/tool/shell/` 目录，加包级注释说明执行语义、超时杀进程组、不实现 Preview
- [x] 1.2 引入包级常量 `defaultTimeout = 120 * time.Second`、`streamCap = 32 * 1024`

## 2. 输出捕获

- [x] 2.1 在 `capwriter.go` 实现 `*capWriter`：`Write(p []byte) (int, error)` 维持 buffer 前 32 KB + 真实总字节数计数
- [x] 2.2 提供 `(*capWriter).Bytes() []byte` 与 `(*capWriter).Total() int`

## 3. 跨平台占位

- [x] 3.1 新建 `bash_unix.go`（`//go:build !windows`）：暴露 `setProcessGroup(cmd *exec.Cmd)` 与 `killProcessGroup(pid int, sig syscall.Signal) error`
- [x] 3.2 新建 `bash_windows.go`（`//go:build windows`）：同名函数返回 not-supported 错误

## 4. bash 工具实现

- [x] 4.1 `bash.go`：定义 `bashArgs` 与 `bashTool`，`Risk=tool.RiskExec`，schema 用 `invopop/jsonschema`
- [x] 4.2 Execute 参数解析：校验 `command` 非空、`timeout_ms` 非负、用 `tool.Resolve` 校验 cwd
- [x] 4.3 用 `exec.Command("/bin/sh", "-c", command)`，设置 `Dir`、`Stdin = devNullReader`、`Stdout/Stderr = *capWriter`、`setProcessGroup`
- [x] 4.4 启动失败返回 IsError=true 的 Result（无 exit_code 时写 `exit_code=-1` 并附 `error=` 行）
- [x] 4.5 用 `time.NewTimer` 和 `ctx.Done()` 触发超时 / 取消；超时路径 `killProcessGroup(pid, SIGTERM)` → 2s 后再 SIGKILL；用 `sync.Once` 保证只触发一次
- [x] 4.6 拼装 `Result.Content`：`exit_code` / `duration_ms` / 分隔行 / stdout(可截断) / stderr(可截断)
- [x] 4.7 截断标记：超出 `streamCap` 时附加 `... (truncated, total <N> bytes)`

## 5. Register 入口

- [x] 5.1 `register.go`：实现 `shell.Register(reg *tool.Registry, root string) error`
- [x] 5.2 注册前 `filepath.Clean(root)`，与已注册同名工具冲突时返回 Registry 错误

## 6. 单测

- [x] 6.1 `capwriter_test.go`：覆盖小写入、超出 streamCap 行为、Total 计数准确
- [x] 6.2 `bash_test.go`：happy path `echo hello` → Content 含 hello、exit_code=0、IsError=false（用 `t.Skip` 在 windows 跳过）
- [x] 6.3 `bash_test.go`：cwd 注入 → 在 tempdir 写入 marker 文件，bash 命令 `cat <marker>` 返回该内容
- [x] 6.4 `bash_test.go`：非零退出 `exit 7` → Content 含 `exit_code=7`、IsError=true
- [x] 6.5 `bash_test.go`：超时 `sleep 10` + timeout_ms=200 → IsError=true、Content 含 timeout 标记、总耗时小于 5s
- [x] 6.6 `bash_test.go`：输出截断：用 `awk 'BEGIN{for(i=0;i<40000;i++)printf "x"}'` 或 `sh -c 'for i in $(seq ... )'` 产出 ~40 KB → stdout 段含 `... (truncated, total 40000 bytes)`，buffer 不超过 32 KB
- [x] 6.7 `bash_test.go`：cwd 跳出 root → 返回错误，不启动进程（用 `os.Stat` 之类断言进程未发生不可行，改为断言错误消息含 `outside workspace`）
- [x] 6.8 `bash_test.go`：空 command 与负 timeout_ms 报错
- [x] 6.9 `register_test.go`：覆盖 `shell.Register` 成功、重复注册报错、nil registry / 空 root

## 7. 验证

- [x] 7.1 运行 `go test ./internal/tool/shell/...`
- [x] 7.2 运行 `go test ./...`
- [x] 7.3 运行 `make lint`
- [x] 7.4 运行 `make build`
- [x] 7.5 运行 `openspec validate add-bash-tool --strict`
