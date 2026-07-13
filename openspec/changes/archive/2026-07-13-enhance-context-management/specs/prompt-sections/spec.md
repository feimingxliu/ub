## ADDED Requirements

### Requirement: Compact prompt section 的本地检查

系统 MUST 以命名的 `compact_instructions` prompt section 表达 structured summary 模板，并把它纳入与 main/no-tool prompt 相同的 section manifest 模型。compact section MUST 标记为 stable、builtin 和无工具；其内容 MUST 使用既定的工作续接字段，并且 compact manifest MUST 包含 summary model 的 token 估算。CLI MUST 支持本地检查该 compact manifest，且检查不得调用 provider、创建 session、写 rollout 或执行维护。

#### Scenario: 检查 compact prompt

- **WHEN** 用户运行 `ub prompt inspect --variant compact --json`
- **THEN** CLI MUST 输出可解析的 compact manifest
- **AND** manifest MUST 包含 `compact_instructions` 的 ID、状态、稳定性、来源、字符数和 token 估算
- **AND** manifest MUST 表明该请求不携带工具定义

#### Scenario: compact prompt 默认不泄露内容

- **WHEN** 用户运行 `ub prompt inspect --variant compact --json` 且未提供 `--show-content`
- **THEN** 输出 MUST 包含 compact section 元数据
- **AND** 输出 MUST NOT 包含 compact 模板正文

#### Scenario: compact prompt 与实际 summary 请求一致

- **GIVEN** Agent 使用默认或 short compact style 生成 summary
- **WHEN** 测试比较 compact manifest 的 section 内容与 summary provider request 的模板
- **THEN** 两者 MUST 使用相同 style 对应的模板
- **AND** summary provider request MUST 不包含工具定义
