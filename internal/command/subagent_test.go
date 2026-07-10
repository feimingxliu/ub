package command

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/agent"
	"github.com/feimingxliu/ub/internal/hook"
	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/provider/fake"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
)

type probeTool struct {
	name   string
	risk   tool.Risk
	called bool
	schema *jsonschema.Schema
}

func (t *probeTool) Name() string        { return t.name }
func (t *probeTool) Description() string { return "records execution" }
func (t *probeTool) Schema() *jsonschema.Schema {
	if t.schema == nil {
		t.schema = jsonschema.Reflect(&struct{}{})
	}
	return t.schema
}
func (t *probeTool) Risk() tool.Risk { return t.risk }
func (t *probeTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	t.called = true
	return tool.Result{Content: "called"}, nil
}

func TestCLISubagentRunnerInheritsPlanMode(t *testing.T) {
	probe := &probeTool{name: "write_probe", risk: tool.RiskWrite}
	reg := tool.New()
	if err := reg.Register(probe); err != nil {
		t.Fatalf("register: %v", err)
	}
	perm, err := permission.NewManager(permission.Options{Asker: autoAllowAsker{}})
	if err != nil {
		t.Fatalf("permission: %v", err)
	}
	runner := &cliSubagentRunner{
		provider: fake.NewRounds(
			fake.Script{fake.ToolCall("write_probe", map[string]any{}), fake.Done()},
			fake.Script{fake.TextDelta("done"), fake.Done()},
		),
		tools:           reg,
		permission:      perm,
		model:           "fake/model",
		mode:            execmode.ModePlan,
		defaultMaxTurns: 3,
	}
	got, err := runner.RunSubagent(context.Background(), "try writing", 3)
	if err != nil {
		t.Fatalf("RunSubagent: %v", err)
	}
	if probe.called {
		t.Fatalf("write tool executed; child did not inherit plan-mode write gate")
	}
	if !strings.Contains(got, "done") {
		t.Fatalf("answer = %q, want final provider text", got)
	}
}

func TestCLISubagentRunnerAppliesToolHooks(t *testing.T) {
	probe := &probeTool{name: "safe_probe", risk: tool.RiskSafe}
	reg := tool.New()
	if err := reg.Register(probe); err != nil {
		t.Fatalf("register: %v", err)
	}
	perm, err := permission.NewManager(permission.Options{Asker: autoAllowAsker{}})
	if err != nil {
		t.Fatalf("permission: %v", err)
	}
	runner := &cliSubagentRunner{
		provider: fake.NewRounds(
			fake.Script{fake.ToolCall("safe_probe", map[string]any{}), fake.Done()},
			fake.Script{fake.TextDelta("done"), fake.Done()},
		),
		tools:           reg,
		permission:      perm,
		model:           "fake/model",
		mode:            execmode.ModeWork,
		hooks:           blockingPreToolHook{},
		defaultMaxTurns: 3,
	}
	got, err := runner.RunSubagent(context.Background(), "call tool", 3)
	if err != nil {
		t.Fatalf("RunSubagent: %v", err)
	}
	if probe.called {
		t.Fatalf("tool executed; child did not inherit blocking pre_tool_call hook")
	}
	if !strings.Contains(got, "done") {
		t.Fatalf("answer = %q, want final provider text", got)
	}
}

func TestCLISubagentRunnerMirrorsChildActivityToParentTurn(t *testing.T) {
	probe := &probeTool{name: "safe_probe", risk: tool.RiskSafe}
	reg := tool.New()
	if err := reg.Register(probe); err != nil {
		t.Fatalf("register: %v", err)
	}
	perm, err := permission.NewManager(permission.Options{Asker: autoAllowAsker{}})
	if err != nil {
		t.Fatalf("permission: %v", err)
	}
	ro := &recordingRollout{}
	var events []agent.Event
	runner := &cliSubagentRunner{
		provider: fake.NewRounds(
			fake.Script{fake.ToolCall("safe_probe", map[string]any{}), fake.Done()},
			fake.Script{fake.TextDelta("done"), fake.Done()},
		),
		tools:           reg,
		permission:      perm,
		model:           "fake/model",
		mode:            execmode.ModeWork,
		defaultMaxTurns: 3,
		rollout:         ro,
		events: func(event agent.Event) {
			events = append(events, event)
		},
	}
	ctx := tool.WithSessionID(context.Background(), "parent-session")
	ctx = tool.WithAgentTurn(ctx, 7)
	ctx = tool.WithToolUseID(ctx, "call_task")
	got, err := runner.RunSubagent(ctx, "call tool", 3)
	if err != nil {
		t.Fatalf("RunSubagent: %v", err)
	}
	if !strings.Contains(got, "done") {
		t.Fatalf("answer = %q, want final provider text", got)
	}
	if !probe.called {
		t.Fatalf("child tool was not executed")
	}

	var sawLiveChildTool bool
	var subagentID string
	for _, event := range events {
		if event.Type != agent.EventActivity || event.ActivityKind != agent.ActivityTool || event.ToolName != "safe_probe" {
			continue
		}
		if event.ParentToolUseID != "call_task" {
			t.Fatalf("live parent tool id = %q, want call_task", event.ParentToolUseID)
		}
		if !strings.HasPrefix(event.ToolUseID, "subagent:call_task:") {
			t.Fatalf("live child tool id = %q, want subagent namespace", event.ToolUseID)
		}
		if strings.TrimSpace(event.SubagentID) == "" {
			t.Fatalf("live child event missing subagent id: %#v", event)
		}
		subagentID = event.SubagentID
		sawLiveChildTool = true
		break
	}
	if !sawLiveChildTool {
		t.Fatalf("live events missing child tool activity: %#v", events)
	}

	var sawStart, sawPersistedChildTool, sawDone bool
	for _, event := range ro.events {
		if event.SessionID != "parent-session" || event.Turn != 7 || event.Type != rollout.TypeActivity {
			continue
		}
		payload, ok, err := rollout.ActivityFromEvent(event)
		if err != nil {
			t.Fatalf("ActivityFromEvent: %v", err)
		}
		if !ok {
			continue
		}
		if payload.ParentToolUseID != "call_task" {
			t.Fatalf("persisted parent tool id = %q, want call_task", payload.ParentToolUseID)
		}
		if payload.SubagentID != subagentID {
			t.Fatalf("persisted subagent id = %q, want %q", payload.SubagentID, subagentID)
		}
		switch {
		case payload.ActivityKind == string(agent.ActivityNotice) && payload.Summary == "subagent started":
			sawStart = true
		case payload.ActivityKind == string(agent.ActivityTool) && payload.ToolName == "safe_probe":
			if !strings.HasPrefix(payload.ToolUseID, "subagent:call_task:") {
				t.Fatalf("persisted child tool id = %q, want subagent namespace", payload.ToolUseID)
			}
			sawPersistedChildTool = true
		case payload.ActivityKind == string(agent.ActivityNotice) && payload.Summary == "subagent completed":
			sawDone = true
		}
	}
	if !sawStart || !sawPersistedChildTool || !sawDone {
		t.Fatalf("persisted activity start=%v child_tool=%v done=%v events=%#v", sawStart, sawPersistedChildTool, sawDone, ro.events)
	}
}

func TestCLISubagentRunnerClearsInheritedLimitAsker(t *testing.T) {
	probe := &probeTool{name: "safe_probe", risk: tool.RiskSafe}
	reg := tool.New()
	if err := reg.Register(probe); err != nil {
		t.Fatalf("register: %v", err)
	}
	asker := &countingLimitAsker{extension: 1}
	runner := &cliSubagentRunner{
		factory: agent.NewFactory(agent.Options{
			Provider: fake.NewRounds(
				fake.Script{fake.ToolCall("safe_probe", map[string]any{}), fake.Done()},
				fake.Script{fake.TextDelta("finalized"), fake.Done()},
			),
			Tools:      reg,
			Model:      "fake/model",
			Mode:       execmode.ModeWork,
			LimitAsker: asker,
		}),
	}
	got, err := runner.RunSubagent(context.Background(), "call tool", 1)
	if err != nil {
		t.Fatalf("RunSubagent: %v", err)
	}
	if got != "finalized" {
		t.Fatalf("answer = %q, want max-turn finalize text", got)
	}
	if asker.calls != 0 {
		t.Fatalf("inherited LimitAsker called %d times, want 0", asker.calls)
	}
}

type countingLimitAsker struct {
	calls     int
	extension int
}

func (a *countingLimitAsker) AskExtension(context.Context, agent.LimitExtensionRequest) (agent.LimitExtensionResponse, error) {
	a.calls++
	return agent.LimitExtensionResponse{ExtraTurns: a.extension}, nil
}

type blockingPreToolHook struct{}

func (blockingPreToolHook) Run(_ context.Context, event hook.Event) hook.Decision {
	if event.Kind != hook.KindPreToolCall {
		return hook.Decision{}
	}
	return hook.Decision{Block: true, Reason: "blocked by test hook"}
}

type recordingRollout struct {
	events []rollout.Event
}

func (r *recordingRollout) Append(_ context.Context, event rollout.Event) error {
	r.events = append(r.events, event)
	return nil
}

func (r *recordingRollout) Close() error { return nil }
