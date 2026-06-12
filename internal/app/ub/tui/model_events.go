package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	permissiondialog "github.com/feimingxliu/ub/internal/app/ub/tui/dialog/permission"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/runtime/permission"
)

type modelRefreshResultMsg struct {
	models           []string
	modelErr         error
	modelsOK         bool
	approvalModels   []string
	approvalErr      error
	approvalModelsOK bool
	smallModels      []string
	smallErr         error
	smallModelsOK    bool
}

type messagesLoadedMsg struct {
	messages []InitialMessage
	err      error
}

func refreshModelLists(ctx context.Context, runner Runner) tea.Cmd {
	modelRunner, hasModels := runner.(ModelRefreshRunner)
	approvalRunner, hasApprovalModels := runner.(ApprovalModelRefreshRunner)
	smallRunner, hasSmallModels := runner.(SmallModelRefreshRunner)
	if !hasModels && !hasApprovalModels && !hasSmallModels {
		return nil
	}
	return func() tea.Msg {
		msg := modelRefreshResultMsg{}
		if hasModels {
			msg.models, msg.modelErr = modelRunner.RefreshModels(ctx)
			msg.modelsOK = true
		}
		if hasApprovalModels {
			msg.approvalModels, msg.approvalErr = approvalRunner.RefreshApprovalModels(ctx)
			msg.approvalModelsOK = true
		}
		if hasSmallModels {
			msg.smallModels, msg.smallErr = smallRunner.RefreshSmallModels(ctx)
			msg.smallModelsOK = true
		}
		return msg
	}
}

func loadMessagesCmd(ctx context.Context, load func(context.Context) ([]InitialMessage, error)) tea.Cmd {
	return func() tea.Msg {
		if load == nil {
			return messagesLoadedMsg{}
		}
		messages, err := load(ctx)
		return messagesLoadedMsg{messages: messages, err: err}
	}
}

func (m Model) handleStreamEvent(msg streamEventMsg) (tea.Model, tea.Cmd) {
	if msg.runID != m.runID {
		return m, nil
	}
	if !msg.ok {
		m.running = false
		m.status.state = statusIdle
		m.cancel = nil
		m.pending = nil
		m.pendingAsk = nil
		m.events = nil
		m.clearEscInterruptConfirm()
		m.flushBackgroundQueue()
		return m.startNextQueuedPrompt()
	}
	cmd := waitForEventFromUpdate(msg.event, &m)
	return m, cmd
}

func (m Model) handleBackgroundEvent(msg backgroundEventMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	next := waitForBackgroundEvent(m.backgroundEvents)
	if notice, ok := backgroundTranscriptMessageFromEvent(msg.event); ok {
		if m.running {
			m.backgroundQueue = append(m.backgroundQueue, notice)
			return m, next
		}
		m.appendBackgroundTranscriptMessage(notice)
	}
	return m, next
}

func backgroundTranscriptMessageFromEvent(event Event) (backgroundTranscriptMessage, bool) {
	switch event.Type {
	case EventActivity:
		text := strings.TrimSpace(event.Summary)
		if text == "" {
			text = strings.TrimSpace(event.Content)
		}
		if text == "" {
			return backgroundTranscriptMessage{}, false
		}
		role := systemRole
		if event.IsError {
			role = errorRole
		}
		return backgroundTranscriptMessage{role: role, text: text}, true
	case EventError:
		text := strings.TrimSpace(event.Content)
		if text == "" && event.Err != nil {
			text = event.Err.Error()
		}
		return backgroundTranscriptMessage{role: errorRole, text: defaultString(text, "background task failed")}, true
	default:
		return backgroundTranscriptMessage{}, false
	}
}

func (m *Model) appendBackgroundTranscriptMessage(msg backgroundTranscriptMessage) {
	m.messages.append(msg.role, msg.text)
	m.scrollToBottom()
}

func (m *Model) flushBackgroundQueue() {
	if len(m.backgroundQueue) == 0 {
		return
	}
	queued := append([]backgroundTranscriptMessage(nil), m.backgroundQueue...)
	m.backgroundQueue = nil
	for _, msg := range queued {
		m.appendBackgroundTranscriptMessage(msg)
	}
}

func (m Model) handlePermissionRequest(msg permissionRequestMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	m.pending = &msg.request
	m.modal = permissiondialog.New(msg.request.Request)
	return m, nil
}

func (m Model) handleAskRequest(msg askRequestMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	m.pendingAsk = &msg.request
	m.askPrompt = newAskPromptModel(msg.request.Request)
	return m, nil
}

func (m Model) handlePlanModeRequest(msg planModeRequestMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	m.pendingPlanMode = &msg.request
	m.planModePrompt = newPlanModePromptModel(msg.request.Request)
	return m, nil
}

func (m Model) handleLimitRequest(msg limitRequestMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	m.pendingLimit = &msg.request
	return m, nil
}

func (m Model) resolveAsk(skipped bool) (tea.Model, tea.Cmd) {
	var resp agent.AskResponse
	if skipped {
		resp = m.askPrompt.SkipResponse()
	} else {
		resp = m.askPrompt.SubmitResponse()
	}
	if m.pendingAsk != nil && m.pendingAsk.Response != nil {
		m.pendingAsk.Response <- resp
	}
	summary := strings.TrimSpace(m.askPrompt.Summary(resp))
	if summary != "" {
		m.messages.append(systemRole, summary)
		m.scrollToBottom()
	}
	m.pendingAsk = nil
	return m, waitForAsk(m.askReqs)
}

func (m Model) resolvePlanMode(approved bool) (tea.Model, tea.Cmd) {
	from := m.status.executionMode
	to := from
	var err error
	if approved {
		from, to, err = m.applyPlanModeTransition()
	}
	resp := m.planModePrompt.Response(approved, from, to, err)
	if resp.Approved && resp.ToMode != "" {
		m.status.executionMode = string(resp.ToMode)
	}
	if m.pendingPlanMode != nil && m.pendingPlanMode.Response != nil {
		m.pendingPlanMode.Response <- resp
	}
	if summary := strings.TrimSpace(m.planModePrompt.Summary(resp)); summary != "" {
		m.messages.append(systemRole, summary)
		m.scrollToBottom()
	}
	m.pendingPlanMode = nil
	return m, waitForPlanMode(m.planModeReqs)
}

func (m Model) applyPlanModeTransition() (from, to string, err error) {
	if runner, ok := m.runner.(PlanModeControlRunner); ok {
		switch m.planModePrompt.request.Action {
		case agent.PlanModeExit:
			return runner.ExitPlanMode()
		default:
			return runner.EnterPlanMode()
		}
	}
	target := string(execution.ModePlan)
	if m.planModePrompt.request.Action == agent.PlanModeExit {
		target = string(execution.ModeWork)
	}
	from = m.status.executionMode
	to = target
	if runner, ok := m.runner.(ControlRunner); ok {
		err = runner.SetMode(target)
	}
	return from, to, err
}

func (m Model) resolveLimit(extra int) (tea.Model, tea.Cmd) {
	if m.pendingLimit != nil && m.pendingLimit.Response != nil {
		m.pendingLimit.Response <- agent.LimitExtensionResponse{ExtraTurns: extra}
	}
	m.pendingLimit = nil
	return m, waitForLimit(m.limitReqs)
}

func (m Model) handleModelRefreshResult(msg modelRefreshResultMsg) (tea.Model, tea.Cmd) {
	if msg.modelsOK && msg.modelErr == nil {
		selected := ""
		if m.picker != nil && m.pickerTarget == "model" {
			selected = m.picker.selected()
		}
		m.models = normalizeModels(msg.models, m.status.model)
		if m.picker != nil && m.pickerTarget == "model" {
			current := m.status.model
			if modelAllowed(m.models, selected) {
				current = selected
			}
			m.picker = newModelPicker(m.models, current)
		}
	}
	if msg.approvalModelsOK && msg.approvalErr == nil {
		selected := ""
		if m.picker != nil && m.pickerTarget == "approval" {
			selected = m.picker.selected()
		}
		m.approvalModels = normalizeModels(msg.approvalModels, m.approvalModel)
		if m.picker != nil && m.pickerTarget == "approval" {
			current := m.approvalModel
			if modelAllowed(m.approvalModels, selected) {
				current = selected
			}
			m.picker = newModelPicker(m.approvalModels, current)
		}
	}
	if msg.smallModelsOK && msg.smallErr == nil {
		selected := ""
		if m.picker != nil && m.pickerTarget == "small" {
			selected = m.picker.selected()
		}
		m.smallModels = normalizeModels(msg.smallModels, m.smallModel)
		if m.picker != nil && m.pickerTarget == "small" {
			current := m.smallModel
			if modelAllowed(m.smallModels, selected) {
				current = selected
			}
			m.picker = newModelPicker(m.smallModels, current)
		}
	}
	return m, nil
}

func (m Model) handleMessagesLoaded(msg messagesLoadedMsg) (tea.Model, tea.Cmd) {
	m.loadingMessages = false
	m.loadMessages = nil
	if msg.err != nil {
		m.messages.clear()
		m.messages.append(errorRole, "load session history failed: "+msg.err.Error())
		m.scrollToBottom()
		return m, nil
	}
	m.messages.load(msg.messages)
	m.history = promptHistoryFromMessages(msg.messages)
	m.scrollToBottom()
	return m, nil
}

func (m Model) handleSpinnerTick(_ spinnerTickMsg) (tea.Model, tea.Cmd) {
	if !m.running {
		return m, nil
	}
	m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
	return m, spinnerTickCmd()
}

func waitForEventFromUpdate(event Event, m *Model) tea.Cmd {
	m.updateContextUsage(event)
	toastCmd := m.showToastForEvent(event)
	next := waitForEventFromUpdateInner(event, m)
	if toastCmd == nil {
		return next
	}
	// Stream cmd goes first so callers stepping through the batch sequentially
	// (notably drainBatch in tests) can take the head without blocking on the
	// toast tick.
	return tea.Batch(next, toastCmd)
}

func waitForEventFromUpdateInner(event Event, m *Model) tea.Cmd {
	switch event.Type {
	case EventContext:
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	case EventDeltaText:
		m.messages.removeKey(activityRole, thinkingActivityKey(m.runID))
		m.messages.removePlaceholderActivityGroup(thinkingActivityGroupKey(m.runID))
		m.messages.appendAssistantDelta(event.Text)
		m.status.state = statusStreaming
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	case EventActivity:
		m.messages.removeKey(activityRole, thinkingActivityKey(m.runID))
		m.messages.removePlaceholderActivityGroup(thinkingActivityGroupKey(m.runID))
		// Insert tool/thinking events inline so they appear in
		// chronological order relative to assistant text segments,
		// matching the Codex-style interleaved transcript.
		m.messages.appendOrUpdateLiveActivity(event, m.status.turn)
		m.status.state = statusForActivity(event)
		if summary := strings.TrimSpace(event.Summary); summary != "" && strings.TrimSpace(event.ActivityKind) != "thinking" {
			m.activitySummary = summary
		}
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	case EventToolPartialOutput:
		m.messages.removeKey(activityRole, thinkingActivityKey(m.runID))
		m.messages.removePlaceholderActivityGroup(thinkingActivityGroupKey(m.runID))
		m.messages.appendOrUpdateLiveActivity(toolPartialActivity(event), m.status.turn)
		m.status.state = statusTool
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	case EventShellOutput:
		m.messages.removePlaceholderActivityGroup(thinkingActivityGroupKey(m.runID))
		role := systemRole
		if event.IsError {
			role = errorRole
		}
		m.messages.append(role, event.Content)
		m.status.state = statusShell
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	case EventToolCallStart:
		m.messages.removeKey(activityRole, thinkingActivityKey(m.runID))
		m.messages.removePlaceholderActivityGroup(thinkingActivityGroupKey(m.runID))
		m.messages.appendToolStatus(event.ToolName, "started")
		m.status.state = statusTool
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	case EventToolCallEnd:
		m.messages.removeKey(activityRole, thinkingActivityKey(m.runID))
		m.messages.removePlaceholderActivityGroup(thinkingActivityGroupKey(m.runID))
		m.messages.appendToolStatus(event.ToolName, "finished")
		m.status.state = statusThinking
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	case EventPermission:
		m.messages.removeKey(activityRole, thinkingActivityKey(m.runID))
		m.messages.removePlaceholderActivityGroup(thinkingActivityGroupKey(m.runID))
		m.messages.appendPermissionEvent(event)
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	case EventDone:
		m.messages.removeKey(activityRole, thinkingActivityKey(m.runID))
		m.messages.removePlaceholderActivityGroup(thinkingActivityGroupKey(m.runID))
		m.status.state = statusFinalizing
		m.cancel = nil
		m.flushBackgroundQueue()
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	case EventError:
		m.messages.removeKey(activityRole, thinkingActivityKey(m.runID))
		if m.cancel != nil {
			m.cancel()
		}
		m.messages.append(errorRole, defaultString(event.Content, "agent failed"))
		m.running = false
		m.status.state = statusIdle
		m.cancel = nil
		m.clearEscInterruptConfirm()
		return nil
	default:
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	}
}

// View implements tea.Model.
func (m Model) View() tea.View {
	frame := m.renderFrame()
	return tea.View{
		Content:   frame.content,
		Cursor:    frame.cursor,
		AltScreen: true,
		MouseMode: tea.MouseModeCellMotion,
	}
}

func (m Model) resolvePermission(decision permission.Decision) (tea.Model, tea.Cmd) {
	if m.pending != nil && m.pending.Response != nil {
		m.pending.Response <- decision
	}
	var toastCmd tea.Cmd
	if m.pending != nil && permissionDecisionAllows(decision) {
		toastCmd = m.showToast(toastSuccess, fmt.Sprintf("approval allowed %s", defaultString(m.pending.Request.Tool, "tool")))
	}
	m.pending = nil
	return m, tea.Batch(toastCmd, waitForPermission(m.permReqs))
}
