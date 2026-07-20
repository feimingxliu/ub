package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunMatrixStableOrderAndBoundedConcurrency(t *testing.T) {
	tasks := matrixTestTasks("task-a", "task-b")
	targets := []Target{{Provider: "p", Model: "m1"}, {Provider: "p", Model: "m2"}}
	var active atomic.Int32
	var maximum atomic.Int32
	run := func(_ context.Context, task TaskFile, opts RunOptions) (Report, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		if task.Task.Name == "task-a" {
			time.Sleep(3 * time.Millisecond)
		} else {
			time.Sleep(time.Millisecond)
		}
		return passedMatrixTestReport(task.Task.Name, opts.Provider, opts.Model), nil
	}
	report, err := RunMatrix(context.Background(), MatrixRequest{Tasks: tasks, Targets: targets, Repeat: 2, Parallel: 2, Run: run})
	if err != nil || !report.Passed {
		t.Fatalf("RunMatrix report=%#v err=%v", report, err)
	}
	if len(report.Runs) != 8 || maximum.Load() > 2 {
		t.Fatalf("runs=%d maximum=%d", len(report.Runs), maximum.Load())
	}
	wantIDs := []string{
		"p=m1::task-a::1", "p=m1::task-a::2", "p=m1::task-b::1", "p=m1::task-b::2",
		"p=m2::task-a::1", "p=m2::task-a::2", "p=m2::task-b::1", "p=m2::task-b::2",
	}
	for i, want := range wantIDs {
		if report.Runs[i].ID != want {
			t.Fatalf("run[%d].ID=%q want=%q", i, report.Runs[i].ID, want)
		}
	}
	if report.Overall.Planned != 8 || report.Overall.Executed != 8 || report.Overall.Passed != 8 || report.Overall.PassRate != 1 {
		t.Fatalf("overall=%#v", report.Overall)
	}
}

func TestRunMatrixKeepsSampleWorkspacesIsolated(t *testing.T) {
	executor := &recordingExecutor{}
	task := matrixTestTasks("task")[0]
	report, err := RunMatrix(context.Background(), MatrixRequest{
		Tasks: []TaskFile{task}, Targets: []Target{{Provider: "fake", Model: "model"}}, Repeat: 2, Parallel: 1,
		Options: RunOptions{Executor: executor, Executable: "/test/ub", KeepWorkspace: true},
	})
	if err != nil || len(report.Runs) != 2 {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	first := report.Runs[0].Report.Workspace
	second := report.Runs[1].Report.Workspace
	defer os.RemoveAll(filepath.Dir(first))
	defer os.RemoveAll(filepath.Dir(second))
	if first == "" || second == "" || first == second {
		t.Fatalf("workspaces first=%q second=%q", first, second)
	}
	for _, workspace := range []string{first, second} {
		if _, err := os.Stat(filepath.Join(workspace, "result.txt")); err != nil {
			t.Fatalf("workspace %q missing result: %v", workspace, err)
		}
	}
}

func TestRunMatrixInfrastructurePreflightBreaksOnlyTarget(t *testing.T) {
	tasks := matrixTestTasks("task-a", "task-b")
	targets := []Target{{Provider: "p", Model: "bad"}, {Provider: "p", Model: "good"}}
	var mu sync.Mutex
	calls := map[string]int{}
	run := func(_ context.Context, task TaskFile, opts RunOptions) (Report, error) {
		mu.Lock()
		calls[opts.Model]++
		mu.Unlock()
		if opts.Model == "bad" {
			return Report{Task: task.Task.Name, Provider: opts.Provider, Model: opts.Model, FailureCategory: FailureInfrastructure, Failure: "400 Bad Request", Metrics: Metrics{}}, errors.New("bad request")
		}
		return passedMatrixTestReport(task.Task.Name, opts.Provider, opts.Model), nil
	}
	report, err := RunMatrix(context.Background(), MatrixRequest{Tasks: tasks, Targets: targets, Repeat: 2, Parallel: 2, Run: run})
	if !errors.Is(err, ErrMatrixFailed) {
		t.Fatalf("err=%v want ErrMatrixFailed", err)
	}
	if calls["bad"] != 1 || calls["good"] != 4 {
		t.Fatalf("calls=%v", calls)
	}
	if report.Overall.Executed != 5 || report.Overall.Failed != 1 || report.Overall.Skipped != 3 || report.Overall.FailureCategories[FailureInfrastructure] != 1 {
		t.Fatalf("overall=%#v", report.Overall)
	}
	for i := 1; i < 4; i++ {
		if report.Runs[i].Status != MatrixStatusSkipped || !strings.Contains(report.Runs[i].SkipReason, "400 Bad Request") {
			t.Fatalf("run[%d]=%#v", i, report.Runs[i])
		}
	}
}

func TestRunMatrixAssertionFailureDoesNotBreakTarget(t *testing.T) {
	var calls atomic.Int32
	run := func(_ context.Context, task TaskFile, opts RunOptions) (Report, error) {
		call := calls.Add(1)
		if call == 1 {
			return Report{Task: task.Task.Name, Provider: opts.Provider, Model: opts.Model, FailureCategory: FailureAssertion, Failure: "assertion", BehaviorObserved: true}, errors.New("assertion")
		}
		return passedMatrixTestReport(task.Task.Name, opts.Provider, opts.Model), nil
	}
	report, err := RunMatrix(context.Background(), MatrixRequest{Tasks: matrixTestTasks("task"), Targets: []Target{{Provider: "p", Model: "m"}}, Repeat: 3, Parallel: 2, Run: run})
	if !errors.Is(err, ErrMatrixFailed) || calls.Load() != 3 || report.Overall.Skipped != 0 || report.Overall.Failed != 1 {
		t.Fatalf("calls=%d report=%#v err=%v", calls.Load(), report, err)
	}
}

func TestRunMatrixCancellationSkipsUnstartedSamples(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	run := func(_ context.Context, task TaskFile, opts RunOptions) (Report, error) {
		calls.Add(1)
		cancel()
		return passedMatrixTestReport(task.Task.Name, opts.Provider, opts.Model), nil
	}
	report, err := RunMatrix(ctx, MatrixRequest{Tasks: matrixTestTasks("task-a", "task-b"), Targets: []Target{{Provider: "p", Model: "m"}}, Repeat: 2, Parallel: 1, Run: run})
	if !errors.Is(err, ErrMatrixFailed) || calls.Load() != 1 || report.Overall.Executed != 1 || report.Overall.Skipped != 3 {
		t.Fatalf("calls=%d report=%#v err=%v", calls.Load(), report, err)
	}
	for _, run := range report.Runs[1:] {
		if run.Status != MatrixStatusSkipped || !strings.Contains(run.SkipReason, "canceled") {
			t.Fatalf("run=%#v", run)
		}
	}
}

func TestRunMatrixValidatesRequestBeforeRunning(t *testing.T) {
	called := false
	run := func(context.Context, TaskFile, RunOptions) (Report, error) {
		called = true
		return Report{}, nil
	}
	for name, request := range map[string]MatrixRequest{
		"no tasks":   {Targets: []Target{{}}, Repeat: 1, Parallel: 1, Run: run},
		"no target":  {Tasks: matrixTestTasks("task"), Repeat: 1, Parallel: 1, Run: run},
		"repeat":     {Tasks: matrixTestTasks("task"), Targets: []Target{{}}, Repeat: 0, Parallel: 1, Run: run},
		"parallel":   {Tasks: matrixTestTasks("task"), Targets: []Target{{}}, Repeat: 1, Parallel: MaxMatrixParallel + 1, Run: run},
		"duplicates": {Tasks: matrixTestTasks("task", "task"), Targets: []Target{{}}, Repeat: 1, Parallel: 1, Run: run},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := RunMatrix(context.Background(), request); err == nil {
				t.Fatal("RunMatrix succeeded, want validation error")
			}
		})
	}
	if called {
		t.Fatal("runner called for invalid matrix")
	}
}

func TestMatrixAggregateAndRenderingHandleSkippedOnlyGroup(t *testing.T) {
	runs := []MatrixRun{{ID: "p=m::task::1", Task: "task", Target: Target{Provider: "p", Model: "m"}, Repetition: 1, Status: MatrixStatusSkipped, SkipReason: "preflight"}}
	aggregate := aggregateMatrixRuns(runs)
	if aggregate.Executed != 0 || aggregate.PassRate != 0 || aggregate.AverageDurationMS != 0 || aggregate.AverageTurns != 0 {
		t.Fatalf("aggregate=%#v", aggregate)
	}
	report := MatrixReport{Kind: "matrix", Tasks: []string{"task"}, Targets: []Target{{Provider: "p", Model: "m"}}, Repeat: 1, Parallel: 1, Runs: runs, Overall: aggregate, ByTarget: []MatrixGroup{{Key: "p=m", Aggregate: aggregate}}, ByTask: []MatrixGroup{{Key: "task", Aggregate: aggregate}}}
	var jsonOut bytes.Buffer
	if err := RenderMatrixJSON(&jsonOut, report); err != nil {
		t.Fatal(err)
	}
	var decoded MatrixReport
	if err := json.Unmarshal(jsonOut.Bytes(), &decoded); err != nil || decoded.Kind != "matrix" || strings.Contains(jsonOut.String(), "NaN") {
		t.Fatalf("decoded=%#v err=%v json=%s", decoded, err, jsonOut.String())
	}
	var textOut bytes.Buffer
	if err := RenderMatrixText(&textOut, report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Eval Matrix FAIL", "executed=0", "SKIP p=m::task::1"} {
		if !strings.Contains(textOut.String(), want) {
			t.Errorf("text missing %q:\n%s", want, textOut.String())
		}
	}
}

func matrixTestTasks(names ...string) []TaskFile {
	tasks := make([]TaskFile, 0, len(names))
	for _, name := range names {
		tasks = append(tasks, TaskFile{Task: Task{SchemaVersion: 1, Name: name, Prompt: "x", Assertions: Assertions{Rollout: RolloutAssertions{ToolsCalled: []string{"read"}}}}})
	}
	return tasks
}

func passedMatrixTestReport(task, provider, model string) Report {
	return Report{
		Task: task, Provider: provider, Model: model, Passed: true, BehaviorObserved: true,
		Metrics: Metrics{DurationMillis: 10, Turns: 1, InputTokens: 5, OutputTokens: 2, CacheReadTokens: 3, ContextDecisions: []ContextDecision{{Action: "keep"}}},
	}
}

func ExampleRenderMatrixText() {
	report := MatrixReport{Kind: "matrix", Passed: true, Tasks: []string{"task"}, Targets: []Target{{Provider: "p", Model: "m"}}, Repeat: 1, Parallel: 1, Overall: MatrixAggregate{Planned: 1, Executed: 1, Passed: 1, PassRate: 1}}
	var out bytes.Buffer
	_ = RenderMatrixText(&out, report)
	fmt.Print(strings.Split(out.String(), "\n")[0])
	// Output: Eval Matrix PASS: tasks=1 targets=1 repeat=1 parallel=1
}
