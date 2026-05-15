## ADDED Requirements

### Requirement: doctor 命令

系统 SHALL 提供 `ub doctor` 子命令，输出 provider、模型列表、外部命令和 dev profile 建议的纯文本诊断报告。命令 MUST 支持 `--plain` 和 `--suggest`。

#### Scenario: provider endpoint 可达

- **GIVEN** 配置中存在 `openai-compat` provider 且 `base_url` 指向可返回 `/models` 的服务
- **WHEN** 用户运行 `ub doctor --plain`
- **THEN** 输出 MUST 标记该 provider reachable，并列出返回的模型 id

#### Scenario: 外部命令检查

- **WHEN** 用户运行 `ub doctor --plain`
- **THEN** 输出 MUST 包含 `rg`、`gopls`、`typescript-language-server` 和 `npx` 的存在性检查结果

#### Scenario: suggest 输出

- **WHEN** 用户运行 `ub doctor --suggest --plain`
- **THEN** 输出 MUST 包含可用的 `profiles.dev` 配置片段

### Requirement: doctor profile awareness

`ub doctor` SHALL 使用与其他 CLI 命令一致的 profile 与 mode 选择逻辑。

#### Scenario: doctor 使用 dev profile

- **GIVEN** `profiles.dev` 中配置了本地 `openai-compat` provider
- **WHEN** 用户运行 `ub doctor --dev --plain`
- **THEN** doctor MUST 检查 dev profile 生效后的 provider 配置

### Requirement: doctor 安全诊断

doctor MUST NOT 在缺失 API key 时向需要鉴权的远端 provider 发起模型请求。doctor MUST NOT 输出 API key 或敏感 header 值。

#### Scenario: OpenAI 缺少 API key

- **GIVEN** `openai` provider 未配置 `api_key`
- **WHEN** 用户运行 `ub doctor --plain`
- **THEN** 输出 MUST 标记该 provider 为 `NO_API_KEY`，且不发起远端模型请求
