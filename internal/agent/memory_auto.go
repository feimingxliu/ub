package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

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

func (a *Agent) finishSuccessfulTurn(ctx context.Context, sessionID string, turn int, messages []message.Message, text string) {
	a.maybeAutoWriteMemory(ctx, sessionID, turn, messages)
	a.emit(Event{Type: EventDone, Text: text})
}

func (a *Agent) maybeAutoWriteMemory(ctx context.Context, sessionID string, turn int, messages []message.Message) {
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
	p := a.summaryProvider
	if p == nil {
		p = a.provider
	}
	model := strings.TrimSpace(a.summaryModel)
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

func lastTurnMessages(messages []message.Message) []message.Message {
	turns := userTurnWindows(messages)
	if len(turns) == 0 {
		return nil
	}
	last := turns[len(turns)-1]
	return cloneMessages(messages[last.start:last.end])
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
