package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
)

// recordPermissionActivity emits a permission activity event to the
// EventSink and persists it to the rollout. It records the tool name,
// decision source (rule/human/approval-agent/mode), decision (allow/deny),
// reason, and whether the call was ultimately allowed.
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

// append writes one rollout event if a writer and session ID are configured.
// The build callback defers event construction so expensive serialization only
// runs when the rollout is actually being recorded.
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

// emit sends a runtime event to the foreground EventSink. It is safe to
// call when no sink is configured (events is nil).
func (a *Agent) emit(event Event) {
	if a.events != nil {
		a.events(event)
	}
}

// recordError emits an error event to the EventSink, persists it to the
// rollout as an error event, and returns the original error (or a wrapped
// error if the rollout write itself failed). Callers should use the return
// value so the caller's error chain stays consistent.
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
