package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/store"
	goaltool "github.com/feimingxliu/ub/internal/tool/goal"
	mcptool "github.com/feimingxliu/ub/internal/tool/mcp"
	"github.com/feimingxliu/ub/internal/tui"
)

func (r *tuiAgentRunner) ListSessions(ctx context.Context) ([]tui.SessionInfo, error) {
	sessions, err := listCurrentWorkspaceSessions(ctx, 20)
	if err != nil {
		return nil, err
	}
	out := make([]tui.SessionInfo, 0, len(sessions))
	current := r.CurrentSessionID()
	for _, sess := range sessions {
		out = append(out, tui.SessionInfo{
			ID:        sess.ID,
			Title:     sess.Title,
			Provider:  sess.Provider,
			Model:     sess.Model,
			UpdatedAt: sess.UpdatedAt,
			Current:   sess.ID == current,
		})
	}
	return out, nil
}

func (r *tuiAgentRunner) SearchSessions(ctx context.Context, query string, limit int) (string, error) {
	st, closeStore, err := r.sessionSearchStore()
	if err != nil {
		return "", err
	}
	if closeStore != nil {
		defer closeStore()
	}
	sessions, err := st.ListAllSessions(ctx)
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}
	ro, err := rollout.New(st)
	if err != nil {
		return "", fmt.Errorf("open rollout: %w", err)
	}
	matches, err := searchSessions(ctx, ro, sessions, query, limit)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	var out strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&out, "%s  turn %d  %s  %s\n", m.Session.ID, m.Turn, m.Type, m.Time.Format(time.RFC3339))
		snippet := m.Snippet
		if len(snippet) > 120 {
			snippet = snippet[:120] + "…"
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		fmt.Fprintf(&out, "  %s\n", snippet)
	}
	return out.String(), nil
}

func (r *tuiAgentRunner) sessionSearchStore() (*store.Store, func() error, error) {
	if r != nil && r.state != nil && r.state.store != nil && !r.closedStore {
		return r.state.store, nil, nil
	}
	path, err := store.DefaultPath()
	if err != nil {
		return nil, nil, fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return st, st.Close, nil
}

func (r *tuiAgentRunner) Doctor(ctx context.Context) (string, error) {
	var liveStatus []mcptool.ServerStatus
	if r.tools != nil && r.tools.MCPConnections != nil {
		liveStatus = r.tools.MCPConnections.Status()
	}
	return renderDoctorTextWithLive(ctx, r.cfg, true, false, liveStatus)
}

func (r *tuiAgentRunner) NewSession(ctx context.Context) (tui.SessionState, error) {
	state, err := startChatRollout(r.cmd, "", r.providerName, r.model, chatOptions{})
	if err != nil {
		return tui.SessionState{}, err
	}
	state.session.Title = ""
	if err := state.store.UpdateSession(ctx, state.session); err != nil {
		_ = state.Close()
		return tui.SessionState{}, err
	}
	if r.state != nil {
		_ = r.state.Close()
	}
	r.state = state
	r.closedStore = false
	r.invalidateMessageCache()
	return r.sessionState(), nil
}

func (r *tuiAgentRunner) SwitchSession(ctx context.Context, id string) (tui.SessionState, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return tui.SessionState{}, fmt.Errorf("session id is empty")
	}
	state, err := startChatRollout(r.cmd, "", r.providerName, r.model, chatOptions{SessionID: id})
	if err != nil {
		return tui.SessionState{}, err
	}
	cwd, err := currentWorkspace()
	if err != nil {
		_ = state.Close()
		return tui.SessionState{}, err
	}
	if state.session.Workspace != cwd {
		_ = state.Close()
		return tui.SessionState{}, fmt.Errorf("session %q belongs to workspace %q", id, state.session.Workspace)
	}
	if r.state != nil {
		_ = r.state.Close()
	}
	r.state = state
	r.closedStore = false
	r.invalidateMessageCache()
	if err := r.restoreSessionProviderModel(ctx, state.session); err != nil {
		return tui.SessionState{}, err
	}
	return r.sessionState(), nil
}

func (r *tuiAgentRunner) restoreSessionProviderModel(ctx context.Context, sess store.Session) error {
	sessionProvider := strings.TrimSpace(sess.Provider)
	sessionModel := strings.TrimSpace(sess.Model)
	if sessionProvider == "" {
		sessionProvider = r.inferSessionProvider(ctx, sessionModel)
	}
	if sessionProvider == "" {
		if sessionModel != "" {
			r.model = sessionModel
			r.models = appendModelCandidate(r.models, sessionModel)
			r.refreshReasoning()
		}
		return nil
	}
	if _, err := r.setProviderModel(ctx, sessionProvider, sessionModel); err != nil {
		return err
	}
	return nil
}

func (r *tuiAgentRunner) inferSessionProvider(ctx context.Context, model string) string {
	model = strings.TrimSpace(model)
	if r == nil || r.cfg == nil || model == "" {
		return ""
	}
	for _, providerName := range sortedProviderNames(r.cfg.Providers) {
		providerCfg := r.cfg.Providers[providerName]
		if modelInList(configuredProviderModels(providerCfg, ""), model) {
			return providerName
		}
	}
	for _, providerName := range sortedProviderNames(r.cfg.Providers) {
		providerCfg := r.cfg.Providers[providerName]
		check := r.checkProvider(ctx, providerName, providerCfg)
		if modelInList(check.Models, model) {
			return providerName
		}
	}
	return ""
}

func (r *tuiAgentRunner) CurrentSessionID() string {
	if r == nil || r.state == nil {
		return ""
	}
	return r.state.sessionID
}

// CreateGoal implements tui.GoalRunner. It writes a new active goal for the
// current session. If a goal already exists and is non-terminal, it returns
// an error.
func (r *tuiAgentRunner) CreateGoal(objective string, tokenBudget, turnBudget int) error {
	sessionID := r.CurrentSessionID()
	if sessionID == "" {
		return fmt.Errorf("no active session")
	}
	existing, err := goaltool.Load(sessionID)
	if err != nil {
		return err
	}
	if existing != nil && !goaltool.IsTerminal(existing.Status) {
		return fmt.Errorf("an active goal already exists (status=%s)", existing.Status)
	}
	return goaltool.Save(sessionID, &goaltool.Goal{
		SessionID:   sessionID,
		Objective:   objective,
		Status:      goaltool.StatusActive,
		TokenBudget: tokenBudget,
		TurnBudget:  turnBudget,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})
}

// GoalStatus implements tui.GoalRunner. It returns a one-line summary of the
// current goal, or empty if no goal exists.
func (r *tuiAgentRunner) GoalStatus() string {
	sessionID := r.CurrentSessionID()
	if sessionID == "" {
		return ""
	}
	g, err := goaltool.Load(sessionID)
	if err != nil || g == nil {
		return ""
	}
	return fmt.Sprintf("status=%s objective=%s turns=%d tokens=%d", g.Status, truncateGoalObjective(g.Objective, 80), g.TurnsUsed, g.TokensUsed)
}

// ClearGoal implements tui.GoalRunner. It deletes the goal for the current
// session.
func (r *tuiAgentRunner) ClearGoal() error {
	sessionID := r.CurrentSessionID()
	if sessionID == "" {
		return fmt.Errorf("no active session")
	}
	return goaltool.Delete(sessionID)
}

func (r *tuiAgentRunner) Messages() []tui.InitialMessage {
	if r == nil || r.state == nil {
		return nil
	}
	sessionID := r.state.sessionID
	if sessionID != "" && r.cachedMessagesSession == sessionID && r.cachedMessages != nil {
		return r.cachedMessages
	}
	var messages []tui.InitialMessage
	if msgs, err := r.messagesForCurrentSession(); err == nil {
		messages = msgs
	} else {
		messages = messagesForTUI(r.state.history)
	}
	r.cachedMessages = messages
	r.cachedMessagesSession = sessionID
	return messages
}

func (r *tuiAgentRunner) invalidateMessageCache() {
	if r == nil {
		return
	}
	r.cachedMessages = nil
	r.cachedMessagesSession = ""
}

func (r *tuiAgentRunner) Turn() int {
	if r == nil || r.state == nil || r.state.nextTurn <= 1 {
		return 0
	}
	return r.state.nextTurn - 1
}

func (r *tuiAgentRunner) sessionState() tui.SessionState {
	if r == nil || r.state == nil {
		return tui.SessionState{}
	}
	return tui.SessionState{
		ID:        r.state.sessionID,
		Provider:  r.providerName,
		Providers: r.Providers(),
		Model:     r.model,
		Models:    r.Models(),
		Effort:    r.Effort(),
		Efforts:   r.Efforts(),
		Turn:      r.Turn(),
		Messages:  r.Messages(),
	}
}
