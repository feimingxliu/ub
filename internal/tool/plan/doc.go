// Package plan implements the plan-then-execute artifact workflow: agents
// (or users) write a structured markdown plan to the user's state directory
// ($XDG_STATE_HOME/ub/plans/<project-key>/), and later mark individual steps
// done / skipped / failed as work proceeds. The tools deliberately use
// RiskSafe because the storage target is ub-managed metadata outside the
// workspace, not user code. The agent controls mode-specific visibility:
// plan_write is only advertised/executable in plan mode, while
// plan_update_step can mark progress after switching to execution modes.
package plan
