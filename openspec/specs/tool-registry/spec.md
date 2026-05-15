# tool-registry Specification

## Purpose
TBD - created by archiving change add-tool-registry. Update Purpose after archive.
## Requirements
### Requirement: Tool 接口

系统 SHALL 在 `internal/tool` 包定义 `Tool` 接口。`Tool` MUST 暴露 `Name() string`、`Description() string`、`Schema() *jsonschema.Schema`、`Risk() Risk` 和 `Execute(ctx context.Context, args json.RawMessage) (Result, error)`。`Schema()` 返回的 JSON Schema MUST 能直接 `json.Marshal` 不报错，用于后续 provider 请求中的 tool 定义。

#### Scenario: 接口形状

- **GIVEN** 一个实现了所有方法的具体类型
- **WHEN** 调用方用 `var _ tool.Tool = impl` 做编译期断言
- **THEN** 编译通过

#### Scenario: Schema 可序列化

- **GIVEN** 一个返回 `*jsonschema.Schema` 的工具实现
- **WHEN** 调用 `json.Marshal(impl.Schema())`
- **THEN** 序列化成功且结果是有效 JSON

### Requirement: Risk 等级

系统 SHALL 定义 `Risk` 枚举类型，取值 MUST 为 `RiskSafe`、`RiskWrite`、`RiskExec` 三种之一。这些常量 MUST 与 `docs/design.md §4` 的风险分级（safe/write/exec）保持一致，供后续 dispatcher 在 mode gate 与权限审批中使用。

#### Scenario: 三档风险等级

- **GIVEN** 工具实现声明 `Risk()` 返回值
- **WHEN** 调用方比较返回值
- **THEN** 该值 MUST 等于 `RiskSafe`、`RiskWrite` 或 `RiskExec` 中的一个

### Requirement: PreviewableTool 可选接口

系统 SHALL 定义可选接口 `PreviewableTool`，嵌入 `Tool` 并新增 `Preview(ctx context.Context, args json.RawMessage) (Preview, error)`。写类工具（write / edit / multiedit）MUST 在后续 iteration 中实现该接口；非写类工具 MAY 不实现。dispatcher MUST 通过 `tool.(PreviewableTool)` type assertion 检测该接口而不是依赖 `Risk()` 值。

#### Scenario: 实现了 Preview 的工具被识别

- **GIVEN** 一个同时实现 `Tool` 和 `Preview` 方法的工具
- **WHEN** 调用方执行 `_, ok := t.(PreviewableTool)`
- **THEN** `ok` MUST 为 true

#### Scenario: 未实现 Preview 的工具不会被误判

- **GIVEN** 一个只实现 `Tool` 接口的工具
- **WHEN** 调用方执行 `_, ok := t.(PreviewableTool)`
- **THEN** `ok` MUST 为 false

### Requirement: Preview 与 FileDiff 数据结构

系统 SHALL 定义 `Preview` 与 `FileDiff` 结构体。`Preview` MUST 包含一行人类可读 `Summary` 和 `Files []FileDiff`；`FileDiff` MUST 包含 `Path`、`Kind`（取值 `create` / `modify` / `delete`）和 `UnifiedDiff` 字段。结构体 MUST 提供 JSON 标签，使得权限弹窗与 rollout 事件可以直接序列化。

#### Scenario: 序列化字段名稳定

- **GIVEN** 一个填充完整的 `Preview` 值
- **WHEN** 调用 `json.Marshal(preview)`
- **THEN** 输出 JSON 中 MUST 包含 `summary`、`files`、`path`、`kind`、`unified_diff` 这些字段名

### Requirement: Result 数据结构

系统 SHALL 定义 `Result` 结构体，MUST 包含 `Content string`（回灌给模型的 tool_result 文本）、`IsError bool` 和 `Files []FileChange`（执行后的实际改动摘要）。`FileChange` 字段集合 MUST 与 `FileDiff` 对齐至少包含 `Path` 与 `Kind`，允许 dispatcher 把实际改动通过 TUI 渲染。

#### Scenario: 错误 Result

- **GIVEN** 工具执行失败
- **WHEN** 工具返回 `Result{Content: "msg", IsError: true}` 与 nil error
- **THEN** 调用方 MUST 能通过 `result.IsError` 区分业务错误与系统 error

### Requirement: Registry 注册与查找

系统 SHALL 提供 `Registry` 类型与 `New()` 构造函数。`Registry.Register(t Tool) error` MUST 以 `t.Name()` 作为键存入；同名再次注册 MUST 返回非空 error，且原工具 MUST 保留不被覆盖。`Registry.Get(name string) (Tool, bool)` MUST 在命中时返回工具与 true，未命中返回 nil 与 false。

#### Scenario: 注册成功

- **GIVEN** 一个新建的 Registry
- **WHEN** 注册一个工具 `read`
- **THEN** `Get("read")` MUST 返回该工具与 true

#### Scenario: 重名报错

- **GIVEN** Registry 已经注册了 `read`
- **WHEN** 再次注册一个同样名为 `read` 的工具
- **THEN** `Register` MUST 返回非空 error，且 `Get("read")` 仍返回最初注册的实例

#### Scenario: 未注册查不到

- **GIVEN** 一个新建的 Registry
- **WHEN** 查询 `Get("ghost")`
- **THEN** 返回 nil 与 false

### Requirement: Registry 列举与 Schemas

系统 SHALL 提供 `Registry.All() []Tool` 返回稳定顺序（按名称升序）的工具列表，并提供 `Registry.Schemas() map[string]*jsonschema.Schema` 返回所有已注册工具的 input schema。`Schemas()` 的结果 MUST 与 `All()` 中工具的 `Schema()` 一一对应。

#### Scenario: All 返回稳定顺序

- **GIVEN** Registry 顺序注册了 `write`、`read`、`ls`
- **WHEN** 调用 `All()`
- **THEN** 返回切片 MUST 按工具名升序排列为 `ls`、`read`、`write`

#### Scenario: Schemas 与 All 一致

- **GIVEN** Registry 注册了若干工具
- **WHEN** 调用 `Schemas()`
- **THEN** 返回 map 的键集合 MUST 与 `All()` 中工具名集合相等，且每个值序列化为 JSON 后 MUST 与对应工具 `Schema()` 序列化结果一致

