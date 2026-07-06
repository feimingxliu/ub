package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/pkg/tool"
	goaltool "github.com/feimingxliu/ub/internal/pkg/tool/goal"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
)

// recordGoalActivity emits a goal activity event to the EventSink and
// persists it to the rollout. It is called after a goal tool (create_goal,
// update_goal, get_goal) produces a result.
func (a *Agent) recordGoalActivity(ctx context.Context, sessionID string, turn int, call toolCall, result tool.Result) {
	action := goalActionFromMeta(result.Metadata)
	if action == nil {
		return
	}
	status := strings.TrimSpace(result.Metadata["goal_status"])
	if status == "" {
		status = "done"
	}
	summary := fmt.Sprintf("Goal %s", string(*action))
	switch *action {
	case GoalCreate:
		summary = "Create Goal"
	case GoalUpdate:
		summary = "Update Goal → " + status
	case GoalGet:
		summary = "Get Goal"
	}
	evt := Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		ToolUseID:    call.ID,
		ToolName:     call.Name,
		Status:       status,
		Summary:      summary,
		Content:      result.Content,
		Notice:       NoticeGoalStatus,
	}
	a.emit(evt)
	if err := a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.Activity(sessionID, turn, rolloutActivityPayload(evt))
	}); err != nil {
		a.emit(Event{Type: EventError, Content: fmt.Sprintf("record goal activity: %v", err), IsError: true, Err: err})
	}
}

// GoalAction identifies a model-requested goal state change.
type GoalAction string

const (
	GoalCreate GoalAction = "create"
	GoalUpdate GoalAction = "update"
	GoalGet    GoalAction = "get"
)

func goalActionFromMeta(meta map[string]string) *GoalAction {
	if meta == nil {
		return nil
	}
	switch strings.TrimSpace(meta["goal_action"]) {
	case "created":
		a := GoalCreate
		return &a
	case "complete", "blocked", "paused":
		a := GoalUpdate
		return &a
	default:
		return nil
	}
}

// GoalContinuationPrompt builds the hidden user message that is injected at
// the start of each auto-continued goal turn.
func GoalContinuationPrompt(g *goaltool.Goal) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<goal_continuation>

You are continuing work toward an active goal.

## Active Goal

Status: %s
Objective: %s`, g.Status, g.Objective))
	if g.TokenBudget > 0 {
		pct := float64(g.TokensUsed) / float64(g.TokenBudget) * 100
		b.WriteString(fmt.Sprintf("\nToken Budget: %d used / %d total (%.0f%%)", g.TokensUsed, g.TokenBudget, pct))
	} else if g.TokensUsed > 0 {
		b.WriteString(fmt.Sprintf("\nTokens Used: %d", g.TokensUsed))
	}
	if g.TurnBudget > 0 {
		pct := float64(g.TurnsUsed) / float64(g.TurnBudget) * 100
		b.WriteString(fmt.Sprintf("\nTurn Budget: %d used / %d total (%.0f%%)", g.TurnsUsed, g.TurnBudget, pct))
	} else if g.TurnsUsed > 0 {
		b.WriteString(fmt.Sprintf("\nTurns Used: %d", g.TurnsUsed))
	}
	b.WriteString(fmt.Sprintf("\nCreated: %s", g.CreatedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("\nUpdated: %s", g.UpdatedAt.Format(time.RFC3339)))
	b.WriteString(`

Continue working toward the goal. Do NOT call create_goal again — the goal
already exists. Call update_goal(status="complete") only when the objective
is fully achieved and verified against the actual current state.

Call update_goal(status="blocked", block_reason="...") only after the same
blocking condition has repeated for 3 consecutive goal turns. If this is the
first or second occurrence, keep trying to make progress instead.

Use todo_write / todo_update to track sub-task progress if helpful.
</goal_continuation>`)
	return b.String()
}
