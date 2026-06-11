// Package tui contains the Bubble Tea terminal interface for ub.
package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-runewidth"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	permissiondialog "github.com/feimingxliu/ub/internal/app/ub/tui/dialog/permission"
	"github.com/feimingxliu/ub/internal/app/ub/tui/slash"
	"github.com/feimingxliu/ub/internal/app/ub/tui/tuitheme"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/runtime/permission"
	"github.com/feimingxliu/ub/internal/pkg/tool/plan"
)

const minRecommendedWidth = 80

// escInterruptConfirmWindow is the time window within which a second Esc press
// interrupts a running model. It matches the visible toast lifetime so the hint
// stays true while it is on screen.
const escInterruptConfirmWindow = toastTTL

const initCommandPrompt = `You are running ub /init.

Create or update AGENTS.md in the current workspace root so future AI coding agent sessions have concise, accurate repository guidance.

Process:
1. Inspect the repository before editing. Read AGENTS.md if it exists, plus high-signal files such as README, CONTRIBUTING, package manifests, Makefile/justfile, docs, and a shallow source/test layout.
2. Capture only guidance future coding agents need: project overview, important directories, build/test/lint commands, coding style, validation expectations, documentation/release notes, and repository-specific safety or workflow gotchas.
3. If AGENTS.md already exists, improve it in place. Preserve accurate human-authored guidance, remove stale or generic generated content when appropriate, and keep the result coherent rather than appending a managed block.
4. If AGENTS.md does not exist, create it.
5. Use AGENTS.md as the only target. Do not create or update CLAUDE.md, .ub/instructions.md, or other instruction files.
6. Keep the file concise, actionable, and safe to commit. Do not include secrets, private local configuration, or unnecessary absolute paths.
7. Finish by summarizing what you inspected, what changed in AGENTS.md, and any assumptions the user should review.`

// Options configures the initial TUI shell.
type Options struct {
	Input            io.Reader
	Output           io.Writer
	Context          context.Context
	Runner           Runner
	Permissions      <-chan PermissionRequest
	Limits           <-chan LimitRequest
	BackgroundEvents <-chan Event
	Provider         string
	Providers        []string
	Model            string
	Models           []string
	Effort           string
	Efforts          []string
	ApprovalModel    string
	ApprovalModels   []string
	SmallModel       string
	SmallModels      []string
	Messages         []InitialMessage
	Turn             int
	ExecutionMode    string
	Cwd              string
	Theme            string
	EventTimeout     time.Duration
	SelectSession    bool
	Clipboard        Clipboard
	LoadMessages     func(context.Context) ([]InitialMessage, error)
	initialWidth     int
	initialHeight    int
}

// Run starts the terminal UI and blocks until it exits.
func Run(ctx context.Context, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	width, height := detectInitialWindowSize(opts.Output)
	opts.initialWidth = width
	opts.initialHeight = height
	programOpts := []tea.ProgramOption{tea.WithContext(ctx)}
	programOpts = append(programOpts, tea.WithWindowSize(width, height))
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
	input            textinput.Model
	messages         messageList
	status           statusBar
	styles           tuitheme.Styles
	runner           Runner
	permReqs         <-chan PermissionRequest
	pending          *PermissionRequest
	modal            permissiondialog.Model
	limitReqs        <-chan LimitRequest
	pendingLimit     *LimitRequest
	backgroundEvents <-chan Event
	ctx              context.Context
	cancel           context.CancelFunc
	running          bool
	events           <-chan Event
	providers        []string
	models           []string
	efforts          []string
	approvalModel    string
	approvalModels   []string
	smallModel       string
	smallModels      []string
	picker           *modelPicker
	pickerTarget     string
	sessions         *sessionPicker
	plans            *planPicker
	rewind           *rewindPicker
	files            *filePicker
	slashIdx         int
	history          []string
	histIdx          int
	draft            string
	queuedPrompts    []string
	queueIdx         int
	queueDraft       string
	scroll           int
	runID            int
	timeout          time.Duration
	width            int
	height           int
	spinnerFrame     int
	runStartedAt     time.Time
	activitySummary  string
	toast            toastState
	btw              sideQuestionState
	clipboard        Clipboard
	loadMessages     func(context.Context) ([]InitialMessage, error)
	loadingMessages  bool
	lastEscTime      time.Time
	lastEscRunID     int
}

// NewModel creates the root TUI model.
func NewModel(opts Options) Model {
	styles := tuitheme.ForTheme(opts.Theme)
	width, height := normalizedWindowSize(opts.initialWidth, opts.initialHeight)
	input := textinput.New()
	input.Placeholder = "Type a message or /help"
	input.Prompt = "› "
	input.SetStyles(inputTextStyles())
	input.SetVirtualCursor(false)
	input.SetWidth(inputWidthForTerminal(width, input.Prompt))
	_ = input.Focus()
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	providerName := strings.TrimSpace(opts.Provider)
	providers := opts.Providers
	if providerRunner, ok := opts.Runner.(ProviderControlRunner); ok {
		if providerName == "" {
			providerName = providerRunner.Provider()
		}
		if len(providers) == 0 {
			providers = providerRunner.Providers()
		}
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
	smallModel := strings.TrimSpace(opts.SmallModel)
	smallModels := opts.SmallModels
	if smallRunner, ok := opts.Runner.(SmallModelControlRunner); ok {
		if smallModel == "" {
			smallModel = smallRunner.SmallModel()
		}
		if len(smallModels) == 0 {
			smallModels = smallRunner.SmallModels()
		}
	}

	m := Model{
		input:            input,
		messages:         newMessageList(),
		styles:           styles,
		runner:           opts.Runner,
		clipboard:        opts.Clipboard,
		permReqs:         opts.Permissions,
		limitReqs:        opts.Limits,
		backgroundEvents: opts.BackgroundEvents,
		ctx:              ctx,
		providers:        normalizeOptions(providers, providerName),
		models:           normalizeModels(models, modelName),
		efforts:          normalizeOptions(efforts, effort),
		approvalModel:    approvalModel,
		approvalModels:   normalizeModels(approvalModels, approvalModel),
		smallModel:       smallModel,
		smallModels:      normalizeModels(smallModels, smallModel),
		history:          promptHistoryFromMessages(opts.Messages),
		histIdx:          -1,
		queueIdx:         -1,
		timeout:          opts.EventTimeout,
		loadMessages:     opts.LoadMessages,
		loadingMessages:  opts.LoadMessages != nil,
		width:            width,
		height:           height,
		btw:              newSideQuestionState(),
		status: statusBar{
			provider:      defaultString(providerName, "unknown"),
			model:         modelName,
			effort:        effort,
			executionMode: defaultString(opts.ExecutionMode, string(execution.ModeWork)),
			cwd:           defaultString(opts.Cwd, "."),
			turn:          opts.Turn,
			state:         statusIdle,
		},
	}
	if m.clipboard == nil {
		m.clipboard = systemClipboard{}
	}
	m.messages.load(opts.Messages)
	if m.loadingMessages && len(opts.Messages) == 0 {
		m.messages.append(systemRole, "loading session history...")
	}
	if opts.initialWidth > 0 && opts.initialWidth < minRecommendedWidth {
		m.messages.append(systemRole, fmt.Sprintf("terminal width is %d columns; ub works best at %d columns or wider", opts.initialWidth, minRecommendedWidth))
	}
	if opts.SelectSession {
		updated, _ := m.openSessionPicker()
		if selected, ok := updated.(Model); ok {
			m = selected
		}
	}
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{windowSizeCmd(m.width, m.height), requestWindowSize(), waitForPermission(m.permReqs), waitForLimit(m.limitReqs), refreshModelLists(m.ctx, m.runner)}
	if m.backgroundEvents != nil {
		cmds = append(cmds, waitForBackgroundEvent(m.backgroundEvents))
	}
	if m.loadMessages != nil {
		cmds = append(cmds, loadMessagesCmd(m.ctx, m.loadMessages))
	}
	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case streamEventMsg:
		return m.handleStreamEvent(msg)
	case sideQuestionEventMsg:
		return m.handleSideQuestionEvent(msg)
	case backgroundEventMsg:
		return m.handleBackgroundEvent(msg)
	case permissionRequestMsg:
		return m.handlePermissionRequest(msg)
	case limitRequestMsg:
		return m.handleLimitRequest(msg)
	case spinnerTickMsg:
		return m.handleSpinnerTick(msg)
	case toastExpireMsg:
		m.handleToastExpire(msg)
		return m, nil
	case doctorResultMsg:
		return m.handleDoctorResult(msg)
	case planEditFinishedMsg:
		return m.handlePlanEditFinished(msg)
	case copyResultMsg:
		return m.handleCopyResult(msg)
	case modelRefreshResultMsg:
		return m.handleModelRefreshResult(msg)
	case messagesLoadedMsg:
		return m.handleMessagesLoaded(msg)
	}
	m.clearToastForInteraction(msg)
	m.clearEscInterruptConfirmForInteraction(msg)

	if m.pendingLimit != nil {
		if mouseMsg, ok := msg.(tea.MouseWheelMsg); ok {
			switch mouseMsg.Mouse().Button {
			case tea.MouseWheelUp:
				m.scrollMessages(3)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollMessages(-3)
				return m, nil
			}
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+home":
				m.scrollToTop()
				return m, nil
			case "ctrl+end":
				m.scrollToBottom()
				return m, nil
			case "y", "Y", "enter":
				return m.resolveLimit(defaultLimitExtension)
			case "n", "N", "esc", "ctrl+c":
				return m.resolveLimit(0)
			}
		}
		return m, nil
	}

	if m.pending != nil {
		if mouseMsg, ok := msg.(tea.MouseWheelMsg); ok {
			switch mouseMsg.Mouse().Button {
			case tea.MouseWheelUp:
				m.scrollMessages(3)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollMessages(-3)
				return m, nil
			}
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				if m.confirmEscInterrupt(key) {
					m.interruptCurrent()
					return m, waitForPermission(m.permReqs)
				}
				return m, m.showToast(toastNotice, "press Esc again to interrupt")
			case "shift+tab":
				m.clearEscInterruptConfirm()
				return m.cycleMode()
			case "ctrl+home":
				m.scrollToTop()
				return m, nil
			case "ctrl+end":
				m.scrollToBottom()
				return m, nil
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
		if key, ok := msg.(tea.KeyPressMsg); ok {
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
				if target == "small" {
					if err := m.setSmallModel(selected); err != nil {
						m.messages.append(systemRole, err.Error())
						return m, nil
					}
					m.messages.append(systemRole, "small model set to "+selected)
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
				if target == "provider" {
					if err := m.setProvider(selected, ""); err != nil {
						m.messages.append(systemRole, err.Error())
						return m, nil
					}
					m.messages.append(systemRole, "provider set to "+m.status.provider+" model "+m.status.model)
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

	if m.plans != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				m.plans = nil
				return m, nil
			case "up", "k":
				m.plans.previous()
				return m, nil
			case "down", "j", "tab":
				m.plans.next()
				return m, nil
			case "backspace", "delete":
				m.plans.backspace()
				return m, nil
			case "ctrl+u":
				m.plans.clearQuery()
				return m, nil
			case "enter":
				selected := m.plans.selected()
				if selected.ID == "" {
					return m, nil
				}
				m.plans = nil
				return m.editPlanArtifact([]string{selected.ID})
			}
			for _, r := range key.Text {
				m.plans.appendRune(r)
			}
		}
		return m, nil
	}

	if m.rewind != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				m.rewind = nil
				return m, nil
			case "up", "k":
				m.rewind.previous()
				return m, nil
			case "down", "j", "tab":
				m.rewind.next()
				return m, nil
			case "backspace", "delete":
				m.rewind.backspace()
				return m, nil
			case "ctrl+u":
				m.rewind.clearQuery()
				return m, nil
			case "enter":
				if m.rewind.phase == rewindPickerMode {
					target := m.rewind.chosen
					mode := m.rewind.selectedMode()
					m.rewind = nil
					return m.applyRewind(target, mode.revertFiles)
				}
				target := m.rewind.selectedTarget()
				if target.Turn <= 0 {
					return m, nil
				}
				if len(target.AffectedFiles) > 0 {
					m.rewind.chooseTarget(target)
					return m, nil
				}
				m.rewind = nil
				return m.applyRewind(target, false)
			}
			for _, r := range key.Text {
				m.rewind.appendRune(r)
			}
		}
		return m, nil
	}

	if m.sessions != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
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
			case "backspace", "delete":
				m.sessions.backspace()
				return m, nil
			case "ctrl+u":
				m.sessions.clearQuery()
				return m, nil
			case "enter":
				selected := m.sessions.selected()
				if selected.ID == "" {
					return m, nil
				}
				m.sessions = nil
				return m.switchSession(selected.ID)
			}
			for _, r := range key.Text {
				m.sessions.appendRune(r)
			}
		}
		return m, nil
	}

	if m.btw.visible {
		switch msg := msg.(type) {
		case tea.MouseWheelMsg:
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				m.scrollSideQuestion(sideQuestionWheelScrollLines)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollSideQuestion(-sideQuestionWheelScrollLines)
				return m, nil
			}
		case tea.MouseClickMsg, tea.MouseReleaseMsg:
			return m, nil
		case tea.KeyPressMsg:
			if updated, cmd, handled := m.handleSideQuestionKey(msg); handled {
				return updated, cmd
			}
		}
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft {
			if m.statusHelpHit(mouse.X, mouse.Y) {
				return m.showCheatsheet()
			}
			if m.toggleMessageAt(mouse.X, mouse.Y) {
				m.scrollFocusedMessageIntoView()
				return m, nil
			}
		}
	case tea.MouseWheelMsg:
		switch msg.Mouse().Button {
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
		m.input.SetWidth(inputWidthForTerminal(msg.Width, m.input.Prompt))
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "?":
			return m.showCheatsheet()
		case "esc":
			if m.files != nil {
				m.files = nil
				m.clearEscInterruptConfirm()
				return m, nil
			}
			if !m.running && m.messages.clearFocus() {
				m.clearEscInterruptConfirm()
				return m, nil
			}
			if m.running {
				if m.confirmEscInterrupt(msg) {
					m.interruptCurrent()
				} else {
					return m, m.showToast(toastNotice, "press Esc again to interrupt")
				}
			}
			return m, nil
		case "pgup":
			m.scrollMessages(m.pageScrollLines())
			return m, nil
		case "pgdown":
			m.scrollMessages(-m.pageScrollLines())
			return m, nil
		case "ctrl+home":
			m.scrollToTop()
			return m, nil
		case "ctrl+end":
			m.scrollToBottom()
			return m, nil
		case "ctrl+o":
			if m.messages.toggleLatestCollapsible() {
				m.scrollFocusedMessageIntoView()
				return m, nil
			}
		case "ctrl+n":
			if m.messages.focusNextCollapsible() {
				m.scrollFocusedMessageIntoView()
				return m, nil
			}
		case "ctrl+p":
			if m.messages.focusPreviousCollapsible() {
				m.scrollFocusedMessageIntoView()
				return m, nil
			}
		case "up":
			if m.moveFileSelection(-1) {
				return m, nil
			}
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
			if m.moveFileSelection(1) {
				return m, nil
			}
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
			if m.completeFileMention() {
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
			if m.loadingMessages {
				return m, nil
			}
			if strings.TrimSpace(m.input.Value()) == "" && m.messages.hasFocusedCollapsible() {
				if m.messages.toggleFocusedCollapsible() {
					m.scrollFocusedMessageIntoView()
					return m, nil
				}
			}
			if m.running {
				if text := strings.TrimSpace(m.input.Value()); isSideQuestionInput(text) {
					m.input.SetValue("")
					m.files = nil
					m.resetPromptHistoryNavigation()
					return m.executeSlash(text)
				}
				if m.queueInput() {
					return m, nil
				}
				return m, nil
			}
			if m.completeFileMention() {
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
					m.files = nil
					m.resetPromptHistoryNavigation()
					return m.executeSlash(text)
				}
				if isShellInput(text) {
					return m.startShell(text, true)
				}
				return m.startPrompt(text, true)
			}
			return m, nil
		case "space":
			if strings.TrimSpace(m.input.Value()) == "" && m.messages.hasFocusedCollapsible() {
				if m.messages.toggleFocusedCollapsible() {
					m.scrollFocusedMessageIntoView()
					return m, nil
				}
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.saveQueuedPromptEdit()
	m.refreshFilePicker()
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
		m.clearEscInterruptConfirm()
		return m.startNextQueuedPrompt()
	}
	cmd := waitForEventFromUpdate(msg.event, &m)
	return m, cmd
}

func (m Model) handleBackgroundEvent(msg backgroundEventMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	switch msg.event.Type {
	case EventActivity:
		text := strings.TrimSpace(msg.event.Summary)
		if text == "" {
			text = strings.TrimSpace(msg.event.Content)
		}
		if text != "" {
			role := systemRole
			if msg.event.IsError {
				role = errorRole
			}
			m.messages.append(role, text)
			m.scrollToBottom()
		}
	case EventError:
		text := strings.TrimSpace(msg.event.Content)
		if text == "" && msg.event.Err != nil {
			text = msg.event.Err.Error()
		}
		m.messages.append(errorRole, defaultString(text, "background task failed"))
		m.scrollToBottom()
	}
	return m, waitForBackgroundEvent(m.backgroundEvents)
}

func (m Model) handlePermissionRequest(msg permissionRequestMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	m.pending = &msg.request
	m.modal = permissiondialog.New(msg.request.Request)
	return m, nil
}

func (m Model) handleLimitRequest(msg limitRequestMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	m.pendingLimit = &msg.request
	return m, nil
}

func (m Model) resolveLimit(extra int) (tea.Model, tea.Cmd) {
	if m.pendingLimit != nil && m.pendingLimit.Response != nil {
		m.pendingLimit.Response <- agent.LimitExtensionResponse{ExtraTurns: extra}
	}
	m.pendingLimit = nil
	return m, waitForLimit(m.limitReqs)
}

func (m Model) handleSpinnerTick(_ spinnerTickMsg) (tea.Model, tea.Cmd) {
	if !m.running {
		return m, nil
	}
	m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
	return m, spinnerTickCmd()
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

func (m Model) footerView(width int) string {
	return strings.Join(m.footerFrame(width).lines, "\n")
}

func (m Model) showCheatsheet() (tea.Model, tea.Cmd) {
	m.messages.append(systemRole, slashHelp())
	m.scrollToBottom()
	return m, nil
}

func (m Model) statusHelpHit(x, y int) bool {
	if y != frameHeight(m.height)-1 {
		return false
	}
	return m.status.helpHit(contentWidth(m.width), m.styles, x)
}

func inputWidthForTerminal(width int, prompt string) int {
	available := contentWidth(width) - runewidth.StringWidth(prompt) - 1
	return max(1, available)
}

func detectInitialWindowSize(output io.Writer) (int, int) {
	// Precedence follows the standard Unix convention: COLUMNS/LINES override
	// auto-detection. This lets users opt into a fixed window (e.g. for tests
	// or recordings) without the auto-detect path silently clamping it back
	// to the real terminal size.
	if envWidth, envHeight, ok := envWindowSize(); ok {
		return normalizedWindowSize(envWidth, envHeight)
	}
	if width, height, ok := terminalWindowSize(output); ok {
		return normalizedWindowSize(width, height)
	}
	return normalizedWindowSize(defaultViewWidth, defaultViewHeight)
}

func terminalWindowSize(output io.Writer) (int, int, bool) {
	if output == nil {
		output = os.Stdout
	}
	file, ok := output.(interface{ Fd() uintptr })
	if !ok {
		return 0, 0, false
	}
	width, height, err := term.GetSize(file.Fd())
	if err != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func envWindowSize() (int, int, bool) {
	width, errWidth := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS")))
	height, errHeight := strconv.Atoi(strings.TrimSpace(os.Getenv("LINES")))
	if errWidth != nil || errHeight != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func normalizedWindowSize(width, height int) (int, int) {
	return contentWidth(width), frameHeight(height)
}

func windowSizeCmd(width, height int) tea.Cmd {
	width, height = normalizedWindowSize(width, height)
	return func() tea.Msg {
		return tea.WindowSizeMsg{Width: width, Height: height}
	}
}

func requestWindowSize() tea.Cmd {
	return func() tea.Msg {
		return tea.RequestWindowSize()
	}
}

func inputTextStyles() textinput.Styles {
	styles := textinput.DefaultDarkStyles()
	prompt := lipgloss.NewStyle().Foreground(lipgloss.Color("43")).Bold(true)
	text := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	placeholder := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
	styles.Focused.Prompt = prompt
	styles.Focused.Text = text
	styles.Focused.Placeholder = placeholder
	styles.Blurred.Prompt = prompt
	styles.Blurred.Text = text
	styles.Blurred.Placeholder = placeholder
	styles.Cursor.Color = lipgloss.Color("43")
	styles.Cursor.Shape = tea.CursorBlock
	styles.Cursor.Blink = false
	return styles
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

func toolPartialActivity(event Event) Event {
	return Event{
		Type:            EventActivity,
		ActivityKind:    "tool",
		ToolUseID:       event.ToolUseID,
		ToolName:        event.ToolName,
		ParentToolUseID: event.ParentToolUseID,
		SubagentID:      event.SubagentID,
		Status:          "running",
		Summary:         event.Summary,
		Content:         event.Content,
		IsError:         event.IsError,
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
	text := fmt.Sprintf("Permission %s %s %s", source, decision, toolName)
	if reason := strings.TrimSpace(event.Reason); reason != "" {
		text += ": " + reason
	}
	return text
}

func activityEventText(event Event) string {
	prefix := subagentActivityPrefix(event)
	switch strings.TrimSpace(event.ActivityKind) {
	case "thinking":
		return prefix + "thinking: " + defaultString(event.Summary, event.Text)
	case "tool":
		return prefix + toolActivityText(event)
	case "permission":
		return prefix + permissionEventText(event)
	case "notice":
		return prefix + "notice: " + defaultString(event.Summary, event.Text)
	default:
		return prefix + defaultString(event.Summary, defaultString(event.Content, "activity"))
	}
}

func activityEventKey(event Event) string {
	subagentID := strings.TrimSpace(event.SubagentID)
	switch strings.TrimSpace(event.ActivityKind) {
	case "tool":
		if strings.TrimSpace(event.ToolUseID) != "" {
			return "tool:" + event.ToolUseID
		}
	case "thinking":
		if subagentID != "" {
			return "subagent:" + subagentID + ":thinking"
		}
		return "thinking"
	}
	return ""
}

func subagentActivityPrefix(event Event) string {
	if strings.TrimSpace(event.SubagentID) == "" {
		return ""
	}
	return "subagent: "
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
	title := toolTitle(name, toolTitleSummary(event))
	switch toolEventStatus(event) {
	case "queued", "running":
		action := toolAction(name)
		if summary := strings.TrimSpace(event.Summary); summary != "" {
			return action + " " + summary
		}
		return action
	case "failed":
		return title + " failed"
	default:
		return title
	}
}

func toolTitleSummary(event Event) string {
	name := strings.TrimSpace(event.ToolName)
	if isPlanArtifactTool(name) {
		if planID := planIDFromToolResult(event.Content); planID != "" {
			return planID
		}
	}
	return event.Summary
}

func isPlanArtifactTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "plan_write", "plan_update":
		return true
	default:
		return false
	}
}

func planIDFromToolResult(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if id, ok := strings.CutPrefix(strings.TrimSpace(line), "plan_id="); ok {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

func toolEventStatus(event Event) string {
	status := strings.TrimSpace(event.Status)
	if status == "" && event.IsError {
		return "failed"
	}
	return status
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
	case "multiedit":
		return "Preparing multi-edit..."
	case "bash":
		return "Writing command..."
	case "task":
		return "Running Task..."
	case "remember":
		return "Writing memory..."
	case "plan_write":
		return "Writing plan..."
	case "plan_update":
		return "Updating plan..."
	case "plan_update_step":
		return "Updating plan step..."
	case "todo_write":
		return "Writing todos..."
	case "todo_update":
		return "Updating todos..."
	case "tool_result":
		return "Reading tool result..."
	case "diagnostics":
		return "Checking diagnostics..."
	case "references":
		return "Finding references..."
	case "hover":
		return "Reading hover..."
	case "completion":
		return "Getting completions..."
	case "document_symbols":
		return "Listing document symbols..."
	case "rename":
		return "Preparing rename..."
	case "code_action":
		return "Listing code actions..."
	case "job_run":
		return "Starting job..."
	case "job_output":
		return "Reading job output..."
	case "job_kill":
		return "Stopping job..."
	default:
		if display, ok := mcpToolDisplayName(name); ok {
			return "Calling " + display + "..."
		}
		if name := strings.TrimSpace(name); name != "" {
			return "Running " + name + "..."
		}
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
	case "multiedit":
		verb = "Edited multiple files"
	case "bash":
		verb = "Ran"
	case "task":
		verb = "Ran Task"
	case "remember":
		verb = "Remembered"
	case "plan_write":
		verb = "Wrote plan"
	case "plan_update":
		verb = "Updated plan"
	case "plan_update_step":
		verb = "Updated plan step"
	case "todo_write":
		verb = "Wrote todos"
	case "todo_update":
		verb = "Updated todos"
	case "tool_result":
		verb = "Read tool result"
	case "diagnostics":
		verb = "Checked diagnostics"
	case "references":
		verb = "Found references"
	case "hover":
		verb = "Read hover"
	case "completion":
		verb = "Got completions"
	case "document_symbols":
		verb = "Listed document symbols"
	case "rename":
		verb = "Prepared rename"
	case "code_action":
		verb = "Listed code actions"
	case "job_run":
		verb = "Started job"
	case "job_output":
		verb = "Read job output"
	case "job_kill":
		verb = "Stopped job"
	default:
		if display, ok := mcpToolDisplayName(name); ok {
			verb = "Called " + display
			break
		}
		if strings.TrimSpace(name) != "" {
			verb = "Ran " + name
		}
	}
	if summary == "" {
		return verb
	}
	return verb + " " + summary
}

func mcpToolDisplayName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, "mcp__") {
		return "", false
	}
	parts := strings.SplitN(name, "__", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return "MCP tool", true
	}
	return "MCP " + parts[1] + "/" + parts[2], true
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
		string(execution.ModeFullAccess),
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

func (m *Model) setModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model cannot be empty")
	}
	if runner, ok := m.runner.(ControlRunner); ok {
		if err := runner.SetModel(model); err != nil {
			return err
		}
		m.models = normalizeModels(runner.Models(), model)
	} else if !modelAllowed(m.models, model) {
		return fmt.Errorf("model %q is not available for the current provider; use /model to list candidates", model)
	}
	m.status.model = model
	m.refreshEffortFromRunner()
	return nil
}

func (m *Model) setProvider(providerName, model string) error {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return fmt.Errorf("provider cannot be empty")
	}
	if !modelAllowed(m.providers, providerName) {
		return fmt.Errorf("provider %q is not available; use /provider to list candidates", providerName)
	}
	runner, ok := m.runner.(ProviderControlRunner)
	if !ok {
		return fmt.Errorf("provider switching is unavailable in this runner")
	}
	state, err := runner.SetProvider(providerName, strings.TrimSpace(model))
	if err != nil {
		return err
	}
	if state.Provider == "" {
		state.Provider = providerName
	}
	m.status.provider = state.Provider
	m.providers = normalizeOptions(state.Providers, state.Provider)
	if state.Model != "" {
		m.status.model = state.Model
		m.models = normalizeModels(state.Models, state.Model)
	} else {
		m.status.model = "unknown"
		m.models = append([]string(nil), state.Models...)
	}
	if state.Effort != "" || len(state.Efforts) > 0 {
		m.status.effort = defaultString(state.Effort, "none")
		m.efforts = normalizeOptions(state.Efforts, m.status.effort)
	} else {
		m.refreshEffortFromRunner()
	}
	m.refreshApprovalModelFromRunner()
	m.refreshSmallModelFromRunner()
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

func (m *Model) refreshApprovalModelFromRunner() {
	runner, ok := m.runner.(ApprovalControlRunner)
	if !ok {
		return
	}
	model := strings.TrimSpace(runner.ApprovalModel())
	m.approvalModel = model
	m.approvalModels = normalizeModels(runner.ApprovalModels(), model)
}

func (m *Model) refreshSmallModelFromRunner() {
	runner, ok := m.runner.(SmallModelControlRunner)
	if !ok {
		return
	}
	model := strings.TrimSpace(runner.SmallModel())
	m.smallModel = model
	m.smallModels = normalizeModels(runner.SmallModels(), model)
}

func (m *Model) setApprovalModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("approval model cannot be empty")
	}
	if runner, ok := m.runner.(ApprovalControlRunner); ok {
		if err := runner.SetApprovalModel(model); err != nil {
			return err
		}
		m.approvalModels = normalizeModels(runner.ApprovalModels(), model)
	} else if !modelAllowed(m.approvalModels, model) {
		return fmt.Errorf("approval model %q is not available for the current approval provider; use /approval-model to list candidates", model)
	}
	m.approvalModel = model
	return nil
}

func (m *Model) setSmallModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("small model cannot be empty")
	}
	if runner, ok := m.runner.(SmallModelControlRunner); ok {
		if err := runner.SetSmallModel(model); err != nil {
			return err
		}
		m.smallModels = normalizeModels(runner.SmallModels(), model)
	} else if !modelAllowed(m.smallModels, model) {
		return fmt.Errorf("small model %q is not available for the current provider; use /small-model to list candidates", model)
	}
	m.smallModel = model
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
	}
}

func helpKeyboardLines() []string {
	return []string{
		"Enter - send prompt; while running, queue a normal prompt; with a selected candidate, accept it",
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
		"Up/Down - move through suggestions, queued prompts, or prompt history",
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

func (m Model) planPickerView(width int) string {
	if m.plans == nil {
		return ""
	}
	return m.plans.view(width, m.styles)
}

func (m Model) rewindPickerView(width int) string {
	if m.rewind == nil {
		return ""
	}
	return m.rewind.view(width, m.styles)
}

func (m Model) filePickerView(width int) string {
	if m.files == nil {
		return ""
	}
	return m.files.view(width, m.styles)
}

func (m Model) shellHintView(width int) string {
	value := strings.TrimSpace(m.input.Value())
	if !isShellInput(value) {
		return ""
	}
	command := strings.TrimSpace(strings.TrimPrefix(value, "!"))
	label := "shell mode · enter runs locally"
	if command == "" {
		label = "shell mode · type a command, enter runs locally"
	}
	if cwd := strings.TrimSpace(m.status.cwd); cwd != "" {
		label += " · cwd " + cwd
	}
	return m.styles.Render(m.styles.Picker.Title, truncateText(label, width))
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

func (m *Model) completeFileMention() bool {
	if m.files == nil {
		return false
	}
	selected := m.files.selected()
	if strings.TrimSpace(selected) == "" {
		return false
	}
	token, ok := activeFileMention(m.input.Value(), m.input.Position())
	if !ok {
		m.files = nil
		return false
	}
	next, cursor := insertFileMention(m.input.Value(), token, selected)
	m.input.SetValue(next)
	m.input.SetCursor(cursor)
	m.files = nil
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

func (m *Model) moveFileSelection(delta int) bool {
	if m.files == nil {
		return false
	}
	if delta < 0 {
		m.files.previous()
	} else {
		m.files.next()
	}
	return true
}

func (m *Model) refreshFilePicker() {
	value := m.input.Value()
	if strings.HasPrefix(strings.TrimSpace(value), "/") {
		m.files = nil
		return
	}
	token, ok := activeFileMention(value, m.input.Position())
	if !ok {
		m.files = nil
		return
	}
	runner, ok := m.runner.(WorkspaceFileRunner)
	if !ok {
		m.files = newFilePicker(nil, token.prefix, fmt.Errorf("file selection is unavailable in this runner"))
		return
	}
	files, err := runner.ListWorkspaceFiles(m.ctx, token.prefix, maxFileMentionCandidates)
	m.files = newFilePicker(files, token.prefix, err)
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
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "provider"); ok {
		return slashValueSource{command: "provider", label: "provider", prefix: prefix, values: m.providers}, true
	}
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "model"); ok {
		return slashValueSource{command: "model", label: "model", prefix: prefix, values: m.models}, true
	}
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "effort"); ok {
		return slashValueSource{command: "effort", label: "effort", prefix: prefix, values: m.efforts}, true
	}
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "approval-model"); ok {
		return slashValueSource{command: "approval-model", label: "approval model", prefix: prefix, values: m.approvalModels}, true
	}
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "small-model"); ok {
		return slashValueSource{command: "small-model", label: "small model", prefix: prefix, values: m.smallModels}, true
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
	if isShellInput(text) {
		return false
	}
	if m.queueIdx >= 0 && m.queueIdx < len(m.queuedPrompts) {
		m.queuedPrompts[m.queueIdx] = text
		m.resetQueuedPromptNavigation()
	} else {
		m.queuedPrompts = append(m.queuedPrompts, text)
	}
	m.input.SetValue("")
	m.files = nil
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
	rail := "┃ "
	bodyWidth := max(1, contentWidth(width)-runewidth.StringWidth(rail)-2)
	body := truncateText(fmt.Sprintf("%s%s%s: %s", prefix, statusSeparator, label, m.queuedPrompts[index]), bodyWidth)
	return m.styles.Render(m.styles.SubtleLine, rail) + m.styles.Render(m.styles.Status.Segment, body)
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

func (m *Model) scrollToTop() {
	m.scroll = m.maxMessageScroll()
}

func (m *Model) scrollFocusedMessageIntoView() {
	width := contentWidth(m.width)
	footer := m.footerView(width)
	height := m.messageViewHeight(footer)
	if height <= 0 {
		return
	}
	line, total, ok := m.messages.focusedLine(width, m.styles)
	if !ok {
		return
	}
	if total <= height {
		m.scroll = 0
		return
	}
	maxScroll := total - height
	start := visibleStart(total, height, m.clampedScroll())
	switch {
	case line < start:
		m.scroll = maxScroll - line
	case line >= start+height:
		m.scroll = maxScroll - (line - height + 1)
	default:
		return
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
}

func (m Model) clampedScroll() int {
	if m.scroll <= 0 {
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
	lines := len(m.messages.render(width, m.styles).lines)
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
	return max(1, frameHeight(m.height)-lineCount(footer)-2)
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
