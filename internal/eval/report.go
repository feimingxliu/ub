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
