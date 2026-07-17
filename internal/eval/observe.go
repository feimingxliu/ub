package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/store"
)

func Observe(ctx context.Context, dataHome, workspace string) (Observation, error) {
	st, err := store.Open(filepath.Join(dataHome, "ub", "ub.db"))
	if err != nil {
		return Observation{}, fmt.Errorf("open eval session store: %w", err)
	}
	defer st.Close()
	sessions, err := st.ListSessions(ctx, workspace, 100)
	if err != nil {
		return Observation{}, err
	}
	if len(sessions) == 0 {
		return Observation{}, fmt.Errorf("eval run created no session for workspace %q", workspace)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})
	session := sessions[0]
	ro, err := rollout.New(st)
	if err != nil {
		return Observation{}, err
	}
	defer ro.Close()

	observation := Observation{
		SessionID: session.ID,
		Provider:  session.Provider,
		Model:     session.Model,
		Metrics: Metrics{
			ToolCalls:        []string{},
			ContextDecisions: []ContextDecision{},
		},
	}
	if err := ro.ForEach(ctx, session.ID, func(event rollout.Event) error {
		if event.Turn > observation.Metrics.Turns {
			observation.Metrics.Turns = event.Turn
		}
		switch event.Type {
		case rollout.TypeToolResult:
			var payload rollout.ToolResultPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return fmt.Errorf("decode tool result: %w", err)
			}
			observation.Metrics.ToolCalls = append(observation.Metrics.ToolCalls, payload.ToolName)
		case rollout.TypeUsage:
			var payload rollout.UsagePayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return fmt.Errorf("decode usage: %w", err)
			}
			observation.Metrics.InputTokens += payload.InputTokens
			observation.Metrics.OutputTokens += payload.OutputTokens
			observation.Metrics.ReasoningTokens += payload.ReasoningTokens
			observation.Metrics.CacheReadTokens += payload.CacheReadTokens
			observation.Metrics.CacheWriteTokens += payload.CacheWriteTokens
		case rollout.TypeSummary:
			var payload rollout.SummaryPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return fmt.Errorf("decode summary: %w", err)
			}
			if payload.Maintenance != nil {
				observation.Metrics.ContextDecisions = append(observation.Metrics.ContextDecisions, ContextDecision{
					Action: payload.Maintenance.Decision,
					Reason: payload.Maintenance.Reason,
				})
			}
		case rollout.TypeAssistantMessage:
			msg, ok, err := rollout.MessageFromEvent(event)
			if err != nil {
				return err
			}
			if ok {
				observation.AssistantText = msg.Text()
			}
		}
		return nil
	}); err != nil {
		return Observation{}, err
	}
	return observation, nil
}
