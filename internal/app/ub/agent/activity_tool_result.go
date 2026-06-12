package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

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
