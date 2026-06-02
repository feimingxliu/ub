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

// CapsForModel returns provider capabilities adjusted for a specific model.
// Providers that have model-dependent capabilities (e.g. different context
// windows, reasoning support, or prompt caching) should implement CapsForModel
// on their concrete type. The default implementation delegates to Caps(), so
// providers that return uniform capabilities regardless of model do not need
// to implement it.
func CapsForModel(p Provider, model string) Caps {
	if cm, ok := p.(interface {
		CapsForModel(model string) Caps
	}); ok {
		return cm.CapsForModel(model)
	}
	return p.Caps()
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
	Type               EventType       `json:"type"`
	Text               string          `json:"text,omitempty"`
	Reasoning          string          `json:"reasoning,omitempty"`
	ReasoningSignature string          `json:"reasoning_signature,omitempty"`
	ToolUseID          string          `json:"tool_use_id,omitempty"`
	ToolName           string          `json:"tool_name,omitempty"`
	Input              json.RawMessage `json:"input,omitempty"`
	Usage              *Usage          `json:"usage,omitempty"`
	Err                error           `json:"-"`
}

// AuthError reports that a provider rejected the API key (HTTP 401/403).
// Its message is user-facing and actionable — it names the provider and,
// when known, the environment variable the key comes from — so the CLI can
// surface it verbatim instead of dumping a raw status body. Providers
// should return this rather than a generic status error for auth failures.
type AuthError struct {
	Provider string // the provider instance name, e.g. "openai"
	KeyEnv   string // the environment variable the key is read from, when known
	Status   int    // the HTTP status (401 or 403)
}

func (e *AuthError) Error() string {
	key := "the API key"
	if e.KeyEnv != "" {
		key = e.KeyEnv
	}
	return fmt.Sprintf("authentication failed for provider %q (HTTP %d): %s is invalid or expired - update it and retry",
		e.Provider, e.Status, key)
}

// IsRetryableStatus reports whether an HTTP status code is worth retrying.
// It matches 408 (request timeout), 429 (rate limit), and 5xx (server errors).
func IsRetryableStatus(status int) bool {
	return status == 408 || status == 429 || (status >= 500 && status <= 599)
}

// IsTransientErr classifies HTTP client errors. Context cancellation and
// deadline expiry are caller intent — never retry those. Everything else
// (DNS failures, connection resets, abrupt EOF, etc.) is transient.
func IsTransientErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, ioEOF) {
		return false
	}
	return true
}

// SanitizeToolPairing repairs a message history so it satisfies the tool-call
// contract the OpenAI-compatible and Anthropic APIs enforce: every assistant
// tool_use block must be followed by a tool_result block for its ID, and a
// tool_result block must follow such a call. It backfills a placeholder result
// for any unanswered call (so the turn stays intact) and drops orphan tool
// results. Well-formed histories pass through unchanged.
func SanitizeToolPairing(msgs []message.Message) []message.Message {
	out := make([]message.Message, 0, len(msgs))
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == message.RoleAssistant && hasToolUseBlocks(m) {
			// Collect the consecutive tool result messages following this assistant message.
			j := i + 1
			for j < len(msgs) && msgs[j].Role == message.RoleTool {
				j++
			}
			out = append(out, m)
			out = append(out, pairToolResults(m, msgs[i+1:j])...)
			i = j // tool messages consumed here; any orphans are dropped
			continue
		}
		if m.Role == message.RoleTool {
			// Orphan tool result (no preceding assistant tool_use) — drop
			i++
			continue
		}
		out = append(out, m)
		i++
	}
	return out
}

const interruptedToolResult = "[no result: the previous turn was interrupted before this tool call completed]"

// hasToolUseBlocks reports whether the message contains at least one tool_use content block.
func hasToolUseBlocks(m message.Message) bool {
	for _, b := range m.Content {
		if b.Type == message.BlockToolUse {
			return true
		}
	}
	return false
}

// toolUseIDs returns the tool use IDs from the assistant message, in block order.
func toolUseIDs(m message.Message) []string {
	var ids []string
	for _, b := range m.Content {
		if b.Type == message.BlockToolUse {
			ids = append(ids, b.ToolUseID)
		}
	}
	return ids
}

// toolResultByID builds a map from ToolUseID to the first tool result block.
func toolResultByID(msgs []message.Message) map[string]message.ContentBlock {
	byID := make(map[string]message.ContentBlock)
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == message.BlockToolResult && b.ToolUseID != "" {
				if _, exists := byID[b.ToolUseID]; !exists {
					byID[b.ToolUseID] = b
				}
			}
		}
	}
	return byID
}

// idDistinct reports whether every tool use ID in the list is non-empty and unique.
func idDistinct(ids []string) bool {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return false
		}
		if _, dup := seen[id]; dup {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func pairToolResults(asst message.Message, toolMsgs []message.Message) []message.Message {
	ids := toolUseIDs(asst)
	if len(ids) == 0 {
		return nil
	}

	out := make([]message.Message, 0, len(ids))

	if idDistinct(ids) {
		byID := toolResultByID(toolMsgs)
		for _, id := range ids {
			if block, ok := byID[id]; ok {
				out = append(out, message.New(message.RoleTool, block))
			} else {
				out = append(out, message.New(message.RoleTool,
					message.ToolResultBlock(id, interruptedToolResult, false)))
			}
		}
		return out
	}

	// Fallback: pair by position when IDs are empty or duplicated.
	resultBlocks := collectToolResultBlocks(toolMsgs)
	for k, id := range ids {
		if k < len(resultBlocks) {
			block := resultBlocks[k]
			block.ToolUseID = id
			out = append(out, message.New(message.RoleTool, block))
		} else {
			out = append(out, message.New(message.RoleTool,
				message.ToolResultBlock(id, interruptedToolResult, false)))
		}
	}
	return out
}

func collectToolResultBlocks(msgs []message.Message) []message.ContentBlock {
	var blocks []message.ContentBlock
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == message.BlockToolResult {
				blocks = append(blocks, b)
			}
		}
	}
	return blocks
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
