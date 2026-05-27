# task-subagent Specification

## Purpose
TBD - created by archiving change add-task-subagent. Update Purpose after archive.
## Requirements
### Requirement: SubagentRunner ctx 助手

`internal/tool` 包 SHALL 暴露 `SubagentRunner` 接口与配套的 ctx 助手:

```go
type SubagentRunner interface {
    RunSubagent(ctx context.Context, prompt string, maxTurns int) (string, error)
}

func WithSubagentRunner(ctx context.Context, runner SubagentRunner) context.Context
func SubagentRunnerFromContext(ctx context.Context) SubagentRunner

func WithSubagentDepth(ctx context.Context, depth int) context.Context
func SubagentDepthFromContext(ctx context.Context) int
```

`runner` 为 nil 时 `WithSubagentRunner` MUST 直接返回原 ctx。`depth` 默认 0,`task` 工具在调用 sub-runner 前 MUST 把 depth + 1 注入到 ctx 上,以便深度限制生效。

#### Scenario: round-trip

- **GIVEN** 一个实现 SubagentRunner 的 R
- **WHEN** `ctx := WithSubagentRunner(parent, R); got := SubagentRunnerFromContext(ctx)`
- **THEN** got == R

#### Scenario: nil runner 不入 ctx

- **WHEN** 调用 `WithSubagentRunner(parent, nil)`
- **THEN** 返回的 ctx 与 parent 一致,SubagentRunnerFromContext 返回 nil

#### Scenario: depth 默认为 0

- **GIVEN** 全新的 context.Background()
- **WHEN** 调用 `SubagentDepthFromContext`
- **THEN** 返回 0

### Requirement: task 工具

系统 SHALL 提供 `task` 工具,`Risk` 为 `RiskSafe`。input MUST 含 `prompt: string`(必填),可选 `max_turns: int`(默认由子 agent 自己的 maxTurns 决定)。

Execute MUST 依次执行:

1. 校验 `prompt` 非空
2. 从 ctx 取 SubagentRunner;为 nil 时返回错误 `task: subagent runner not configured`
3. 从 ctx 读当前 depth;`>= 1` 时返回错误 `task: max subagent depth (1) reached`
4. 调用 `runner.RunSubagent(WithSubagentDepth(ctx, depth+1), prompt, maxTurns)`
5. 把返回的字符串作为 `Result.Content` 返回;子 agent 报错则把错误信息作为 IsError=true 的 Content

`Result.Files` MUST 为空(子 agent 的文件改动各自走自己的 tool result;这里不重复声明)。

#### Scenario: happy path

- **GIVEN** ctx 含一个 runner,且它的 `RunSubagent` 返回 `"sub did X"`,nil
- **WHEN** 调用 `task(prompt="explore module X")`
- **THEN** `Result.Content` MUST 包含 `"sub did X"`,IsError 为 false

#### Scenario: 缺 runner

- **GIVEN** ctx 中没有 SubagentRunner
- **WHEN** 调用 `task(prompt="...")`
- **THEN** 工具 MUST 返回错误 `task: subagent runner not configured`

#### Scenario: 深度限制

- **GIVEN** ctx 中 depth = 1
- **WHEN** 调用 `task(prompt="recurse")`
- **THEN** 工具 MUST 返回包含 `max subagent depth` 字样的错误且不调用 runner

#### Scenario: 空 prompt

- **WHEN** 调用 `task(prompt="")`
- **THEN** 工具 MUST 返回 `prompt is required` 错误

### Requirement: agent runtime 注入 SubagentRunner

agent runtime MUST 在 `runTool` 调用 `t.Execute(ctx, ...)` 之前,当 `Options.SubagentRunner` 非 nil 时,把它注入到 ctx:`ctx = tool.WithSubagentRunner(ctx, a.subagentRunner)`。`Options.SubagentRunner` 为 nil 时 MUST 不调用 WithSubagentRunner。

#### Scenario: 注入存在

- **GIVEN** 一个 agent 配了 `Options.SubagentRunner = R`
- **WHEN** 它的 `task` 工具被调用
- **THEN** `task` 工具 Execute 时从 ctx 取到的 runner MUST == R

