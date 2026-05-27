# output-spillover Specification

## Purpose
TBD - created by archiving change refine-output-spillover. Update Purpose after archive.
## Requirements
### Requirement: bash shell_metadata 块

`bash` 工具 `Result.Content` MUST 以 `<shell_metadata>` 行起始,包含 metadata 后以 `</shell_metadata>` 结束;紧接其后 MUST 是 `--- stdout ---`、stdout 文本、`--- stderr ---`、stderr 文本。metadata 块内 MUST 至少含 `exit_code=<int>` 与 `duration_ms=<int>` 两行;`timeout=true` 出现 IFF kill 是因超时触发;`aborted=true` 出现 IFF kill 是因 ctx 取消触发;`error=<text>` 出现 IFF 启动失败或非超时/取消的其他错误。

#### Scenario: 正常退出

- **GIVEN** 命令 `/bin/true` 在限时内正常退出
- **WHEN** 调用 `bash(command="/bin/true")`
- **THEN** `Result.Content` MUST 含 `<shell_metadata>` 块,块内 `exit_code=0` 且不含 `timeout=true` / `aborted=true`

#### Scenario: timeout

- **GIVEN** 命令 `sleep 5` 配 `timeout_ms=50`
- **THEN** metadata 块内 MUST 含 `timeout=true`,且 `exit_code` 不为 0

#### Scenario: ctx 取消

- **GIVEN** 通过 ctx 取消正在跑的命令
- **THEN** metadata 块内 MUST 含 `aborted=true`

### Requirement: spillover 文件大小上限

`tooloutput.Limits.FullMaxBytes`(默认 50 \* 1024 \* 1024)MUST 限制写入 spillover 文件的最大字节数。`full` 长度超过上限时,系统 MUST 在 UTF-8 安全边界截断到上限值,并在截断后的尾部追加一行 `... [spillover truncated: original_bytes=<N> kept=<M>]\n`。`Result.OriginalBytes` MUST 反映 **截断前** 的原始字节数(便于模型理解被丢弃了多少)。`Result.FullOutputPath` MUST 指向截断后的文件。

#### Scenario: 输出超 cap

- **GIVEN** 50MB cap、tool 输出 60MB
- **WHEN** `LimitResult` 落盘
- **THEN** 落盘文件 ≤ 50MB + footer 行;`Result.OriginalBytes == 60*1024*1024`

### Requirement: spillover 目录配置

`tooloutput.Limits.SpilloverDir`(对应 config `tools.output.spillover_dir`,本 change 在 `ContextToolResultConfig.SpilloverDir`)非空时,MUST 替代默认的 `<state-root>/tool_outputs/` 作为 spillover 根。空时回退到默认。

#### Scenario: 自定义 spillover 目录

- **GIVEN** `SpilloverDir = "/var/tmp/ub-out"`
- **THEN** spillover 文件 MUST 位于 `/var/tmp/ub-out/<safeSession>/<safeToolUseID>.txt`

