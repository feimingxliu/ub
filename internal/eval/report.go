package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func RenderJSON(w io.Writer, report Report) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func RenderMatrixJSON(w io.Writer, report MatrixReport) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func RenderMatrixText(w io.Writer, report MatrixReport) error {
	status := "PASS"
	if !report.Passed {
		status = "FAIL"
	}
	if _, err := fmt.Fprintf(w, "Eval Matrix %s: tasks=%d targets=%d repeat=%d parallel=%d\n",
		status, len(report.Tasks), len(report.Targets), report.Repeat, report.Parallel); err != nil {
		return err
	}
	if err := renderMatrixAggregate(w, "Overall", report.Overall); err != nil {
		return err
	}
	for _, group := range report.ByTarget {
		if err := renderMatrixAggregate(w, "Target "+group.Key, group.Aggregate); err != nil {
			return err
		}
	}
	for _, group := range report.ByTask {
		if err := renderMatrixAggregate(w, "Task "+group.Key, group.Aggregate); err != nil {
			return err
		}
	}
	for _, run := range report.Runs {
		switch run.Status {
		case MatrixStatusFailed:
			failure := "failed"
			category := ""
			if run.Report != nil {
				failure = run.Report.Failure
				category = run.Report.FailureCategory
			}
			if _, err := fmt.Fprintf(w, "FAIL %s [%s]: %s (%s)\n", run.ID, category, failure, run.Status); err != nil {
				return err
			}
		case MatrixStatusSkipped:
			if _, err := fmt.Fprintf(w, "SKIP %s: %s\n", run.ID, run.SkipReason); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderMatrixAggregate(w io.Writer, label string, aggregate MatrixAggregate) error {
	_, err := fmt.Fprintf(w,
		"%s: planned=%d executed=%d passed=%d failed=%d skipped=%d pass_rate=%.1f%% duration=%dms avg_duration=%.1fms avg_turns=%.2f tokens=%d/%d reasoning=%d cache=%d/%d\n",
		label, aggregate.Planned, aggregate.Executed, aggregate.Passed, aggregate.Failed, aggregate.Skipped,
		aggregate.PassRate*100, aggregate.DurationMillis, aggregate.AverageDurationMS, aggregate.AverageTurns,
		aggregate.InputTokens, aggregate.OutputTokens, aggregate.ReasoningTokens,
		aggregate.CacheReadTokens, aggregate.CacheWriteTokens)
	return err
}

func RenderText(w io.Writer, report Report) error {
	status := "PASS"
	if !report.Passed {
		status = "FAIL"
	}
	if _, err := fmt.Fprintf(w, "Eval %s: %s\n", status, report.Task); err != nil {
		return err
	}
	if report.Failure != "" {
		if _, err := fmt.Fprintf(w, "Failure: %s (%s)\n", report.Failure, report.FailureCategory); err != nil {
			return err
		}
	}
	if report.AgentStderr != "" {
		if _, err := fmt.Fprintf(w, "Agent stderr: %s\n", report.AgentStderr); err != nil {
			return err
		}
	}
	if !report.Runtime.Empty() {
		if _, err := fmt.Fprintf(w, "Runtime: max_context_tokens=%d trigger_ratio=%g keep_recent_turns=%d\n",
			intValue(report.Runtime.MaxContextTokens), floatValue(report.Runtime.Context.TriggerRatio), intValue(report.Runtime.Context.KeepRecentTurns)); err != nil {
			return err
		}
	}
	metrics := report.Metrics
	if _, err := fmt.Fprintf(w, "Metrics: duration=%dms turns=%d tokens=%d/%d reasoning=%d cache=%d/%d\n",
		metrics.DurationMillis, metrics.Turns, metrics.InputTokens, metrics.OutputTokens,
		metrics.ReasoningTokens, metrics.CacheReadTokens, metrics.CacheWriteTokens); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Tools: %s\n", strings.Join(metrics.ToolCalls, " -> ")); err != nil {
		return err
	}
	if len(metrics.ContextDecisions) > 0 {
		parts := make([]string, 0, len(metrics.ContextDecisions))
		for _, decision := range metrics.ContextDecisions {
			part := decision.Action
			if decision.Reason != "" {
				part += "(" + decision.Reason + ")"
			}
			parts = append(parts, part)
		}
		if _, err := fmt.Fprintf(w, "Context: %s\n", strings.Join(parts, " -> ")); err != nil {
			return err
		}
	}
	for _, assertion := range report.Assertions {
		mark := "✓"
		if !assertion.Passed {
			mark = "✗"
		}
		if _, err := fmt.Fprintf(w, "%s %s", mark, assertion.Name); err != nil {
			return err
		}
		if assertion.Message != "" {
			if _, err := fmt.Fprintf(w, ": %s", assertion.Message); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if report.Workspace != "" {
		_, err := fmt.Fprintf(w, "Workspace: %s\n", report.Workspace)
		return err
	}
	return nil
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func floatValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
