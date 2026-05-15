package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/feimingxliu/ub/internal/tool"
)

const maxActivitySummaryRunes = 180

func (a *Agent) emitThinkingActivity(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityThinking,
		Summary:      truncateActivitySummary(text),
	})
}

func (a *Agent) emitToolActivity(call toolCall, status, summary, content string, isError bool) {
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityTool,
		ToolUseID:    call.ID,
		ToolName:     call.Name,
		Status:       status,
		Summary:      truncateActivitySummary(summary),
		Content:      truncateActivitySummary(content),
		IsError:      isError,
	})
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

func summarizeToolInput(name string, raw json.RawMessage) string {
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

func reasoningText(text, fallback string) string {
	text = strings.TrimSpace(text)
	if text != "" {
		return truncateActivitySummary(redactText("reasoning", text))
	}
	return truncateActivitySummary(redactText("reasoning", fallback))
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
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxActivitySummaryRunes {
		return text
	}
	return string(runes[:maxActivitySummaryRunes-3]) + "..."
}
