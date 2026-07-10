package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/tool/plan"
)

type doctorResultMsg struct {
	report string
	err    error
}

type planEditFinishedMsg struct {
	path string
	err  error
}

type copyResultMsg struct {
	label string
	err   error
}

func (m Model) runDoctor() (tea.Model, tea.Cmd) {
	runner, ok := m.runner.(DoctorRunner)
	if !ok {
		m.messages.append(systemRole, "doctor is unavailable in this runner")
		return m, nil
	}
	m.messages.append(systemRole, "running doctor…")
	m.scrollToBottom()
	ctx := m.ctx
	return m, func() tea.Msg {
		report, err := runner.Doctor(ctx)
		return doctorResultMsg{report: report, err: err}
	}
}

func (m Model) handleDoctorResult(msg doctorResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.messages.append(systemRole, msg.err.Error())
		m.scrollToBottom()
		return m, nil
	}
	report := strings.TrimSpace(msg.report)
	if report == "" {
		report = "doctor completed with no output"
	}
	m.messages.append(systemRole, report)
	m.scrollToBottom()
	return m, nil
}

func (m Model) editPlanArtifact(args []string) (tea.Model, tea.Cmd) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		m.messages.append(errorRole, "usage: /plan-edit <plan-id>")
		return m, nil
	}
	workspace, err := m.workspace()
	if err != nil {
		m.messages.append(errorRole, "plan edit failed: "+err.Error())
		return m, nil
	}
	planID := strings.TrimSpace(args[0])
	path, err := plan.Path(workspace, planID)
	if err != nil {
		m.messages.append(errorRole, "plan edit failed: "+err.Error())
		return m, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			m.messages.append(errorRole, "plan not found: "+planID)
			return m, nil
		}
		m.messages.append(errorRole, "plan edit failed: "+err.Error())
		return m, nil
	}
	if info.IsDir() {
		m.messages.append(errorRole, "plan edit failed: path is a directory")
		return m, nil
	}
	name, editorArgs := planEditorCommand()
	editorArgs = append(append([]string(nil), editorArgs...), path)
	m.messages.append(systemRole, "editing plan "+path)
	m.scrollToBottom()
	return m, tea.ExecProcess(exec.Command(name, editorArgs...), func(err error) tea.Msg {
		return planEditFinishedMsg{path: path, err: err}
	})
}

func (m Model) openPlanPicker() (tea.Model, tea.Cmd) {
	workspace, err := m.workspace()
	if err != nil {
		m.messages.append(errorRole, "plans failed: "+err.Error())
		return m, nil
	}
	plans, err := plan.List(workspace)
	if err != nil {
		m.messages.append(errorRole, "plans failed: "+err.Error())
		return m, nil
	}
	if len(plans) == 0 {
		m.messages.append(systemRole, "no plans in this workspace")
		return m, nil
	}
	m.plans = newPlanPicker(plans)
	return m, nil
}

func (m Model) workspace() (string, error) {
	workspace := strings.TrimSpace(m.status.cwd)
	if workspace != "" {
		return workspace, nil
	}
	return os.Getwd()
}

func planEditorCommand() (string, []string) {
	return planEditorCommandFromEnv(os.LookupEnv)
}

func planEditorCommandFromEnv(lookup func(string) (string, bool)) (string, []string) {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if value, ok := lookup(key); ok {
			if name, args := splitEditorCommand(value); name != "" {
				return name, args
			}
		}
	}
	return "vi", nil
}

func splitEditorCommand(value string) (string, []string) {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], append([]string(nil), parts[1:]...)
}

func (m Model) handlePlanEditFinished(msg planEditFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.messages.append(errorRole, "plan edit failed: "+msg.err.Error())
		m.scrollToBottom()
		return m, nil
	}
	m.messages.append(systemRole, "plan edited "+msg.path)
	m.scrollToBottom()
	return m, nil
}

func (m Model) copyMessage(args []string) (tea.Model, tea.Cmd) {
	// /copy with no args: copy the last assistant response (Codex-style).
	// /copy <N>: copy the Nth user/assistant message (1-based, [N] shown in transcript).
	var text string
	var label string
	if len(args) > 1 {
		m.messages.append(errorRole, "usage: /copy [N]  (no arg = last response, N shown as [N] in transcript)")
		return m, nil
	}
	if len(args) == 0 {
		var ok bool
		text, ok = m.messages.lastAssistantText()
		if !ok {
			m.messages.append(errorRole, "no assistant response to copy")
			return m, nil
		}
		label = "last response"
	} else {
		n, err := strconv.Atoi(args[0])
		if err != nil || n <= 0 {
			m.messages.append(errorRole, "usage: /copy [N]  (no arg = last response, N shown as [N] in transcript)")
			return m, nil
		}
		var ok bool
		text, ok = m.messages.copyText(n)
		if !ok {
			m.messages.append(errorRole, fmt.Sprintf("message %d not found", n))
			return m, nil
		}
		label = fmt.Sprintf("message %d", n)
	}
	clipboard := m.clipboard
	ctx := m.ctx
	return m, func() tea.Msg {
		if err := clipboard.WriteText(ctx, text); err != nil {
			return copyResultMsg{label: label, err: err}
		}
		return copyResultMsg{label: label}
	}
}

func (m Model) handleCopyResult(msg copyResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.messages.append(errorRole, "copy failed: "+msg.err.Error())
		m.scrollToBottom()
		return m, nil
	}
	return m, m.showToast(toastSuccess, fmt.Sprintf("copied %s", msg.label))
}

func (m Model) cycleMode() (tea.Model, tea.Cmd) {
	next := nextExecutionMode(m.status.executionMode)
	if runner, ok := m.runner.(ControlRunner); ok {
		if err := runner.SetMode(next); err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
	}
	m.status.executionMode = next
	if m.pending != nil {
		mode := execmode.Mode(next)
		m.pending.Request.Mode = mode
		m.modal.Request.Mode = mode
	}
	return m, nil
}

func (m *Model) confirmEscInterrupt(key tea.KeyPressMsg) bool {
	if key.Key().IsRepeat {
		return false
	}
	now := time.Now()
	if !m.lastEscTime.IsZero() && m.lastEscRunID == m.runID && now.Sub(m.lastEscTime) <= escInterruptConfirmWindow {
		m.clearEscInterruptConfirm()
		return true
	}
	m.lastEscTime = now
	m.lastEscRunID = m.runID
	return false
}

func (m *Model) clearEscInterruptConfirmForInteraction(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() != "esc" {
			m.clearEscInterruptConfirm()
		}
	case tea.MouseClickMsg:
		m.clearEscInterruptConfirm()
	}
}

func (m *Model) clearEscInterruptConfirm() {
	m.lastEscTime = time.Time{}
	m.lastEscRunID = 0
}

func (m *Model) interruptCurrent() {
	m.clearEscInterruptConfirm()
	if m.pending != nil && m.pending.Response != nil {
		select {
		case m.pending.Response <- permission.DecisionDeny:
		default:
		}
	}
	m.pending = nil
	if m.pendingAsk != nil && m.pendingAsk.Response != nil {
		select {
		case m.pendingAsk.Response <- m.askPrompt.SkipResponse():
		default:
		}
	}
	m.pendingAsk = nil
	if m.pendingPlanMode != nil && m.pendingPlanMode.Response != nil {
		select {
		case m.pendingPlanMode.Response <- m.planModePrompt.Response(false, m.status.executionMode, m.status.executionMode, nil):
		default:
		}
	}
	m.pendingPlanMode = nil
	if m.cancel != nil {
		m.cancel()
	}
	m.cancel = nil
	m.running = false
	m.status.state = statusIdle
	m.events = nil
	m.runID++
}

func nextExecutionMode(current string) string {
	order := []string{
		string(execmode.ModeWork),
		string(execmode.ModePlan),
		string(execmode.ModeAuto),
		string(execmode.ModeFullAccess),
	}
	for i, mode := range order {
		if current == mode {
			return order[(i+1)%len(order)]
		}
	}
	return string(execmode.ModeWork)
}

func (m Model) startCompact() (tea.Model, tea.Cmd) {
	runner, ok := m.runner.(CompactRunner)
	if !ok {
		m.messages.append(systemRole, "compact is unavailable in this runner")
		return m, nil
	}
	m.running = true
	m.status.state = statusThinking
	m.beginRunIndicator()
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	events := make(chan Event, 64)
	m.events = events
	m.runID++
	runID := m.runID
	m.messages.startActivityGroup(thinkingActivityGroupKey(runID), "Compacting...")
	return m, tea.Batch(runCompact(ctx, runner, events), waitForEventWithTimeout(events, runID, m.timeout), spinnerTickCmd())
}

func (m *Model) updateContextUsage(event Event) {
	if event.ContextUsedTokens <= 0 {
		return
	}
	if m.status.contextUsedTokens > 0 && event.ContextUsedTokens < m.status.contextUsedTokens && !event.ContextReset {
		return
	}
	m.status.contextUsedTokens = event.ContextUsedTokens
	m.status.contextMaxTokens = event.ContextMaxTokens
	m.status.contextRatio = event.ContextRatio
	m.status.contextKind = defaultString(event.ContextKind, "est")
}
