// Package tui contains the Bubble Tea terminal interface for ub.
package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/feimingxliu/ub/internal/execution"
	permissiondialog "github.com/feimingxliu/ub/internal/tui/dialog/permission"
	"github.com/feimingxliu/ub/internal/tui/slash"
)

// Options configures the initial TUI shell.
type Options struct {
	Input         io.Reader
	Output        io.Writer
	Context       context.Context
	Runner        Runner
	Permissions   <-chan PermissionRequest
	Model         string
	Models        []string
	Messages      []InitialMessage
	Turn          int
	ExecutionMode string
	Cwd           string
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
	input    textinput.Model
	messages messageList
	status   statusBar
	runner   Runner
	permReqs <-chan PermissionRequest
	pending  *PermissionRequest
	modal    permissiondialog.Model
	ctx      context.Context
	cancel   context.CancelFunc
	running  bool
	events   <-chan Event
	models   []string
	picker   *modelPicker
	sessions *sessionPicker
	slashIdx int
	history  []string
	histIdx  int
	draft    string
	scroll   int
	width    int
	height   int
}

// NewModel creates the root TUI model.
func NewModel(opts Options) Model {
	input := textinput.New()
	input.Placeholder = "Type a message"
	input.Prompt = "> "
	input.Width = defaultViewWidth - 2
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

	m := Model{
		input:    input,
		messages: newMessageList(),
		runner:   opts.Runner,
		permReqs: opts.Permissions,
		ctx:      ctx,
		models:   normalizeModels(models, modelName),
		history:  promptHistoryFromMessages(opts.Messages),
		histIdx:  -1,
		status: statusBar{
			model:         modelName,
			executionMode: defaultString(opts.ExecutionMode, "default"),
			cwd:           defaultString(opts.Cwd, "."),
			turn:          opts.Turn,
		},
	}
	m.messages.load(opts.Messages)
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, waitForPermission(m.permReqs))
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.pending != nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "d":
				m.modal = m.modal.ToggleDiff()
				return m, nil
			default:
				if m.modal.HandleKey(key.String()) {
					return m, nil
				}
				if decision, ok := permissiondialog.DecisionForKey(key.String()); ok {
					m.pending.Response <- decision
					m.pending = nil
					return m, waitForPermission(m.permReqs)
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
				m.picker = nil
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
		m.input.Width = max(20, msg.Width-2)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "pgup":
			m.scrollMessages(m.pageScrollLines())
			return m, nil
		case "pgdown":
			m.scrollMessages(-m.pageScrollLines())
			return m, nil
		case "up":
			if m.moveSlashSelection(-1) {
				return m, nil
			}
			if m.navigatePromptHistory(-1) {
				return m, nil
			}
		case "down":
			if m.moveSlashSelection(1) {
				return m, nil
			}
			if m.navigatePromptHistory(1) {
				return m, nil
			}
		case "shift+tab":
			if m.running {
				return m, nil
			}
			return m.cycleMode()
		case "tab":
			if m.running {
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
				return m, nil
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
				m.scrollToBottom()
				m.messages.append(userRole, text)
				m.recordPromptHistory(text)
				m.input.SetValue("")
				if m.runner == nil {
					return m, nil
				}
				m.messages.startAssistant()
				m.running = true
				m.status.running = true
				m.status.turn++
				ctx, cancel := context.WithCancel(m.ctx)
				m.cancel = cancel
				events := make(chan Event, 64)
				m.events = events
				return m, tea.Batch(runPrompt(ctx, m.runner, text, events), waitForEvent(events))
			}
			return m, nil
		}
	case streamEventMsg:
		if !msg.ok {
			m.running = false
			m.status.running = false
			m.cancel = nil
			return m, nil
		}
		cmd := waitForEventFromUpdate(msg.event, &m)
		return m, cmd
	case permissionRequestMsg:
		if !msg.ok {
			return m, nil
		}
		m.pending = &msg.request
		m.modal = permissiondialog.New(msg.request.Request)
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m Model) View() string {
	var b strings.Builder
	width := contentWidth(m.width)
	footer := m.footerView(width)
	b.WriteString(m.messages.view(width, m.messageViewHeight(footer), m.clampedScroll()))
	b.WriteString("\n\n")
	b.WriteString(footer)
	return b.String()
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
	b.WriteString(m.status.view(width))
	if m.pending != nil {
		b.WriteString("\n\n")
		b.WriteString(m.modal.View())
	}
	return b.String()
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

// Turn returns the current TUI turn number.
func (m Model) Turn() int {
	return m.status.turn
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
	case "help":
		m.messages.append(systemRole, slashHelp())
		return m, nil
	case "quit", "exit":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "config":
		m.messages.append(systemRole, fmt.Sprintf("model=%s mode=%s cwd=%s", m.status.model, m.status.executionMode, m.status.cwd))
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
			return m, nil
		}
		model := strings.Join(cmd.Args, " ")
		if err := m.setModel(model); err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
		m.messages.append(systemRole, "model set to "+model)
		return m, nil
	case "mode":
		if len(cmd.Args) == 0 {
			m.messages.append(systemRole, "mode: "+m.status.executionMode)
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
		m.messages.append(systemRole, "mode set to "+string(mode))
		return m, nil
	default:
		m.messages.append(systemRole, "unknown slash command "+cmd.Name)
		return m, nil
	}
}

func waitForEventFromUpdate(event Event, m *Model) tea.Cmd {
	switch event.Type {
	case EventDeltaText:
		m.messages.appendAssistantDelta(event.Text)
		return waitForEvent(m.events)
	case EventToolCallStart:
		m.messages.appendToolStatus(event.ToolName, "started")
		return waitForEvent(m.events)
	case EventToolCallEnd:
		m.messages.appendToolStatus(event.ToolName, "finished")
		return waitForEvent(m.events)
	case EventDone:
		m.running = false
		m.status.running = false
		m.cancel = nil
		return nil
	case EventError:
		m.messages.append(errorRole, defaultString(event.Content, "agent failed"))
		m.running = false
		m.status.running = false
		m.cancel = nil
		return nil
	default:
		return waitForEvent(m.events)
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
	m.messages.append(systemRole, "mode set to "+next)
	return m, nil
}

func nextExecutionMode(current string) string {
	order := []string{
		string(execution.ModeDefault),
		string(execution.ModePlan),
		string(execution.ModeAgentApprove),
	}
	for i, mode := range order {
		if current == mode {
			return order[(i+1)%len(order)]
		}
	}
	return string(execution.ModeDefault)
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
	m.messages.load(state.Messages)
	m.messages.append(systemRole, "session set to "+state.ID)
	m.history = promptHistoryFromMessages(state.Messages)
	m.resetPromptHistoryNavigation()
	m.scrollToBottom()
	if strings.TrimSpace(state.Model) != "" {
		m.status.model = state.Model
		m.models = normalizeModels(m.models, state.Model)
	}
	m.status.turn = state.Turn
	return m, nil
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
	if strings.HasPrefix(raw, "/model ") {
		return m.modelSuggestions(strings.TrimSpace(strings.TrimPrefix(raw, "/model")), width)
	}
	matches := slash.Match(value)
	if len(matches) == 0 {
		return truncateText("  no matching slash command", width)
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
		b.WriteString(truncateText(fmt.Sprintf("%s%-34s %s", marker, spec.Usage, spec.Description), width))
	}
	return b.String()
}

func (m Model) pickerView(width int) string {
	if m.picker == nil {
		return ""
	}
	return m.picker.view(width)
}

func (m Model) sessionPickerView(width int) string {
	if m.sessions == nil {
		return ""
	}
	return m.sessions.view(width)
}

func (m Model) modelSuggestions(prefix string, width int) string {
	var b strings.Builder
	matches := 0
	for _, model := range m.models {
		if prefix != "" && !strings.Contains(strings.ToLower(model), strings.ToLower(prefix)) {
			continue
		}
		if matches > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(truncateText("  "+model, width))
		matches++
		if matches >= 8 {
			break
		}
	}
	if matches == 0 {
		return truncateText("  no matching model", width)
	}
	return b.String()
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

func (m Model) slashCommandMatches() []slash.Spec {
	raw := m.input.Value()
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "/") || slashInputHasArgs(raw) {
		return nil
	}
	return slash.Match(value)
}

func (m Model) selectedSlashIndex(matches []slash.Spec) int {
	if len(matches) == 0 {
		return 0
	}
	if m.slashIdx < 0 {
		return 0
	}
	if m.slashIdx >= len(matches) {
		return len(matches) - 1
	}
	return m.slashIdx
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
