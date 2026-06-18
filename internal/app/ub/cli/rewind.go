package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/app/ub/tui"
	"github.com/feimingxliu/ub/internal/pkg/workspace/filehistory"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
)

func (r *tuiAgentRunner) ListRewindTargets(ctx context.Context) ([]tui.RewindTarget, error) {
	if r == nil || r.state == nil || r.state.rollout == nil || strings.TrimSpace(r.state.sessionID) == "" {
		return nil, fmt.Errorf("rewind requires an active session")
	}
	events, err := r.rewindEvents(ctx, 1)
	if err != nil {
		return nil, err
	}
	targets := rewindTargetsFromEvents(events)
	if fh, err := r.fileHistoryManager(ctx); err == nil {
		for i := range targets {
			targets[i].AffectedFiles = rewindAffectedFilesFromHistory(fh, targets[i].Turn)
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Turn != targets[j].Turn {
			return targets[i].Turn > targets[j].Turn
		}
		return targets[i].Time.After(targets[j].Time)
	})
	return targets, nil
}

func (r *tuiAgentRunner) Rewind(ctx context.Context, req tui.RewindRequest) (tui.SessionState, tui.RewindResult, error) {
	if r == nil || r.state == nil || r.state.rollout == nil || strings.TrimSpace(r.state.sessionID) == "" {
		return tui.SessionState{}, tui.RewindResult{}, fmt.Errorf("rewind requires an active session")
	}
	if req.Turn <= 0 {
		return tui.SessionState{}, tui.RewindResult{}, fmt.Errorf("rewind turn must be positive")
	}
	events, err := r.rewindEvents(ctx, 1)
	if err != nil {
		return tui.SessionState{}, tui.RewindResult{}, err
	}
	target, ok := rewindTargetForTurn(events, req.Turn)
	if !ok {
		return tui.SessionState{}, tui.RewindResult{}, fmt.Errorf("rewind target turn %d not found", req.Turn)
	}
	reverted, skipped := []string(nil), []string(nil)
	var fh *filehistory.Manager
	if manager, err := r.fileHistoryManager(ctx); err == nil {
		fh = manager
		target.AffectedFiles = rewindAffectedFilesFromHistory(fh, target.Turn)
	} else if req.RevertFiles {
		skipped = []string{"file checkpoint unavailable (" + err.Error() + ")"}
	}
	if req.RevertFiles {
		if fh != nil {
			changes, skip, err := fh.Rewind(req.Turn)
			if err != nil {
				skipped = append(skipped, "file checkpoint unavailable ("+err.Error()+")")
			}
			reverted = rewindChangeLabels(changes)
			skipped = append(skipped, skip...)
		}
	}
	deleted, err := r.state.rollout.DeleteFromTurn(ctx, r.state.sessionID, req.Turn)
	if err != nil {
		return tui.SessionState{}, tui.RewindResult{}, err
	}
	history, contextHistory, nextTurn, err := readChatHistory(r.cmd, r.state.rollout, r.state.sessionID)
	if err != nil {
		return tui.SessionState{}, tui.RewindResult{}, err
	}
	r.state.history = history
	r.state.contextHistory = contextHistory
	r.state.nextTurn = nextTurn
	r.state.session.UpdatedAt = time.Now().UTC()
	if err := r.state.store.UpdateSession(ctx, r.state.session); err != nil {
		return tui.SessionState{}, tui.RewindResult{}, err
	}
	r.invalidateMessageCache()
	result := tui.RewindResult{
		Target:        target,
		DeletedEvents: deleted,
		RevertedFiles: reverted,
		SkippedFiles:  skipped,
	}
	return r.sessionState(), result, nil
}

func (r *tuiAgentRunner) fileHistoryManager(ctx context.Context) (*filehistory.Manager, error) {
	if r == nil || r.tools == nil || r.state == nil || r.state.rollout == nil {
		return nil, fmt.Errorf("workspace unavailable")
	}
	return newFileHistoryManager(ctx, r.tools.Workspace, r.state.sessionID, r.state.rollout)
}

func (r *tuiAgentRunner) rewindEvents(ctx context.Context, startTurn int) ([]rollout.Event, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var events []rollout.Event
	if err := r.state.rollout.ForEachFromTurn(ctx, r.state.sessionID, startTurn, func(event rollout.Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		return nil, err
	}
	return events, nil
}

func rewindTargetsFromEvents(events []rollout.Event) []tui.RewindTarget {
	// Events are ordered by turn, then time/rowid. A single turn may carry
	// more than one user_message: the initial prompt plus any mid-turn inject
	// guidance (which reuses the turn). Deduplicate by turn so each rewind
	// target is one turn; DeleteFromTurn(N) removes the whole turn including
	// injects, so the first user_message is the right representative.
	var targets []tui.RewindTarget
	seen := map[int]struct{}{}
	for _, event := range events {
		if event.Type != rollout.TypeUserMessage {
			continue
		}
		if _, ok := seen[event.Turn]; ok {
			continue
		}
		text, err := rewindUserText(event)
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		seen[event.Turn] = struct{}{}
		targets = append(targets, tui.RewindTarget{
			Turn: event.Turn,
			Text: text,
			Time: event.Time,
		})
	}
	return targets
}

func rewindTargetForTurn(events []rollout.Event, turn int) (tui.RewindTarget, bool) {
	for _, event := range events {
		if event.Turn != turn || event.Type != rollout.TypeUserMessage {
			continue
		}
		text, err := rewindUserText(event)
		if err != nil || strings.TrimSpace(text) == "" {
			return tui.RewindTarget{}, false
		}
		return tui.RewindTarget{
			Turn: event.Turn,
			Text: text,
			Time: event.Time,
		}, true
	}
	return tui.RewindTarget{}, false
}

func rewindUserText(event rollout.Event) (string, error) {
	var payload rollout.MessagePayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return "", fmt.Errorf("decode rewind user event %s: %w", event.ID, err)
	}
	if strings.TrimSpace(payload.Text) != "" {
		return payload.Text, nil
	}
	return payload.Message.Text(), nil
}

func rewindAffectedFilesFromHistory(fh *filehistory.Manager, turn int) []tui.RewindFileChange {
	changes := fh.ChangedFiles(turn)
	out := make([]tui.RewindFileChange, 0, len(changes))
	for _, change := range changes {
		out = append(out, tui.RewindFileChange{Path: change.Path, Kind: change.Kind})
	}
	return out
}

func rewindChangeLabels(changes []filehistory.Change) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, change := range changes {
		label := strings.TrimSpace(change.Path)
		if strings.TrimSpace(change.Kind) != "" {
			label += " " + strings.TrimSpace(change.Kind)
		}
		addUnique(&out, seen, label)
	}
	return out
}

func addUnique(values *[]string, seen map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if _, ok := seen[value]; ok {
		return
	}
	seen[value] = struct{}{}
	*values = append(*values, value)
}
