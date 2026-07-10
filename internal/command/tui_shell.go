package command

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/tui"
)

func (r *tuiAgentRunner) RunShell(ctx context.Context, command string, events chan<- tui.Event) error {
	if r == nil || r.tools == nil || r.tools.Registry == nil {
		return fmt.Errorf("shell execution is unavailable")
	}
	bash, ok := r.tools.Registry.Get("bash")
	if !ok {
		return fmt.Errorf("bash tool is unavailable")
	}
	raw, err := json.Marshal(map[string]any{
		"command": command,
		"cwd":     ".",
	})
	if err != nil {
		return err
	}
	result, execErr := bash.Execute(ctx, raw)
	content := strings.TrimRight(result.Content, "\n")
	isError := result.IsError
	if execErr != nil {
		content = execErr.Error()
		isError = true
	}
	sendTUIEvent(ctx, events, tui.Event{
		Type:    tui.EventShellOutput,
		Content: formatShellOutput(content, isError),
		IsError: isError,
	})
	sendTUIEvent(ctx, events, tui.Event{Type: tui.EventDone})
	return nil
}

func formatShellOutput(content string, isError bool) string {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return "(no output)"
	}
	parsed, ok := parseShellToolOutput(content)
	if !ok {
		return content
	}
	var parts []string
	if strings.TrimSpace(parsed.stdout) != "" {
		parts = append(parts, strings.TrimRight(parsed.stdout, "\n"))
	}
	if strings.TrimSpace(parsed.stderr) != "" {
		stderr := strings.TrimRight(parsed.stderr, "\n")
		if len(parts) > 0 {
			parts = append(parts, "--- stderr ---\n"+stderr)
		} else {
			parts = append(parts, stderr)
		}
	}
	if strings.TrimSpace(parsed.errorLine) != "" {
		parts = append(parts, "error: "+parsed.errorLine)
	}
	if isError && parsed.exitCode != "" && parsed.exitCode != "0" {
		parts = append(parts, "exit code: "+parsed.exitCode)
	}
	if len(parts) == 0 {
		if isError && parsed.exitCode != "" && parsed.exitCode != "0" {
			return "exit code: " + parsed.exitCode
		}
		return "(no output)"
	}
	return strings.Join(parts, "\n")
}

type shellToolOutput struct {
	exitCode  string
	errorLine string
	stdout    string
	stderr    string
}

func parseShellToolOutput(content string) (shellToolOutput, bool) {
	stdoutMarker := "\n--- stdout ---\n"
	stderrMarker := "\n--- stderr ---"
	stdoutStart := strings.Index(content, stdoutMarker)
	if stdoutStart < 0 {
		return shellToolOutput{}, false
	}
	stderrStart := strings.Index(content[stdoutStart+len(stdoutMarker):], stderrMarker)
	if stderrStart < 0 {
		return shellToolOutput{}, false
	}
	stderrStart += stdoutStart + len(stdoutMarker)
	header := content[:stdoutStart]
	stdout := content[stdoutStart+len(stdoutMarker) : stderrStart]
	stderr := strings.TrimPrefix(content[stderrStart+len(stderrMarker):], "\n")
	parsed := shellToolOutput{
		exitCode:  shellHeaderValue(header, "exit_code"),
		errorLine: shellHeaderValue(header, "error"),
		stdout:    stdout,
		stderr:    stderr,
	}
	return parsed, true
}

func shellHeaderValue(header, key string) string {
	prefix := key + "="
	for _, line := range strings.Split(header, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}
