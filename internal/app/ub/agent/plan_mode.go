package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
)

type planModeContextKey struct{}

// PlanModeAction identifies a model-requested plan-mode transition.
type PlanModeAction string

const (
	PlanModeEnter PlanModeAction = "enter"
	PlanModeExit  PlanModeAction = "exit"
)

// PlanModeRequest is the host-facing confirmation request used by the
// enter_plan_mode and exit_plan_mode tools.
type PlanModeRequest struct {
	Action    PlanModeAction `json:"action"`
	SessionID string         `json:"session_id,omitempty"`
	UserTurn  int            `json:"user_turn,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	PlanID    string         `json:"plan_id,omitempty"`
	Summary   string         `json:"summary,omitempty"`
}

// PlanModeResponse is returned by the host after confirming or denying the
// mode transition. Approved responses should include the effective from/to
// modes after the transition.
type PlanModeResponse struct {
	Approved bool           `json:"approved"`
	FromMode execution.Mode `json:"from_mode,omitempty"`
	ToMode   execution.Mode `json:"to_mode,omitempty"`
	Reason   string         `json:"reason,omitempty"`
}

// PlanModeController asks the host to enter or exit plan mode. TUI
// implementations usually show a confirmation dialog; headless runs omit it.
type PlanModeController interface {
	ConfirmPlanMode(ctx context.Context, req PlanModeRequest) (PlanModeResponse, error)
}

func contextWithPlanModeController(ctx context.Context, controller PlanModeController) context.Context {
	if controller == nil {
		return ctx
	}
	return context.WithValue(ctx, planModeContextKey{}, controller)
}

func planModeControllerFromContext(ctx context.Context) PlanModeController {
	if ctx == nil {
		return nil
	}
	controller, _ := ctx.Value(planModeContextKey{}).(PlanModeController)
	return controller
}

// NewPlanModeTools returns the model-callable mode-transition tools.
func NewPlanModeTools() []tool.Tool {
	return []tool.Tool{
		&enterPlanModeTool{schema: jsonschema.Reflect(&enterPlanModeArgs{})},
		&exitPlanModeTool{schema: jsonschema.Reflect(&exitPlanModeArgs{})},
	}
}

type enterPlanModeArgs struct {
	Reason string `json:"reason,omitempty" jsonschema:"description=Brief reason planning is useful before implementation."`
}

type enterPlanModeTool struct {
	schema *jsonschema.Schema
}

func (t *enterPlanModeTool) Name() string { return "enter_plan_mode" }

func (t *enterPlanModeTool) Description() string {
	return "Ask the user to enter plan mode before doing complex implementation work. Use for new features, multi-file behavior changes, architecture choices, risky migrations, or ambiguous requirements that need read-only investigation and a persistent plan first. Do not use for small typo fixes, simple known bug fixes, already-specified implementation steps, or pure read-only questions. This tool is the only way to enter plan mode: stating \"entering plan mode\" in text does nothing, so call this tool instead of announcing it. Do not use todo_write as a substitute; todo_write tracks execution progress, not the planning step. In plan mode, create or revise the plan with plan_write or plan_update, then call exit_plan_mode when the plan is ready for approval."
}

func (t *enterPlanModeTool) Schema() *jsonschema.Schema { return t.schema }
func (t *enterPlanModeTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *enterPlanModeTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var args enterPlanModeArgs
	if err := tool.UnmarshalArgs(raw, &args); err != nil {
		return tool.Result{}, fmt.Errorf("enter_plan_mode: invalid args: %w", err)
	}
	controller := planModeControllerFromContext(ctx)
	if controller == nil {
		return tool.Result{
			Content: "No interactive plan-mode controller is available for this run. Continue in the current mode, and if planning is needed, explain the plan in text before making changes.",
			Metadata: map[string]string{
				"mode_action":   string(PlanModeEnter),
				"mode_approved": "false",
				"mode_status":   "unavailable",
			},
		}, nil
	}
	resp, err := controller.ConfirmPlanMode(ctx, PlanModeRequest{
		Action:    PlanModeEnter,
		SessionID: tool.SessionIDFromContext(ctx),
		UserTurn:  tool.AgentTurnFromContext(ctx),
		ToolUseID: tool.ToolUseIDFromContext(ctx),
		Reason:    strings.TrimSpace(args.Reason),
	})
	if err != nil {
		return tool.Result{}, fmt.Errorf("enter_plan_mode: %w", err)
	}
	return planModeToolResult(PlanModeEnter, resp, "entered plan mode; inspect read-only context, write or update a plan artifact, then call exit_plan_mode for user approval", "user declined plan mode; continue in the current mode")
}

type exitPlanModeArgs struct {
	PlanID  string `json:"plan_id,omitempty" jsonschema:"description=Plan artifact id returned by plan_write or plan_update."`
	Summary string `json:"summary,omitempty" jsonschema:"description=Concise summary of the plan the user is approving."`
}

type exitPlanModeTool struct {
	schema *jsonschema.Schema
}

func (t *exitPlanModeTool) Name() string { return "exit_plan_mode" }

func (t *exitPlanModeTool) Description() string {
	return "Ask the user to approve the current plan and exit plan mode. Use only after plan_write or plan_update has captured the intended implementation plan. Include the plan_id returned by the plan tool and a concise summary. If the user declines, stay in plan mode and revise the existing plan."
}

func (t *exitPlanModeTool) Schema() *jsonschema.Schema { return t.schema }
func (t *exitPlanModeTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *exitPlanModeTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var args exitPlanModeArgs
	if err := tool.UnmarshalArgs(raw, &args); err != nil {
		return tool.Result{}, fmt.Errorf("exit_plan_mode: invalid args: %w", err)
	}
	planID := strings.TrimSpace(args.PlanID)
	if planID == "" {
		return tool.Result{
			Content: "exit_plan_mode requires a plan_id from plan_write or plan_update before asking for approval; stay in plan mode and create or update a plan artifact first.",
			IsError: true,
			Metadata: map[string]string{
				"mode_action":   string(PlanModeExit),
				"mode_approved": "false",
				"mode_status":   "missing_plan",
				"reason":        "missing plan_id",
			},
		}, nil
	}
	controller := planModeControllerFromContext(ctx)
	if controller == nil {
		return tool.Result{
			Content: "No interactive plan-mode controller is available for this run. Stay in plan mode if active, or ask the user to approve the plan in text.",
			Metadata: map[string]string{
				"mode_action":   string(PlanModeExit),
				"mode_approved": "false",
				"mode_status":   "unavailable",
			},
		}, nil
	}
	resp, err := controller.ConfirmPlanMode(ctx, PlanModeRequest{
		Action:    PlanModeExit,
		SessionID: tool.SessionIDFromContext(ctx),
		UserTurn:  tool.AgentTurnFromContext(ctx),
		ToolUseID: tool.ToolUseIDFromContext(ctx),
		PlanID:    planID,
		Summary:   strings.TrimSpace(args.Summary),
	})
	if err != nil {
		return tool.Result{}, fmt.Errorf("exit_plan_mode: %w", err)
	}
	return planModeToolResult(PlanModeExit, resp, "plan approved; exited plan mode and restored the previous execution mode", "user declined the plan; stay in plan mode and revise the existing plan")
}

func planModeToolResult(action PlanModeAction, resp PlanModeResponse, approvedText, deniedText string) (tool.Result, error) {
	status := "denied"
	content := deniedText
	if resp.Approved {
		status = "approved"
		content = approvedText
	}
	reason := strings.TrimSpace(resp.Reason)
	if reason != "" {
		content += ": " + reason
	}
	metadata := map[string]string{
		"mode_action":   string(action),
		"mode_approved": fmt.Sprintf("%t", resp.Approved),
		"mode_status":   status,
	}
	if resp.FromMode != "" {
		metadata["from_mode"] = string(resp.FromMode)
	}
	if resp.ToMode != "" {
		metadata["to_mode"] = string(resp.ToMode)
	}
	if reason != "" {
		metadata["reason"] = reason
	}
	return tool.Result{Content: content, Metadata: metadata}, nil
}

func (a *Agent) recordModeActivity(ctx context.Context, sessionID string, turn int, call toolCall, result tool.Result) {
	action := strings.TrimSpace(result.Metadata["mode_action"])
	if action == "" {
		return
	}
	status := strings.TrimSpace(result.Metadata["mode_status"])
	if status == "" {
		status = "done"
	}
	from := strings.TrimSpace(result.Metadata["from_mode"])
	to := strings.TrimSpace(result.Metadata["to_mode"])
	approved := strings.EqualFold(strings.TrimSpace(result.Metadata["mode_approved"]), "true")
	summary := "Mode switch"
	switch PlanModeAction(action) {
	case PlanModeEnter:
		summary = "Enter Plan Mode"
	case PlanModeExit:
		summary = "Exit Plan Mode"
	}
	content := planModeActivityContent(from, to, approved, result.Content)
	event := Event{
		Type:         EventActivity,
		ActivityKind: ActivityMode,
		ToolUseID:    call.ID,
		ToolName:     call.Name,
		Status:       status,
		Summary:      summary,
		Content:      content,
		Decision:     status,
		Source:       "tool",
		Reason:       strings.TrimSpace(result.Metadata["reason"]),
		Allowed:      approved,
		IsError:      result.IsError,
	}
	a.emit(event)
	if err := a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.Activity(sessionID, turn, rolloutActivityPayload(event))
	}); err != nil {
		a.emit(Event{Type: EventError, Content: fmt.Sprintf("record mode activity: %v", err), IsError: true, Err: err})
	}
}

func planModeActivityContent(from, to string, approved bool, detail string) string {
	var parts []string
	if from != "" {
		parts = append(parts, "from="+from)
	}
	if to != "" {
		parts = append(parts, "to="+to)
	}
	parts = append(parts, fmt.Sprintf("approved=%t", approved))
	if trimmed := strings.TrimSpace(detail); trimmed != "" {
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, "\n")
}
