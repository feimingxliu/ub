package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m Model) openSessionPicker() (tea.Model, tea.Cmd) {
	runner, ok := m.runner.(SessionRunner)
	if !ok {
		m.messages.append(systemRole, "sessions are unavailable in this runner")
		return m, nil
	}
	sessions, err := runner.ListSessions(m.ctx)
	if err != nil {
		m.messages.append(systemRole, err.Error())
		return m, nil
	}
	if len(sessions) == 0 {
		m.messages.append(systemRole, "no sessions in this workspace")
		return m, nil
	}
	m.sessions = newSessionPicker(sessions)
	return m, nil
}

func (m Model) switchSession(id string) (tea.Model, tea.Cmd) {
	id = strings.TrimSpace(id)
	if id == "" {
		m.messages.append(systemRole, "session id is empty")
		return m, nil
	}
	runner, ok := m.runner.(SessionRunner)
	if !ok {
		m.messages.append(systemRole, "sessions are unavailable in this runner")
		return m, nil
	}
	state, err := runner.SwitchSession(m.ctx, id)
	if err != nil {
		m.messages.append(systemRole, err.Error())
		return m, nil
	}
	m.applySessionState(state)
	m.messages.append(systemRole, "session set to "+state.ID)
	return m, nil
}

func (m Model) newSession() (tea.Model, tea.Cmd) {
	runner, ok := m.runner.(SessionRunner)
	if !ok {
		m.messages.append(systemRole, "new session is unavailable in this runner")
		return m, nil
	}
	state, err := runner.NewSession(m.ctx)
	if err != nil {
		m.messages.append(systemRole, err.Error())
		return m, nil
	}
	m.applySessionState(state)
	if strings.TrimSpace(state.ID) != "" {
		m.messages.append(systemRole, "new session "+state.ID)
	}
	return m, nil
}

func (m Model) openRewindPicker(args []string) (tea.Model, tea.Cmd) {
	runner, ok := m.runner.(RewindRunner)
	if !ok {
		m.messages.append(systemRole, "rewind is unavailable in this runner")
		return m, nil
	}
	targets, err := runner.ListRewindTargets(m.ctx)
	if err != nil {
		m.messages.append(systemRole, "rewind failed: "+err.Error())
		return m, nil
	}
	if len(targets) == 0 {
		m.messages.append(systemRole, "no user turns to rewind")
		return m, nil
	}
	if len(args) > 1 {
		m.messages.append(systemRole, "usage: /rewind [turn]")
		return m, nil
	}
	if len(args) == 1 {
		turn, err := strconv.Atoi(args[0])
		if err != nil || turn <= 0 {
			m.messages.append(systemRole, "usage: /rewind [turn]")
			return m, nil
		}
		for _, target := range targets {
			if target.Turn != turn {
				continue
			}
			if len(target.AffectedFiles) > 0 {
				m.rewind = newRewindPicker(targets)
				m.rewind.chooseTarget(target)
				return m, nil
			}
			return m.applyRewind(target, false)
		}
		m.messages.append(systemRole, fmt.Sprintf("rewind target turn %d not found", turn))
		return m, nil
	}
	m.rewind = newRewindPicker(targets)
	return m, nil
}

func (m Model) applyRewind(target RewindTarget, revertFiles bool) (tea.Model, tea.Cmd) {
	runner, ok := m.runner.(RewindRunner)
	if !ok {
		m.messages.append(systemRole, "rewind is unavailable in this runner")
		return m, nil
	}
	state, result, err := runner.Rewind(m.ctx, RewindRequest{
		Turn:        target.Turn,
		RevertFiles: revertFiles,
	})
	if err != nil {
		m.messages.append(systemRole, "rewind failed: "+err.Error())
		return m, nil
	}
	m.applySessionState(state)
	prompt := strings.TrimSpace(result.Target.Text)
	if prompt == "" {
		prompt = strings.TrimSpace(target.Text)
	}
	m.input.SetValue(prompt)
	m.input.CursorEnd()
	m.messages.append(systemRole, rewindNotice(result, revertFiles))
	m.scrollToBottom()
	return m, nil
}

func rewindNotice(result RewindResult, requestedFiles bool) string {
	turn := result.Target.Turn
	if turn <= 0 {
		turn = 0
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("rewound to before turn %d; prompt restored in input", turn))
	if requestedFiles {
		if len(result.RevertedFiles) > 0 {
			parts = append(parts, "reverted files: "+strings.Join(result.RevertedFiles, ", "))
		}
		if len(result.SkippedFiles) > 0 {
			parts = append(parts, "could not safely revert files: "+strings.Join(result.SkippedFiles, ", "))
		}
		if len(result.RevertedFiles) == 0 && len(result.SkippedFiles) == 0 {
			parts = append(parts, "no file changes needed reverting")
		}
	} else if len(result.Target.AffectedFiles) > 0 {
		parts = append(parts, "workspace files were left unchanged")
	}
	return strings.Join(parts, "\n")
}

func (m Model) searchSessions(queryParts []string) (tea.Model, tea.Cmd) {
	query := strings.Join(queryParts, " ")
	if strings.TrimSpace(query) == "" {
		m.messages.append(systemRole, "usage: /sessions search <query>")
		return m, nil
	}
	runner, ok := m.runner.(SessionSearchRunner)
	if !ok {
		m.messages.append(systemRole, "session search is unavailable in this runner")
		return m, nil
	}
	m.messages.append(systemRole, "searching sessions…")
	result, err := runner.SearchSessions(m.ctx, query, 50)
	if err != nil {
		m.messages.append(systemRole, "search error: "+err.Error())
		return m, nil
	}
	if strings.TrimSpace(result) == "" {
		m.messages.append(systemRole, "no matches for "+query)
		return m, nil
	}
	m.messages.append(systemRole, result)
	return m, nil
}

func (m *Model) applySessionState(state SessionState) {
	m.messages.load(state.Messages)
	m.history = promptHistoryFromMessages(state.Messages)
	m.queuedPrompts = nil
	m.resetQueuedPromptNavigation()
	m.resetPromptHistoryNavigation()
	m.scrollToBottom()
	if strings.TrimSpace(state.Provider) != "" {
		m.status.provider = state.Provider
		m.providers = normalizeOptions(state.Providers, state.Provider)
	}
	if strings.TrimSpace(state.Model) != "" {
		m.status.model = state.Model
		if len(state.Models) > 0 {
			m.models = normalizeModels(state.Models, state.Model)
		} else {
			m.models = normalizeModels(m.models, state.Model)
		}
	}
	if state.Effort != "" || len(state.Efforts) > 0 {
		m.status.effort = defaultString(state.Effort, "none")
		m.efforts = normalizeOptions(state.Efforts, m.status.effort)
	} else if strings.TrimSpace(state.Model) != "" {
		m.refreshEffortFromRunner()
	}
	m.refreshApprovalModelFromRunner()
	m.refreshSmallModelFromRunner()
	m.status.turn = state.Turn
	m.status.contextUsedTokens = 0
	m.status.contextMaxTokens = 0
	m.status.contextRatio = 0
	m.status.contextKind = ""
}
