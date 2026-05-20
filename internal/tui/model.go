// Package tui contains the Bubble Tea terminal interface for ub.
package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/permission"
	permissiondialog "github.com/feimingxliu/ub/internal/tui/dialog/permission"
	"github.com/feimingxliu/ub/internal/tui/slash"
	"github.com/feimingxliu/ub/internal/tui/tuitheme"
)

// Options configures the initial TUI shell.
type Options struct {
	Input          io.Reader
	Output         io.Writer
	Context        context.Context
	Runner         Runner
	Permissions    <-chan PermissionRequest
	Model          string
	Models         []string
	Effort         string
	Efforts        []string
	ApprovalModel  string
	ApprovalModels []string
	Messages       []InitialMessage
	Turn           int
	ExecutionMode  string
	Cwd            string
	EventTimeout   time.Duration
}

// Run starts the terminal UI and blocks until it exits.
func Run(ctx context.Context, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	programOpts := []tea.ProgramOption{tea.WithContext(ctx), tea.WithAltScreen(), tea.WithMouseCellMotion()}
	if opts.Input != nil {
		programOpts = append(programOpts, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		programOpts = append(programOpts, tea.WithOutput(opts.Output))
	}
	opts.Context = ctx
	_, err := tea.NewProgram(NewModel(opts), programOpts...).Run()
	return err
}

// Model is the root Bubble Tea model for the chat shell.
type Model struct {
	input          textinput.Model
	messages       messageList
	status         statusBar
	styles         tuitheme.Styles
	runner         Runner
	permReqs       <-chan PermissionRequest
	pending        *PermissionRequest
	modal          permissiondialog.Model
	ctx            context.Context
	cancel         context.CancelFunc
	running        bool
	events         <-chan Event
	models         []string
	efforts        []string
	approvalModel  string
	approvalModels []string
	picker         *modelPicker
	pickerTarget   string
	sessions       *sessionPicker
	slashIdx       int
	history        []string
	histIdx        int
	draft          string
	queuedPrompts  []string
	queueIdx       int
	queueDraft     string
	scroll         int
	runID          int
	timeout        time.Duration
	width          int
	height         int
}

// NewModel creates the root TUI model.
func NewModel(opts Options) Model {
	styles := tuitheme.Default()
	input := textinput.New()
	input.Placeholder = "Type a message"
	input.Prompt = "› "
	input.PromptStyle = styles.Input.Prompt
	input.TextStyle = styles.Input.Text
	input.PlaceholderStyle = styles.Input.Placeholder
	input.Cursor.Style = styles.Input.Cursor
	input.Cursor.SetMode(cursor.CursorStatic)
	input.Width = inputWidthForTerminal(defaultViewWidth, input.Prompt)
	input.Focus()
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	modelName := defaultString(opts.Model, "unknown")
	models := opts.Models
	if len(models) == 0 {
		if runner, ok := opts.Runner.(ControlRunner); ok {
			models = runner.Models()
		}
	}
	effort := strings.TrimSpace(opts.Effort)
	efforts := opts.Efforts
	if effortRunner, ok := opts.Runner.(EffortControlRunner); ok {
		if effort == "" {
			effort = effortRunner.Effort()
		}
		if len(efforts) == 0 {
			efforts = effortRunner.Efforts()
		}
	}
	effort = defaultString(effort, "none")
	approvalModel := strings.TrimSpace(opts.ApprovalModel)
	approvalModels := opts.ApprovalModels
	if approvalRunner, ok := opts.Runner.(ApprovalControlRunner); ok {
		if approvalModel == "" {
			approvalModel = approvalRunner.ApprovalModel()
		}
		if len(approvalModels) == 0 {
			approvalModels = approvalRunner.ApprovalModels()
		}
	}

	m := Model{
		input:          input,
		messages:       newMessageList(),
		styles:         styles,
		runner:         opts.Runner,
		permReqs:       opts.Permissions,
		ctx:            ctx,
		models:         normalizeModels(models, modelName),
		efforts:        normalizeOptions(efforts, effort),
		approvalModel:  approvalModel,
		approvalModels: normalizeModels(approvalModels, approvalModel),
		history:        promptHistoryFromMessages(opts.Messages),
		histIdx:        -1,
		queueIdx:       -1,
		timeout:        opts.EventTimeout,
		status: statusBar{
			model:         modelName,
			effort:        effort,
			executionMode: defaultString(opts.ExecutionMode, string(execution.ModeWork)),
			cwd:           defaultString(opts.Cwd, "."),
			turn:          opts.Turn,
			state:         statusIdle,
		},
	}
	m.messages.load(opts.Messages)
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return waitForPermission(m.permReqs)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case streamEventMsg:
		return m.handleStreamEvent(msg)
	case permissionRequestMsg:
		return m.handlePermissionRequest(msg)
	}

	if m.pending != nil {
		if mouse, ok := msg.(tea.MouseMsg); ok {
			switch mouse.Type {
			case tea.MouseWheelUp:
				m.scrollMessages(3)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollMessages(-3)
				return m, nil
			}
		}
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				m.interruptCurrent()
				return m, waitForPermission(m.permReqs)
			case "shift+tab":
				return m.cycleMode()
			case "pgup":
				m.scrollMessages(m.pageScrollLines())
				return m, nil
			case "pgdown":
				m.scrollMessages(-m.pageScrollLines())
				return m, nil
			case "d":
				m.modal = m.modal.ToggleDiff()
				return m, nil
			case "enter":
				return m.resolvePermission(m.modal.SelectedDecision())
			default:
				if m.modal.HandleKey(key.String()) {
					return m, nil
				}
				if decision, ok := permissiondialog.DecisionForKey(key.String()); ok {
					return m.resolvePermission(decision)
				}
			}
		}
		return m, nil
	}

	if m.picker != nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				m.picker = nil
				return m, nil
			case "up", "k":
				m.picker.previous()
				return m, nil
			case "down", "j", "tab":
				m.picker.next()
				return m, nil
			case "enter":
				selected := m.picker.selected()
				target := m.pickerTarget
				m.picker = nil
				m.pickerTarget = ""
				if target == "approval" {
					if err := m.setApprovalModel(selected); err != nil {
						m.messages.append(systemRole, err.Error())
						return m, nil
					}
					m.messages.append(systemRole, "approval model set to "+selected)
					return m, nil
				}
				if target == "effort" {
					if err := m.setEffort(selected); err != nil {
						m.messages.append(systemRole, err.Error())
						return m, nil
					}
					m.messages.append(systemRole, "effort set to "+selected)
					return m, nil
				}
				if err := m.setModel(selected); err != nil {
					m.messages.append(systemRole, err.Error())
					return m, nil
				}
				m.messages.append(systemRole, "model set to "+selected)
				return m, nil
			}
		}
		return m, nil
	}

	if m.sessions != nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				m.sessions = nil
				return m, nil
			case "up", "k":
				m.sessions.previous()
				return m, nil
			case "down", "j", "tab":
				m.sessions.next()
				return m, nil
			case "enter":
				selected := m.sessions.selected()
				m.sessions = nil
				return m.switchSession(selected.ID)
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseLeft:
			if m.toggleMessageAt(msg.X, msg.Y) {
				return m, nil
			}
		case tea.MouseWheelUp:
			m.scrollMessages(3)
			return m, nil
		case tea.MouseWheelDown:
			m.scrollMessages(-3)
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.status.width = msg.Width
		m.input.Width = inputWidthForTerminal(msg.Width, m.input.Prompt)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "esc":
			if m.running {
				m.interruptCurrent()
			}
			return m, nil
		case "pgup":
			m.scrollMessages(m.pageScrollLines())
			return m, nil
		case "pgdown":
			m.scrollMessages(-m.pageScrollLines())
			return m, nil
		case "up":
			if m.moveSlashValueSelection(-1) {
				return m, nil
			}
			if m.moveSlashSelection(-1) {
				return m, nil
			}
			if m.navigateQueuedPrompts(-1) {
				return m, nil
			}
			if m.navigatePromptHistory(-1) {
				return m, nil
			}
		case "down":
			if m.moveSlashValueSelection(1) {
				return m, nil
			}
			if m.moveSlashSelection(1) {
				return m, nil
			}
			if m.navigateQueuedPrompts(1) {
				return m, nil
			}
			if m.navigatePromptHistory(1) {
				return m, nil
			}
		case "shift+tab":
			return m.cycleMode()
		case "tab":
			if m.running {
				return m, nil
			}
			if m.completeSlashValue() {
				return m, nil
			}
			if m.completeSlash() {
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		case "enter":
			if m.running {
				if m.queueInput() {
					return m, nil
				}
				return m, nil
			}
			if updated, cmd, ok := m.acceptSlashValueSuggestion(); ok {
				return updated, cmd
			}
			if m.completeSlashOnEnter() {
				return m, nil
			}
			if text := strings.TrimSpace(m.input.Value()); text != "" {
				if strings.HasPrefix(text, "/") {
					m.input.SetValue("")
					m.resetPromptHistoryNavigation()
					return m.executeSlash(text)
				}
				return m.startPrompt(text, true)
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.saveQueuedPromptEdit()
	return m, cmd
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
		m.events = nil
		return m.startNextQueuedPrompt()
	}
	cmd := waitForEventFromUpdate(msg.event, &m)
	return m, cmd
}

func (m Model) handlePermissionRequest(msg permissionRequestMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	m.pending = &msg.request
	m.modal = permissiondialog.New(msg.request.Request)
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	var b strings.Builder
	width := contentWidth(m.width)
	footer := m.footerView(width)
	b.WriteString(m.messages.view(width, m.messageViewHeight(footer), m.clampedScroll(), m.styles))
	b.WriteString("\n\n")
	b.WriteString(footer)
	return b.String()
}

func (m Model) resolvePermission(decision permission.Decision) (tea.Model, tea.Cmd) {
	if m.pending != nil && m.pending.Response != nil {
		m.pending.Response <- decision
	}
	m.pending = nil
	return m, waitForPermission(m.permReqs)
}

func (m Model) footerView(width int) string {
	var b strings.Builder
	b.WriteString(m.input.View())
	b.WriteByte('\n')
	if picker := m.pickerView(width); picker != "" {
		b.WriteString(picker)
		b.WriteByte('\n')
	} else if picker := m.sessionPickerView(width); picker != "" {
		b.WriteString(picker)
		b.WriteByte('\n')
	} else if suggestions := m.slashSuggestions(width); suggestions != "" {
		b.WriteString(suggestions)
		b.WriteByte('\n')
	}
	if queued := m.queuedPromptView(width); queued != "" {
		b.WriteString(queued)
		b.WriteByte('\n')
	}
	b.WriteString(m.status.view(width, m.styles))
	if m.pending != nil {
		b.WriteString("\n\n")
		b.WriteString(m.modal.View())
	}
	return b.String()
}

func inputWidthForTerminal(width int, prompt string) int {
	available := contentWidth(width) - runewidth.StringWidth(prompt) - 1
	return max(1, available)
}

func (m *Model) toggleMessageAt(x, y int) bool {
	width := contentWidth(m.width)
	footer := m.footerView(width)
	height := m.messageViewHeight(footer)
	return m.messages.toggleAt(width, height, m.clampedScroll(), x, y, m.styles)
}

// MessageTexts returns the rendered message text values for tests.
func (m Model) MessageTexts() []string {
	return m.messages.texts()
}

// InputValue returns the current input value for tests.
func (m Model) InputValue() string {
	return m.input.Value()
}

// Running reports whether an Agent turn is in progress.
func (m Model) Running() bool {
	return m.running
}

// QueuedPrompts returns queued user prompts for tests.
func (m Model) QueuedPrompts() []string {
	return append([]string(nil), m.queuedPrompts...)
}

// Turn returns the current TUI turn number.
func (m Model) Turn() int {
	return m.status.turn
}

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
	}
	if m.runner == nil {
		return m, nil
	}
	m.running = true
	m.status.state = statusThinking
	m.status.turn++
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	events := make(chan Event, 64)
	m.events = events
	m.runID++
	runID := m.runID
	m.messages.startActivityGroup(thinkingActivityGroupKey(runID), "Thinking...")
	return m, tea.Batch(runPrompt(ctx, m.runner, text, events), waitForEventWithTimeout(events, runID, m.timeout))
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
	case "quit", "exit":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "config":
		approvalModel := defaultString(m.approvalModel, "none")
		m.messages.append(systemRole, fmt.Sprintf("model=%s effort=%s approval_model=%s mode=%s cwd=%s", m.status.model, m.status.effort, approvalModel, m.status.executionMode, m.status.cwd))
		return m, nil
	case "sessions":
		if len(cmd.Args) > 0 {
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
	case "mode":
		if len(cmd.Args) == 0 {
			return m, nil
		}
		mode, err := execution.ParseMode(cmd.Args[0])
		if err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
		if runner, ok := m.runner.(ControlRunner); ok {
			if err := runner.SetMode(string(mode)); err != nil {
				m.messages.append(systemRole, err.Error())
				return m, nil
			}
		}
		m.status.executionMode = string(mode)
		return m, nil
	default:
		m.messages.append(systemRole, "unknown slash command "+cmd.Name)
		return m, nil
	}
}

func waitForEventFromUpdate(event Event, m *Model) tea.Cmd {
	m.updateContextUsage(event)
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
		if activityGroupNameForEvent(event) != thinkingGroupName {
			m.messages.removePlaceholderActivityGroup(thinkingActivityGroupKey(m.runID))
		}
		if groupName := activityGroupNameForEvent(event); groupName != "" {
			m.messages.appendOrUpdateActivityInGroup(activityGroupKeyForName(m.runID, groupName), groupName, event)
		} else {
			m.messages.appendOrUpdateActivity(event)
		}
		m.status.state = statusForActivity(event)
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
		m.messages.finishActivityGroup(thinkingActivityGroupKey(m.runID), "done")
		m.messages.finishActivityGroup(toolActivityGroupKey(m.runID), "done")
		m.status.state = statusFinalizing
		m.cancel = nil
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	case EventError:
		m.messages.removeKey(activityRole, thinkingActivityKey(m.runID))
		m.messages.finishActivityGroup(thinkingActivityGroupKey(m.runID), "failed")
		m.messages.finishActivityGroup(toolActivityGroupKey(m.runID), "failed")
		if m.cancel != nil {
			m.cancel()
		}
		m.messages.append(errorRole, defaultString(event.Content, "agent failed"))
		m.running = false
		m.status.state = statusIdle
		m.cancel = nil
		return nil
	default:
		return waitForEventWithTimeout(m.events, m.runID, m.timeout)
	}
}

func permissionEventText(event Event) string {
	source := defaultString(event.Source, "permission")
	decision := defaultString(event.Decision, "")
	if decision == "" {
		if event.Allowed {
			decision = "allow"
		} else {
			decision = "deny"
		}
	}
	toolName := defaultString(event.ToolName, "tool")
	text := fmt.Sprintf("permission %s %s %s", source, decision, toolName)
	if reason := strings.TrimSpace(event.Reason); reason != "" {
		text += ": " + reason
	}
	return text
}

func activityEventText(event Event) string {
	switch strings.TrimSpace(event.ActivityKind) {
	case "thinking":
		return "thinking: " + defaultString(event.Summary, event.Text)
	case "tool":
		return toolActivityText(event)
	case "permission":
		return permissionEventText(event)
	case "notice":
		return "notice: " + defaultString(event.Summary, event.Text)
	default:
		return defaultString(event.Summary, defaultString(event.Content, "activity"))
	}
}

func activityEventKey(event Event) string {
	switch strings.TrimSpace(event.ActivityKind) {
	case "tool":
		if strings.TrimSpace(event.ToolUseID) != "" {
			return "tool:" + event.ToolUseID
		}
	case "thinking":
		return "thinking"
	}
	return ""
}

func thinkingActivityKey(runID int) string {
	return fmt.Sprintf("thinking:%d", runID)
}

func thinkingActivityGroupKey(runID int) string {
	return activityGroupKeyForName(runID, thinkingGroupName)
}

func toolActivityGroupKey(runID int) string {
	return activityGroupKeyForName(runID, toolGroupName)
}

func activityGroupKeyForName(runID int, groupName string) string {
	return fmt.Sprintf("activity:%s:%d", groupName, runID)
}

func activityGroupNameForEvent(event Event) string {
	switch strings.TrimSpace(event.ActivityKind) {
	case "thinking":
		return thinkingGroupName
	case "tool", "permission":
		return toolGroupName
	default:
		return ""
	}
}

func toolActivityText(event Event) string {
	name := strings.TrimSpace(event.ToolName)
	title := toolTitle(name, event.Summary)
	switch event.Status {
	case "queued", "running":
		action := toolAction(name)
		if summary := strings.TrimSpace(event.Summary); summary != "" {
			return action + " " + summary
		}
		return action
	case "failed":
		text := title + " failed"
		if detail := strings.TrimSpace(event.Content); detail != "" {
			text += ": " + detail
		}
		return text
	default:
		return title
	}
}

func toolAction(name string) string {
	switch strings.TrimSpace(name) {
	case "read":
		return "Reading file..."
	case "ls":
		return "Listing directory..."
	case "grep":
		return "Searching content..."
	case "glob":
		return "Finding files..."
	case "write":
		return "Preparing write..."
	case "edit":
		return "Preparing edit..."
	case "bash":
		return "Writing command..."
	case "job_run":
		return "Starting job..."
	case "job_output":
		return "Reading job output..."
	case "job_kill":
		return "Stopping job..."
	default:
		return "Working..."
	}
}

func toolTitle(name, summary string) string {
	summary = strings.TrimSpace(summary)
	verb := "Tool"
	switch strings.TrimSpace(name) {
	case "read":
		verb = "Read"
	case "ls":
		verb = "Listed"
	case "grep":
		verb = "Searched"
	case "glob":
		verb = "Found"
	case "write":
		verb = "Wrote"
	case "edit":
		verb = "Edited"
	case "bash":
		verb = "Ran"
	case "job_run":
		verb = "Started job"
	case "job_output":
		verb = "Read job output"
	case "job_kill":
		verb = "Stopped job"
	default:
		if strings.TrimSpace(name) != "" {
			verb = name
		}
	}
	if summary == "" {
		return verb
	}
	return verb + " " + summary
}

func statusForActivity(event Event) string {
	switch strings.TrimSpace(event.ActivityKind) {
	case "tool":
		switch event.Status {
		case "queued", "running":
			return statusTool
		default:
			return statusThinking
		}
	case "thinking":
		return statusThinking
	case "permission":
		return statusTool
	default:
		return statusThinking
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
		mode := execution.Mode(next)
		m.pending.Request.Mode = mode
		m.modal.Request.Mode = mode
	}
	return m, nil
}

func (m *Model) interruptCurrent() {
	if m.pending != nil && m.pending.Response != nil {
		select {
		case m.pending.Response <- permission.DecisionDeny:
		default:
		}
	}
	m.pending = nil
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
		string(execution.ModeWork),
		string(execution.ModePlan),
		string(execution.ModeAuto),
	}
	for i, mode := range order {
		if current == mode {
			return order[(i+1)%len(order)]
		}
	}
	return string(execution.ModeWork)
}

func (m Model) startCompact() (tea.Model, tea.Cmd) {
	runner, ok := m.runner.(CompactRunner)
	if !ok {
		m.messages.append(systemRole, "compact is unavailable in this runner")
		return m, nil
	}
	m.running = true
	m.status.state = statusThinking
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	events := make(chan Event, 64)
	m.events = events
	m.runID++
	runID := m.runID
	m.messages.startActivityGroup(thinkingActivityGroupKey(runID), "Compacting...")
	return m, tea.Batch(runCompact(ctx, runner, events), waitForEventWithTimeout(events, runID, m.timeout))
}

func (m *Model) setModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model cannot be empty")
	}
	if !modelAllowed(m.models, model) {
		return fmt.Errorf("model %q is not available for the current provider; use /model to list candidates", model)
	}
	if runner, ok := m.runner.(ControlRunner); ok {
		if err := runner.SetModel(model); err != nil {
			return err
		}
	}
	m.status.model = model
	m.models = normalizeModels(m.models, model)
	m.refreshEffortFromRunner()
	return nil
}

func (m *Model) setEffort(effort string) error {
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return fmt.Errorf("effort cannot be empty")
	}
	if runner, ok := m.runner.(EffortControlRunner); ok {
		if err := runner.SetEffort(effort); err != nil {
			return err
		}
		m.refreshEffortFromRunner()
		return nil
	}
	if !modelAllowed(m.efforts, effort) {
		return fmt.Errorf("effort %q is not available for the current model; use /effort to list candidates", effort)
	}
	m.status.effort = effort
	m.efforts = normalizeModels(m.efforts, effort)
	return nil
}

func (m *Model) refreshEffortFromRunner() {
	runner, ok := m.runner.(EffortControlRunner)
	if !ok {
		return
	}
	effort := defaultString(runner.Effort(), "none")
	m.status.effort = effort
	m.efforts = normalizeOptions(runner.Efforts(), effort)
}

func (m *Model) setApprovalModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("approval model cannot be empty")
	}
	if !modelAllowed(m.approvalModels, model) {
		return fmt.Errorf("approval model %q is not available for the current approval provider; use /approval-model to list candidates", model)
	}
	if runner, ok := m.runner.(ApprovalControlRunner); ok {
		if err := runner.SetApprovalModel(model); err != nil {
			return err
		}
	}
	m.approvalModel = model
	m.approvalModels = normalizeModels(m.approvalModels, model)
	return nil
}

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

func (m *Model) applySessionState(state SessionState) {
	m.messages.load(state.Messages)
	m.history = promptHistoryFromMessages(state.Messages)
	m.queuedPrompts = nil
	m.resetQueuedPromptNavigation()
	m.resetPromptHistoryNavigation()
	m.scrollToBottom()
	if strings.TrimSpace(state.Model) != "" {
		m.status.model = state.Model
		m.models = normalizeModels(m.models, state.Model)
		m.refreshEffortFromRunner()
	}
	m.status.turn = state.Turn
	m.status.contextUsedTokens = 0
	m.status.contextMaxTokens = 0
	m.status.contextRatio = 0
	m.status.contextKind = ""
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
	return b.String()
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

func (m Model) pickerView(width int) string {
	if m.picker == nil {
		return ""
	}
	return m.picker.view(width, m.styles)
}

func (m Model) sessionPickerView(width int) string {
	if m.sessions == nil {
		return ""
	}
	return m.sessions.view(width, m.styles)
}

func (m Model) modelSuggestions(prefix string, width int) string {
	return valueSuggestionsFrom(filterValueSuggestions(m.models, prefix), width, "model", m.slashIdx, m.styles)
}

func valueSuggestionsFrom(values []string, width int, label string, selected int, styles tuitheme.Styles) string {
	var b strings.Builder
	for i, value := range values {
		if i > 0 {
			b.WriteByte('\n')
		}
		marker := "  "
		if i == selectedIndex(selected, len(values)) {
			marker = "> "
		}
		line := truncateText(marker+value, width)
		if i == selectedIndex(selected, len(values)) {
			b.WriteString(styles.Render(styles.Picker.Selected, line))
			continue
		}
		b.WriteString(styles.Render(styles.Picker.Item, line))
	}
	if len(values) == 0 {
		return styles.Render(styles.Picker.Empty, truncateText("  no matching "+label, width))
	}
	return b.String()
}

func filterValueSuggestions(values []string, prefix string) []string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if prefix != "" && !strings.Contains(strings.ToLower(value), prefix) {
			continue
		}
		out = append(out, value)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func normalizeModels(models []string, current string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		if _, ok := seen[model]; ok {
			return
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	add(current)
	for _, model := range models {
		add(model)
	}
	return out
}

func normalizeOptions(options []string, current string) []string {
	current = strings.TrimSpace(current)
	seen := map[string]struct{}{}
	var out []string
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" {
			continue
		}
		if _, ok := seen[option]; ok {
			continue
		}
		seen[option] = struct{}{}
		out = append(out, option)
	}
	if current != "" {
		if _, ok := seen[current]; !ok {
			out = append([]string{current}, out...)
		}
	}
	return out
}

func modelAllowed(models []string, model string) bool {
	for _, candidate := range models {
		if candidate == model {
			return true
		}
	}
	return false
}

func (m *Model) completeSlash() bool {
	matches := m.slashCommandMatches()
	if len(matches) == 0 {
		return false
	}
	completion := "/" + matches[m.selectedSlashIndex(matches)].Name + " "
	m.input.SetValue(completion)
	m.input.CursorEnd()
	m.slashIdx = 0
	m.resetPromptHistoryNavigation()
	return true
}

func (m *Model) completeSlashValue() bool {
	values, command, _, ok := m.slashValueMatches()
	if !ok || len(values) == 0 {
		return false
	}
	selected := values[selectedIndex(m.slashIdx, len(values))]
	m.input.SetValue("/" + command + " " + selected)
	m.input.CursorEnd()
	m.slashIdx = 0
	m.resetPromptHistoryNavigation()
	return true
}

func (m Model) acceptSlashValueSuggestion() (tea.Model, tea.Cmd, bool) {
	values, command, _, ok := m.slashValueMatches()
	if !ok || len(values) == 0 {
		return m, nil, false
	}
	selected := values[selectedIndex(m.slashIdx, len(values))]
	next := "/" + command + " " + selected
	m.input.SetValue("")
	m.slashIdx = 0
	m.resetPromptHistoryNavigation()
	updated, cmd := m.executeSlash(next)
	return updated, cmd, true
}

func (m *Model) completeSlashOnEnter() bool {
	raw := m.input.Value()
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "/") || slashInputHasArgs(raw) {
		return false
	}
	if _, err := slash.Parse(value); err == nil {
		return false
	}
	return m.completeSlash()
}

func (m *Model) moveSlashSelection(delta int) bool {
	matches := m.slashCommandMatches()
	if len(matches) == 0 {
		return false
	}
	m.slashIdx = (m.selectedSlashIndex(matches) + delta + len(matches)) % len(matches)
	return true
}

func (m *Model) moveSlashValueSelection(delta int) bool {
	values, _, _, ok := m.slashValueMatches()
	if !ok {
		return false
	}
	if len(values) == 0 {
		return true
	}
	m.slashIdx = (selectedIndex(m.slashIdx, len(values)) + delta + len(values)) % len(values)
	return true
}

func (m Model) slashCommandMatches() []slash.Spec {
	raw := m.input.Value()
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "/") || slashInputHasArgs(raw) {
		return nil
	}
	return slash.Match(value)
}

func (m Model) slashValueSuggestions(width int) string {
	values, _, label, ok := m.slashValueMatches()
	if !ok {
		return ""
	}
	return valueSuggestionsFrom(values, width, label, m.slashIdx, m.styles)
}

func (m Model) slashValueMatches() ([]string, string, string, bool) {
	source, ok := m.slashValueSource()
	if !ok {
		return nil, "", "", false
	}
	return filterValueSuggestions(source.values, source.prefix), source.command, source.label, true
}

type slashValueSource struct {
	command string
	label   string
	prefix  string
	values  []string
}

func (m Model) slashValueSource() (slashValueSource, bool) {
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "model"); ok {
		return slashValueSource{command: "model", label: "model", prefix: prefix, values: m.models}, true
	}
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "effort"); ok {
		return slashValueSource{command: "effort", label: "effort", prefix: prefix, values: m.efforts}, true
	}
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "approval-model"); ok {
		return slashValueSource{command: "approval-model", label: "approval model", prefix: prefix, values: m.approvalModels}, true
	}
	return slashValueSource{}, false
}

func slashCommandArgPrefix(raw, command string) (string, bool) {
	value := strings.TrimLeft(raw, " \t\r\n")
	head := "/" + command
	if !strings.HasPrefix(strings.ToLower(value), head) {
		return "", false
	}
	rest := value[len(head):]
	if rest == "" || !strings.ContainsAny(rest[:1], " \t\r\n") {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

func (m Model) selectedSlashIndex(matches []slash.Spec) int {
	return selectedIndex(m.slashIdx, len(matches))
}

func selectedIndex(index, length int) int {
	if length == 0 || index < 0 {
		return 0
	}
	if index >= length {
		return length - 1
	}
	return index
}

func slashInputHasArgs(raw string) bool {
	trimmedLeft := strings.TrimLeft(raw, " \t\r\n")
	if !strings.HasPrefix(trimmedLeft, "/") {
		return false
	}
	withoutSlash := strings.TrimPrefix(trimmedLeft, "/")
	return strings.ContainsAny(withoutSlash, " \t\r\n")
}

func promptHistoryFromMessages(messages []InitialMessage) []string {
	var out []string
	for _, msg := range messages {
		if normalizeRole(msg.Role) != userRole {
			continue
		}
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func (m *Model) recordPromptHistory(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		m.resetPromptHistoryNavigation()
		return
	}
	if len(m.history) == 0 || m.history[len(m.history)-1] != text {
		m.history = append(m.history, text)
	}
	m.resetPromptHistoryNavigation()
}

func (m *Model) queueInput() bool {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		if m.queueIdx >= 0 {
			m.saveQueuedPromptEdit()
			return true
		}
		return false
	}
	if strings.HasPrefix(text, "/") {
		return false
	}
	if m.queueIdx >= 0 && m.queueIdx < len(m.queuedPrompts) {
		m.queuedPrompts[m.queueIdx] = text
		m.resetQueuedPromptNavigation()
	} else {
		m.queuedPrompts = append(m.queuedPrompts, text)
	}
	m.input.SetValue("")
	m.input.CursorEnd()
	m.resetPromptHistoryNavigation()
	return true
}

func (m *Model) saveQueuedPromptEdit() bool {
	if m.queueIdx < 0 || m.queueIdx >= len(m.queuedPrompts) {
		return false
	}
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		m.queuedPrompts = append(m.queuedPrompts[:m.queueIdx], m.queuedPrompts[m.queueIdx+1:]...)
		m.input.SetValue(m.queueDraft)
		m.input.CursorEnd()
		m.resetQueuedPromptNavigation()
		return true
	}
	m.queuedPrompts[m.queueIdx] = text
	return false
}

func (m *Model) navigateQueuedPrompts(delta int) bool {
	if !m.running || len(m.queuedPrompts) == 0 || delta == 0 {
		return false
	}
	if m.queueIdx >= 0 {
		if m.saveQueuedPromptEdit() {
			return true
		}
	} else if delta < 0 {
		m.queueDraft = m.input.Value()
	} else {
		return false
	}

	switch {
	case delta < 0:
		if m.queueIdx < 0 || m.queueIdx >= len(m.queuedPrompts) {
			m.queueIdx = len(m.queuedPrompts) - 1
		} else if m.queueIdx > 0 {
			m.queueIdx--
		}
	case delta > 0:
		if m.queueIdx < len(m.queuedPrompts)-1 {
			m.queueIdx++
		} else {
			m.input.SetValue(m.queueDraft)
			m.input.CursorEnd()
			m.resetQueuedPromptNavigation()
			return true
		}
	}
	m.input.SetValue(m.queuedPrompts[m.queueIdx])
	m.input.CursorEnd()
	return true
}

func (m *Model) resetQueuedPromptNavigation() {
	m.queueIdx = -1
	m.queueDraft = ""
}

func (m Model) startNextQueuedPrompt() (tea.Model, tea.Cmd) {
	if len(m.queuedPrompts) == 0 {
		return m, nil
	}
	restoreInput := m.input.Value()
	if m.queueIdx >= 0 {
		restoreInput = m.queueDraft
		m.saveQueuedPromptEdit()
	}
	if len(m.queuedPrompts) == 0 {
		m.input.SetValue(restoreInput)
		m.input.CursorEnd()
		m.resetQueuedPromptNavigation()
		return m, nil
	}
	next := m.queuedPrompts[0]
	m.queuedPrompts = append([]string(nil), m.queuedPrompts[1:]...)
	m.input.SetValue(restoreInput)
	m.input.CursorEnd()
	m.resetQueuedPromptNavigation()
	return m.startPrompt(next, false)
}

func (m Model) queuedPromptView(width int) string {
	if len(m.queuedPrompts) == 0 {
		return ""
	}
	prefix := fmt.Sprintf("queued: %d", len(m.queuedPrompts))
	index := 0
	label := "next"
	if m.queueIdx >= 0 && m.queueIdx < len(m.queuedPrompts) {
		index = m.queueIdx
		label = fmt.Sprintf("editing %d/%d", m.queueIdx+1, len(m.queuedPrompts))
	}
	line := fmt.Sprintf("%s · %s: %s", prefix, label, m.queuedPrompts[index])
	return m.styles.Render(m.styles.Picker.Item, truncateText(line, width))
}

func (m *Model) scrollMessages(delta int) {
	if delta == 0 {
		return
	}
	m.scroll += delta
	maxScroll := m.maxMessageScroll()
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
}

func (m *Model) scrollToBottom() {
	m.scroll = 0
}

func (m Model) clampedScroll() int {
	if m.scroll < 0 {
		return 0
	}
	maxScroll := m.maxMessageScroll()
	if m.scroll > maxScroll {
		return maxScroll
	}
	return m.scroll
}

func (m Model) maxMessageScroll() int {
	width := contentWidth(m.width)
	footer := m.footerView(width)
	height := m.messageViewHeight(footer)
	if height <= 0 {
		return 0
	}
	lines := len(m.messages.lines(width))
	if lines <= height {
		return 0
	}
	return lines - height
}

func (m Model) pageScrollLines() int {
	height := m.messageViewHeight(m.footerView(contentWidth(m.width)))
	if height <= 1 {
		return 1
	}
	return height - 1
}

func (m Model) messageViewHeight(footer string) int {
	if m.height <= 0 {
		return 0
	}
	return max(1, m.height-lineCount(footer)-2)
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func (m *Model) navigatePromptHistory(delta int) bool {
	if len(m.history) == 0 || delta == 0 {
		return false
	}
	switch {
	case delta < 0:
		if m.histIdx < 0 {
			m.draft = m.input.Value()
			m.histIdx = len(m.history) - 1
		} else if m.histIdx > 0 {
			m.histIdx--
		}
	case delta > 0:
		if m.histIdx < 0 {
			return false
		}
		if m.histIdx < len(m.history)-1 {
			m.histIdx++
		} else {
			m.input.SetValue(m.draft)
			m.input.CursorEnd()
			m.resetPromptHistoryNavigation()
			return true
		}
	}
	m.input.SetValue(m.history[m.histIdx])
	m.input.CursorEnd()
	return true
}

func (m *Model) resetPromptHistoryNavigation() {
	m.histIdx = -1
	m.draft = ""
}
