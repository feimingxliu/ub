package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/reasoning"
)

const providerReviewSystemPrompt = `You are ub's command approval reviewer. Review one proposed local command.

You must not execute tools. Answer with JSON only:
{"decision":"allow|deny|unsure","reason":"short explanation"}

Decision rules:
- allow: the command is low-risk, bounded, and clearly relevant.
- deny: the command is destructive, exfiltrates secrets/data, escalates privileges, installs software, changes persistence, or has broad uncontrolled effects.
- unsure: the command is ambiguous, depends on missing context, or may be unsafe without explicit user approval.`

// ProviderAgent asks a configured LLM provider to review command approvals for
// auto mode. It never exposes tools to the provider request.
type ProviderAgent struct {
	provider  provider.Provider
	model     string
	reasoning *reasoning.Config
}

// NewProviderAgent constructs a provider-backed approval agent.
func NewProviderAgent(p provider.Provider, model string) (*ProviderAgent, error) {
	return NewProviderAgentWithReasoning(p, model, nil)
}

// NewProviderAgentWithReasoning constructs a provider-backed approval agent with
// an optional reasoning request config.
func NewProviderAgentWithReasoning(p provider.Provider, model string, cfg *reasoning.Config) (*ProviderAgent, error) {
	if p == nil {
		return nil, errors.New("approval provider is required")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, errors.New("approval model is required")
	}
	return &ProviderAgent{provider: p, model: model, reasoning: cloneReasoning(cfg)}, nil
}

// ReviewCommand reviews one sanitized command request.
func (a *ProviderAgent) ReviewCommand(ctx context.Context, req Request) (Result, error) {
	payload, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("marshal approval request: %w", err)
	}
	stream, err := a.provider.Chat(ctx, provider.Request{
		Model:     a.model,
		Reasoning: cloneReasoning(a.reasoning),
		Messages: []message.Message{
			message.Text(message.RoleSystem, providerReviewSystemPrompt),
			message.Text(message.RoleUser, "Review this command approval request:\n"+string(payload)),
		},
	})
	if err != nil {
		return Result{}, err
	}
	defer stream.Close()

	var raw strings.Builder
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Result{}, err
		}
		switch event.Type {
		case provider.EventTextDelta:
			raw.WriteString(event.Text)
		case provider.EventReasoningDelta:
			continue
		case provider.EventDone:
			return parseReviewResult(raw.String())
		case provider.EventError:
			if event.Err != nil {
				return Result{}, event.Err
			}
			return Result{}, errors.New("approval provider returned error event")
		case provider.EventToolCall:
			return Result{}, fmt.Errorf("approval provider attempted tool call %q", event.ToolName)
		case provider.EventUsage:
			continue
		default:
			return Result{}, fmt.Errorf("approval provider returned unsupported event type %q", event.Type)
		}
	}
	return parseReviewResult(raw.String())
}

func cloneReasoning(cfg *reasoning.Config) *reasoning.Config {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}

func parseReviewResult(raw string) (Result, error) {
	object, err := jsonObject(raw)
	if err != nil {
		return Result{}, err
	}
	var res Result
	if err := json.Unmarshal([]byte(object), &res); err != nil {
		return Result{}, fmt.Errorf("parse approval response: %w", err)
	}
	res.Decision = Decision(strings.ToLower(strings.TrimSpace(string(res.Decision))))
	res.Reason = trimReason(res.Reason)
	switch res.Decision {
	case DecisionAllow, DecisionDeny, DecisionUnsure:
		return res, nil
	default:
		return Result{}, fmt.Errorf("approval response has invalid decision %q", res.Decision)
	}
}

func jsonObject(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end < start {
		return "", errors.New("approval response did not contain a JSON object")
	}
	return raw[start : end+1], nil
}

func trimReason(reason string) string {
	reason = strings.TrimSpace(reason)
	const maxRunes = 500
	runes := []rune(reason)
	if len(runes) <= maxRunes {
		return reason
	}
	return string(runes[:maxRunes])
}
