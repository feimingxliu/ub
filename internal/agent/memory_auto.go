package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/feimingxliu/ub/internal/config"
	contextmgr "github.com/feimingxliu/ub/internal/context"
	"github.com/feimingxliu/ub/internal/execution"
	memorypkg "github.com/feimingxliu/ub/internal/memory"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
)

const autoMemoryPromptTemplate = `Decide whether this coding-agent turn contains durable memory worth saving for future sessions.

Return ONLY JSON in this shape:
{"memories":[{"category":"project|preference|pattern|decision|debug|general","text":"short durable fact"}]}

Rules:
- Save only stable facts, user preferences, project conventions, commands, decisions, or reusable debugging knowledge.
- Do not save secrets, credentials, tokens, private keys, one-off command output, temporary paths, stack traces, or transient debugging state.
- Do not save facts already obvious from generic coding practice.
- Prefer project, preference, pattern, or decision over general. Use debug only for reusable failure-mode notes.
- Keep each text under 180 characters. Return {"memories":[]} when nothing should be saved.

Turn:
{{turn}}`

type autoMemoryResponse struct {
	Memories []autoMemoryCandidate `json:"memories"`
}

type autoMemoryCandidate struct {
	Category string `json:"category"`
	Text     string `json:"text"`
}

// MemoryAutoScheduler coalesces post-turn auto-memory extraction so the main
// agent response path does not synchronously call the small model on every
// successful turn.
type MemoryAutoScheduler struct {
	mu sync.Mutex

	inProgress bool
	pending    *autoMemoryJob

	bufferedMessages []message.Message
	bufferedTurns    int
	lastExtraction   time.Time

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

func (a *Agent) finishSuccessfulTurn(ctx context.Context, sessionID string, turn int, messages []message.Message, text string) {
	a.emit(Event{Type: EventDone, Text: text})
	a.scheduleAutoMemory(ctx, sessionID, turn, messages)
}

func (a *Agent) scheduleAutoMemory(ctx context.Context, sessionID string, turn int, messages []message.Message) {
	if !a.autoMemoryEnabled() {
		return
	}
	if strings.TrimSpace(a.workspaceRoot) == "" {
		return
	}
	if a.currentMode() == execution.ModePlan {
		return
	}
	turnMessages := lastTurnMessages(messages)
	if len(turnMessages) == 0 {
		return
	}
	if turnHasRememberToolUse(turnMessages) {
		return
	}
	if effectiveMemoryAutoDisableOnExternalContext(a.memoryCfg) && turnHasExternalContextToolUse(turnMessages) {
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityNotice,
			Status:       "skipped",
			Summary:      "auto memory skipped: external context used this turn",
		})
		return
	}
	if a.memoryAutoScheduler == nil {
		return
	}
	a.memoryAutoScheduler.Observe(autoMemoryJob{
		agent:    a,
		ctx:      context.WithoutCancel(ctx),
		session:  sessionID,
		turn:     turn,
		messages: turnMessages,
	})
}

func (s *MemoryAutoScheduler) Observe(job autoMemoryJob) {
	if s == nil || job.agent == nil || len(job.messages) == 0 {
		return
	}
	cfg := job.agent.memoryCfg
	force := memoryAutoStrongSignal(job.messages) || effectiveMemoryAutoTrigger(cfg) == "immediate"

	s.mu.Lock()
	defer s.mu.Unlock()

	s.bufferedMessages = append(s.bufferedMessages, cloneMessages(job.messages)...)
	s.bufferedTurns++

	minInterval := effectiveMemoryAutoMinInterval(cfg)
	intervalReady := s.lastExtraction.IsZero() || time.Since(s.lastExtraction) >= minInterval
	if !force && !intervalReady {
		return
	}
	if !force &&
		s.bufferedTurns < effectiveMemoryAutoMinTurnsSinceExtraction(cfg) &&
		memoryAutoVisibleMessageCount(s.bufferedMessages) < effectiveMemoryAutoMinNewMessages(cfg) {
		return
	}

	job.messages = cloneMessages(s.bufferedMessages)
	s.bufferedMessages = nil
	s.bufferedTurns = 0
	s.lastExtraction = time.Now()
	s.enqueueLocked(job)
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
	go s.run(job)
}

func (s *MemoryAutoScheduler) run(job autoMemoryJob) {
	job.agent.runAutoMemoryJob(job.ctx, job.session, job.turn, job.messages)

	s.mu.Lock()
	next := s.pending
	s.pending = nil
	if next == nil {
		s.inProgress = false
		s.cond.Broadcast()
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	s.run(*next)
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

func (a *Agent) runAutoMemoryJob(ctx context.Context, sessionID string, turn int, turnMessages []message.Message) {
	candidates, err := a.generateAutoMemoryCandidates(ctx, turnMessages)
	if err != nil {
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityNotice,
			Status:       "failed",
			Summary:      "auto memory skipped: " + truncateActivitySummary(err.Error()),
			IsError:      true,
		})
		return
	}
	limit := effectiveMemoryAutoMaxCandidates(a.memoryCfg)
	written := 0
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
	rendered := truncateRunes(renderMessages(turnMessages), effectiveMemoryAutoMaxPromptChars(a.memoryCfg))
	prompt := strings.ReplaceAll(autoMemoryPromptTemplate, "{{turn}}", rendered)
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

func (a *Agent) recordRememberToolMemoryWrite(ctx context.Context, sessionID string, turn int, result tool.Result) {
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

func turnHasRememberToolUse(messages []message.Message) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == message.BlockToolUse && block.ToolName == "remember" {
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

func memoryAutoStrongSignal(messages []message.Message) bool {
	for _, msg := range messages {
		if msg.Role != message.RoleUser {
			continue
		}
		text := strings.ToLower(msg.Text())
		for _, marker := range []string{
			"remember",
			"from now on",
			"for future",
			"next time",
			"preference",
			"prefer ",
			"记住",
			"记一下",
			"以后",
			"下次",
		} {
			if strings.Contains(text, marker) {
				return true
			}
		}
	}
	return false
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
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "\n... [turn truncated for memory extraction]"
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
