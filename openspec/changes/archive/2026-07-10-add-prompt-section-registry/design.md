## Context

当前 `buildStartupPromptMessages` 在 Agent 构造时生成 coding-agent、runtime、workspace instructions 与 Git snapshot；`withRuntimeContext` 在每次 provider 请求前再附加 execution mode 与 memory。`RuntimeContextMessages` 和 `NoToolRuntimeContextMessages` 各自重复了部分组装逻辑，summary prompt 与 tool schema 又位于其它文件。消息本身没有 section identity，因此无法可靠检查来源、截断、稳定性或未来 cache eligibility。

这次 change 跨越 Agent prompt 组装、CLI 命令和测试，但必须保持 provider-facing message 的内容、角色与顺序兼容。实现仍位于 `internal/app/ub/agent` 包内，先稳定 registry 契约；是否进一步拆成 `agent/prompt` 子包留给后续重构，避免本次同时引入包迁移和行为变化。

## Goals / Non-Goals

**Goals:**

- 为现有 main/no-tool prompt 建立固定顺序的 section registry 与 manifest。
- 区分 section 是否 included、disabled、unavailable 或 intentionally omitted，并记录 stable/dynamic、source、字符数、token 估算和截断状态。
- 让 Agent 主路径、只读请求与 no-tool 请求复用 registry，而不改变现有 provider 请求语义。
- 提供不调用 provider、不创建 session、不写 rollout 的 `ub prompt inspect` 文本/JSON接口。
- 默认不输出 section content；只有 `--show-content` 明确开启后才返回内容。

**Non-Goals:**

- 不修改 prompt 文案、summary 算法、tool schema 或 provider adapter。
- 不实现 CachePlan、cache breakpoint、ContextDecision、semantic pruning、eventbus 或 tracing。
- 不新增 prompt 配置字段，不更新生成的 config schema。
- 不把所有 prompt 代码迁移到新子包，也不把 summary/tool schema 强行伪装成 system-message section。

## Decisions

### 1. 先在现有 agent 包内建立 registry

新增内部 `promptRegistry`、`promptSection` 与构造结果类型；Agent 在构造时捕获 startup sections，在每次请求时追加 mode/memory dynamic sections。CLI 通过只读的导出 inspect 函数取得 manifest。

这样能保留 Git snapshot 只在 Agent 启动时采集一次的现有语义，同时避免 `agent/prompt` 子包为了使用 `RuntimeContext`、memory 与配置而产生大范围类型迁移。替代方案是立即创建子包并移动所有构造函数，但会显著放大 diff 和回归面，因此本 change 不采用。

### 2. 固定 section ID 与顺序

main prompt 的 registry 顺序固定为：

1. `coding_agent`
2. `runtime`
3. `workspace_instructions`
4. `git_snapshot`
5. `execution_mode`
6. `memory`

no-tool 构造仍只向 provider 发送 no-tool runtime 和 memory；其它 section 在 manifest 中标为 `omitted`，而不是错误地发送工具指导。section 的消息 role 继续使用当前 `system`。

### 3. manifest 与 provider messages 来源相同

每个 `promptSection` 同时携带内部 message 和元数据，provider message slice 只从 status=`included` 的 section 投影生成，inspect manifest 也从同一批 section 生成。不得单独实现一套“看起来像 prompt”的 CLI 扫描逻辑，否则 inspect 会与真实请求漂移。

manifest 对外包含：`id`、`position`、`role`、`status`、`stability`、`source`、`chars`、`estimated_tokens`、`truncated`，以及可选 `content`。token 数按用户指定或配置的模型，通过现有 provider-neutral estimator 对单个 section message 估算；它是诊断估算，不承诺与 provider billing 完全一致。

### 4. 显式建模状态而不是只用 enabled bool

状态使用：

- `included`：section 实际进入请求；
- `disabled`：配置明确禁用；
- `unavailable`：已启用但本次没有来源内容，例如非 Git workspace 或没有 memory；
- `omitted`：该请求类型有意排除，例如 no-tool 请求中的 coding-agent/tool guidance。

这能区分“用户关掉了”和“当前没有内容”，也让后续 prompt inspect、CachePlan 与 eval 能复用相同语义。

### 5. inspect 是纯本地、默认脱敏的命令

新增 `ub prompt inspect` 子命令，复用 root 的 `--profile`、`--dev`、`--mode` 配置覆盖，并支持：

- `--json`：输出稳定 JSON；
- `--show-content`：显式包含 section content；
- `--model`：只影响 token 估算，缺省使用 `default_model`，不做远程模型发现。

命令使用 canonical current workspace 和现有 `agentRuntimeContext`，只读取配置、AGENTS.md、Git snapshot 与 memory。它不得初始化 provider/tool runtime，不得执行 startup maintenance，也不得创建 session/rollout。

文本输出每个 section 一行元数据；只有 `--show-content` 时追加内容块。JSON 在未指定 `--show-content` 时省略 `content` 字段，而不是输出空内容造成歧义。

### 6. 兼容性测试优先于字符串实现细节

现有 prompt harness 测试保留并转为验证 registry 投影；新增 golden 覆盖默认、plan、no-tool、disabled、无 Git/无 memory。fake provider 行为测试确认主 Agent 实际收到的消息顺序和关键语义没有变化。CLI 测试使用临时 `XDG_CONFIG_HOME`/`XDG_STATE_HOME` 与 workspace，保证不会读取或写入用户真实状态。

## Risks / Trade-offs

- [Risk] registry 元数据与真正发送的 messages 漂移 → provider messages 必须从同一 `promptSection` slice 投影，不维护第二套列表。
- [Risk] inspect 泄露 memory 或项目指令 → 默认省略 content，`--show-content` 必须显式开启；测试锁定 JSON 不含敏感正文。
- [Risk] token 估算逐 section 相加与整体请求略有差异 → 将字段命名为 estimated，并在输出中保留模型；整体 total 使用完整 included message slice 再估算。
- [Risk] 每次请求重新读取 Git snapshot 破坏稳定前缀 → startup sections 在 registry 创建时捕获，Agent 复用；只有独立 inspect 每次运行时重新采集。
- [Risk] 为 no-tool 复用 registry 时误带工具指导 → no-tool variant 显式将不适用 section 标为 omitted，并用现有 BTW/fake-provider 行为测试防回归。

## Migration Plan

1. 增加 registry/manifest 类型并以现有 helper 为底层来源。
2. 把 Agent、`RuntimeContextMessages`、`NoToolRuntimeContextMessages` 切换到 registry 投影，保持原 helper 的兼容测试。
3. 增加 CLI inspect 和文档。
4. 运行 focused tests、repo-wide tests、lint、build/check；若发现 provider-facing 消息差异，回滚调用点到现有构造函数而无需数据迁移。

本 change 不修改持久数据或配置 schema，因此没有磁盘迁移。

## Open Questions

无阻塞问题。section cache eligibility 的最终枚举与 summary/tool schema 是否进入同一 manifest，留到 S4-07/S4-08 设计时决定。
