# runtime-logging Specification

## Purpose

Define process-level logging, CLI error rendering, and panic recovery behavior for `ub`.

## Requirements

### Requirement: 全局日志初始化

系统 SHALL 在 CLI 命令执行前初始化全局 `slog` logger。`UB_LOG_LEVEL` MUST 控制最小日志级别，支持 `debug`、`info`、`warn`、`error`；未设置时 MUST 默认为 `info`。

#### Scenario: 默认 info 级别

- **WHEN** 用户未设置 `UB_LOG_LEVEL` 并运行任意命令
- **THEN** 全局 logger 的最小级别为 `info`

#### Scenario: debug 级别可见

- **GIVEN** 环境变量 `UB_LOG_LEVEL=debug`
- **WHEN** 用户运行 `ub config show`
- **THEN** stderr 中包含至少一条 debug 级别日志，stdout 仍为合法 YAML

#### Scenario: 非法日志级别报错

- **GIVEN** 环境变量 `UB_LOG_LEVEL=verbose`
- **WHEN** 用户运行任意命令
- **THEN** 命令以非零码退出，stderr 包含 `invalid UB_LOG_LEVEL`

### Requirement: 日志输出目标

系统 SHALL 默认把人类可读日志写到 stderr。若设置 `UB_LOG_FILE`，系统 MUST 追加写入 JSON 格式日志文件，且不把结构化日志写入 stdout。

#### Scenario: 默认写 stderr

- **WHEN** 用户未设置 `UB_LOG_FILE` 并运行命令
- **THEN** 日志写入 stderr，命令业务输出仍写 stdout

#### Scenario: UB_LOG_FILE 写 JSON

- **GIVEN** 环境变量 `UB_LOG_FILE=/tmp/ub.log`
- **WHEN** 用户运行命令产生日志
- **THEN** `/tmp/ub.log` 包含 JSON 日志行，且每行至少包含 time、level、msg 字段

#### Scenario: 日志文件无法打开

- **GIVEN** 环境变量 `UB_LOG_FILE` 指向不可创建的位置
- **WHEN** 用户运行任意命令
- **THEN** 命令以非零码退出，stderr 包含日志文件打开失败原因

### Requirement: CLI 错误统一渲染

系统 SHALL 统一渲染命令执行错误。普通运行时错误 MUST 以 `error: <message>` 写入 stderr，并 MUST NOT 输出 Cobra usage，除非用户显式请求 help。

#### Scenario: 占位命令错误可读

- **WHEN** 用户运行仍未实现的 `ub run`
- **THEN** stderr 包含 `error:` 和 roadmap iteration 提示，且不包含完整 usage 文本

#### Scenario: help 正常输出

- **WHEN** 用户运行 `ub --help`
- **THEN** stdout 或 stderr 输出 Cobra help，命令以 exit code 0 退出

### Requirement: Panic recovery

系统 SHALL 在 CLI 顶层恢复 panic。发生 panic 时，系统 MUST 打印 panic 值和调用栈到 stderr，并以非零码退出。

#### Scenario: panic 被恢复

- **WHEN** Cobra 命令执行路径发生 panic
- **THEN** CLI 顶层 recovery 捕获 panic，stderr 包含 `panic:` 和调用栈，进程返回非零码

#### Scenario: panic 不写 stdout

- **WHEN** Cobra 命令执行路径发生 panic
- **THEN** stdout 不包含 panic 详情
