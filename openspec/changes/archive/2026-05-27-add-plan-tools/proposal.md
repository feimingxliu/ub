## Why

`roadmap-v2.md` §3-04 把 plan-then-execute 列为 V2 主体之一。现状的 plan
模式只是"工具拦截"(`execution.Gate` 在 plan 模式下拒绝所有 `RiskWrite`
工具),并没有"先产出 plan 工件,再切到 work 按工件执行"的工作流。

把 plan artifact 落到 `.ub/plans/<id>.md` 有两个直接收益:

1. plan 模式不再只是"什么都不做地告诉用户该做什么",而是真的产出一个
   可 review、可 git diff、可在多轮会话里 reference 的工件
2. work 模式下 agent 可以 `read` 这个文件、`plan_update_step` 标记进度,
   形成"长任务可中断/可恢复"的最小语义

本 change 只交付**最小可用版本**:plan_write + plan_update_step 两个工具,
存储格式定型。"切到 work 后自动注入 plan 到 system prompt"这一步留给后续
change(避免与 §3-02 memory 的注入路径冲突)。

## What Changes

- 新建 `internal/tool/plan/` 包:
  - `plan_write(title, steps[], notes?)`:在 `<workspace>/.ub/plans/` 写一个
    新 plan markdown,文件名为 `<RFC3339 时间戳>-<slug>.md`(从 title 派生);
    返回 plan_id(= 文件 basename 去掉 .md)、绝对路径与渲染后的初始 markdown
  - `plan_update_step(plan_id, step_index, status, note?)`:把第 N 步(1-based)
    标记为 `done` / `skipped` / `failed`,并在文件末尾 append 一条日志行
  - 两个工具的 `Risk` 都是 `RiskSafe`,这样 plan 模式不会拦截它们。理由:
    它们只写 ub 内部艺术品目录 `.ub/plans/`,不动用户代码;调用方可以选择是否
    在 permission 层把它们也走人审批
- markdown 模板(详见 spec.md):标题、metadata block(created/status)、Steps
  以 GitHub-style task list 表达、Notes、Log;`plan_update_step` 通过原地
  改写 Steps 行的 checkbox + 在 Log section 追加新行实现
- 注册:`internal/tool/plan/register.go` 暴露 `Register(reg, workspaceRoot)`,
  在 `cli/root.go` 与 `cli/tui.go` 的 `newToolRuntime` 中调用
- 不修改 `execution.Gate`:`RiskSafe` 工具天然能在 plan 模式下跑
- 文档:`docs/usage.md` 加一个"plan-then-execute"工作流小节;`docs/design.md`
  在 tools 列表里补上 plan 工具家族

## Capabilities

### New Capabilities

- `plan-tools`:`plan_write` 与 `plan_update_step` 两个工具的 schema、文件
  存储位置、markdown 模板与原地更新规则

## Impact

- 新增 `internal/tool/plan/` 包(write.go / update.go / register.go + 测试)
- 修改 `internal/cli/root.go`:把 plan.Register 加入 newToolRuntime 注册链
- 修改 `docs/usage.md` 与 `docs/design.md`
- 不引入新依赖
- 不改 execution / agent / config schema
- breaking change:无
