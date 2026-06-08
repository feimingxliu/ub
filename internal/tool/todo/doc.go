// Package todo implements the short-lived execution todo view used by
// plan-then-execute workflows. Unlike plan artifacts, todo lists are scoped to
// an agent session and stored as JSON state so the TUI can restore the current
// execution view after resume without mutating plan markdown.
package todo
