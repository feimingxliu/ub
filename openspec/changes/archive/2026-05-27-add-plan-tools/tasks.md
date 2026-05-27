## 1. 包骨架

- [x] 1.1 `internal/tool/plan/doc.go`:包注释,讲清 `.ub/plans/<id>.md` 是 ub
  生成的可读可改的会话艺术品
- [x] 1.2 `internal/tool/plan/storage.go`:常量(plan 子目录、时间戳格式、
  slug 规则)、`planRoot(workspace) string`、`slugify(title)`、`newPlanID`、
  `loadPlan(path) (planDoc, error)` 与 `savePlan(path, planDoc) error`

## 2. plan_write 工具

- [x] 2.1 args:`{title string, steps []string, notes string?}`
- [x] 2.2 Execute:校验非空 → 生成 plan_id → 渲染初始 markdown → 写盘 → 返回
  `Result.Content` 含 plan_id / 绝对路径 / 完整初始内容
- [x] 2.3 Risk = safe

## 3. plan_update_step 工具

- [x] 3.1 args:`{plan_id string, step_index int, status string, note string?}`
  - status ∈ {done, skipped, failed}
- [x] 3.2 Execute:
  - 根据 plan_id 拼路径,读取并解析现有 markdown
  - 校验 step_index ∈ [1, len(steps)]
  - 改写 Steps section 中对应行的 `- [ ]` / `- [x]` / `- [~]` / `- [!]` 标记
  - 在 Log section 末尾追加一行:`- <RFC3339 now> step <i> → <status>[: note]`
  - 当所有步骤都不再是 `[ ]` 时,把 metadata block 的 Status 改成 `complete`
  - 保存覆写
- [x] 3.3 Risk = safe

## 4. 注册

- [x] 4.1 `internal/tool/plan/register.go`:`Register(reg, workspaceRoot string) error`
- [x] 4.2 `internal/cli/root.go` `newToolRuntime`:把 `plan.Register` 加入注册链

## 5. 单测

- [x] 5.1 `storage_test.go`:slugify(空 / 含路径分隔符 / 极长 title)、newPlanID 单调递增
- [x] 5.2 `write_test.go`:happy path、空 steps 拒绝、文件已存在(plan_id 冲突极小概率)报错
- [x] 5.3 `update_test.go`:
  - happy path:`[ ]` → `[x]`,并 append 一条 log 行
  - 不同 status 渲染不同标记
  - 越界 step_index 拒绝
  - 全部完成时 metadata.Status 自动变 complete
  - 不存在的 plan_id 拒绝
- [x] 5.4 `register_test.go`:两个工具都被注册

## 6. 文档

- [x] 6.1 `docs/usage.md`:加 "plan-then-execute" 子节,讲在 plan 模式下用
  `plan_write`、然后切到 work 模式 reference 这个 plan 的工作流
- [x] 6.2 `docs/design.md`:tools 列表补 plan 家族
- [x] 6.3 `openspec/changes/add-plan-tools/specs/plan-tools/spec.md`:Requirements + Scenarios

## 7. 验证

- [x] 7.1 `go test ./internal/tool/plan/...`
- [x] 7.2 `go test ./...`
- [x] 7.3 `make lint`
- [x] 7.4 `make build`
