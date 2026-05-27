package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/hook"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
)

const (
	maxActivitySummaryRunes = 180
	maxActivityDetailRunes  = 4000
)

func (a *Agent) emitThinkingActivity(summary, detail string) (Event, bool) {
	// Use raw equality rather than TrimSpace so paragraph-break deltas
	// ("\n\n") still emit — they carry the only signal the TUI has to insert
	// a paragraph break into the streamed thinking block.
	if summary == "" && detail == "" {
		return Event{}, false
	}
	event := Event{
		Type:         EventActivity,
		ActivityKind: ActivityThinking,
		Summary:      truncateActivitySummary(summary),
		Content:      truncateActivityDetail(detail),
	}
	a.emit(event)
	return event, true
}

func (a *Agent) emitToolActivity(call toolCall, status, summary, content string, isError bool) {
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityTool,
		ToolUseID:    call.ID,
		ToolName:     call.Name,
		Status:       status,
		Summary:      truncateActivitySummary(summary),
		Content:      truncateActivityDetail(content),
		IsError:      isError,
	})
}

// emitHookOutcomes turns one Decision into one Activity per hook outcome so
// the TUI and rollout can show "which hook ran, did it succeed, what did it
// say." Skipped (filtered-out) hooks produce no outcome here, so they're
// silently absent — matching the user's intent.
func (a *Agent) emitHookOutcomes(dec hook.Decision) {
	if len(dec.Outcomes) == 0 {
		return
	}
	for _, out := range dec.Outcomes {
		status := "done"
		summary := fmt.Sprintf("hook %s exit=%d in %s", strings.Join(out.Command, " "), out.ExitCode, out.Duration.Round(time.Millisecond))
		isError := false
		if out.Err != nil {
			status = "failed"
			isError = true
		} else if out.ExitCode != 0 {
			status = "failed"
			isError = true
		}
		if dec.Block {
			status = "blocked"
			isError = true
		}
		var content strings.Builder
		if out.Stdout != "" {
			content.WriteString("stdout:\n")
			content.WriteString(out.Stdout)
		}
		if out.Stderr != "" {
			if content.Len() > 0 {
				content.WriteString("\n")
			}
			content.WriteString("stderr:\n")
			content.WriteString(out.Stderr)
		}
		if out.Err != nil {
			if content.Len() > 0 {
				content.WriteString("\n")
			}
			content.WriteString("err: ")
			content.WriteString(out.Err.Error())
		}
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityHook,
			Status:       status,
			Summary:      truncateActivitySummary(summary),
			Content:      truncateActivityDetail(content.String()),
			IsError:      isError,
		})
	}
}

func (a *Agent) emitPermissionActivity(toolName, source, decision, reason string, allowed bool) {
	if strings.TrimSpace(decision) == "" {
		decision = "unknown"
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityPermission,
		ToolName:     toolName,
		Source:       source,
		Decision:     decision,
		Reason:       truncateActivitySummary(redactText("reason", reason)),
		Allowed:      allowed,
		IsError:      decision == "error",
	})
}

func rolloutActivityPayload(event Event) rollout.ActivityPayload {
	return rollout.ActivityPayload{
		ActivityKind: string(event.ActivityKind),
		ToolUseID:    event.ToolUseID,
		ToolName:     event.ToolName,
		Status:       event.Status,
		Summary:      event.Summary,
		Content:      event.Content,
		Decision:     event.Decision,
		Source:       event.Source,
		Reason:       event.Reason,
		Allowed:      event.Allowed,
		IsError:      event.IsError,
	}
}

func SummarizeToolInput(name string, raw json.RawMessage) string {
	var body map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			return "input: invalid JSON"
		}
	}
	if body == nil {
		body = map[string]any{}
	}

	var parts []string
	add := func(label, key string) {
		if value, ok := safeStringField(body, key); ok {
			parts = append(parts, label+"="+value)
		}
	}
	addCommand := func() {
		if value, ok := commandStringField(body, "command"); ok {
			parts = append(parts, "cmd="+truncateActivitySummary(redactText("command", firstLine(value))))
		}
		add("cwd", "cwd")
	}

	switch strings.TrimSpace(name) {
	case "bash", "job_run":
		addCommand()
	case "read", "ls":
		add("path", "path")
	case "write":
		add("path", "path")
		if value, ok := rawStringField(body, "content"); ok {
			parts = append(parts, fmt.Sprintf("content=%d bytes", len(value)))
		}
	case "edit":
		add("path", "path")
		if value, ok := safeStringField(body, "replace_all"); ok {
			parts = append(parts, "replace_all="+value)
		}
		parts = append(parts, "change=text replacement")
	case "glob":
		add("pattern", "pattern")
		add("path", "path")
	case "grep":
		add("pattern", "pattern")
		add("path", "path")
		add("include", "include")
	case "job_output", "job_kill":
		add("job_id", "job_id")
	default:
		for _, key := range []string{"path", "pattern", "include", "command", "cwd", "job_id", "limit", "offset", "tail", "timeout_ms"} {
			add(key, key)
		}
	}

	if len(parts) == 0 {
		return "input accepted"
	}
	return truncateActivitySummary(strings.Join(parts, ", "))
}

func summarizeToolResult(result tool.Result) string {
	if len(result.Files) > 0 {
		paths := make([]string, 0, min(len(result.Files), 3))
		for i, file := range result.Files {
			if i >= 3 {
				break
			}
			path := strings.TrimSpace(file.Path)
			if path == "" {
				path = "file"
			}
			kind := strings.TrimSpace(file.Kind)
			if kind == "" {
				kind = "changed"
			}
			paths = append(paths, kind+" "+path)
		}
		suffix := ""
		if len(result.Files) > len(paths) {
			suffix = fmt.Sprintf(", +%d more", len(result.Files)-len(paths))
		}
		return truncateActivitySummary(strings.Join(paths, ", ") + suffix)
	}
	content := strings.TrimSpace(result.Content)
	if content == "" {
		if result.IsError {
			return "failed"
		}
		return "completed"
	}
	return truncateActivitySummary(redactText("content", firstLine(content)))
}

func toolResultDetail(result tool.Result) string {
	if len(result.Files) == 0 {
		return summarizeToolResult(result)
	}
	var b strings.Builder
	for _, file := range result.Files {
		path := strings.TrimSpace(file.Path)
		if path == "" {
			path = "file"
		}
		kind := strings.TrimSpace(file.Kind)
		if kind == "" {
			kind = "changed"
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(kind)
		b.WriteString(" ")
		b.WriteString(path)
		if diff := strings.TrimRight(file.UnifiedDiff, "\n"); strings.TrimSpace(diff) != "" {
			b.WriteString("\n")
			b.WriteString(diff)
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		return summarizeToolResult(result)
	}
	return b.String()
}

func reasoningSummary(text, fallback string) string {
	text = strings.TrimSpace(text)
	if text != "" {
		return truncateActivitySummary(redactText("reasoning", text))
	}
	return truncateActivitySummary(redactText("reasoning", fallback))
}

func reasoningDetail(text, fallback string) string {
	// Preserve whitespace-only chunks (paragraph breaks) — trimming here would
	// drop "\n\n" deltas and collapse the streamed thinking into one paragraph.
	if text == "" {
		text = fallback
	}
	return redactText("reasoning", text)
}

func safeStringField(body map[string]any, key string) (string, bool) {
	value, ok := rawStringField(body, key)
	if !ok {
		return "", false
	}
	value = redactText(key, value)
	if strings.TrimSpace(value) == "" {
		return "", false
	}
	return truncateActivitySummary(value), true
}

func commandStringField(body map[string]any, key string) (string, bool) {
	value, ok := body[key]
	if !ok || value == nil {
		return "", false
	}
	if typed, ok := value.(string); ok {
		typed = strings.TrimSpace(typed)
		return typed, typed != ""
	}
	return rawStringField(body, key)
}

func rawStringField(body map[string]any, key string) (string, bool) {
	value, ok := body[key]
	if !ok || value == nil {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return "", false
		}
		return strings.Join(strings.Fields(typed), " "), true
	case bool:
		return strconv.FormatBool(typed), true
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10), true
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	default:
		raw, err := json.Marshal(typed)
		if err != nil || len(raw) == 0 || string(raw) == "null" {
			return "", false
		}
		return string(raw), true
	}
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return strings.TrimSpace(text)
}

func redactText(key, value string) string {
	if isSensitive(key) || isSensitive(value) {
		return "[redacted]"
	}
	return value
}

func isSensitive(text string) bool {
	text = strings.ToLower(text)
	for _, marker := range []string{"api_key", "apikey", "authorization", "bearer ", "password", "passwd", "secret", "token"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func truncateActivitySummary(text string) string {
	// Activity summaries are rendered as a single-line label (chip or status
	// row). Collapse all interior whitespace so reasoning summaries — which the
	// model often produces with embedded "\n\n" paragraph breaks — don't end up
	// pushing the TUI footer off-screen when the chip is rendered.
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= maxActivitySummaryRunes {
		return text
	}
	return string(runes[:maxActivitySummaryRunes-3]) + "..."
}

func truncateActivityDetail(text string) string {
	text = strings.TrimRight(text, " \t\r\n")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxActivityDetailRunes {
		return text
	}
	marker := "\n... (truncated)"
	budget := maxActivityDetailRunes - len([]rune(marker))
	if budget < 0 {
		budget = 0
	}
	return strings.TrimRight(string(runes[:budget]), " \t\r\n") + marker
}
