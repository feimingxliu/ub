package command

import (
	"context"

	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/workspace/filehistory"
)

func newFileHistoryManager(ctx context.Context, workspace, sessionID string, ro *rollout.SQLite) (*filehistory.Manager, error) {
	if ro == nil {
		return nil, nil
	}
	var events []rollout.Event
	if err := ro.ForEach(ctx, sessionID, func(event rollout.Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		return nil, err
	}
	return filehistory.New(filehistory.Options{
		Workspace: workspace,
		SessionID: sessionID,
		Rollout:   ro,
		Events:    events,
	})
}
