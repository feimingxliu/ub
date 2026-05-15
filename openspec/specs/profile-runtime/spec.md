# profile-runtime Specification

## Purpose

Define runtime profiles, profile selection, and execution mode configuration overlays.

## Requirements

### Requirement: Profile 配置与选择

系统 SHALL 支持 `profiles` 配置节，每个 profile 可覆盖 `default_model`、`small_model`、`execution_mode`、`approval_agent`、`providers`、`permissions`、`tui`、`context`、`mcp_servers`、`lsp_servers` 和 `tools_disabled`。profile 选择优先级 MUST 为 CLI `--profile` / `--dev` 高于 `UB_PROFILE`，未指定时不应用 profile。

#### Scenario: 应用指定 profile

- **GIVEN** 顶层配置 `default_model=fake/base` 且 `profiles.dev.default_model=fake/dev`
- **WHEN** 以 profile `dev` 加载配置
- **THEN** 最终 `Config.DefaultModel` MUST 等于 `fake/dev`

#### Scenario: UB_PROFILE

- **GIVEN** 环境变量 `UB_PROFILE=dev`
- **WHEN** CLI 未传 `--profile` 或 `--dev`
- **THEN** 配置加载 MUST 应用 `dev` profile

#### Scenario: profile 不存在

- **WHEN** 请求应用不存在的 profile
- **THEN** 配置加载 MUST 返回可读错误

### Requirement: CLI profile flags

CLI SHALL 提供全局 `--profile <name>`、`--dev` 和 `--mode <mode>` flags。`--dev` MUST 等价于 `--profile dev`。这些 flags MUST 对 `config show`、`chat` 和 `doctor` 生效。

#### Scenario: --dev 别名

- **GIVEN** 配置含 `profiles.dev.default_model=fake/dev`
- **WHEN** 用户运行 `ub config show --dev`
- **THEN** 输出 MUST 使用 dev profile 后的有效配置

#### Scenario: --profile 与 --dev 同时出现

- **WHEN** 用户同时传入 `--profile prod` 和 `--dev`
- **THEN** CLI MUST 返回可读错误

### Requirement: Execution mode 配置

系统 SHALL 支持 `execution_mode` 字段，可选值为 `default`、`plan`、`agent-approve`。最终执行模式优先级 MUST 为 CLI `--mode` 高于 profile 高于配置默认值。

#### Scenario: profile 覆盖 mode

- **GIVEN** 顶层配置 `execution_mode=default` 且 `profiles.dev.execution_mode=plan`
- **WHEN** 应用 `dev` profile
- **THEN** 最终 `Config.ExecutionMode` MUST 等于 `plan`

#### Scenario: CLI mode 覆盖 profile

- **GIVEN** `profiles.dev.execution_mode=plan`
- **WHEN** 用户传入 `--dev --mode agent-approve`
- **THEN** 最终 `Config.ExecutionMode` MUST 等于 `agent-approve`

#### Scenario: 非法 mode

- **WHEN** 配置或 CLI 传入未知 execution mode
- **THEN** 配置加载 MUST 返回可读错误
