## ADDED Requirements

### Requirement: Reasoning 与模型能力配置

配置系统 SHALL 支持全局、profile、approval agent 和 provider model 级别的 reasoning 相关配置。`reasoning.effort` MUST 接受 `none`、`minimal`、`low`、`medium`、`high`、`xhigh`。provider model 配置 MAY 声明 `supports_reasoning`、`supported_efforts` 和 `default_effort`，用于覆盖内置模型能力表。JSON Schema MUST 覆盖这些字段。

#### Scenario: 解析全局 reasoning 配置

- **WHEN** 配置文件包含 `reasoning.effort: high`
- **THEN** 加载后的 Config MUST 保留该 effort

#### Scenario: 解析 provider 模型能力覆盖

- **WHEN** 配置文件包含 `providers.openai.models.custom.supports_reasoning: true`
- **THEN** 加载后的 provider config MUST 保留该模型能力覆盖

#### Scenario: schema 覆盖 reasoning 字段

- **WHEN** 用户运行 `make schema`
- **THEN** `schema/config.schema.json` MUST 包含 `reasoning`、`approval_agent.reasoning` 和 `providers.*.models` 相关字段
