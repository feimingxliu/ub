package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/pkg/runtime/hook"
	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
)

const (
	maxActivitySummaryRunes    = 180
	maxActivityDetailRunes     = 4000
	maxToolActivityDetailRunes = 12000
	shellMetadataOpenTag       = "<shell_metadata>\n"
	shellMetadataCloseTag      = "</shell_metadata>"
	toolResultTruncatedMarker  = "... [tool result truncated:"
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
		Content:      truncateToolActivityDetail(content),
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

func (a *Agent) emitPermissionActivity(toolName, source, decision, reason string, allowed bool) Event {
	if strings.TrimSpace(decision) == "" {
		decision = "unknown"
	}
	event := Event{
		Type:         EventActivity,
		ActivityKind: ActivityPermission,
		ToolName:     toolName,
		Source:       source,
		Decision:     decision,
		Reason:       truncateActivitySummary(redactText("reason", reason)),
		Allowed:      allowed,
		IsError:      decision == "error",
	}
	a.emit(event)
	return event
}

func rolloutActivityPayload(event Event) rollout.ActivityPayload {
	return rollout.ActivityPayload{
		ActivityKind:    string(event.ActivityKind),
		ToolUseID:       event.ToolUseID,
		ToolName:        event.ToolName,
		ParentToolUseID: event.ParentToolUseID,
		SubagentID:      event.SubagentID,
		Status:          event.Status,
		Summary:         event.Summary,
		Content:         event.Content,
		Decision:        event.Decision,
		Source:          event.Source,
		Reason:          event.Reason,
		Allowed:         event.Allowed,
		IsError:         event.IsError,
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
	addPositiveInt := func(label, key string) {
		if value, ok := positiveIntField(body, key); ok {
			parts = append(parts, fmt.Sprintf("%s=%d", label, value))
		}
	}
	addCount := func(label, key string) {
		value, ok := body[key]
		if !ok || value == nil {
			return
		}
		if items, ok := value.([]any); ok {
			parts = append(parts, fmt.Sprintf("%s=%d", label, len(items)))
		}
	}
	addUniqueObjectStringCount := func(label, key, field string) {
		value, ok := body[key]
		if !ok || value == nil {
			return
		}
		items, ok := value.([]any)
		if !ok {
			return
		}
		seen := map[string]struct{}{}
		for _, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			value, ok := rawStringField(obj, field)
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			if value != "" {
				seen[value] = struct{}{}
			}
		}
		if len(seen) > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", label, len(seen)))
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
	case "task":
		if value, ok := rawStringField(body, "prompt"); ok {
			parts = append(parts, "prompt="+truncateActivitySummary(redactText("prompt", firstLine(value))))
		}
		add("max_turns", "max_turns")
	case "remember":
		add("scope", "scope")
		add("text", "text")
	case "ask":
		addCount("questions", "questions")
	case "enter_plan_mode":
		add("reason", "reason")
	case "exit_plan_mode":
		add("plan_id", "plan_id")
		add("summary", "summary")
	case "plan_write":
		add("title", "title")
		addCount("steps", "steps")
	case "plan_update":
		add("plan_id", "plan_id")
		add("title", "title")
		addCount("steps", "steps")
	case "plan_update_step":
		add("plan_id", "plan_id")
		add("step", "step_index")
		add("status", "status")
	case "todo_write":
		addCount("items", "items")
		addCount("items", "todos")
		addCount("items", "tasks")
	case "todo_update":
		add("id", "id")
		addPositiveInt("item", "item_index")
		add("status", "status")
	case "multiedit":
		addCount("edits", "edits")
		addUniqueObjectStringCount("files", "edits", "path")
	case "tool_result":
		add("tool_use_id", "tool_use_id")
		add("offset", "offset")
		add("limit", "limit")
	case "diagnostics", "document_symbols":
		add("file", "file")
	case "references":
		add("symbol", "symbol")
		add("file", "file")
		add("line", "line")
		add("col", "col")
	case "hover", "completion", "code_action":
		add("file", "file")
		add("line", "line")
		add("col", "col")
	case "rename":
		add("file", "file")
		add("line", "line")
		add("col", "col")
		add("new_name", "new_name")
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
	case "web_search":
		add("query", "query")
		addCount("domains", "domains")
		add("limit", "limit")
	case "web_fetch":
		add("url", "url")
		add("max_chars", "max_chars")
	case "job_output", "job_kill":
		add("job_id", "job_id")
	default:
		for _, key := range []string{"path", "file", "pattern", "include", "command", "cwd", "job_id", "tool_use_id", "prompt", "title", "symbol", "query", "scope", "text", "line", "col", "limit", "offset", "tail", "timeout_ms", "max_turns"} {
			add(key, key)
		}
	}

	if len(parts) == 0 {
		if len(body) > 0 {
			if raw, err := json.Marshal(body); err == nil && len(raw) > 0 && string(raw) != "null" {
				return "input=" + truncateActivitySummary(redactText("input", string(raw)))
			}
		}
		return "input accepted"
	}
	return truncateActivitySummary(strings.Join(parts, ", "))
}

// ToolInputDetail returns a readable expanded view for high-value tool inputs
// without dumping the raw tool-call JSON into the activity stream.
func ToolInputDetail(name string, raw json.RawMessage) string {
	body, ok := decodeToolInput(raw)
	if !ok {
		return ""
	}
	var b strings.Builder
	addBlock := func(label, key string) {
		value, ok := preservedStringField(body, key)
		if !ok {
			return
		}
		value = redactText(key, strings.TrimRight(value, " \t\r\n"))
		if strings.TrimSpace(value) == "" {
			return
		}
		writeDetailBlock(&b, label, value)
	}
	addLine := func(label, key string) {
		value, ok := rawStringField(body, key)
		if !ok {
			return
		}
		value = redactText(key, value)
		if strings.TrimSpace(value) == "" {
			return
		}
		writeDetailLine(&b, label, value)
	}

	switch strings.TrimSpace(name) {
	case "bash", "job_run":
		addBlock("command", "command")
		addLine("cwd", "cwd")
		addLine("timeout_ms", "timeout_ms")
	case "task":
		addBlock("prompt", "prompt")
		addLine("max_turns", "max_turns")
	case "ask":
		if questions, ok := body["questions"].([]any); ok {
			var lines []string
			for _, item := range questions {
				q, ok := item.(map[string]any)
				if !ok {
					continue
				}
				header, _ := rawStringField(q, "header")
				question, _ := rawStringField(q, "question")
				if strings.TrimSpace(header) == "" && strings.TrimSpace(question) == "" {
					continue
				}
				line := strings.TrimSpace(header)
				if strings.TrimSpace(question) != "" {
					if line != "" {
						line += ": "
					}
					line += strings.TrimSpace(question)
				}
				lines = append(lines, line)
			}
			if len(lines) > 0 {
				writeDetailBlock(&b, "questions", strings.Join(lines, "\n"))
			}
		}
	case "enter_plan_mode":
		addBlock("reason", "reason")
	case "exit_plan_mode":
		addLine("plan_id", "plan_id")
		addBlock("summary", "summary")
	case "web_search":
		addBlock("query", "query")
		addLine("limit", "limit")
	case "web_fetch":
		addBlock("url", "url")
		addLine("max_chars", "max_chars")
	default:
		if _, hasCommand := body["command"]; hasCommand {
			addBlock("command", "command")
			addLine("cwd", "cwd")
			addLine("timeout_ms", "timeout_ms")
		} else if _, hasPrompt := body["prompt"]; hasPrompt {
			addBlock("prompt", "prompt")
			addLine("max_turns", "max_turns")
		}
	}
	return strings.TrimRight(b.String(), " \t\r\n")
}

func decodeToolInput(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 {
		return map[string]any{}, true
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, false
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, true
}

func writeDetailBlock(b *strings.Builder, label, value string) {
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(label)
	b.WriteString(":\n")
	b.WriteString(value)
}

func writeDetailLine(b *strings.Builder, label, value string) {
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(value)
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

// ToolActivityResult returns the short title summary and expandable detail used
// by live TUI events and rollout resume reconstruction.
func ToolActivityResult(toolName, inputSummary string, result tool.Result) (string, string) {
	return toolActivityResult(toolName, inputSummary, "", result)
}

// ToolActivityResultWithInput includes the expanded invocation details that are
// useful after the single-line activity summary has been truncated.
func ToolActivityResultWithInput(toolName string, input json.RawMessage, result tool.Result) (string, string) {
	return toolActivityResult(toolName, SummarizeToolInput(toolName, input), ToolInputDetail(toolName, input), result)
}

func toolActivityResult(toolName, inputSummary, inputDetail string, result tool.Result) (string, string) {
	summary := strings.TrimSpace(inputSummary)
	if len(result.Files) > 0 || summary == "" {
		summary = summarizeToolResult(result)
	}
	detail := joinActivityDetails(inputDetail, toolActivityDetail(toolName, result))
	return truncateActivitySummary(summary), truncateToolActivityDetail(detail)
}

func joinActivityDetails(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimRight(part, " \t\r\n")
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "\n\n")
}

func toolActivityDetail(toolName string, result tool.Result) string {
	content := strings.TrimRight(result.Content, " \t\r\n")
	if strings.TrimSpace(toolName) == "bash" {
		if detail, ok := shellToolDetail(content); ok {
			return detail
		}
	}
	fileDetail, hasDiff := fileToolResultDetail(result)
	if len(result.Files) == 0 {
		if strings.TrimSpace(content) != "" {
			return content
		}
		return summarizeToolResult(result)
	}
	if !hasDiff && strings.TrimSpace(content) != "" {
		return content
	}
	if strings.TrimSpace(fileDetail) != "" {
		return toolResultDetail(result)
	}
	if strings.TrimSpace(content) != "" {
		return content
	}
	return summarizeToolResult(result)
}

func toolResultDetail(result tool.Result) string {
	detail, _ := fileToolResultDetail(result)
	if strings.TrimSpace(detail) == "" {
		return summarizeToolResult(result)
	}
	return detail
}

func fileToolResultDetail(result tool.Result) (string, bool) {
	var b strings.Builder
	hasDiff := false
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
			hasDiff = true
			b.WriteString("\n")
			b.WriteString(diff)
		}
	}
	return b.String(), hasDiff
}

func shellToolDetail(content string) (string, bool) {
	if !strings.HasPrefix(content, shellMetadataOpenTag) {
		return "", false
	}
	closeIndex := strings.Index(content, shellMetadataCloseTag)
	if closeIndex < 0 {
		return "", false
	}
	metadata := strings.TrimSpace(content[len(shellMetadataOpenTag):closeIndex])
	rest := strings.TrimLeft(content[closeIndex+len(shellMetadataCloseTag):], "\n")
	rest = strings.TrimRight(rest, " \t\r\n")
	if metadata == "" {
		return rest, true
	}
	if rest == "" {
		return metadata, true
	}
	return metadata + "\n" + rest, true
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

func positiveIntField(body map[string]any, key string) (int, bool) {
	value, ok := body[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil || parsed < 1 {
			return 0, false
		}
		return parsed, true
	case float64:
		if typed != float64(int64(typed)) {
			return 0, false
		}
		parsed := int(typed)
		if parsed < 1 {
			return 0, false
		}
		return parsed, true
	default:
		raw, ok := rawStringField(body, key)
		if !ok {
			return 0, false
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || parsed < 1 {
			return 0, false
		}
		return parsed, true
	}
}

func commandStringField(body map[string]any, key string) (string, bool) {
	value, ok := preservedStringField(body, key)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

func preservedStringField(body map[string]any, key string) (string, bool) {
	value, ok := body[key]
	if !ok || value == nil {
		return "", false
	}
	if typed, ok := value.(string); ok {
		return typed, strings.TrimSpace(typed) != ""
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
	return truncateActivityDetailToRunes(text, maxActivityDetailRunes)
}

func truncateToolActivityDetail(text string) string {
	return truncateActivityDetailToRunes(text, maxToolActivityDetailRunes)
}

func truncateActivityDetailToRunes(text string, maxRunes int) string {
	text = strings.TrimRight(text, " \t\r\n")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	notice := fmt.Sprintf("[activity detail truncated: showing preview of %d runes; original %d runes]", maxRunes, len(runes))
	footer := ""
	if foundFooter, ok := toolResultTruncationFooter(text); ok {
		footer = strings.TrimRight(foundFooter, " \t\r\n")
		notice = "[activity detail truncated: showing preview; tool result footer preserved]"
	}
	suffix := ""
	if footer != "" {
		suffix = "\n" + footer
	}
	prefix := notice + "\n"
	budget := maxRunes - len([]rune(prefix)) - len([]rune(suffix))
	if budget < 0 {
		budget = 0
	}
	preview := strings.TrimRight(string(runes[:budget]), " \t\r\n")
	if preview == "" {
		return notice + suffix
	}
	return prefix + preview + suffix
}

func toolResultTruncationFooter(text string) (string, bool) {
	index := strings.LastIndex(text, toolResultTruncatedMarker)
	if index < 0 {
		return "", false
	}
	footer := strings.TrimSpace(text[index:])
	return footer, footer != ""
}
