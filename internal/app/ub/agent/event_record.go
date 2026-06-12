package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
)

func (a *Agent) recordPermissionActivity(ctx context.Context, sessionID string, turn int, toolName, source, decision, reason string, allowed bool) {
	event := a.emitPermissionActivity(toolName, source, decision, reason, allowed)
	if err := a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.Activity(sessionID, turn, rolloutActivityPayload(event))
	}); err != nil {
		a.emit(Event{
			Type:    EventError,
			Content: fmt.Sprintf("record permission activity: %v", err),
			IsError: true,
			Err:     err,
		})
	}
}

func (a *Agent) append(ctx context.Context, sessionID string, build func() (rollout.Event, error)) error {
	if a.rollout == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	event, err := build()
	if err != nil {
		return err
	}
	return a.rollout.Append(ctx, event)
}

func (a *Agent) emit(event Event) {
	if a.events != nil {
		a.events(event)
	}
}

func (a *Agent) recordError(ctx context.Context, sessionID string, turn int, err error) error {
	if err == nil {
		return nil
	}
	a.emit(Event{Type: EventError, Content: err.Error(), IsError: true, Err: err})
	if appendErr := a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.Error(sessionID, turn, err)
	}); appendErr != nil {
		return fmt.Errorf("record rollout error: %v; original error: %w", appendErr, err)
	}
	return err
}
