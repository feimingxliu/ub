package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/feimingxliu/ub/internal/app/ub/tui/slash"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
)

func (m Model) lastUserTurn() (string, bool) {
	for i := len(m.messages.items) - 1; i >= 0; i-- {
		item := m.messages.items[i]
		if item.role != userRole {
			continue
		}
		text := strings.TrimSpace(item.text)
		if text == "" {
			continue
		}
		return text, true
	}
	return "", false
}

func isShellInput(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "!")
}

func (m Model) executeSlash(input string) (tea.Model, tea.Cmd) {
	cmd, err := slash.Parse(input)
	if err != nil {
		m.messages.append(systemRole, err.Error())
		return m, nil
	}
	switch cmd.Name {
	case "clear":
		m.messages.clear()
		m.scrollToBottom()
		return m, nil
	case "new":
		return m.newSession()
	case "help":
		m.messages.append(systemRole, slashHelp())
		return m, nil
	case "compact":
		return m.startCompact()
	case "goal":
		return m.startGoalCommand(cmd.Args)
	case "init":
		return m.startInitCommand(cmd.Args)
	case "plan-edit":
		return m.editPlanArtifact(cmd.Args)
	case "plans":
		if len(cmd.Args) > 0 {
			return m.editPlanArtifact(cmd.Args)
		}
		return m.openPlanPicker()
	case "doctor":
		return m.runDoctor()
	case "retry":
		return m.retryLastTurn()
	case "rewind":
		return m.openRewindPicker(cmd.Args)
	case "btw":
		return m.startSideQuestion(cmd.Args)
	case "copy":
		return m.copyMessage(cmd.Args)
	case "quit", "exit":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "config":
		approvalModel := defaultString(m.approvalModel, "none")
		smallModel := defaultString(m.smallModel, "none")
		m.messages.append(systemRole, fmt.Sprintf("provider=%s model=%s effort=%s approval_model=%s small_model=%s mode=%s cwd=%s", m.status.provider, m.status.model, m.status.effort, approvalModel, smallModel, m.status.executionMode, m.status.cwd))
		return m, nil
	case "sessions":
		if len(cmd.Args) >= 1 && cmd.Args[0] == "search" {
			queryParts := cmd.Args[1:]
			if len(queryParts) == 0 {
				m.messages.append(systemRole, "usage: /sessions search <query>")
				return m, nil
			}
			return m.searchSessions(queryParts)
		}
		if len(cmd.Args) > 0 {
			return m.switchSession(cmd.Args[0])
		}
		return m.openSessionPicker()
	case "resume":
		if len(cmd.Args) > 1 || (len(cmd.Args) == 1 && cmd.Args[0] == "search") {
			m.messages.append(systemRole, "usage: /resume [session-id]")
			return m, nil
		}
		if len(cmd.Args) == 1 {
			return m.switchSession(cmd.Args[0])
		}
		return m.openSessionPicker()
	case "profile":
		if len(cmd.Args) == 0 {
			m.messages.append(systemRole, "profile: use `/profile <name>` to show restart guidance")
		} else {
			m.messages.append(systemRole, fmt.Sprintf("profile %q requires restart via `ub --profile %s` or UB_PROFILE=%s", cmd.Args[0], cmd.Args[0], cmd.Args[0]))
		}
		return m, nil
	case "provider":
		if len(cmd.Args) == 0 {
			if len(m.providers) == 0 {
				m.messages.append(systemRole, "no providers available")
				return m, nil
			}
			m.picker = newProviderPicker(m.providers, m.status.provider)
			m.pickerTarget = "provider"
			return m, nil
		}
		providerName := cmd.Args[0]
		model := strings.Join(cmd.Args[1:], " ")
		if err := m.setProvider(providerName, model); err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
		m.messages.append(systemRole, "provider set to "+m.status.provider+" model "+m.status.model)
		return m, nil
	case "model":
		if len(cmd.Args) == 0 {
			m.picker = newModelPicker(m.models, m.status.model)
			m.pickerTarget = "model"
			return m, nil
		}
		model := strings.Join(cmd.Args, " ")
		if err := m.setModel(model); err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
		m.messages.append(systemRole, "model set to "+model)
		return m, nil
	case "effort":
		if len(cmd.Args) == 0 {
			m.picker = newEffortPicker(m.efforts, m.status.effort)
			m.pickerTarget = "effort"
			return m, nil
		}
		effort := strings.Join(cmd.Args, " ")
		if err := m.setEffort(effort); err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
		m.messages.append(systemRole, "effort set to "+m.status.effort)
		return m, nil
	case "approval-model":
		if len(cmd.Args) == 0 {
			if len(m.approvalModels) == 0 {
				m.messages.append(systemRole, "no approval models available")
				return m, nil
			}
			m.picker = newModelPicker(m.approvalModels, m.approvalModel)
			m.pickerTarget = "approval"
			return m, nil
		}
		model := strings.Join(cmd.Args, " ")
		if err := m.setApprovalModel(model); err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
		m.messages.append(systemRole, "approval model set to "+model)
		return m, nil
	case "small-model":
		if len(cmd.Args) == 0 {
			if len(m.smallModels) == 0 {
				m.messages.append(systemRole, "no small models available")
				return m, nil
			}
			m.picker = newModelPicker(m.smallModels, m.smallModel)
			m.pickerTarget = "small"
			return m, nil
		}
		model := strings.Join(cmd.Args, " ")
		if err := m.setSmallModel(model); err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
		m.messages.append(systemRole, "small model set to "+model)
		return m, nil
	case "mode":
		if len(cmd.Args) == 0 {
			return m, nil
		}
		mode, err := execution.ParseMode(cmd.Args[0])
		if err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
		next := string(mode)
		if runner, ok := m.runner.(ControlRunner); ok {
			if err := runner.SetMode(next); err != nil {
				m.messages.append(systemRole, err.Error())
				return m, nil
			}
		}
		m.status.executionMode = next
		return m, nil
	default:
		m.messages.append(systemRole, "unknown slash command "+cmd.Name)
		return m, nil
	}
}

func (m Model) startGoalCommand(args []string) (tea.Model, tea.Cmd) {
	runner, ok := m.runner.(GoalRunner)
	if !ok {
		m.messages.append(systemRole, "goal is unavailable in this runner")
		return m, nil
	}
	// /goal clear — delete the current goal.
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "clear") {
		if err := runner.ClearGoal(); err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
		m.status.goalStatus = ""
		m.messages.append(systemRole, "goal cleared")
		return m, nil
	}
	objective := strings.TrimSpace(strings.Join(args, " "))
	if objective == "" {
		// No args: show current goal status.
		status := runner.GoalStatus()
		if status == "" {
			m.messages.append(systemRole, "no goal is set for this session")
		} else {
			m.messages.append(systemRole, "goal: "+status)
		}
		return m, nil
	}
	// Ensure a session exists before creating the goal. On a fresh TUI launch
	// the runner's state is nil (it is lazily created on the first prompt), so
	// /goal would otherwise fail with "no active session".
	if sessionRunner, ok := runner.(SessionRunner); ok && sessionRunner.CurrentSessionID() == "" {
		state, err := sessionRunner.NewSession(m.ctx)
		if err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
		m.applySessionState(state)
	}
	// Create the goal, then start the agent with a goal-oriented prompt.
	if err := runner.CreateGoal(objective, 0, 0); err != nil {
		m.messages.append(systemRole, err.Error())
		return m, nil
	}
	m.status.goalStatus = "active"
	prompt := "I have created a goal for you. Use get_goal to check its status, then work toward completing it. When done, call update_goal(status=\"complete\").\n\nObjective: " + objective
	return m.startInternalPrompt(prompt, "goal mode started: "+truncateText(objective, 200))
}

func (m Model) startInitCommand(args []string) (tea.Model, tea.Cmd) {
	if m.runner == nil {
		m.messages.append(systemRole, "init is unavailable in this runner")
		return m, nil
	}
	prompt := initCommandPrompt
	if extra := strings.TrimSpace(strings.Join(args, " ")); extra != "" {
		prompt += "\n\nAdditional user guidance for this initialization: " + extra
	}
	return m.startInternalPrompt(prompt, "running /init: exploring the workspace and creating or updating AGENTS.md")
}

func slashHelp() string {
	var b strings.Builder
	b.WriteString("commands:")
	for _, spec := range slash.Specs() {
		b.WriteByte('\n')
		b.WriteString("  ")
		b.WriteString(spec.Usage)
		b.WriteString(" - ")
		b.WriteString(spec.Description)
	}
	b.WriteString("\n\ninput:")
	for _, line := range helpInputLines() {
		b.WriteByte('\n')
		b.WriteString("  ")
		b.WriteString(line)
	}
	b.WriteString("\n\nkeyboard:")
	for _, line := range helpKeyboardLines() {
		b.WriteByte('\n')
		b.WriteString("  ")
		b.WriteString(line)
	}
	b.WriteString("\n\npickers and permission:")
	for _, line := range helpPickerLines() {
		b.WriteByte('\n')
		b.WriteString("  ")
		b.WriteString(line)
	}
	return b.String()
}

func helpInputLines() []string {
	return []string{
		"!<command> - run a local shell command in the workspace",
		"@<prefix> - search workspace files and insert a @relative/path reference",
		"/<command> - open slash command suggestions",
		"Ctrl+J - insert a newline for multiline input (Enter sends); Shift+Enter also works on Kitty-capable terminals",
	}
}

func helpKeyboardLines() []string {
	return []string{
		"Enter - send prompt; while running, inject it as mid-turn guidance (Tab queues for next turn); with a selected candidate, accept it",
		"Ctrl+J - insert a newline (multiline input); the box grows up to ~1/3 of the terminal. Shift+Enter also works on terminals with Kitty keyboard support (WezTerm/Ghostty/Kitty/iTerm2), but not over SSH/tmux",
		"Ctrl+C - quit the TUI, cancelling the current run first",
		"Esc - clear activity focus or cancel an active picker/file search; while running, press twice to interrupt the current turn",
		"Shift+Tab - cycle execution mode: work -> plan -> auto",
		"? - show this cheatsheet",
		"PgUp/PgDown - scroll the transcript",
		"Ctrl+Home/Ctrl+End - jump to the start/end of the transcript",
		"Mouse wheel - scroll the transcript; click an activity row to expand/collapse it",
		"Shift+drag - select text for copy (terminal native, bypasses TUI mouse capture)",
		"Ctrl+O - expand/collapse the latest activity detail",
		"Ctrl+N/Ctrl+P - move activity focus; Enter/Space toggles the focused activity",
		"Up/Down - on the first/last line, browse queued prompts or history; on a middle line, move the cursor within multiline input; with a picker open, move its selection",
		"Tab - complete slash commands/values or insert the selected @ file",
	}
}

func helpPickerLines() []string {
	return []string{
		"model/effort/session pickers: Up/Down or k/j/Tab moves selection, Enter selects, Esc cancels",
		"@ file picker: Up/Down moves selection, Tab/Enter inserts, Esc cancels",
		"permission modal: Up/Down or k/j/Tab moves decision, Enter confirms, Esc denies and interrupts",
		"permission modal: 1-5 choose the visible decisions directly",
		"permission diff preview: d toggles preview, Left/Right switches files when expanded",
	}
}

func (m Model) slashSuggestions(width int) string {
	raw := m.input.Value()
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "/") {
		return ""
	}
	if suggestions := m.slashValueSuggestions(width); suggestions != "" {
		return suggestions
	}
	matches := slash.Match(value)
	if len(matches) == 0 {
		return m.styles.Render(m.styles.Picker.Empty, truncateText("  no matching slash command", width))
	}
	selected := m.selectedSlashIndex(matches)
	var b strings.Builder
	for i, spec := range matches {
		if i > 0 {
			b.WriteByte('\n')
		}
		marker := "  "
		if i == selected {
			marker = "> "
		}
		line := truncateText(fmt.Sprintf("%s%-34s %s", marker, spec.Usage, spec.Description), width)
		if i == selected {
			b.WriteString(m.styles.Render(m.styles.Picker.Selected, line))
			continue
		}
		b.WriteString(m.styles.Render(m.styles.Picker.Item, line))
	}
	return b.String()
}
