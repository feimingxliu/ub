package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

func Evaluate(ctx context.Context, task Task, workspace string, env []string, observation Observation) ([]AssertionResult, error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var results []AssertionResult
	for _, assertion := range task.Assertions.Files {
		results = append(results, evaluateFile(root, assertion))
	}
	for i, assertion := range task.Assertions.Commands {
		results = append(results, evaluateCommand(ctx, workspace, env, i, assertion))
	}
	results = append(results, evaluateRollout(task.Assertions.Rollout, observation)...)
	return results, nil
}

func evaluateFile(root *os.Root, assertion FileAssertion) AssertionResult {
	name := "file " + assertion.Path
	data, err := root.ReadFile(assertion.Path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return failed(name, err.Error())
	}
	if assertion.Exists != nil && exists != *assertion.Exists {
		return failed(name, fmt.Sprintf("exists=%v, want %v", exists, *assertion.Exists))
	}
	if !exists && (len(assertion.Contains) > 0 || len(assertion.NotContains) > 0) {
		return failed(name, "file does not exist")
	}
	text := string(data)
	for _, value := range assertion.Contains {
		if !strings.Contains(text, value) {
			return failed(name, fmt.Sprintf("missing content %q", value))
		}
	}
	for _, value := range assertion.NotContains {
		if strings.Contains(text, value) {
			return failed(name, fmt.Sprintf("unexpected content %q", value))
		}
	}
	return passed(name)
}

func evaluateCommand(ctx context.Context, workspace string, env []string, index int, assertion CommandAssertion) AssertionResult {
	name := assertion.Name
	if name == "" {
		name = fmt.Sprintf("command %d", index+1)
	}
	cmd := exec.CommandContext(ctx, assertion.Run[0], assertion.Run[1:]...)
	cmd.Dir = workspace
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return failed(name, err.Error())
		}
	}
	if exitCode != assertion.ExitCode {
		return failed(name, fmt.Sprintf("exit_code=%d, want %d; stderr=%s", exitCode, assertion.ExitCode, summarize(stderr.String())))
	}
	for _, value := range assertion.StdoutContains {
		if !strings.Contains(stdout.String(), value) {
			return failed(name, fmt.Sprintf("stdout missing %q", value))
		}
	}
	for _, value := range assertion.StderrContains {
		if !strings.Contains(stderr.String(), value) {
			return failed(name, fmt.Sprintf("stderr missing %q", value))
		}
	}
	return passed(name)
}

func evaluateRollout(assertions RolloutAssertions, observation Observation) []AssertionResult {
	var results []AssertionResult
	tools := observation.Metrics.ToolCalls
	for _, tool := range assertions.ToolsCalled {
		result := passed("tool called " + tool)
		if !slices.Contains(tools, tool) {
			result = failed(result.Name, fmt.Sprintf("tool calls: %v", tools))
		}
		results = append(results, result)
	}
	for _, tool := range assertions.ToolsNotCalled {
		result := passed("tool not called " + tool)
		if slices.Contains(tools, tool) {
			result = failed(result.Name, fmt.Sprintf("tool calls: %v", tools))
		}
		results = append(results, result)
	}
	if len(assertions.ToolOrder) > 0 {
		result := passed("tool order " + strings.Join(assertions.ToolOrder, " -> "))
		if !isSubsequence(tools, assertions.ToolOrder) {
			result = failed(result.Name, fmt.Sprintf("tool calls: %v", tools))
		}
		results = append(results, result)
	}
	if len(assertions.ToolOrderAny) > 0 {
		var labels []string
		matched := false
		for _, order := range assertions.ToolOrderAny {
			labels = append(labels, strings.Join(order, " -> "))
			matched = matched || isSubsequence(tools, order)
		}
		result := passed("any tool order: " + strings.Join(labels, " | "))
		if !matched {
			result = failed(result.Name, fmt.Sprintf("tool calls: %v", tools))
		}
		results = append(results, result)
	}
	for _, group := range assertions.ToolsCalledAny {
		result := passed("any tool called: " + strings.Join(group, ", "))
		found := false
		for _, tool := range group {
			found = found || slices.Contains(tools, tool)
		}
		if !found {
			result = failed(result.Name, fmt.Sprintf("tool calls: %v", tools))
		}
		results = append(results, result)
	}
	for _, value := range assertions.AssistantContains {
		result := passed("assistant contains " + value)
		if !strings.Contains(observation.AssistantText, value) {
			result = failed(result.Name, "assistant response did not contain expected text")
		}
		results = append(results, result)
	}
	for _, value := range assertions.AssistantNotContains {
		result := passed("assistant excludes " + value)
		if strings.Contains(observation.AssistantText, value) {
			result = failed(result.Name, "assistant response contained forbidden text")
		}
		results = append(results, result)
	}
	for _, action := range assertions.ContextActions {
		result := passed("context action " + action)
		found := false
		for _, decision := range observation.Metrics.ContextDecisions {
			found = found || decision.Action == action
		}
		if !found {
			result = failed(result.Name, fmt.Sprintf("context decisions: %v", observation.Metrics.ContextDecisions))
		}
		results = append(results, result)
	}
	return results
}

func isSubsequence(actual, expected []string) bool {
	index := 0
	for _, value := range actual {
		if index < len(expected) && value == expected[index] {
			index++
		}
	}
	return index == len(expected)
}

func passed(name string) AssertionResult { return AssertionResult{Name: name, Passed: true} }
func failed(name, message string) AssertionResult {
	return AssertionResult{Name: name, Passed: false, Message: message}
}

func summarize(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		return value[:500] + "..."
	}
	return value
}
