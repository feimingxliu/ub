package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"

	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/tui/theme"
)

// NewModel creates the root TUI model.
func NewModel(opts Options) Model {
	styles := tuitheme.ForTheme(opts.Theme)
	width, height := normalizedWindowSize(opts.initialWidth, opts.initialHeight)
	input := textarea.New()
	input.Placeholder = "Type a message or /help (Ctrl+J newline)"
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.EndOfBufferCharacter = ' '
	input.SetStyles(textareaTextStyles())
	input.SetVirtualCursor(true)
	input.DynamicHeight = true
	input.MinHeight = 1
	input.MaxHeight = inputMaxHeight(height)
	input.SetWidth(inputContentWidth(width))
	input.KeyMap = inputKeyMap()
	input.SetHeight(1)
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
		askReqs:          opts.Asks,
		planModeReqs:     opts.PlanModes,
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
			executionMode: defaultString(opts.ExecutionMode, string(execmode.ModeWork)),
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
