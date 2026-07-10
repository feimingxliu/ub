package goal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

// Register adds the create_goal, update_goal, and get_goal tools to reg.
// Goal state is session-scoped and persisted under the state root.
func Register(reg *tool.Registry) error {
	if reg == nil {
		return fmt.Errorf("goal: nil registry")
	}
	for _, t := range []tool.Tool{
		&createGoalTool{schema: jsonschema.Reflect(&createGoalArgs{})},
		&updateGoalTool{schema: jsonschema.Reflect(&updateGoalArgs{})},
		&getGoalTool{schema: jsonschema.Reflect(&getGoalArgs{})},
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// --- create_goal ---

type createGoalArgs struct {
	Objective   string `json:"objective" jsonschema:"required,description=The objective the agent should work towards. Max 4000 characters."`
	TokenBudget int    `json:"token_budget,omitempty" jsonschema:"description=Optional maximum total tokens to consume across all turns. Zero means no limit."`
	TurnBudget  int    `json:"turn_budget,omitempty" jsonschema:"description=Optional maximum agent turns to use. Zero means no limit."`
}

type createGoalTool struct {
	schema *jsonschema.Schema
}

func (t *createGoalTool) Name() string { return "create_goal" }
func (t *createGoalTool) Description() string {
	return "Create an active goal for autonomous multi-turn work. The host will automatically continue the agent loop until the goal is marked complete, blocked, or budget-limited. Call this at the start of a long-running task. If a goal already exists, this tool fails; call get_goal or update_goal instead."
}
func (t *createGoalTool) Schema() *jsonschema.Schema { return t.schema }
func (t *createGoalTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *createGoalTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var args createGoalArgs
	if err := tool.DecodeArgs("create_goal", raw, &args); err != nil {
		return tool.Result{}, err
	}
	sessionID := strings.TrimSpace(tool.SessionIDFromContext(ctx))
	if sessionID == "" {
		return tool.Result{}, fmt.Errorf("create_goal: session id is required")
	}
	objective := strings.TrimSpace(args.Objective)
	if objective == "" {
		return tool.Result{}, fmt.Errorf("create_goal: objective is required")
	}
	if len([]rune(objective)) > maxObjectiveChars {
		return tool.Result{}, fmt.Errorf("create_goal: objective exceeds %d characters", maxObjectiveChars)
	}
	// Reject if a goal already exists.
	existing, err := Load(sessionID)
	if err != nil {
		return tool.Result{}, fmt.Errorf("create_goal: %w", err)
	}
	if existing != nil && !IsTerminal(existing.Status) {
		return tool.Result{
			Content: fmt.Sprintf("An active goal already exists (status=%s). Complete or cancel it first with update_goal.", existing.Status),
			IsError: true,
		}, nil
	}
	now := time.Now().UTC()
	g := &Goal{
		SessionID:   sessionID,
		Objective:   objective,
		Status:      StatusActive,
		TokenBudget: args.TokenBudget,
		TurnBudget:  args.TurnBudget,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := Save(sessionID, g); err != nil {
		return tool.Result{}, fmt.Errorf("create_goal: %w", err)
	}
	return tool.Result{
		Content: renderGoal(*g),
		Metadata: map[string]string{
			"goal_status": string(g.Status),
			"goal_action": "created",
		},
	}, nil
}

// --- update_goal ---

type updateGoalArgs struct {
	Status      string `json:"status" jsonschema:"required,enum=complete,enum=blocked,enum=paused,description=New goal status. Only complete (fully achieved and verified) and blocked (same condition repeated 3+ turns) are accepted. Pause/resume is manual."`
	BlockReason string `json:"block_reason,omitempty" jsonschema:"description=Required when status=blocked. Describe what is blocking progress."`
}

type updateGoalTool struct {
	schema *jsonschema.Schema
}

func (t *updateGoalTool) Name() string { return "update_goal" }
func (t *updateGoalTool) Description() string {
	return "Update the current goal's status. Use 'complete' when the objective is fully achieved and verified. Use 'blocked' only after the same blocking condition repeats 3+ consecutive goal turns and describes the blocker."
}
func (t *updateGoalTool) Schema() *jsonschema.Schema { return t.schema }
func (t *updateGoalTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *updateGoalTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var args updateGoalArgs
	if err := tool.DecodeArgs("update_goal", raw, &args); err != nil {
		return tool.Result{}, err
	}
	sessionID := strings.TrimSpace(tool.SessionIDFromContext(ctx))
	if sessionID == "" {
		return tool.Result{}, fmt.Errorf("update_goal: session id is required")
	}
	g, err := Load(sessionID)
	if err != nil {
		return tool.Result{}, fmt.Errorf("update_goal: %w", err)
	}
	if g == nil {
		return tool.Result{
			Content: "No active goal exists. Call create_goal first.",
			IsError: true,
		}, nil
	}
	newStatus := normalizeStatus(args.Status)
	if newStatus == "" {
		return tool.Result{}, fmt.Errorf("update_goal: invalid status %q (want complete, blocked, or paused)", args.Status)
	}
	switch newStatus {
	case StatusComplete:
		// Always allowed; agent has verified the objective.
	case StatusBlocked:
		if strings.TrimSpace(args.BlockReason) == "" {
			return tool.Result{}, fmt.Errorf("update_goal: block_reason is required when status=blocked")
		}
		// Track consecutive blockers.
		if g.Status == StatusBlocked && g.BlockReason == args.BlockReason {
			g.ConsecutiveBlockCount++
		} else {
			g.ConsecutiveBlockCount = 1
		}
		g.BlockReason = strings.TrimSpace(args.BlockReason)
		if g.ConsecutiveBlockCount < 3 {
			return tool.Result{
				Content: fmt.Sprintf("Blocked (occurrence %d/3). The goal host only accepts blocked after 3 consecutive turns with the same blocking condition. Set the status to 'complete' if the objective is achieved, or continue working.", g.ConsecutiveBlockCount),
				IsError: true,
			}, nil
		}
	case StatusPaused:
		// Manual pause; always accepted.
	default:
		return tool.Result{}, fmt.Errorf("update_goal: invalid target status %q", newStatus)
	}
	g.Status = newStatus
	g.UpdatedAt = time.Now().UTC()
	if err := Save(sessionID, g); err != nil {
		return tool.Result{}, fmt.Errorf("update_goal: %w", err)
	}
	return tool.Result{
		Content: renderGoal(*g),
		Metadata: map[string]string{
			"goal_status": string(g.Status),
			"goal_action": string(newStatus),
		},
	}, nil
}

// --- get_goal ---

type getGoalArgs struct{}

type getGoalTool struct {
	schema *jsonschema.Schema
}

func (t *getGoalTool) Name() string { return "get_goal" }
func (t *getGoalTool) Description() string {
	return "Return the current goal state including status, objective, and token/turn usage. Use this to check progress toward the goal."
}
func (t *getGoalTool) Schema() *jsonschema.Schema { return t.schema }
func (t *getGoalTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *getGoalTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	sessionID := strings.TrimSpace(tool.SessionIDFromContext(ctx))
	if sessionID == "" {
		return tool.Result{}, fmt.Errorf("get_goal: session id is required")
	}
	g, err := Load(sessionID)
	if err != nil {
		return tool.Result{}, fmt.Errorf("get_goal: %w", err)
	}
	if g == nil {
		return tool.Result{Content: "No goal is set for this session."}, nil
	}
	return tool.Result{Content: renderGoal(*g)}, nil
}

// --- helpers ---

func normalizeStatus(s string) Status {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "complete", "done", "completed":
		return StatusComplete
	case "blocked", "stuck", "cannot_proceed":
		return StatusBlocked
	case "paused", "pause":
		return StatusPaused
	default:
		return ""
	}
}

func renderGoal(g Goal) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Goal\n\nStatus: %s\n", g.Status))
	b.WriteString(fmt.Sprintf("Objective: %s\n", g.Objective))
	if g.TokenBudget > 0 {
		pct := float64(g.TokensUsed) / float64(g.TokenBudget) * 100
		b.WriteString(fmt.Sprintf("Tokens: %d / %d (%.0f%%)\n", g.TokensUsed, g.TokenBudget, pct))
	} else if g.TokensUsed > 0 {
		b.WriteString(fmt.Sprintf("Tokens: %d\n", g.TokensUsed))
	}
	if g.TurnBudget > 0 {
		pct := float64(g.TurnsUsed) / float64(g.TurnBudget) * 100
		b.WriteString(fmt.Sprintf("Turns: %d / %d (%.0f%%)\n", g.TurnsUsed, g.TurnBudget, pct))
	} else if g.TurnsUsed > 0 {
		b.WriteString(fmt.Sprintf("Turns: %d\n", g.TurnsUsed))
	}
	if g.BlockReason != "" {
		b.WriteString(fmt.Sprintf("Block reason: %s (x%d)\n", g.BlockReason, g.ConsecutiveBlockCount))
	}
	b.WriteString(fmt.Sprintf("Created: %s\n", g.CreatedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("Updated: %s\n", g.UpdatedAt.Format(time.RFC3339)))
	return b.String()
}

// RecordUsage increments the token and turn counters for the session's goal.
// It is called by the host after each completed agent turn.
func RecordUsage(sessionID string, tokensUsed int) error {
	g, err := Load(sessionID)
	if err != nil {
		return err
	}
	if g == nil || IsTerminal(g.Status) {
		return nil
	}
	g.TokensUsed += tokensUsed
	g.TurnsUsed++
	g.UpdatedAt = time.Now().UTC()
	// Check budgets. When tokensUsed is 0, the caller didn't provide real
	// usage data, so skip token_budget enforcement to avoid false triggers.
	tokenBudgetActive := g.TokenBudget > 0 && tokensUsed > 0
	turnBudgetActive := g.TurnBudget > 0
	if tokenBudgetActive && g.TokensUsed >= g.TokenBudget {
		g.Status = StatusBudgetLimited
	} else if turnBudgetActive && g.TurnsUsed >= g.TurnBudget {
		g.Status = StatusBudgetLimited
	}
	return Save(sessionID, g)
}
