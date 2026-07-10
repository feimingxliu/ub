package tui

import (
	"context"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Options configures the initial TUI shell.
type Options struct {
	Input            io.Reader
	Output           io.Writer
	Context          context.Context
	Runner           Runner
	Permissions      <-chan PermissionRequest
	Asks             <-chan AskRequest
	PlanModes        <-chan PlanModeRequest
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
