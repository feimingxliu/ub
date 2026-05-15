## Why

Sprint 1 已完成 provider 与 rollout 基础能力，但本地模型开发路径仍需要手工改配置。I-13 把 profile、执行模式配置和环境诊断做成一等入口，方便后续 agent/tool 开发用本地服务验证。

## What Changes

- 配置新增 `profiles`、`execution_mode`、`approval_agent` 和 `tools_disabled` 字段。
- 配置加载支持 `UB_PROFILE`、CLI `--profile`、`--dev` 和 `--mode` 覆盖。
- CLI 新增全局 flags：`--profile`、`--dev`、`--mode default|plan|agent-approve`。
- 新增 `ub doctor`，检查 provider endpoint、模型列表、常用外部命令，并支持 `--plain` 和 `--suggest`。
- 更新 schema 与相关配置规格。

## Capabilities

### New Capabilities

- `profile-runtime`: profile 选择、profile 覆盖、执行模式字段和 CLI 覆盖规则。
- `environment-doctor`: `ub doctor` 的 provider/模型/命令体检与 dev profile 建议输出。

### Modified Capabilities

- `config-loader`: 配置加载从"容忍 profiles 但不生效"改为正式解析和应用 profiles，并把新字段写入 schema。

## Impact

- 修改 `internal/config/` 类型、合并、加载和 schema。
- 修改 `internal/cli/` root/config/chat/run 命令装配。
- 新增 doctor 诊断逻辑与测试。
- 不实现 MCP server 连通性检查，不自动写用户配置。
