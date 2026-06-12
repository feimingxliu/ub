package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

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
