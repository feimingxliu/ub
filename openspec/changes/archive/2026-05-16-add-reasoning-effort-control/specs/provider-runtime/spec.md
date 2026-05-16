## ADDED Requirements

### Requirement: 模型 reasoning 能力解析

Provider runtime SHALL 为 provider/model 组合解析模型能力，至少包含是否支持 reasoning、支持的 effort 列表和默认 effort。解析 MUST 合并 provider 发现的模型列表、内置模型能力表和用户配置覆盖；用户配置覆盖优先级最高。未知模型 MUST 默认为不支持 reasoning，除非用户配置显式声明支持。

#### Scenario: 内置能力补全已发现模型

- **GIVEN** provider 发现模型 `gpt-known`
- **WHEN** 内置能力表声明 `gpt-known` 支持 `low`、`medium`、`high`
- **THEN** runtime MUST 返回该模型的 supported efforts 和默认 effort

#### Scenario: 用户配置覆盖能力

- **GIVEN** 用户配置声明模型 `custom-reasoner` 支持 `high`
- **WHEN** provider 发现或当前选择该模型
- **THEN** runtime MUST 使用用户配置中的 reasoning 能力

#### Scenario: 未知模型保守处理

- **GIVEN** 当前模型不在内置能力表且用户配置未声明 reasoning 能力
- **WHEN** runtime 解析模型能力
- **THEN** 模型 MUST 被视为不支持 reasoning

### Requirement: Provider Request reasoning 配置

Provider Request SHALL 支持可选 reasoning 配置。调用方 MUST 在发送请求前校验当前 effort 属于模型支持列表；不支持 reasoning 的模型 MUST 使用空 reasoning 配置，provider adapter MUST NOT 发送 reasoning 参数。

#### Scenario: 支持的 effort 进入请求

- **GIVEN** 当前模型支持 `medium`
- **WHEN** 用户选择 `/effort medium` 后发送消息
- **THEN** provider Request MUST 包含 effort `medium`

#### Scenario: 不支持的 effort 被拒绝

- **GIVEN** 当前模型只支持 `low`
- **WHEN** 用户尝试设置 `/effort high`
- **THEN** runtime MUST 返回错误
- **THEN** 当前 effort MUST 保持不变

#### Scenario: 不支持 reasoning 时不发送参数

- **GIVEN** 当前模型不支持 reasoning
- **WHEN** Agent 调用 provider
- **THEN** provider Request MUST 不包含 reasoning 配置
