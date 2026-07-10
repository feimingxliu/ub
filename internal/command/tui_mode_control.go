package command

import (
	"context"
	"fmt"
	"strings"

	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/rollout"
)

func (r *tuiAgentRunner) SetMode(mode string) error {
	parsed, err := execmode.ParseMode(mode)
	if err != nil {
		return err
	}
	r.modeMu.Lock()
	from, to := r.setModeLocked(parsed)
	r.modeMu.Unlock()
	r.recordModeSwitchActivity("slash", from, to, true, "")
	return nil
}

func (r *tuiAgentRunner) EnterPlanMode() (string, string, error) {
	if r == nil {
		return "", "", fmt.Errorf("plan mode switching is unavailable")
	}
	r.modeMu.Lock()
	from, to := r.setModeLocked(execmode.ModePlan)
	r.modeMu.Unlock()
	return string(from), string(to), nil
}

func (r *tuiAgentRunner) ExitPlanMode() (string, string, error) {
	if r == nil {
		return "", "", fmt.Errorf("plan mode switching is unavailable")
	}
	r.modeMu.Lock()
	defer r.modeMu.Unlock()
	from := r.mode
	if from != execmode.ModePlan {
		return string(from), string(from), fmt.Errorf("not in plan mode")
	}
	to := r.prePlanMode
	if to == "" {
		to = r.startupMode
	}
	if to == "" || to == execmode.ModePlan {
		to = execmode.ModeWork
	}
	r.mode = to
	r.prePlanMode = ""
	return string(from), string(to), nil
}

func (r *tuiAgentRunner) setModeLocked(mode execmode.Mode) (execmode.Mode, execmode.Mode) {
	from := r.mode
	if mode == execmode.ModePlan && from != execmode.ModePlan {
		r.prePlanMode = from
	}
	if mode != execmode.ModePlan && from == execmode.ModePlan {
		r.prePlanMode = ""
	}
	r.mode = mode
	return from, mode
}

func (r *tuiAgentRunner) recordModeSwitchActivity(source string, from, to execmode.Mode, approved bool, toolUseID string) {
	if r == nil || r.state == nil || r.state.rollout == nil || strings.TrimSpace(r.state.sessionID) == "" || from == to {
		return
	}
	summary := "Mode switch"
	if to == execmode.ModePlan {
		summary = "Enter Plan Mode"
	} else if from == execmode.ModePlan {
		summary = "Exit Plan Mode"
	}
	decision := "approved"
	if !approved {
		decision = "denied"
	}
	payload := rollout.ActivityPayload{
		ActivityKind: "mode",
		ToolUseID:    toolUseID,
		ToolName:     "mode",
		Status:       decision,
		Summary:      summary,
		Content:      fmt.Sprintf("from=%s\nto=%s\napproved=%t", from, to, approved),
		Decision:     decision,
		Source:       source,
		Allowed:      approved,
	}
	event, err := rollout.Activity(r.state.sessionID, max(1, r.state.nextTurn), payload)
	if err != nil {
		return
	}
	_ = r.state.rollout.Append(context.Background(), event)
}

func (r *tuiAgentRunner) currentMode() execmode.Mode {
	r.modeMu.RLock()
	defer r.modeMu.RUnlock()
	return r.mode
}
