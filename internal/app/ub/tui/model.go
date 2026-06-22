// Package tui contains the Bubble Tea terminal interface for ub.
package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/textarea"

	permissiondialog "github.com/feimingxliu/ub/internal/app/ub/tui/dialog/permission"
	"github.com/feimingxliu/ub/internal/app/ub/tui/tuitheme"
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

// Model is the root Bubble Tea model for the chat shell.
type Model struct {
	input            textarea.Model
	messages         messageList
	status           statusBar
	styles           tuitheme.Styles
	runner           Runner
	permReqs         <-chan PermissionRequest
	pending          *PermissionRequest
	modal            permissiondialog.Model
	askReqs          <-chan AskRequest
	pendingAsk       *AskRequest
	askPrompt        askPromptModel
	planModeReqs     <-chan PlanModeRequest
	pendingPlanMode  *PlanModeRequest
	planModePrompt   planModePromptModel
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
	backgroundQueue  []backgroundTranscriptMessage
}

type backgroundTranscriptMessage struct {
	role string
	text string
}
