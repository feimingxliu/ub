// Package provider defines ub's SDK-neutral LLM provider runtime.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/reasoning"
)

// Provider is the behavior interface implemented by every model backend.
type Provider interface {
	Name() string
	Caps() Caps
	Chat(ctx context.Context, req Request) (Stream, error)
}

// Caps describes provider-level capabilities.
type Caps struct {
	SupportsTools       bool
	SupportsStreaming   bool
	SupportsPromptCache bool
	MaxContextTokens    int
	SupportsVision      bool
}

// Request is the provider-neutral chat request.
type Request struct {
	Model     string
	Messages  []message.Message
	Tools     []ToolDefinition
	Reasoning *reasoning.Config
}

// ToolDefinition is the provider-neutral schema for one callable tool.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
}

// Stream produces provider events in order.
type Stream interface {
	Next(ctx context.Context) (Event, error)
	Close() error
}

// EventType identifies a streamed provider event.
type EventType string

const (
	EventTextDelta      EventType = "text_delta"
	EventReasoningDelta EventType = "reasoning_delta"
	EventToolCall       EventType = "tool_call"
	EventUsage          EventType = "usage"
	EventDone           EventType = "done"
	EventError          EventType = "error"
)

// Usage reports token usage when a provider exposes it.
type Usage struct {
	InputTokens      int `json:"input_tokens,omitempty"`
	OutputTokens     int `json:"output_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// Event is one item emitted by a provider stream.
type Event struct {
	Type      EventType       `json:"type"`
	Text      string          `json:"text,omitempty"`
	Reasoning string          `json:"reasoning,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Usage     *Usage          `json:"usage,omitempty"`
	Err       error           `json:"-"`
}

// Constructor creates a provider from one config entry.
type Constructor func(name string, cfg config.ProviderConfig) (Provider, error)

var (
	constructorsMu sync.RWMutex
	constructors   = map[string]Constructor{}
)

// Register makes a provider type available to New.
func Register(providerType string, constructor Constructor) {
	providerType = strings.TrimSpace(providerType)
	if providerType == "" {
		panic("provider: empty provider type")
	}
	if constructor == nil {
		panic(fmt.Sprintf("provider: nil constructor for %q", providerType))
	}
	constructorsMu.Lock()
	defer constructorsMu.Unlock()
	if _, exists := constructors[providerType]; exists {
		panic(fmt.Sprintf("provider: duplicate provider type %q", providerType))
	}
	constructors[providerType] = constructor
}

// New creates a provider instance from a named config entry.
func New(name string, cfg config.ProviderConfig) (Provider, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("provider name is required")
	}
	providerType := strings.TrimSpace(cfg.Type)
	if providerType == "" {
		return nil, fmt.Errorf("provider %q missing type", name)
	}
	constructorsMu.RLock()
	constructor := constructors[providerType]
	constructorsMu.RUnlock()
	if constructor == nil {
		return nil, fmt.Errorf("provider %q has unknown type %q", name, providerType)
	}
	return constructor(name, cfg)
}
