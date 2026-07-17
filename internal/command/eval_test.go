package command

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agenteval "github.com/feimingxliu/ub/internal/eval"
)

func TestEvalRequiresTask(t *testing.T) {
	tc := newTestRootCommand("eval")
	err := tc.cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--task") {
		t.Fatalf("error = %v, want task hint", err)
	}
}

func TestEvalJSONReport(t *testing.T) {
	taskPath := filepath.Join(t.TempDir(), "task.yaml")
	if err := os.WriteFile(taskPath, []byte("schema_version: 1\nname: cli-eval\nprompt: test\nassertions:\n  rollout:\n    tools_called: [read]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := runEvalTask
	runEvalTask = func(_ context.Context, task agenteval.TaskFile, opts agenteval.RunOptions) (agenteval.Report, error) {
		if task.Task.Name != "cli-eval" || opts.Provider != "fake" || opts.Model != "model" {
			t.Fatalf("task=%#v opts=%#v", task, opts)
		}
		return agenteval.Report{Task: task.Task.Name, Passed: true, Metrics: agenteval.Metrics{Turns: 1}}, nil
	}
	t.Cleanup(func() { runEvalTask = original })
	tc := newTestRootCommand("eval", "--task", taskPath, "--provider", "fake", "--model", "model", "--json")
	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("eval: %v", err)
	}
	var report agenteval.Report
	if err := json.Unmarshal(tc.out.Bytes(), &report); err != nil || !report.Passed {
		t.Fatalf("report=%#v err=%v output=%s", report, err, tc.out.String())
	}
}
