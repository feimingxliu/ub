// Package plan implements the plan-then-execute artifact workflow: agents
// (or users) write a structured markdown plan to <workspace>/.ub/plans/, and
// later mark individual steps done / skipped / failed as work proceeds. The
// tools deliberately use RiskSafe because the storage target is ub-managed
// metadata under .ub/, not user code. The agent controls mode-specific
// visibility: plan_write is only advertised/executable in plan mode, while
// plan_update_step can mark progress after switching to execution modes.
package plan
