package agent

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/context"
	"github.com/feimingxliu/ub/internal/hook"
	"github.com/feimingxliu/ub/internal/message"
	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/reasoning"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/workspace/filehistory"
	"github.com/feimingxliu/ub/internal/workspace/tooloutput"
)

// defaultMaxTurns caps how many tool-call iterations a single Run may take.
// A value <= 0 means the normal turn loop is unbounded and ends when the model
// stops asking for tools. Runaway protection comes from repeated-tool detection,
// context compaction, cancellation, and provider errors instead of a small fixed
// default that interrupts legitimate long tasks.
const defaultMaxTurns = 0

// Options configures an Agent.
type Options struct {
	Provider             provider.Provider
	Tools                *tool.Registry
	Permission           *permission.Manager
	Rollout              rollout.Writer
	Model                string
	Mode                 execmode.Mode
	ModeFunc             func() execmode.Mode
	PlanMode             PlanModeController
	MaxTurns             int
	LimitAsker           LimitAsker
	Asker                Asker
	Events               EventSink
	BackgroundEvents     EventSink
	Inject               <-chan string
	Reasoning            *reasoning.Config
	MaxContextTokens     int
	ContextWindow        *contextwindow.Resolver
	SummaryProvider      provider.Provider
	SummaryModel         string
	AutoMemoryProvider   provider.Provider
	AutoMemoryModel      string
	Context              config.ContextConfig
	Prompt               config.PromptConfig
	Runtime              RuntimeContext
	ToolOutputState      string
	Hooks                hook.Runner
	WorkspaceRoot        string
	MemoryMaxChars       int
	Memory               config.MemoryConfig
	MemoryAutoScheduler  *MemoryAutoScheduler
	SubagentRunner       tool.SubagentRunner
	FileHistory          *filehistory.Manager
	FileHistoryToolsOnly bool
}

// Agent runs a single headless agent loop: it sends messages to a provider,
// streams the response, dispatches any tool calls, persists events to the
// rollout, and repeats until the model produces a final reply or the turn
// limit is reached. Agent is designed to be lightweight and stateless between
// runs — conversation history lives in Request and the rollout, not in Agent
// fields. A Factory creates fresh Agent instances from a shared template.
type Agent struct {
	provider             provider.Provider
	tools                *tool.Registry
	toolDefinitionCache  map[execmode.Mode][]provider.ToolDefinition
	permission           *permission.Manager
	rollout              rollout.Writer
	model                string
	mode                 execmode.Mode
	modeFunc             func() execmode.Mode
	planMode             PlanModeController
	maxTurns             int
	limitAsker           LimitAsker
	asker                Asker
	events               EventSink
	backgroundEvents     EventSink
	inject               <-chan string
	reasoning            *reasoning.Config
	maxContextTokens     int
	contextWindow        *contextwindow.Resolver
	summaryProvider      provider.Provider
	summaryModel         string
	autoMemoryProvider   provider.Provider
	autoMemoryModel      string
	contextCfg           config.ContextConfig
	promptCfg            config.PromptConfig
	promptRegistry       *promptRegistry
	toolOutputState      string
	hooks                hook.Runner
	workspaceRoot        string
	memoryCfg            config.MemoryConfig
	memoryAutoScheduler  *MemoryAutoScheduler
	subagentRunner       tool.SubagentRunner
	fileHistory          *filehistory.Manager
	fileHistoryToolsOnly bool
}

// Request is one Agent run input. History and ContextHistory carry the
// conversation so far; Prompt is the new user message for this turn.
// ContextHistory may differ from History when context compaction has shrunk
// the message list sent to the provider. AutoTriggered marks prompts that
// the system injected automatically (e.g. goal continuation) so the TUI can
// exclude them from prompt-history navigation.
type Request struct {
	SessionID      string
	Turn           int
	History        []message.Message
	ContextHistory []message.Message
	Prompt         string
	AutoTriggered  bool
}

// Result is the final Agent run output. Text is the last assistant reply,
// Messages is the full transcript (including tool results), and
// ContextMessages is the potentially-compacted message slice that was sent
// to the provider on the last iteration.
type Result struct {
	Text            string
	Messages        []message.Message
	ContextMessages []message.Message
}

// toolCall captures one tool invocation parsed from a provider stream.
type toolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// streamResult holds the aggregated output of consuming one provider stream:
// accumulated text, the assembled assistant message, any tool calls, and the
// total reasoning character count (used for empty-response diagnostics).
type streamResult struct {
	text         string
	message      message.Message
	toolCalls    []toolCall
	reasoningLen int
}

// New constructs an Agent.
func New(opts Options) (*Agent, error) {
	if opts.Provider == nil {
		return nil, errors.New("agent provider is required")
	}
	if opts.Tools == nil {
		return nil, errors.New("agent tool registry is required")
	}
	mode, err := execmode.ParseMode(string(opts.Mode))
	if err != nil {
		return nil, err
	}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}
	toolOutputState := strings.TrimSpace(opts.ToolOutputState)
	if toolOutputState == "" {
		if stateRoot, err := tooloutput.StateRoot(); err == nil {
			toolOutputState = stateRoot
		}
	}
	hooks := opts.Hooks
	if hooks == nil {
		hooks = hook.NopRunner{}
	}
	runtime := opts.Runtime.normalized()
	workspaceRoot := strings.TrimSpace(opts.WorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = runtime.Workspace
	}
	promptCfg := effectivePromptConfig(opts.Prompt)
	memoryAutoScheduler := opts.MemoryAutoScheduler
	if memoryAutoScheduler == nil {
		memoryAutoScheduler = NewMemoryAutoScheduler()
	}
	return &Agent{
		provider:             opts.Provider,
		tools:                opts.Tools,
		toolDefinitionCache:  map[execmode.Mode][]provider.ToolDefinition{},
		permission:           opts.Permission,
		rollout:              opts.Rollout,
		model:                strings.TrimSpace(opts.Model),
		mode:                 mode,
		modeFunc:             opts.ModeFunc,
		planMode:             opts.PlanMode,
		maxTurns:             maxTurns,
		limitAsker:           opts.LimitAsker,
		asker:                opts.Asker,
		events:               opts.Events,
		backgroundEvents:     opts.BackgroundEvents,
		inject:               opts.Inject,
		reasoning:            cloneReasoning(opts.Reasoning),
		maxContextTokens:     opts.MaxContextTokens,
		contextWindow:        opts.ContextWindow,
		summaryProvider:      opts.SummaryProvider,
		summaryModel:         strings.TrimSpace(opts.SummaryModel),
		autoMemoryProvider:   opts.AutoMemoryProvider,
		autoMemoryModel:      strings.TrimSpace(opts.AutoMemoryModel),
		contextCfg:           opts.Context,
		promptCfg:            promptCfg,
		promptRegistry:       newPromptRegistry(runtime, workspaceRoot, promptCfg, opts.MemoryMaxChars),
		toolOutputState:      toolOutputState,
		hooks:                hooks,
		workspaceRoot:        workspaceRoot,
		memoryCfg:            opts.Memory,
		memoryAutoScheduler:  memoryAutoScheduler,
		subagentRunner:       opts.SubagentRunner,
		fileHistory:          opts.FileHistory,
		fileHistoryToolsOnly: opts.FileHistoryToolsOnly,
	}, nil
}

func cloneReasoning(cfg *reasoning.Config) *reasoning.Config {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}
