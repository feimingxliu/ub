## ADDED Requirements

### Requirement: 上下文窗口解析结果

系统 SHALL 提供统一的 ContextWindowResolver，并为当前 provider endpoint/model 返回非负 `max_tokens`、稳定 `source` 和 `confidence`。当没有任何可信候选时，resolver MUST 返回 `max_tokens=0` 和 unknown 来源，且调用方 MUST 保持仅上报 used tokens、不按窗口阈值自动 compact 的安全降级行为。

#### Scenario: 未知窗口安全降级

- **GIVEN** 当前模型没有显式配置、模型元信息、provider caps 或历史观察
- **WHEN** resolver 解析上下文窗口
- **THEN** 结果 MUST 为 `max_tokens=0`
- **AND** source MUST 表示 unknown

#### Scenario: 解析结果包含来源和置信度

- **GIVEN** resolver 选择了一个有效窗口候选
- **WHEN** 调用方读取解析结果
- **THEN** 结果 MUST 同时包含稳定 source 和 confidence

### Requirement: 历史 usage 与 overflow 学习

系统 SHALL 把主 provider 成功返回的 input usage 记录为同一 cache key 的已接受下界，并在 context overflow 时记录明确窗口或保守上界。带明确窗口数值的 overflow MUST 能修正非显式静态候选；没有明确数值的 overflow 估算上界 MUST 仅在高于已接受 usage 且低于当前静态候选时收紧窗口。成功 usage 大于非显式候选时，resolver MUST 至少把结果提高到已接受值，并标记为低置信度 usage 学习结果。

#### Scenario: 明确 overflow 修正 provider 默认值

- **GIVEN** provider caps 声明 128000 tokens，且没有显式模型配置
- **WHEN** provider 返回包含 `maximum context length is 8192 tokens` 的 context overflow
- **THEN** resolver 后续结果 MUST 为 8192 tokens
- **AND** source MUST 表示 learned overflow

#### Scenario: 无数字 overflow 形成保守上界

- **GIVEN** provider caps 声明 128000 tokens，已成功 usage 为 7000 tokens
- **WHEN** 估算为 9000 tokens 的请求返回无法提取明确窗口的 context overflow
- **THEN** resolver 后续结果 MUST 使用 9000 tokens 的低置信度上界

#### Scenario: 冲突 overflow 不低于成功 usage

- **GIVEN** 同一 cache key 已成功接受 10000 input tokens
- **WHEN** overflow 文本被解析为 8192 tokens
- **THEN** resolver MUST NOT 把有效窗口缩小到 8192 tokens

#### Scenario: Usage 抬高过小静态候选

- **GIVEN** provider caps 声明 8192 tokens，且没有显式模型配置
- **WHEN** provider 成功返回 12000 input tokens 的 usage
- **THEN** resolver 后续结果 MUST 不小于 12000 tokens
- **AND** confidence MUST 表示该结果不是精确最大值

### Requirement: 窗口观察缓存隔离与隐私

系统 SHALL 按 provider、规范化 endpoint 和完整 model ID 隔离窗口观察。默认文件缓存 MUST 位于 ub 的 XDG state root 下，MUST 使用原子替换写入，并 MUST NOT 持久化 endpoint userinfo、query、fragment、API key、prompt 或消息正文。

#### Scenario: 不同 endpoint 不共享观察

- **GIVEN** 两个 provider 配置使用相同 model ID 但不同 base URL
- **WHEN** 其中一个 endpoint 记录 context overflow
- **THEN** 另一个 endpoint 的 resolver MUST NOT 使用该观察

#### Scenario: Endpoint 敏感部分不落盘

- **GIVEN** base URL 包含 userinfo、query 或 fragment
- **WHEN** resolver 生成 cache key 并保存观察
- **THEN** 持久数据 MUST 不包含这些敏感部分

#### Scenario: 原子保存观察

- **WHEN** 文件缓存保存一个窗口观察
- **THEN** 目标 JSON 文件 MUST 通过同目录临时文件和 rename 替换
- **AND** 文件权限 MUST 限制为当前用户读写

### Requirement: 窗口缓存失败安全降级

窗口观察属于可丢弃派生状态。cache 文件缺失、损坏或不可写时，系统 MUST 回退到当前可用的显式配置、模型元信息或 provider caps，并 MUST NOT 因缓存故障阻断 Agent 启动、provider 请求或 overflow recovery。

#### Scenario: 损坏缓存不阻断解析

- **GIVEN** 当前 cache 文件不是合法 JSON
- **WHEN** CLI 创建 ContextWindowResolver
- **THEN** 系统 MUST 使用无历史观察的静态候选继续
- **AND** Agent 启动 MUST NOT 因该文件失败

#### Scenario: 保存失败不改变 provider 结果

- **GIVEN** observation store 不可写
- **WHEN** Agent 收到成功 usage 或 context overflow
- **THEN** Agent MUST 保留进程内观察并继续正常请求或 recovery 流程

## MODIFIED Requirements

### Requirement: 上下文用量上报

系统 SHALL 在 Agent 准备 provider 请求时上报当前请求消息的 token 估算值。若 ContextWindowResolver 返回有效最大上下文，系统 MUST 同时上报最大上下文、使用比例、窗口来源和置信度；若最大上下文未知，系统 MUST 仍上报估算 token 数。

#### Scenario: 请求前上报上下文用量

- **GIVEN** resolver 返回最大上下文 100、source 为 provider caps
- **WHEN** Agent 准备发起 provider 请求并估算当前消息为 25 token
- **THEN** Agent runtime event MUST 包含 used tokens 25
- **AND** runtime event MUST 包含 max tokens 100、ratio 0.25、窗口 source 和 confidence

#### Scenario: 最大上下文未知时上报 used tokens

- **GIVEN** resolver 未得到有效最大上下文
- **WHEN** Agent 准备发起 provider 请求并完成 token 估算
- **THEN** Agent runtime event MUST 包含 used tokens
- **AND** Agent runtime event MUST 不要求包含 context ratio

### Requirement: 模型级上下文窗口优先级

系统 SHALL 在判断自动 summary 阈值、manual compact 保留预算和上报 context used/max/% 时统一使用 ContextWindowResolver。当前模型显式配置的 `max_context_tokens` MUST 具有最高优先级且不被历史观察覆盖；没有显式配置时，resolver MUST 依次考虑有效的 learned overflow、模型元信息、provider model caps 和 learned usage 修正。当所有候选都未知时，系统 MUST 跳过自动 summary 阈值判断并仅上报 used tokens。

#### Scenario: 自动 summary 使用显式模型级窗口

- **GIVEN** provider 默认最大上下文为 128000，当前模型显式配置 `max_context_tokens: 200000`
- **WHEN** 当前请求 token 估算为 170000，`context.trigger_ratio` 为 0.8
- **THEN** Agent MUST 不触发自动 summary

#### Scenario: 显式配置不被历史 overflow 覆盖

- **GIVEN** 当前模型显式配置 `max_context_tokens: 200000`
- **AND** 同一 cache key 存在 learned overflow 128000
- **WHEN** resolver 解析窗口
- **THEN** 结果 MUST 仍为 200000 tokens
- **AND** source MUST 表示显式配置

#### Scenario: 未配置模型级窗口时使用 learned overflow

- **GIVEN** 当前模型未显式配置 `max_context_tokens`，provider 最大上下文为 128000
- **AND** 同一 cache key 学到明确 overflow 窗口 64000
- **WHEN** Agent 上报请求 token 估算为 32000
- **THEN** runtime event MUST 包含 max tokens 64000 和 ratio 0.5

#### Scenario: 无历史观察时回退 provider caps

- **GIVEN** 当前模型未配置窗口、没有模型元信息且没有历史观察，provider 最大上下文为 128000
- **WHEN** Agent 上报请求 token 估算为 64000
- **THEN** runtime event MUST 包含 max tokens 128000 和 ratio 0.5

### Requirement: Usage 估算校正接入

系统 SHALL 在主 provider stream 返回输入 usage 后，把本轮请求前估算值与实际 input usage 传给 token 估算校正接口，并把实际 input usage 作为成功观察回灌当前 ContextWindowResolver。任一回灌失败 MUST 不改变 provider 响应结果。

#### Scenario: Usage 同时回灌估算器与窗口 resolver

- **GIVEN** Agent 发起主 provider 请求前得到估算值
- **WHEN** provider stream 返回 input usage
- **THEN** 系统 MUST 调用 usage 校正接口更新同模型后续 token 估算
- **AND** 系统 MUST 更新同 provider endpoint/model 的已接受 usage 观察
