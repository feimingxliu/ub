package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m Model) startPrompt(text string, clearInput bool) (tea.Model, tea.Cmd) {
	text = strings.TrimSpace(text)
	if text == "" {
		return m, nil
	}
	m.scrollToBottom()
	m.messages.append(userRole, text)
	m.recordPromptHistory(text)
	if clearInput {
		m.input.SetValue("")
		m.files = nil
	}
	return m.startRunnerPrompt(text)
}

func (m Model) startInternalPrompt(prompt, notice string) (tea.Model, tea.Cmd) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return m, nil
	}
	m.scrollToBottom()
	if strings.TrimSpace(notice) != "" {
		m.messages.append(systemRole, strings.TrimSpace(notice))
	}
	return m.startRunnerPrompt(prompt)
}

func (m Model) startRunnerPrompt(prompt string) (tea.Model, tea.Cmd) {
	if m.runner == nil {
		return m, nil
	}
	m.running = true
	m.status.state = statusThinking
	m.status.turn++
	m.beginRunIndicator()
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	events := make(chan Event, 64)
	m.events = events
	m.runID++
	runID := m.runID
	m.messages.startActivityGroup(thinkingActivityGroupKey(runID), "Thinking...")
	return m, tea.Batch(runPrompt(ctx, m.runner, prompt, events), waitForEventWithTimeout(events, runID, m.timeout), spinnerTickCmd())
}

func (m Model) startShell(input string, clearInput bool) (tea.Model, tea.Cmd) {
	input = strings.TrimSpace(input)
	command := strings.TrimSpace(strings.TrimPrefix(input, "!"))
	if clearInput {
		m.input.SetValue("")
		m.files = nil
	}
	if command == "" {
		m.messages.append(errorRole, "shell command is empty")
		return m, nil
	}
	m.scrollToBottom()
	m.messages.append(userRole, "!"+command)
	m.recordPromptHistory("!" + command)
	runner, ok := m.runner.(ShellRunner)
	if !ok {
		m.messages.append(errorRole, "shell execution is unavailable in this runner")
		return m, nil
	}
	m.running = true
	m.status.state = statusShell
	m.beginRunIndicator()
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	events := make(chan Event, 64)
	m.events = events
	m.runID++
	runID := m.runID
	return m, tea.Batch(runShell(ctx, runner, command, events), waitForEventWithTimeout(events, runID, m.timeout), spinnerTickCmd())
}

func (m Model) retryLastTurn() (tea.Model, tea.Cmd) {
	text, ok := m.lastUserTurn()
	if !ok {
		m.messages.append(systemRole, "no user turn to retry")
		return m, nil
	}
	if isShellInput(text) {
		return m.startShell(text, false)
	}
	return m.startPrompt(text, false)
}
