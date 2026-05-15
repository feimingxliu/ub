## Context

`docs/roadmap.md I-18` 和 `docs/design.md §4` 已经定义了 bash 工具的契约：`os/exec` 子进程、120s 超时、stdout/stderr 截断、退出码 + 时长回给模型、不实现 Preview、不带权限审批（I-20 单独做）。本 change 把这些落到代码，并把超时杀进程的语义（SIGTERM → 2s 后 SIGKILL，对整个进程组）做对，免得后续 I-19 后台 job 复用 shell 包时再返工。

## Goals / Non-Goals

**Goals:**

- 一个 `bash` 工具，覆盖 V1 最常用的“跑一两条 shell 命令”场景。
- 超时与杀进程语义可观察、可测：超时一定能杀掉子进程及其孙进程。
- 输出截断在 stdout / stderr 两侧独立，并在尾部提示总字节数，方便模型理解“被截断了多少”。
- 沙箱沿用 `tool.Resolve`，与 fs / search 一致。
- 单测覆盖 happy / 失败退出 / 超时 / 截断 / 沙箱跳出 / 启动失败。

**Non-Goals:**

- 不实现 `PreviewableTool`（shell 命令是黑盒，做不出有意义的 preview）。
- 不实现权限审批、黑名单、`plan` mode gate（属于 I-20）。
- 不实现后台 job（属于 I-19）。
- 不接收 stdin（V1 强制 `os.DevNull`）；不实现交互式 prompt。
- 不在工具层注入额外环境变量；命令继承父进程 env。
- 不支持 Windows shell；V1 直接用 `/bin/sh -c`，与 design.md 描述一致。

## Decisions

- **包结构：** 全部放在 `internal/tool/shell/`：`bash.go`（Tool 实现）、`bash_unix.go`（POSIX 进程组的 `syscall.SysProcAttr`，`//go:build !windows`）、`bash_windows.go`（占位返回错误，`//go:build windows`，避免 cross-compile 失败）、`register.go`（`Register` 入口）、`*_test.go`。
- **命令拼装：** 始终用 `/bin/sh -c <command>`，不解析参数；调用方传一整条 shell 命令字符串。`/bin/sh` 在 Linux/macOS 都存在；找不到时 `os/exec` 自然返回错误，工具透传。
- **stdin：** 显式赋值为 `os.DevNull`，避免子进程意外读到 terminal 输入造成 V1 出现挂死。
- **进程组：** POSIX 实现里 `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`；超时或 ctx 取消时对 `-pid`（整个 group）调用 `syscall.Kill`，先 SIGTERM、等 2 秒、再 SIGKILL。Windows 路径直接报错，留给后续 iteration。
- **输出捕获策略：** 不用 `cmd.Output()`，而是给 stdout/stderr 各配一个 `*capWriter`：内部维持一个有上限的 buffer 与“真实总字节数”计数。前 32 KB 写入 buffer，之后只更新计数器。
- **截断标记：** 输出超出时尾部追加 `\n... (truncated, total N bytes)`。32 KB 上限通过包级常量 `streamCap = 32 * 1024`。
- **`Result.Content` 文本格式：**
  ```
  exit_code=N
  duration_ms=M
  --- stdout ---
  <captured stdout, possibly truncated>
  --- stderr ---
  <captured stderr, possibly truncated>
  ```
  没有 stdout 或 stderr 时仍输出对应分隔符 + 空段，保持模型解析稳定。
- **`IsError` 语义：** 任一情况触发：进程非零退出（包括 `127`）、context 超时杀进程、`cmd.Start()` 失败、cwd 沙箱拒绝。错误细节通过 `Result.Content` 的 `exit_code=` 行或追加的 `error=` 行给到模型。
- **超时实现：** 不依赖 `exec.CommandContext`（它默认只 SIGKILL 自己 PID，不杀整个 group）。改用 `exec.Command` + 自管 `time.NewTimer(timeout)`；定时器到期时调用包内 `killProcessGroup(pid)`：SIGTERM → 2s 等待 → SIGKILL。`cmd.Wait()` 返回后再读 buffer，避免数据竞态。`ctx.Done()` 同样触发 kill 路径。
- **`tool.Resolve` 复用：** `cwd` 一律走 `tool.Resolve(root, cwd)`。空字符串视作 `"."`。结果作为 `cmd.Dir`。
- **超时默认值：** 包级常量 `defaultTimeout = 120 * time.Second`。`timeout_ms` 等于 0 视为“用默认”；负数返回参数错误。
- **`Result.Files` 留空：** bash 工具不汇报文件改动；dispatcher 后续如果要做 “bash 执行后 git diff workspace” 之类的检测，由 dispatcher 层做，不在 tool 层强加。

## Risks / Trade-offs

- **进程组 kill 在 Windows 不可用** → `bash_windows.go` 占位返回明确错误；V1 不支持 Windows，与 design.md 一致。
- **`/bin/sh` 路径硬编码** → 极少数 minimal 环境会缺；缺失时 `os/exec` 报 `file not found`，工具透传。后续若发现实际项目要可配置再加。
- **buffer 截断丢的字节数无法保证精确** → 我们维持总字节计数（即使不写 buffer），所以截断标记里的 `total N bytes` 是准确的；只有 buffer 内容是截断的。
- **超时与 ctx 同时触发** → 用 sync.Once 包装 kill 路径，保证只杀一次；`Result` 的 `error=` 行优先写 timeout，再写 context cancel。
- **stdin = `os.DevNull` 会让需要 stdin 的命令立刻退出（exit 0 or 1）** → V1 接受这个行为；用户跑交互式命令本来就不该走 bash 工具。
- **shell 注入风险** → 由 model 提供命令字符串本就等于让模型在 root 权限内跑任意命令；V1 完全靠 I-20 的 permission 与黑名单做防护。tool 层只保证沙箱 cwd 与超时杀进程。
