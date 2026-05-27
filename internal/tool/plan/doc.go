// Package plan implements the plan-then-execute artifact workflow: agents
// (or users) write a structured markdown plan to <workspace>/.ub/plans/, and
// later mark individual steps done / skipped / failed as work proceeds. The
// tools deliberately use RiskSafe so plan mode (which gates RiskWrite) does
// not block them — the storage target is ub-managed metadata under .ub/,
// not user code.
package plan
