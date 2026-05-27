## Why

`roadmap-v2.md` §3-09 列出长输出 spillover 的目标设计。检查现状:

- ✅ tool result 超长自动落盘到 `<state>/tool_outputs/<sid>/<tool_use_id>.txt` —
  `internal/tooloutput.LimitResult` 已实现
- ✅ result content 含截断提示与 `full_output_path=` 路径 —— 已实现
- ✅ `tool_result` 工具可按 tool_use_id 拉回原始输出 —— S3-08 已交付
- ❌ shell_metadata 段(timeout / aborted 显式标记)—— bash 只在 `error=` 行里
  夹带了原因字符串,model 难以稳定解析
- ❌ `full_max_bytes` 上限(默认 50MB)—— 当前 spillover 无大小上限,
  极端情况(`find /` 之类)会写出几百 MB 文件占爆磁盘
- ❌ `spillover_dir` 配置覆盖 —— 当前只能改 `XDG_STATE_HOME`

本 change 把这三个缺口补上。head vs tail 偏好暂不动(用户反馈未要求)。

## What Changes

- `internal/tool/shell/bash.go` `assembleContent`:把 metadata 部分独立成
  `<shell_metadata>...</shell_metadata>` XML-like 块,固定字段:`exit_code`、
  `duration_ms`、可选 `timeout=true`、`aborted=true`、`error=<msg>`
  (existing 字段保留向后兼容,只是新增结构 + 显式 flag)
- `internal/tooloutput`:
  - `Limits` 新增 `FullMaxBytes int`,从 `ContextToolResultConfig.FullMaxBytes`
    读取;`EffectiveLimits` 默认 50MB
  - `LimitResult` 在写 spillover 之前如果 `len(full) > FullMaxBytes` 就
    截到前 `FullMaxBytes` 字节(切 UTF-8 安全),在 spillover 文件末尾追加
    `\n... [spillover truncated: original_bytes=N kept=M]` 一行
  - `Limits` 新增 `SpilloverDir string`;非空时优先于 `StateRoot/tool_outputs`,
    作为 spillover 根目录
- `internal/config/types.go`:`ContextToolResultConfig` 新增 `FullMaxBytes`
  与 `SpilloverDir`(yaml/json tag,带 `omitempty`)
- 文档:`docs/usage.md` 增 spillover 章节;`docs/design.md` 在工具一节注明
  shell_metadata 块

## Capabilities

### Modified Capabilities

- `output-spillover`:为这个 capability 写下首版 spec,确立 spillover 路径、
  preview 截断、full output 上限、shell_metadata 块约定

## Impact

- 修改 `internal/tool/shell/bash.go` 与 `bash_test.go`
- 修改 `internal/tooloutput/tooloutput.go` 与 `tooloutput_test.go`
- 修改 `internal/config/types.go`(`merge.go` 已经会处理新字段,只要类型存在)
- 不引入新依赖
- breaking change:bash result content 的 metadata 行从平铺改成 XML-like 块;
  rollout / TUI / `cli/rollout.go` 内部都以"截断提示中的 footer 行"为锚点,
  不依赖 metadata 行的具体格式,所以影响仅限于人类读 bash 输出时的视觉
