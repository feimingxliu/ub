package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/rollout"
	contextmgr "github.com/feimingxliu/ub/internal/tokenizer"
	"github.com/feimingxliu/ub/internal/tool"
	memorypkg "github.com/feimingxliu/ub/internal/workspace/memory"
)

const autoMemoryPromptTemplate = `Decide whether this coding-agent turn contains durable memory worth saving for future sessions.

Return ONLY JSON in this shape:
{"memories":[{"category":"preference|project|pattern|decision|debug|general","text":"short durable fact"}]}

<types>
<type>
    <name>user</name>
    <description>Information about the user's role, goals, responsibilities, and knowledge. Helps tailor future behavior to the user's preferences and perspective.</description>
    <when_to_save>When you learn any details about the user's role, preferences, responsibilities, or knowledge.</when_to_save>
    <maps_to>preference</maps_to>
    <examples>
- user: I'm a data scientist investigating what logging we have in place
- save: [preference] user is a data scientist, currently focused on observability/logging

- user: I've been writing Go for ten years but this is my first time touching the React side of this repo
- save: [preference] deep Go expertise, new to React and this project's frontend — frame frontend explanations in terms of backend analogues
    </examples>
</type>
<type>
    <name>feedback</name>
    <description>Guidance the user has given about how to approach work — both what to avoid and what to keep doing. Record from failure AND success: corrections are easy to notice, but quiet confirmations ("yes exactly", "perfect, keep doing that", accepting an unusual choice without pushback) are equally important. Include *why* so future-you can judge edge cases.</description>
    <when_to_save>Any time the user corrects your approach ("no not that", "don't", "stop doing X") OR confirms a non-obvious approach worked. In both cases, save what is applicable to future conversations, especially if surprising or not obvious from the code.</when_to_save>
    <maps_to>preference</maps_to>
    <body_structure>Lead with the rule, then a **Why:** line (the reason the user gave) and a **How to apply:** line (when/where this guidance kicks in).</body_structure>
    <examples>
- user: don't mock the database in these tests — we got burned last quarter when mocked tests passed but the prod migration failed
- save: [preference] integration tests must hit a real database, not mocks. **Why:** prior incident where mock/prod divergence masked a broken migration. **How to apply:** when adding tests that touch DB schema.

- user: stop summarizing what you just did at the end of every response, I can read the diff
- save: [preference] this user wants terse responses with no trailing summaries.

- user: yeah the single bundled PR was the right call here, splitting this one would've just been churn
- save: [preference] for refactors in this area, user prefers one bundled PR over many small ones. Confirmed after I chose this approach — a validated judgment call, not a correction.
    </examples>
</type>
<type>
    <name>project</name>
    <description>Information about ongoing work, goals, initiatives, bugs, or incidents within the project that is NOT otherwise derivable from the code or git history.</description>
    <when_to_save>When you learn who is doing what, why, or by when. Always convert relative dates to absolute dates (e.g., "Thursday" → "2026-03-05") so the memory remains interpretable after time passes.</when_to_save>
    <maps_to>project</maps_to>
    <body_structure>Lead with the fact or decision, then a **Why:** line (the motivation) and a **How to apply:** line (how this should shape your suggestions). Project memories decay fast, so the why helps future-you judge whether the memory is still load-bearing.</body_structure>
    <examples>
- user: we're freezing all non-critical merges after Thursday — mobile team is cutting a release branch
- save: [project] merge freeze begins 2026-03-05 for mobile release cut. Flag any non-critical PR work scheduled after that date.

- user: the reason we're ripping out the old auth middleware is that legal flagged it for storing session tokens in a way that doesn't meet the new compliance requirements
- save: [project] auth middleware rewrite is driven by legal/compliance requirements around session token storage, not tech-debt cleanup — scope decisions should favor compliance over ergonomics.
    </examples>
</type>
<type>
    <name>reference</name>
    <description>Pointers to where information can be found in external systems. Lets you remember where to look to find up-to-date information outside the project directory.</description>
    <when_to_save>When you learn about resources in external systems and their purpose. For example, that bugs are tracked in a specific project in Linear or that feedback can be found in a specific Slack channel.</when_to_save>
    <maps_to>general</maps_to>
    <examples>
- user: check the Linear project "INGEST" if you want context on these tickets, that's where we track all pipeline bugs
- save: [general] pipeline bugs are tracked in Linear project "INGEST".

- user: the Grafana board at grafana.internal/d/api-latency is what oncall watches
- save: [general] grafana.internal/d/api-latency is the oncall latency dashboard — check it when editing request-path code.
    </examples>
</type>
</types>

## What NOT to save in memory

- Code patterns, conventions, architecture, file paths, or project structure — these can be derived by reading the current project state.
- Git history, recent changes, or who-changed-what — git log / git blame are authoritative.
- Debugging solutions or fix recipes — the fix is in the code; the commit message has the context.
- Anything already documented in CLAUDE.md / AGENTS.md files.
- Ephemeral task details: in-progress work, temporary state, current conversation context.
- Secrets, credentials, tokens, private keys, one-off command output, temporary paths, stack traces, or transient debugging state.

These exclusions apply even when the user explicitly asks you to save. If they ask you to save a PR list or activity summary, save only what was *surprising* or *non-obvious* about it — not the activity log itself.

Do not save facts that are already in existing memories below unless they update or correct them.

Keep each text under 180 characters. Return {"memories":[]} when nothing should be saved.

Existing memories:
{{existing}}

Turn:
{{turn}}`

const autoMemoryCompactPromptTemplate = `Extract only durable memory from this coding-agent turn. Return ONLY JSON:
{"memories":[{"category":"preference|project|pattern|decision|debug|general","text":"short durable fact"}]}

Save only: user role/knowledge or durable preferences (preference); user feedback with Why and How to apply (preference); ongoing project facts not derivable from code or git (project); external-system pointers (general). Never save secrets, code structure, git history, debugging recipes, AGENTS.md content, or temporary task state, even when explicitly asked. Do not duplicate existing memories. Each text must be under 180 characters. Return {"memories":[]} when none apply.

Existing memories:
{{existing}}

Turn:
{{turn}}`

const (
	// autoMemoryExistingBudget caps the character budget for the existing-memory
	// summary injected into the extraction prompt so the small model can decide
	// update vs create vs skip.
	autoMemoryExistingBudget = 1000

	// autoMemoryMinTurnBudget makes a configured max_prompt_chars useful for
	// the actual turn, rather than spending the whole limit on instructions.
	autoMemoryMinTurnBudget = 256

	// autoMemoryBackoffThreshold is the number of consecutive empty extraction
	// results after which the scheduler enters backoff (doubles effective
	// thresholds) to avoid wasting small-model calls on low-signal sessions.
	autoMemoryBackoffThreshold = 3

	// autoMemoryBackoffMultiplier is the factor by which effective thresholds
	// are multiplied during backoff.
	autoMemoryBackoffMultiplier = 2
)

type autoMemoryResponse struct {
	Memories []autoMemoryCandidate `json:"memories"`
}

type autoMemoryCandidate struct {
	Category string `json:"category"`
	Text     string `json:"text"`
}

type autoMemoryJobResult struct {
	written   int
	succeeded bool
}

// MemoryAutoScheduler coalesces post-turn auto-memory extraction so the main
// agent response path does not synchronously call the small model on every
// successful turn. It batches turns until a threshold is reached (message count
// or turn count), then runs a single extraction job using the accumulated
// messages. A background goroutine started by maybeSchedule performs the
// actual provider call; the scheduler tracks inProgress to avoid overlapping
// extractions. Hosts that create a fresh Agent per turn should share one
// scheduler for the session lifetime.
type MemoryAutoScheduler struct {
	mu sync.Mutex

	inProgress bool
	pending    *autoMemoryJob
	// runningSession is tracked separately from the buffered session so hosts
	// can retire a session without closing its rollout while its final
	// background job is still recording an audit event.
	runningSession   string
	afterSessionDone map[string][]func()

	bufferedMessages []message.Message
	bufferedTurns    int
	lastExtraction   time.Time
	sessionID        string
	hasSession       bool

	// consecutiveEmpty tracks how many extraction jobs in a row returned
	// zero written memories. When it reaches autoMemoryBackoffThreshold the
	// effective min-turns and min-messages thresholds are multiplied by
	// autoMemoryBackoffMultiplier to reduce small-model calls on sessions
	// that rarely produce durable facts. It resets on the next successful
	// write.
	consecutiveEmpty int

	cond *sync.Cond
}

type autoMemoryJob struct {
	agent    *Agent
	ctx      context.Context
	session  string
	turn     int
	messages []message.Message
}

// NewMemoryAutoScheduler creates an in-process scheduler. Hosts that create a
// fresh Agent per turn should share one scheduler for the session.
func NewMemoryAutoScheduler() *MemoryAutoScheduler {
	s := &MemoryAutoScheduler{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// finishSuccessfulTurn is called when the agent produces a final text reply
// with no pending tool calls. It emits the EventDone, schedules auto-memory
// extraction if configured, and runs the post-user-turn hook.
func (a *Agent) finishSuccessfulTurn(ctx context.Context, sessionID string, turn int, messages []message.Message, text string) {
	a.emit(Event{Type: EventDone, Text: text})
	a.scheduleAutoMemory(ctx, sessionID, turn, messages)
}

func (a *Agent) scheduleAutoMemory(ctx context.Context, sessionID string, turn int, messages []message.Message) {
	if a.memoryAutoScheduler == nil {
		return // subagent or disabled
	}
	if !a.autoMemoryEnabled() {
		return
	}
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return
	}
	if a.currentMode() == execmode.ModePlan {
		return
	}
	turnMessages := lastTurnMessages(messages)
	if len(turnMessages) == 0 {
		return
	}
	if turnHasMemoryToolUse(turnMessages) {
		return
	}
	if effectiveMemoryAutoDisableOnExternalContext(a.memoryCfg) && turnHasExternalContextToolUse(turnMessages) {
		a.backgroundAutoMemoryAgent().emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityNotice,
			Status:       "skipped",
			Summary:      "auto memory skipped: external context used this turn",
		})
		return
	}
	a.memoryAutoScheduler.Observe(autoMemoryJob{
		agent:    a.backgroundAutoMemoryAgent(),
		ctx:      context.WithoutCancel(ctx),
		session:  sessionID,
		turn:     turn,
		messages: turnMessages,
	})
}

func (a *Agent) backgroundAutoMemoryAgent() *Agent {
	if a == nil {
		return nil
	}
	clone := *a
	// Auto-memory intentionally outlives the interactive turn that triggered
	// it. Late notifications go to the long-lived background sink instead of
	// the per-turn TUI event channel, which is closed when the turn finishes.
	clone.events = a.backgroundEvents
	return &clone
}

func (s *MemoryAutoScheduler) Observe(job autoMemoryJob) {
	if s == nil || job.agent == nil || len(job.messages) == 0 {
		return
	}
	cfg := job.agent.memoryCfg
	force := effectiveMemoryAutoTrigger(cfg) == "immediate"

	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetForSessionLocked(job.session)

	s.bufferedMessages = append(s.bufferedMessages, cloneMessages(job.messages)...)
	s.bufferedTurns++

	minInterval := effectiveMemoryAutoMinInterval(cfg)
	intervalReady := s.lastExtraction.IsZero() || time.Since(s.lastExtraction) >= minInterval
	if !force && !intervalReady {
		return
	}

	// During backoff (consecutive empty results), multiply thresholds to
	// reduce small-model calls on low-signal sessions.
	minTurns := effectiveMemoryAutoMinTurnsSinceExtraction(cfg)
	minMessages := effectiveMemoryAutoMinNewMessages(cfg)
	if s.consecutiveEmpty >= autoMemoryBackoffThreshold {
		minTurns *= autoMemoryBackoffMultiplier
		minMessages *= autoMemoryBackoffMultiplier
	}

	if !force &&
		s.bufferedTurns < minTurns &&
		memoryAutoVisibleMessageCount(s.bufferedMessages) < minMessages {
		return
	}

	job.messages = cloneMessages(s.bufferedMessages)
	s.bufferedMessages = nil
	s.bufferedTurns = 0
	s.lastExtraction = time.Now()
	s.enqueueLocked(job)
}

// resetForSessionLocked keeps batching and backoff state session-local even
// when a host reuses one scheduler while the user switches conversations.
// A job already running for the previous session may finish, but it cannot
// contribute messages, pending work, or backoff state to the new session.
func (s *MemoryAutoScheduler) resetForSessionLocked(sessionID string) {
	if s.hasSession && s.sessionID == sessionID {
		return
	}
	s.sessionID = sessionID
	s.hasSession = true
	s.bufferedMessages = nil
	s.bufferedTurns = 0
	s.lastExtraction = time.Time{}
	s.consecutiveEmpty = 0
	if s.pending != nil && s.pending.session != sessionID {
		s.pending = nil
	}
}

// DiscardSession drops buffered and pending extraction work for sessionID. If
// an extraction is already running, after runs once it has fully finished;
// hosts can use that callback to close session-scoped resources only after the
// job has recorded its outcome. It reports whether after was deferred. A nil
// callback is allowed.
func (s *MemoryAutoScheduler) DiscardSession(sessionID string, after func()) bool {
	if s == nil {
		if after != nil {
			after()
		}
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		if after != nil {
			after()
		}
		return false
	}

	callNow := false
	s.mu.Lock()
	if s.hasSession && s.sessionID == sessionID {
		s.sessionID = ""
		s.hasSession = false
		s.bufferedMessages = nil
		s.bufferedTurns = 0
		s.lastExtraction = time.Time{}
		s.consecutiveEmpty = 0
	}
	if s.pending != nil && s.pending.session == sessionID {
		s.pending = nil
	}
	if after != nil {
		if s.inProgress && s.runningSession == sessionID {
			if s.afterSessionDone == nil {
				s.afterSessionDone = make(map[string][]func())
			}
			s.afterSessionDone[sessionID] = append(s.afterSessionDone[sessionID], after)
		} else {
			callNow = true
		}
	}
	s.mu.Unlock()
	if callNow {
		after()
	}
	return !callNow && after != nil
}

func (s *MemoryAutoScheduler) enqueueLocked(job autoMemoryJob) {
	if s.inProgress {
		if s.pending != nil {
			job.messages = append(cloneMessages(s.pending.messages), job.messages...)
		}
		s.pending = &job
		s.cond.Broadcast()
		return
	}
	s.inProgress = true
	s.runningSession = job.session
	go s.run(job)
}

func (s *MemoryAutoScheduler) run(job autoMemoryJob) {
	defer func() {
		if r := recover(); r != nil {
			emitAutoMemoryPanic(job.agent, r)
			// On panic, treat as empty result and proceed to next pending
			// job (if any) without updating backoff counter.
			if next := s.completeRun(job.session); next != nil {
				s.run(*next)
			}
		}
	}()

	result := job.agent.runAutoMemoryJob(job.ctx, job.session, job.turn, job.messages)
	s.mu.Lock()
	if result.succeeded && s.hasSession && s.sessionID == job.session {
		if result.written > 0 {
			s.consecutiveEmpty = 0
		} else {
			s.consecutiveEmpty++
		}
	}
	s.mu.Unlock()
	if next := s.completeRun(job.session); next != nil {
		s.run(*next)
	}
}

func (s *MemoryAutoScheduler) completeRun(sessionID string) *autoMemoryJob {
	s.mu.Lock()
	after := s.afterSessionDone[sessionID]
	delete(s.afterSessionDone, sessionID)
	next := s.pending
	s.pending = nil
	if next == nil {
		s.inProgress = false
		s.runningSession = ""
		s.cond.Broadcast()
		s.mu.Unlock()
		runAutoMemoryCallbacks(after)
		return nil
	}
	s.runningSession = next.session
	s.mu.Unlock()
	runAutoMemoryCallbacks(after)
	return next
}

func runAutoMemoryCallbacks(callbacks []func()) {
	for _, callback := range callbacks {
		if callback != nil {
			callback()
		}
	}
}

func emitAutoMemoryPanic(a *Agent, recovered any) {
	if a == nil {
		return
	}
	defer func() { _ = recover() }()
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Status:       "failed",
		Summary:      "auto memory panic: " + truncateActivitySummary(fmt.Sprint(recovered)),
		IsError:      true,
	})
}

// Drain waits for background auto-memory work to settle. A non-positive timeout
// returns immediately; interactive hosts normally rely on EventDone instead.
func (s *MemoryAutoScheduler) Drain(ctx context.Context, timeout time.Duration) error {
	if s == nil || timeout <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for s.inProgress || s.pending != nil {
			s.cond.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DrainAutoMemory waits for this agent's scheduler using memory.auto.drain_timeout.
func (a *Agent) DrainAutoMemory(ctx context.Context) error {
	if a == nil || a.memoryAutoScheduler == nil {
		return nil
	}
	return a.memoryAutoScheduler.Drain(ctx, effectiveMemoryAutoDrainTimeout(a.memoryCfg))
}

// runAutoMemoryJob calls the small model to extract memory candidates from
// the given turn messages and writes up to maxCandidates entries. Provider or
// write failures are reported separately from a successful empty extraction so
// the scheduler does not mistake a transient outage for a low-signal session.
func (a *Agent) runAutoMemoryJob(ctx context.Context, sessionID string, turn int, turnMessages []message.Message) autoMemoryJobResult {
	candidates, err := a.generateAutoMemoryCandidates(ctx, turnMessages)
	if err != nil {
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityNotice,
			Status:       "failed",
			Summary:      "auto memory skipped: " + truncateActivitySummary(err.Error()),
			IsError:      true,
		})
		return autoMemoryJobResult{}
	}
	limit := effectiveMemoryAutoMaxCandidates(a.memoryCfg)
	written := 0
	writeFailed := false
	for _, candidate := range candidates {
		if written >= limit {
			break
		}
		text := strings.TrimSpace(candidate.Text)
		if text == "" {
			continue
		}
		if len([]rune(text)) > 180 {
			text = string([]rune(text)[:180])
		}
		category := memorypkg.DefaultCategory
		if candidate.Category != "" {
			if !memorypkg.ValidCategory(candidate.Category) {
				continue
			}
			category = memorypkg.Category(candidate.Category)
		}
		out, err := memorypkg.AppendWithOutcome(a.workspaceRoot, memorypkg.ScopeAuto, category, text)
		if err != nil {
			writeFailed = true
			a.emit(Event{
				Type:         EventActivity,
				ActivityKind: ActivityNotice,
				Status:       "failed",
				Summary:      "auto memory rejected: " + truncateActivitySummary(err.Error()),
				IsError:      true,
			})
			continue
		}
		a.recordMemoryWrite(ctx, sessionID, turn, "auto", out)
		written++
	}
	return autoMemoryJobResult{written: written, succeeded: written > 0 || !writeFailed}
}

func (a *Agent) generateAutoMemoryCandidates(ctx context.Context, turnMessages []message.Message) ([]autoMemoryCandidate, error) {
	p := a.autoMemoryProvider
	if p == nil {
		p = a.provider
	}
	model := strings.TrimSpace(a.autoMemoryModel)
	if model == "" {
		model = a.model
	}
	prompt, err := buildAutoMemoryPrompt(a.workspaceRoot, turnMessages, effectiveMemoryAutoMaxPromptChars(a.memoryCfg))
	if err != nil {
		return nil, err
	}
	request := []message.Message{message.Text(message.RoleUser, prompt)}
	estimated := contextmgr.Estimate(request, model)
	stream, err := p.Chat(ctx, provider.Request{
		Model:    model,
		Messages: request,
	})
	if err != nil {
		return nil, fmt.Errorf("memory provider chat: %w", err)
	}
	defer stream.Close()

	var body strings.Builder
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("memory provider stream: %w", err)
		}
		switch event.Type {
		case provider.EventTextDelta:
			body.WriteString(event.Text)
		case provider.EventReasoningDelta:
			continue
		case provider.EventUsage:
			if event.Usage != nil {
				observeInputUsage(model, estimated, event.Usage.InputTokens)
			}
		case provider.EventDone:
			goto done
		case provider.EventError:
			if event.Err != nil {
				return nil, fmt.Errorf("memory provider returned error: %w", event.Err)
			}
			return nil, errors.New("memory provider returned error event")
		case provider.EventToolCall:
			return nil, errors.New("memory provider returned unsupported tool call")
		default:
			return nil, fmt.Errorf("memory provider returned unsupported event type %q", event.Type)
		}
	}
done:
	raw := extractJSONObject(body.String())
	if raw == "" {
		return nil, errors.New("memory provider returned no JSON object")
	}
	var parsed autoMemoryResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse memory response: %w", err)
	}
	return parsed.Memories, nil
}

func (a *Agent) recordMemoryToolWrite(ctx context.Context, sessionID string, turn int, result tool.Result) {
	if result.IsError || result.Metadata == nil {
		return
	}
	scope := memorypkg.Scope(result.Metadata["memory_scope"])
	category := memorypkg.Category(result.Metadata["memory_category"])
	if !memorypkg.ValidScope(string(scope)) || !memorypkg.ValidCategory(string(category)) {
		return
	}
	out := memorypkg.AppendOutcome{
		Path:     result.Metadata["memory_path"],
		Heading:  result.Metadata["memory_heading"],
		Scope:    memorypkg.Scope(result.Metadata["memory_scope"]),
		Category: category,
		Text:     result.Metadata["memory_text"],
		Action:   memorypkg.AppendAction(result.Metadata["memory_action"]),
	}
	a.recordMemoryWrite(ctx, sessionID, turn, "tool", out)
}

func (a *Agent) recordMemoryWrite(ctx context.Context, sessionID string, turn int, source string, out memorypkg.AppendOutcome) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(out.Text) == "" {
		return
	}
	if err := a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.MemoryWrite(sessionID, turn, rollout.MemoryWritePayload{
			Scope:           string(out.Scope),
			Category:        string(out.Category),
			Text:            out.Text,
			Path:            out.Path,
			Heading:         out.Heading,
			Source:          source,
			Action:          string(out.Action),
			DroppedExpired:  out.DroppedExpired,
			DroppedOverflow: out.DroppedOverflow,
		})
	}); err != nil {
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityNotice,
			Status:       "failed",
			Summary:      "memory audit event failed: " + truncateActivitySummary(err.Error()),
			IsError:      true,
		})
		return
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Status:       "done",
		Summary:      fmt.Sprintf("memory %s: %s", defaultString(string(out.Action), "wrote"), truncateActivitySummary(out.Text)),
	})
}

func (a *Agent) autoMemoryEnabled() bool {
	return a.memoryCfg.Auto.Enabled != nil && *a.memoryCfg.Auto.Enabled
}

func effectiveMemoryAutoMaxCandidates(cfg config.MemoryConfig) int {
	if cfg.Auto.MaxCandidates <= 0 {
		return config.DefaultMemoryAutoMaxCandidates
	}
	return cfg.Auto.MaxCandidates
}

func effectiveMemoryAutoMaxPromptChars(cfg config.MemoryConfig) int {
	if cfg.Auto.MaxPromptChars <= 0 {
		return config.DefaultMemoryAutoMaxPromptChars
	}
	return cfg.Auto.MaxPromptChars
}

func effectiveMemoryAutoTrigger(cfg config.MemoryConfig) string {
	switch strings.TrimSpace(cfg.Auto.Trigger) {
	case "immediate":
		return "immediate"
	default:
		return config.DefaultMemoryAutoTrigger
	}
}

func effectiveMemoryAutoMinTurnsSinceExtraction(cfg config.MemoryConfig) int {
	if cfg.Auto.MinTurnsSinceExtraction <= 0 {
		return config.DefaultMemoryAutoMinTurnsSinceExtraction
	}
	return cfg.Auto.MinTurnsSinceExtraction
}

func effectiveMemoryAutoMinNewMessages(cfg config.MemoryConfig) int {
	if cfg.Auto.MinNewMessages <= 0 {
		return config.DefaultMemoryAutoMinNewMessages
	}
	return cfg.Auto.MinNewMessages
}

func effectiveMemoryAutoMinInterval(cfg config.MemoryConfig) time.Duration {
	if cfg.Auto.MinInterval <= 0 {
		return config.DefaultMemoryAutoMinInterval
	}
	return cfg.Auto.MinInterval
}

func effectiveMemoryAutoDrainTimeout(cfg config.MemoryConfig) time.Duration {
	if cfg.Auto.DrainTimeout <= 0 {
		return config.DefaultMemoryAutoDrainTimeout
	}
	return cfg.Auto.DrainTimeout
}

func effectiveMemoryAutoDisableOnExternalContext(cfg config.MemoryConfig) bool {
	return cfg.Auto.DisableOnExternalContext != nil && *cfg.Auto.DisableOnExternalContext
}

func lastTurnMessages(messages []message.Message) []message.Message {
	turns := userTurnWindows(messages)
	if len(turns) == 0 {
		return nil
	}
	last := turns[len(turns)-1]
	return cloneMessages(messages[last.start:last.end])
}

func turnHasMemoryToolUse(messages []message.Message) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == message.BlockToolUse && (block.ToolName == "remember" || block.ToolName == "forget") {
				return true
			}
		}
	}
	return false
}

func turnHasExternalContextToolUse(messages []message.Message) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type != message.BlockToolUse {
				continue
			}
			name := strings.TrimSpace(block.ToolName)
			if strings.HasPrefix(name, "mcp__") ||
				strings.HasPrefix(name, "web_") ||
				strings.HasPrefix(name, "tool_search") ||
				name == "search_query" ||
				name == "web_search" {
				return true
			}
		}
	}
	return false
}

// buildAutoMemoryPrompt keeps the complete provider request within
// memory.auto.max_prompt_chars. The detailed taxonomy is used when it leaves
// room for turn content; smaller configured budgets use a compact equivalent.
func buildAutoMemoryPrompt(workspaceRoot string, turnMessages []message.Message, maxChars int) (string, error) {
	template := autoMemoryPromptTemplate
	if !autoMemoryTemplateFits(template, maxChars) {
		template = autoMemoryCompactPromptTemplate
	}
	fixedChars := utf8.RuneCountInString(renderAutoMemoryPrompt(template, "", ""))
	if maxChars-fixedChars < autoMemoryMinTurnBudget {
		return "", fmt.Errorf("memory auto max_prompt_chars=%d is too small; need at least %d", maxChars, fixedChars+autoMemoryMinTurnBudget)
	}
	existingBudget := min(autoMemoryExistingBudget, maxChars-fixedChars-autoMemoryMinTurnBudget)
	existing := renderExistingMemory(workspaceRoot, existingBudget)
	turnBudget := maxChars - fixedChars - utf8.RuneCountInString(existing)
	rendered := truncateRunes(renderMessages(turnMessages), turnBudget)
	prompt := renderAutoMemoryPrompt(template, existing, rendered)
	if utf8.RuneCountInString(prompt) > maxChars {
		return "", fmt.Errorf("memory auto prompt exceeded max_prompt_chars=%d", maxChars)
	}
	return prompt, nil
}

func autoMemoryTemplateFits(template string, maxChars int) bool {
	fixedChars := utf8.RuneCountInString(renderAutoMemoryPrompt(template, "", ""))
	return maxChars-fixedChars >= autoMemoryMinTurnBudget
}

func renderAutoMemoryPrompt(template, existing, turn string) string {
	return strings.NewReplacer("{{existing}}", existing, "{{turn}}", turn).Replace(template)
}

// renderExistingMemory returns a compact, priority-ordered summary of existing
// auto memory entries for injection into the extraction prompt. This lets the
// small model decide update vs create vs skip, reducing duplicate writes.
func renderExistingMemory(workspaceRoot string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		return truncateRunes("(none)", maxChars)
	}
	entries, err := memorypkg.Recall(workspaceRoot, "", "")
	if err != nil || len(entries) == 0 {
		return truncateRunes("(none)", maxChars)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := memoryCategoryPriority(entries[i].Category), memoryCategoryPriority(entries[j].Category)
		if left != right {
			return left < right
		}
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})
	var b strings.Builder
	for _, e := range entries {
		line := fmt.Sprintf("[%s] %s\n", e.Category, e.Text)
		if utf8.RuneCountInString(b.String())+utf8.RuneCountInString(line) > maxChars {
			return appendPromptTruncationMarker(b.String(), maxChars)
		}
		b.WriteString(line)
	}
	return b.String()
}

func memoryCategoryPriority(category memorypkg.Category) int {
	switch category {
	case memorypkg.CatPreference:
		return 0
	case memorypkg.CatProject:
		return 1
	case memorypkg.CatPattern:
		return 2
	case memorypkg.CatDecision:
		return 3
	case memorypkg.CatGeneral:
		return 4
	default:
		return 5
	}
}

func memoryAutoVisibleMessageCount(messages []message.Message) int {
	count := 0
	for _, msg := range messages {
		switch msg.Role {
		case message.RoleUser, message.RoleAssistant, message.RoleTool:
			count++
		}
	}
	return count
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return ""
	}
	return text[start : end+1]
}

func truncateRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return appendPromptTruncationMarker(text, maxRunes)
}

func appendPromptTruncationMarker(text string, maxRunes int) string {
	const marker = "\n... [turn truncated for memory extraction]"
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	markerRunes := []rune(marker)
	if len(markerRunes) >= maxRunes {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-len(markerRunes)]) + marker
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
