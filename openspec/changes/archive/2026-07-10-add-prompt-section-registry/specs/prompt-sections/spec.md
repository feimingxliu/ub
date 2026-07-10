## ADDED Requirements

### Requirement: Prompt section registry
系统 MUST 使用固定顺序的 prompt section registry 表达 provider 请求前缀，并为每个已知 section 提供稳定 ID、角色、状态、稳定性、来源、字符数、token 估算和截断元数据。main prompt 的 section 顺序 MUST 为 `coding_agent`、`runtime`、`workspace_instructions`、`git_snapshot`、`execution_mode`、`memory`。

#### Scenario: Main prompt 使用固定顺序
- **WHEN** 系统为普通 Agent provider 请求构造 prompt
- **THEN** 实际进入请求的 section MUST 按 registry 固定顺序投影为 messages
- **AND** 缺失或禁用的 section MUST NOT 改变其余 section 的相对顺序

#### Scenario: Section 状态可区分
- **WHEN** section 被配置禁用、缺少可用内容或因请求类型不适用
- **THEN** manifest MUST 分别报告 `disabled`、`unavailable` 或 `omitted`
- **AND** 只有状态为 `included` 的 section MUST 进入 provider messages

### Requirement: 统一 prompt 构造路径
普通 Agent、只读 provider 请求和 no-tool 请求 MUST 从同一个 registry 模型生成 provider-facing messages 与 manifest，同时保持各请求类型现有的 section 可见性和消息语义。

#### Scenario: 普通与只读请求共享 main prompt 语义
- **WHEN** 普通 Agent 和只读 provider 请求使用相同 runtime、workspace、配置、mode 与 memory
- **THEN** 两者的 main prompt section 内容和顺序 MUST 一致

#### Scenario: No-tool 请求不携带工具指导
- **WHEN** 系统构造 no-tool provider 请求
- **THEN** 请求 MUST 只包含 no-tool runtime 与可用 memory section
- **AND** coding-agent、workspace instructions、Git snapshot 与 execution mode section MUST NOT 进入 provider messages

### Requirement: Prompt manifest 估算
系统 MUST 为 included section 报告字符数与基于指定模型的 token 估算，并 MUST 对完整 included message slice 报告整体 token 估算。估算值 MUST 明确属于诊断信息，不得被当作 provider 计费的精确值。

#### Scenario: 指定模型估算
- **WHEN** 调用方为 prompt manifest 提供模型 ID
- **THEN** 每个 included section 和整体 manifest MUST 使用现有 provider-neutral estimator 生成非负 token 估算
- **AND** manifest MUST 报告该模型 ID

### Requirement: Prompt inspect 命令
CLI MUST 提供 `ub prompt inspect` 和 `ub prompt inspect --json`，使用当前有效配置、mode 和 canonical workspace 构造 main prompt manifest。该命令 MUST NOT 调用 provider、初始化工具运行时、创建 session、写 rollout 或执行 startup maintenance。

#### Scenario: 文本检查
- **WHEN** 用户运行 `ub prompt inspect`
- **THEN** CLI MUST 按固定顺序输出每个 section 的 ID、状态、稳定性、来源、字符数、token 估算和截断状态

#### Scenario: JSON 检查
- **WHEN** 用户运行 `ub prompt inspect --json`
- **THEN** CLI MUST 输出可解析的 JSON manifest
- **AND** section 顺序 MUST 与文本输出和 registry 顺序一致

#### Scenario: Inspect 不产生持久副作用
- **WHEN** 用户在隔离的 XDG state/config 环境运行 prompt inspect
- **THEN** 命令 MUST 不创建 session、rollout、todo、goal 或其它 Agent 运行 artifact

### Requirement: Prompt 内容默认保密
prompt inspect MUST 默认省略 section 正文；只有用户显式提供 `--show-content` 时，文本或 JSON 输出才 MUST 包含 included section 的内容。

#### Scenario: 默认 JSON 不泄露内容
- **WHEN** workspace instructions 或 memory 包含敏感测试标记且用户运行 `ub prompt inspect --json`
- **THEN** JSON MUST 包含对应 section 的元数据
- **AND** JSON MUST NOT 包含敏感测试标记或 `content` 字段

#### Scenario: 显式展示内容
- **WHEN** 用户运行 `ub prompt inspect --show-content`
- **THEN** 输出 MUST 包含 included section 正文
- **AND** disabled、unavailable 或 omitted section MUST NOT 伪造正文

### Requirement: Prompt 兼容性验证
重构后的 provider-facing prompt MUST 保持现有默认、plan 和 no-tool 行为；测试 MUST 同时覆盖结构快照与 fake provider 可观察行为，而不能只检查实现源码字符串。

#### Scenario: Plan mode 保留执行模式指令
- **WHEN** Agent 以 plan mode 构造 main prompt
- **THEN** `execution_mode` section MUST 被 included
- **AND** fake provider MUST 收到现有 plan mode 约束

#### Scenario: 缺少可选来源时安全降级
- **WHEN** workspace 不是 Git 仓库、没有 workspace instructions 或没有 memory
- **THEN** 对应 section MUST 报告 unavailable
- **AND** prompt 构造 MUST 成功且其它 section 保持有效
