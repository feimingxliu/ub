package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/invopop/jsonschema"

	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tool/plan"
)

// planModeContextKey is the context key for the PlanModeController that
// tools (enter_plan_mode/exit_plan_mode) use to request mode transitions.
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
	PlanBody  string         `json:"plan_body,omitempty"`
}

// PlanModeResponse is returned by the host after confirming or denying the
// mode transition. Approved responses should include the effective from/to
// modes after the transition.
type PlanModeResponse struct {
	Approved bool          `json:"approved"`
	FromMode execmode.Mode `json:"from_mode,omitempty"`
	ToMode   execmode.Mode `json:"to_mode,omitempty"`
	Reason   string        `json:"reason,omitempty"`
}

// PlanModeController asks the host to enter or exit plan execmode. TUI
// implementations enter silently and show the plan approval dialog on exit;
// headless runs omit the controller.
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
	return "Enter plan mode to design an approach before implementing. Use before complex new features, multi-file changes, architecture decisions, risky migrations, or ambiguous requirements. In plan mode only read tools (read, ls, glob, grep) and plan_write/plan_update are available — write/edit/bash are blocked. Use plan_write to create a plan, plan_update to revise it, then call exit_plan_mode for user approval. The only way to enter plan mode is to call this tool; stating \"entering plan mode\" in text does nothing. Do not use todo_write as a substitute."
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
	// Enter silently — no user confirmation popup at entry time.
	// The single approval point is exit_plan_mode.
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
	// If the controller returns approved=false (e.g. headless mode rejecting),
	// respect it. In TUI mode the controller auto-approves enter requests.
	return planModeToolResult(PlanModeEnter, resp,
		"Entered plan mode. You are now in read-only mode. Explore the codebase, write a plan with plan_write, revise with plan_update, and call exit_plan_mode when ready for approval.",
		"user declined plan mode; continue in the current mode")
}

type exitPlanModeArgs struct {
	PlanID  string `json:"plan_id" jsonschema:"required,description=Plan artifact id returned by plan_write or plan_update. The exact artifact is displayed for approval."`
	Summary string `json:"summary,omitempty" jsonschema:"description=Concise summary of the plan the user is approving."`
}

type exitPlanModeTool struct {
	schema *jsonschema.Schema
}

func (t *exitPlanModeTool) Name() string { return "exit_plan_mode" }

func (t *exitPlanModeTool) Description() string {
	return "Exit plan mode by presenting a specific saved plan for user approval. Use only after plan_write or plan_update has captured the implementation plan, and include its plan_id. If the user declines, stay in plan mode and revise the existing plan. This is the single approval point — enter_plan_mode does not require confirmation."
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
			Content: "exit_plan_mode requires the plan_id returned by plan_write or plan_update; stay in plan mode and create or revise the plan artifact first.",
			IsError: true,
			Metadata: map[string]string{
				"mode_action":   string(PlanModeExit),
				"mode_approved": "false",
				"mode_status":   "missing_plan",
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
	// Read the exact plan artifact for display in the TUI approval dialog.
	workspace := tool.WorkspaceFromContext(ctx)
	planBody, planPath, err := readPlanBody(workspace, planID)
	if err != nil {
		return tool.Result{
			Content:  "exit_plan_mode could not load plan " + planID + ": " + err.Error(),
			IsError:  true,
			Metadata: map[string]string{"mode_action": string(PlanModeExit), "mode_approved": "false", "mode_status": "missing_plan"},
		}, nil
	}
	resp, err := controller.ConfirmPlanMode(ctx, PlanModeRequest{
		Action:    PlanModeExit,
		SessionID: tool.SessionIDFromContext(ctx),
		UserTurn:  tool.AgentTurnFromContext(ctx),
		ToolUseID: tool.ToolUseIDFromContext(ctx),
		PlanID:    planID,
		Summary:   strings.TrimSpace(args.Summary),
		PlanBody:  planBody,
	})
	if err != nil {
		return tool.Result{}, fmt.Errorf("exit_plan_mode: %w", err)
	}
	approvedText := "plan approved; exited plan mode and restored the previous execution mode"
	if planPath != "" {
		approvedText += "; plan saved at " + planPath
	}
	return planModeToolResult(PlanModeExit, resp, approvedText, "user declined the plan; stay in plan mode and revise the existing plan")
}

// readPlanBody loads the exact requested plan artifact for the approval
// dialog. The caller must not open the confirmation dialog without it.
func readPlanBody(workspace, planID string) (string, string, error) {
	if workspace == "" {
		return "", "", fmt.Errorf("workspace is unavailable")
	}
	path, err := plan.Path(workspace, planID)
	if err != nil {
		return "", "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", path, err
	}
	return string(body), path, nil
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
