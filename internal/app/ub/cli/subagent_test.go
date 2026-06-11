package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider/fake"
	"github.com/feimingxliu/ub/internal/pkg/runtime/hook"
	"github.com/feimingxliu/ub/internal/pkg/runtime/permission"
	"github.com/feimingxliu/ub/internal/pkg/tool"
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
		mode:            execution.ModePlan,
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
		mode:            execution.ModeWork,
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

type blockingPreToolHook struct{}

func (blockingPreToolHook) Run(_ context.Context, event hook.Event) hook.Decision {
	if event.Kind != hook.KindPreToolCall {
		return hook.Decision{}
	}
	return hook.Decision{Block: true, Reason: "blocked by test hook"}
}
