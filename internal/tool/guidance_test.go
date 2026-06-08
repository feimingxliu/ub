package tool_test

import (
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tool/fs"
	"github.com/feimingxliu/ub/internal/tool/plan"
	"github.com/feimingxliu/ub/internal/tool/search"
	"github.com/feimingxliu/ub/internal/tool/shell"
	tasktool "github.com/feimingxliu/ub/internal/tool/task"
	todotool "github.com/feimingxliu/ub/internal/tool/todo"
)

func TestCodingAgentToolDescriptionsCarryGuidance(t *testing.T) {
	root := t.TempDir()
	reg := tool.New()
	for _, register := range []func(*tool.Registry, string) error{
		fs.Register,
		search.Register,
		shell.Register,
		plan.Register,
	} {
		if err := register(reg, root); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	if err := tasktool.Register(reg); err != nil {
		t.Fatalf("register task: %v", err)
	}
	if err := todotool.Register(reg); err != nil {
		t.Fatalf("register todo: %v", err)
	}

	checks := map[string][]string{
		"bash":             {"Prefer cwd", "exit_code=0", "prefer the dedicated", "retry edit/multiedit"},
		"read":             {"Never use read for directories", "before editing"},
		"grep":             {"locate symbols", "then read"},
		"task":             {"self-contained", "independent from the main context"},
		"plan_write":       {"Available only in plan mode", "validation"},
		"plan_update":      {"Available only in plan mode", "instead of plan_write"},
		"plan_update_step": {"Mark a step done only after", "verification evidence"},
		"todo_write":       {"current session execution todo list", "at most one item may be in_progress"},
		"todo_update":      {"Update one item", "Mark completed only after"},
	}
	for name, wants := range checks {
		t.Run(name, func(t *testing.T) {
			registered, ok := reg.Get(name)
			if !ok {
				t.Fatalf("tool %q not registered", name)
			}
			desc := registered.Description()
			for _, want := range wants {
				if !strings.Contains(desc, want) {
					t.Fatalf("%s description missing %q:\n%s", name, want, desc)
				}
			}
		})
	}
}
