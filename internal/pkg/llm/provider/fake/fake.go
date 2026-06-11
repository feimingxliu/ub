// Package fake implements a deterministic script-driven provider.
package fake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
)

// toolCallSeq generates unique tool call IDs for fake provider events.
var toolCallSeq atomic.Int64

// Script is the ordered event list emitted by the fake provider.
type Script []provider.Event

// Provider implements provider.Provider with an in-memory script.
type Provider struct {
	name    string
	mu      sync.Mutex
	scripts []Script
	calls   int
}

func init() {
	provider.Register("fake", NewFromConfig)
}

// New creates a fake provider named "fake".
func New(script Script) *Provider {
	return NewNamed("fake", script)
}

// NewNamed creates a fake provider with a specific configured name.
func NewNamed(name string, script Script) *Provider {
	return NewNamedRounds(name, script)
}

// NewRounds creates a fake provider named "fake" with one script per Chat call.
func NewRounds(scripts ...Script) *Provider {
	return NewNamedRounds("fake", scripts...)
}

// NewNamedRounds creates a fake provider with one script per Chat call.
func NewNamedRounds(name string, scripts ...Script) *Provider {
	if len(scripts) == 0 {
		scripts = []Script{{}}
	}
	return &Provider{name: name, scripts: cloneScripts(scripts)}
}

// NewFromConfig creates a fake provider from a ProviderConfig script.
func NewFromConfig(name string, cfg config.ProviderConfig) (provider.Provider, error) {
	script := make(Script, 0, len(cfg.Script))
	for i, item := range cfg.Script {
		event, err := eventFromConfig(item)
		if err != nil {
			return nil, fmt.Errorf("fake provider %q script[%d]: %w", name, i, err)
		}
		script = append(script, event)
	}
	return NewNamed(name, script), nil
}

// Name returns the configured provider name.
func (p *Provider) Name() string {
	return p.name
}

// Caps returns the deterministic capabilities fake exposes for tests.
func (p *Provider) Caps() provider.Caps {
	return provider.Caps{
		SupportsTools:     true,
		SupportsStreaming: true,
		MaxContextTokens:  1_000_000,
		SupportsVision:    true,
	}
}

// Chat returns a fresh stream over the provider script.
func (p *Provider) Chat(ctx context.Context, req provider.Request) (provider.Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	index := p.calls
	p.calls++
	if index >= len(p.scripts) {
		index = len(p.scripts) - 1
	}
	script := cloneScript(p.scripts[index])
	p.mu.Unlock()
	return &stream{events: script}, nil
}

// TextDelta creates a text_delta script event.
func TextDelta(text string) provider.Event {
	return provider.Event{Type: provider.EventTextDelta, Text: text}
}

// ReasoningDelta creates a reasoning_delta script event.
func ReasoningDelta(text string) provider.Event {
	return provider.Event{Type: provider.EventReasoningDelta, Reasoning: text}
}

// ToolCall creates a tool_call script event with a unique ID.
func ToolCall(name string, input any) provider.Event {
	raw, err := json.Marshal(input)
	if err != nil {
		raw = []byte(`null`)
	}
	return provider.Event{
		Type:      provider.EventToolCall,
		ToolUseID: fmt.Sprintf("fake-tool-call-%d", toolCallSeq.Add(1)),
		ToolName:  name,
		Input:     raw,
	}
}

// Usage creates a usage script event.
func Usage(inputTokens, outputTokens int) provider.Event {
	return UsageDetails(inputTokens, outputTokens, 0, 0, 0)
}

// UsageDetails creates a usage event with optional detailed counters.
func UsageDetails(inputTokens, outputTokens, reasoningTokens, cacheReadTokens, cacheWriteTokens int) provider.Event {
	return provider.Event{
		Type: provider.EventUsage,
		Usage: &provider.Usage{
			InputTokens:      inputTokens,
			OutputTokens:     outputTokens,
			ReasoningTokens:  reasoningTokens,
			CacheReadTokens:  cacheReadTokens,
			CacheWriteTokens: cacheWriteTokens,
		},
	}
}

// Done creates a done script event.
func Done() provider.Event {
	return provider.Event{Type: provider.EventDone}
}

// Error creates an error script event.
func Error(message string) provider.Event {
	return provider.Event{Type: provider.EventError, Err: errors.New(message)}
}

type stream struct {
	events []provider.Event
	next   int
	closed bool
}

func (s *stream) Next(ctx context.Context) (provider.Event, error) {
	if err := ctx.Err(); err != nil {
		return provider.Event{}, err
	}
	if s.closed {
		return provider.Event{}, io.EOF
	}
	if s.next >= len(s.events) {
		return provider.Event{}, io.EOF
	}
	event := cloneEvent(s.events[s.next])
	s.next++
	return event, nil
}

func (s *stream) Close() error {
	s.closed = true
	return nil
}

func eventFromConfig(item config.ProviderScriptEvent) (provider.Event, error) {
	switch strings.TrimSpace(item.Type) {
	case string(provider.EventTextDelta):
		return TextDelta(item.Text), nil
	case string(provider.EventReasoningDelta):
		text := item.Reasoning
		if text == "" {
			text = item.Text
		}
		return ReasoningDelta(text), nil
	case string(provider.EventToolCall):
		raw, err := json.Marshal(item.Input)
		if err != nil {
			return provider.Event{}, fmt.Errorf("marshal tool input: %w", err)
		}
		if len(raw) == 0 {
			raw = []byte(`null`)
		}
		toolUseID := item.ToolUseID
		if toolUseID == "" {
			toolUseID = fmt.Sprintf("fake-tool-call-%d", toolCallSeq.Add(1))
		}
		return provider.Event{
			Type:      provider.EventToolCall,
			ToolUseID: toolUseID,
			ToolName:  item.ToolName,
			Input:     raw,
		}, nil
	case string(provider.EventUsage):
		return UsageDetails(item.InputTokens, item.OutputTokens, item.ReasoningTokens, item.CacheReadTokens, item.CacheWriteTokens), nil
	case string(provider.EventDone):
		return Done(), nil
	case string(provider.EventError):
		return Error(item.Error), nil
	default:
		return provider.Event{}, fmt.Errorf("unknown event type %q", item.Type)
	}
}

func cloneScript(script Script) Script {
	if script == nil {
		return nil
	}
	out := make(Script, len(script))
	for i, event := range script {
		out[i] = cloneEvent(event)
	}
	return out
}

func cloneScripts(scripts []Script) []Script {
	if scripts == nil {
		return nil
	}
	out := make([]Script, len(scripts))
	for i, script := range scripts {
		out[i] = cloneScript(script)
	}
	return out
}

func cloneEvent(event provider.Event) provider.Event {
	out := event
	if event.Input != nil {
		out.Input = append([]byte(nil), event.Input...)
	}
	if event.Usage != nil {
		usage := *event.Usage
		out.Usage = &usage
	}
	return out
}
