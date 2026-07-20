package eval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const MaxMatrixParallel = 16

const (
	MatrixStatusPassed  = "passed"
	MatrixStatusFailed  = "failed"
	MatrixStatusSkipped = "skipped"
)

var ErrMatrixFailed = errors.New("one or more matrix evaluation samples failed or were skipped")

type Target struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

func (t Target) Key() string {
	if strings.TrimSpace(t.Provider) == "" && strings.TrimSpace(t.Model) == "" {
		return "default"
	}
	return strings.TrimSpace(t.Provider) + "=" + strings.TrimSpace(t.Model)
}

type RunFunc func(context.Context, TaskFile, RunOptions) (Report, error)

type MatrixRequest struct {
	Tasks    []TaskFile
	Targets  []Target
	Repeat   int
	Parallel int
	Options  RunOptions
	Run      RunFunc
}

type MatrixRun struct {
	ID         string  `json:"id"`
	Task       string  `json:"task"`
	Target     Target  `json:"target"`
	Repetition int     `json:"repetition"`
	Status     string  `json:"status"`
	Report     *Report `json:"report,omitempty"`
	SkipReason string  `json:"skip_reason,omitempty"`
}

type MatrixAggregate struct {
	Planned           int            `json:"planned"`
	Executed          int            `json:"executed"`
	Passed            int            `json:"passed"`
	Failed            int            `json:"failed"`
	Skipped           int            `json:"skipped"`
	PassRate          float64        `json:"pass_rate"`
	DurationMillis    int64          `json:"duration_ms"`
	AverageDurationMS float64        `json:"average_duration_ms"`
	Turns             int            `json:"turns"`
	AverageTurns      float64        `json:"average_turns"`
	InputTokens       int            `json:"input_tokens"`
	OutputTokens      int            `json:"output_tokens"`
	ReasoningTokens   int            `json:"reasoning_tokens"`
	CacheReadTokens   int            `json:"cache_read_tokens"`
	CacheWriteTokens  int            `json:"cache_write_tokens"`
	FailureCategories map[string]int `json:"failure_categories"`
	ContextActions    map[string]int `json:"context_actions"`
}

type MatrixGroup struct {
	Key       string          `json:"key"`
	Aggregate MatrixAggregate `json:"aggregate"`
}

type MatrixReport struct {
	Kind     string          `json:"kind"`
	Passed   bool            `json:"passed"`
	Repeat   int             `json:"repeat"`
	Parallel int             `json:"parallel"`
	Tasks    []string        `json:"tasks"`
	Targets  []Target        `json:"targets"`
	Runs     []MatrixRun     `json:"runs"`
	Overall  MatrixAggregate `json:"overall"`
	ByTarget []MatrixGroup   `json:"by_target"`
	ByTask   []MatrixGroup   `json:"by_task"`
}

type matrixPlan struct {
	index      int
	task       TaskFile
	target     Target
	repetition int
}

func RunMatrix(ctx context.Context, request MatrixRequest) (MatrixReport, error) {
	if err := validateMatrixRequest(request); err != nil {
		return MatrixReport{}, err
	}
	run := request.Run
	if run == nil {
		run = Run
	}
	plans := buildMatrixPlans(request)
	runs := make([]MatrixRun, len(plans))
	completed := make([]bool, len(plans))
	pending := make([]matrixPlan, 0, len(plans))

	for targetIndex := range request.Targets {
		preflight := plans[targetIndex*len(request.Tasks)*request.Repeat]
		if err := ctx.Err(); err != nil {
			for _, plan := range plansForTarget(plans, preflight.target) {
				runs[plan.index] = skippedMatrixRun(plan, "matrix canceled: "+err.Error())
				completed[plan.index] = true
			}
			continue
		}
		runs[preflight.index] = executeMatrixPlan(ctx, preflight, request.Options, run)
		completed[preflight.index] = true
		if isCircuitBreakingRun(runs[preflight.index]) {
			reason := "target preflight failed: " + runs[preflight.index].Report.Failure
			for _, plan := range plansForTarget(plans, preflight.target) {
				if completed[plan.index] {
					continue
				}
				runs[plan.index] = skippedMatrixRun(plan, reason)
				completed[plan.index] = true
			}
			continue
		}
	}
	for _, plan := range plans {
		if !completed[plan.index] {
			pending = append(pending, plan)
		}
	}
	executePendingMatrixPlans(ctx, pending, request.Options, request.Parallel, run, runs)

	report := buildMatrixReport(request, runs)
	if !report.Passed {
		return report, ErrMatrixFailed
	}
	return report, nil
}

func validateMatrixRequest(request MatrixRequest) error {
	if len(request.Tasks) == 0 {
		return errors.New("matrix requires at least one task")
	}
	if len(request.Targets) == 0 {
		return errors.New("matrix requires at least one target")
	}
	if request.Repeat <= 0 {
		return errors.New("matrix repeat must be positive")
	}
	if request.Parallel <= 0 || request.Parallel > MaxMatrixParallel {
		return fmt.Errorf("matrix parallel must be between 1 and %d", MaxMatrixParallel)
	}
	seenTasks := make(map[string]struct{}, len(request.Tasks))
	for _, task := range request.Tasks {
		name := strings.TrimSpace(task.Task.Name)
		if name == "" {
			return errors.New("matrix task name is empty")
		}
		if _, ok := seenTasks[name]; ok {
			return fmt.Errorf("duplicate matrix task %q", name)
		}
		seenTasks[name] = struct{}{}
	}
	seenTargets := make(map[string]struct{}, len(request.Targets))
	for _, target := range request.Targets {
		key := target.Key()
		if _, ok := seenTargets[key]; ok {
			return fmt.Errorf("duplicate matrix target %q", key)
		}
		seenTargets[key] = struct{}{}
	}
	return nil
}

func buildMatrixPlans(request MatrixRequest) []matrixPlan {
	plans := make([]matrixPlan, 0, len(request.Targets)*len(request.Tasks)*request.Repeat)
	for _, target := range request.Targets {
		for _, task := range request.Tasks {
			for repetition := 1; repetition <= request.Repeat; repetition++ {
				plans = append(plans, matrixPlan{index: len(plans), task: task, target: target, repetition: repetition})
			}
		}
	}
	return plans
}

func plansForTarget(plans []matrixPlan, target Target) []matrixPlan {
	out := make([]matrixPlan, 0)
	for _, plan := range plans {
		if plan.target.Key() == target.Key() {
			out = append(out, plan)
		}
	}
	return out
}

func executePendingMatrixPlans(ctx context.Context, plans []matrixPlan, options RunOptions, parallel int, run RunFunc, results []MatrixRun) {
	jobs := make(chan matrixPlan)
	var wg sync.WaitGroup
	for range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for plan := range jobs {
				if err := ctx.Err(); err != nil {
					results[plan.index] = skippedMatrixRun(plan, "matrix canceled: "+err.Error())
					continue
				}
				results[plan.index] = executeMatrixPlan(ctx, plan, options, run)
			}
		}()
	}
	for _, plan := range plans {
		select {
		case jobs <- plan:
		case <-ctx.Done():
			results[plan.index] = skippedMatrixRun(plan, "matrix canceled: "+ctx.Err().Error())
		}
	}
	close(jobs)
	wg.Wait()
}

func executeMatrixPlan(ctx context.Context, plan matrixPlan, options RunOptions, run RunFunc) MatrixRun {
	options.Provider = plan.target.Provider
	options.Model = plan.target.Model
	report, err := run(ctx, plan.task, options)
	status := MatrixStatusPassed
	if err != nil || !report.Passed {
		status = MatrixStatusFailed
	}
	return MatrixRun{
		ID:         matrixRunID(plan),
		Task:       plan.task.Task.Name,
		Target:     plan.target,
		Repetition: plan.repetition,
		Status:     status,
		Report:     &report,
	}
}

func skippedMatrixRun(plan matrixPlan, reason string) MatrixRun {
	return MatrixRun{
		ID:         matrixRunID(plan),
		Task:       plan.task.Task.Name,
		Target:     plan.target,
		Repetition: plan.repetition,
		Status:     MatrixStatusSkipped,
		SkipReason: reason,
	}
}

func matrixRunID(plan matrixPlan) string {
	return fmt.Sprintf("%s::%s::%d", plan.target.Key(), plan.task.Task.Name, plan.repetition)
}

func isCircuitBreakingRun(run MatrixRun) bool {
	return run.Status == MatrixStatusFailed && run.Report != nil &&
		run.Report.FailureCategory == FailureInfrastructure && !run.Report.BehaviorObserved
}

func buildMatrixReport(request MatrixRequest, runs []MatrixRun) MatrixReport {
	report := MatrixReport{
		Kind:     "matrix",
		Repeat:   request.Repeat,
		Parallel: request.Parallel,
		Runs:     runs,
		Targets:  append([]Target(nil), request.Targets...),
	}
	for _, task := range request.Tasks {
		report.Tasks = append(report.Tasks, task.Task.Name)
	}
	report.Overall = aggregateMatrixRuns(runs)
	for _, target := range request.Targets {
		report.ByTarget = append(report.ByTarget, MatrixGroup{Key: target.Key(), Aggregate: aggregateMatrixRuns(filterMatrixRuns(runs, func(run MatrixRun) bool {
			return run.Target.Key() == target.Key()
		}))})
	}
	for _, task := range request.Tasks {
		report.ByTask = append(report.ByTask, MatrixGroup{Key: task.Task.Name, Aggregate: aggregateMatrixRuns(filterMatrixRuns(runs, func(run MatrixRun) bool {
			return run.Task == task.Task.Name
		}))})
	}
	report.Passed = report.Overall.Failed == 0 && report.Overall.Skipped == 0
	return report
}

func filterMatrixRuns(runs []MatrixRun, include func(MatrixRun) bool) []MatrixRun {
	out := make([]MatrixRun, 0, len(runs))
	for _, run := range runs {
		if include(run) {
			out = append(out, run)
		}
	}
	return out
}

func aggregateMatrixRuns(runs []MatrixRun) MatrixAggregate {
	aggregate := MatrixAggregate{
		Planned:           len(runs),
		FailureCategories: map[string]int{},
		ContextActions:    map[string]int{},
	}
	for _, run := range runs {
		if run.Status == MatrixStatusSkipped || run.Report == nil {
			aggregate.Skipped++
			continue
		}
		aggregate.Executed++
		if run.Status == MatrixStatusPassed {
			aggregate.Passed++
		} else {
			aggregate.Failed++
			category := run.Report.FailureCategory
			if category == "" {
				category = FailureAgent
			}
			aggregate.FailureCategories[category]++
		}
		metrics := run.Report.Metrics
		aggregate.DurationMillis += metrics.DurationMillis
		aggregate.Turns += metrics.Turns
		aggregate.InputTokens += metrics.InputTokens
		aggregate.OutputTokens += metrics.OutputTokens
		aggregate.ReasoningTokens += metrics.ReasoningTokens
		aggregate.CacheReadTokens += metrics.CacheReadTokens
		aggregate.CacheWriteTokens += metrics.CacheWriteTokens
		for _, decision := range metrics.ContextDecisions {
			aggregate.ContextActions[decision.Action]++
		}
	}
	if aggregate.Executed > 0 {
		aggregate.PassRate = float64(aggregate.Passed) / float64(aggregate.Executed)
		aggregate.AverageDurationMS = float64(aggregate.DurationMillis) / float64(aggregate.Executed)
		aggregate.AverageTurns = float64(aggregate.Turns) / float64(aggregate.Executed)
	}
	return aggregate
}
