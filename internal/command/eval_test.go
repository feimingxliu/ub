package command

import (
	"context"
	"encoding/json"
	"errors"
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

func TestEvalMatrixJSONReport(t *testing.T) {
	dir := t.TempDir()
	taskA := filepath.Join(dir, "a.yaml")
	taskB := filepath.Join(dir, "b.yaml")
	for path, name := range map[string]string{taskA: "task-a", taskB: "task-b"} {
		if err := os.WriteFile(path, []byte("schema_version: 1\nname: "+name+"\nprompt: test\nassertions:\n  rollout:\n    tools_called: [read]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	original := runEvalMatrix
	runEvalMatrix = func(_ context.Context, request agenteval.MatrixRequest) (agenteval.MatrixReport, error) {
		if len(request.Tasks) != 2 || len(request.Targets) != 2 || request.Repeat != 2 || request.Parallel != 2 {
			t.Fatalf("request=%#v", request)
		}
		if request.Targets[0].Provider != "p" || request.Targets[0].Model != "openai/model/a" || request.Targets[1].Model != "model-b" {
			t.Fatalf("targets=%#v", request.Targets)
		}
		return agenteval.MatrixReport{Kind: "matrix", Passed: true, Repeat: request.Repeat, Parallel: request.Parallel}, nil
	}
	t.Cleanup(func() { runEvalMatrix = original })
	tc := newTestRootCommand("eval", "--task", taskA, "--task", taskB, "--target", "p=openai/model/a", "--target", "p=model-b", "--repeat", "2", "--parallel", "2", "--json")
	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("eval matrix: %v", err)
	}
	var report agenteval.MatrixReport
	if err := json.Unmarshal(tc.out.Bytes(), &report); err != nil || report.Kind != "matrix" {
		t.Fatalf("report=%#v err=%v output=%s", report, err, tc.out.String())
	}
}

func TestEvalMatrixRejectsInvalidFlagsBeforeRun(t *testing.T) {
	taskPath := filepath.Join(t.TempDir(), "task.yaml")
	if err := os.WriteFile(taskPath, []byte("schema_version: 1\nname: task\nprompt: test\nassertions:\n  rollout:\n    tools_called: [read]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"mixed target":   {"eval", "--task", taskPath, "--target", "p=m", "--provider", "p"},
		"invalid target": {"eval", "--task", taskPath, "--target", "missing-model"},
		"repeat":         {"eval", "--task", taskPath, "--repeat", "0"},
		"parallel":       {"eval", "--task", taskPath, "--parallel", "17"},
	} {
		t.Run(name, func(t *testing.T) {
			tc := newTestRootCommand(args...)
			if err := tc.cmd.Execute(); err == nil {
				t.Fatal("eval succeeded, want flag validation error")
			}
		})
	}
}

func TestEvalTargetsPreserveModelSlash(t *testing.T) {
	targets, err := evalTargets([]string{"vibecoding=openai/glm-5.2"}, "", "")
	if err != nil || len(targets) != 1 || targets[0].Provider != "vibecoding" || targets[0].Model != "openai/glm-5.2" {
		t.Fatalf("targets=%#v err=%v", targets, err)
	}
}

func TestEvalMatrixFailureRendersReportBeforeReturningError(t *testing.T) {
	taskPath := filepath.Join(t.TempDir(), "task.yaml")
	if err := os.WriteFile(taskPath, []byte("schema_version: 1\nname: task\nprompt: test\nassertions:\n  rollout:\n    tools_called: [read]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := runEvalMatrix
	runEvalMatrix = func(_ context.Context, _ agenteval.MatrixRequest) (agenteval.MatrixReport, error) {
		return agenteval.MatrixReport{Kind: "matrix", Passed: false, Repeat: 2, Parallel: 1, Overall: agenteval.MatrixAggregate{Planned: 2, Executed: 1, Failed: 1, Skipped: 1}}, agenteval.ErrMatrixFailed
	}
	t.Cleanup(func() { runEvalMatrix = original })
	tc := newTestRootCommand("eval", "--task", taskPath, "--repeat", "2", "--json")
	if err := tc.cmd.Execute(); !errors.Is(err, agenteval.ErrMatrixFailed) {
		t.Fatalf("error=%v want ErrMatrixFailed", err)
	}
	var report agenteval.MatrixReport
	if err := json.Unmarshal(tc.out.Bytes(), &report); err != nil || report.Kind != "matrix" || report.Passed {
		t.Fatalf("report=%#v err=%v output=%s", report, err, tc.out.String())
	}
}
