package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluateAssertions(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "result.txt"), []byte("fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exists := true
	task := Task{Assertions: Assertions{
		Files:    []FileAssertion{{Path: "result.txt", Exists: &exists, Contains: []string{"fixed"}, NotContains: []string{"broken"}}},
		Commands: []CommandAssertion{{Name: "check", Run: []string{"go", "version"}, ExitCode: 0, StdoutContains: []string{"go version"}}},
		Rollout: RolloutAssertions{
			ToolsCalled:          []string{"read"},
			ToolsNotCalled:       []string{"job_kill"},
			ToolOrder:            []string{"read", "edit"},
			ToolOrderAny:         [][]string{{"grep", "edit"}, {"read", "edit"}},
			ToolsCalledAny:       [][]string{{"todo_write", "plan_write"}},
			AssistantContains:    []string{"done"},
			AssistantNotContains: []string{"failed"},
			ContextActions:       []string{"compact"},
		},
	}}
	observation := Observation{AssistantText: "done", Metrics: Metrics{
		ToolCalls:        []string{"todo_write", "read", "grep", "edit"},
		ContextDecisions: []ContextDecision{{Action: "compact", Reason: "threshold"}},
	}}
	results, err := Evaluate(context.Background(), task, workspace, os.Environ(), observation)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	for _, result := range results {
		if !result.Passed {
			t.Errorf("assertion %q failed: %s", result.Name, result.Message)
		}
	}
}

func TestEvaluateReportsFailures(t *testing.T) {
	task := Task{Assertions: Assertions{Rollout: RolloutAssertions{ToolsCalled: []string{"edit"}}}}
	results, err := Evaluate(context.Background(), task, t.TempDir(), os.Environ(), Observation{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Passed {
		t.Fatalf("results = %#v, want one failure", results)
	}
}
