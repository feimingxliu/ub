// Package rollout persists the event log for ub sessions.
package rollout

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"time"

	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/tool"
)

// Type identifies a rollout event kind.
type Type string

const (
	TypeUserMessage      Type = "user_message"
	TypeAssistantMessage Type = "assistant_message"
	TypeToolResult       Type = "tool_result"
	TypeSummary          Type = "summary"
	TypeUsage            Type = "usage"
	TypeError            Type = "error"
)

// Event is the persisted rollout event shape.
type Event struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Turn      int             `json:"turn"`
	Time      time.Time       `json:"time"`
	Type      Type            `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// MessagePayload stores a provider-neutral message and its extracted text.
type MessagePayload struct {
	Message message.Message `json:"message"`
	Text    string          `json:"text,omitempty"`
}

// UsagePayload stores provider token usage.
type UsagePayload struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// ErrorPayload stores a user-readable error.
type ErrorPayload struct {
	Message string `json:"message"`
}

// ToolResultPayload stores one executed tool result.
type ToolResultPayload struct {
	ToolUseID string            `json:"tool_use_id"`
	ToolName  string            `json:"tool_name,omitempty"`
	Output    string            `json:"output"`
	IsError   bool              `json:"is_error,omitempty"`
	Files     []tool.FileChange `json:"files,omitempty"`
}

// SummaryPayload stores an automatic context summary.
type SummaryPayload struct {
	Text               string `json:"text"`
	CompressedMessages int    `json:"compressed_messages,omitempty"`
	KeptMessages       int    `json:"kept_messages,omitempty"`
	EstimatedTokens    int    `json:"estimated_tokens,omitempty"`
}

// MarshalPayload marshals a typed payload into raw JSON.
func MarshalPayload(payload any) (json.RawMessage, error) {
	if payload == nil {
		return nil, errors.New("rollout payload is nil")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal rollout payload: %w", err)
	}
	return json.RawMessage(raw), nil
}

// NewEvent constructs a validated event with a generated ID and timestamp.
func NewEvent(sessionID string, turn int, typ Type, payload any) (Event, error) {
	raw, err := MarshalPayload(payload)
	if err != nil {
		return Event{}, err
	}
	return Event{
		ID:        NewID("evt"),
		SessionID: sessionID,
		Turn:      turn,
		Time:      time.Now().UTC(),
		Type:      typ,
		Payload:   raw,
	}, nil
}

// UserMessage creates a user_message event.
func UserMessage(sessionID string, turn int, msg message.Message) (Event, error) {
	return NewEvent(sessionID, turn, TypeUserMessage, MessagePayload{
		Message: msg.Clone(),
		Text:    msg.Text(),
	})
}

// AssistantMessage creates an assistant_message event.
func AssistantMessage(sessionID string, turn int, msg message.Message) (Event, error) {
	return NewEvent(sessionID, turn, TypeAssistantMessage, MessagePayload{
		Message: msg.Clone(),
		Text:    msg.Text(),
	})
}

// ToolResult creates a tool_result event.
func ToolResult(sessionID string, turn int, toolUseID, toolName string, result tool.Result) (Event, error) {
	return NewEvent(sessionID, turn, TypeToolResult, ToolResultPayload{
		ToolUseID: toolUseID,
		ToolName:  toolName,
		Output:    result.Content,
		IsError:   result.IsError,
		Files:     append([]tool.FileChange(nil), result.Files...),
	})
}

// Summary creates a summary event.
func Summary(sessionID string, turn int, text string, compressedMessages, keptMessages, estimatedTokens int) (Event, error) {
	return NewEvent(sessionID, turn, TypeSummary, SummaryPayload{
		Text:               text,
		CompressedMessages: compressedMessages,
		KeptMessages:       keptMessages,
		EstimatedTokens:    estimatedTokens,
	})
}

// SummaryMessage converts summary text into the system message sent to providers.
func SummaryMessage(text string) message.Message {
	return message.Text(message.RoleSystem, "Conversation summary:\n"+text)
}

// MessageFromEvent converts persisted message-like rollout events to internal messages.
func MessageFromEvent(event Event) (message.Message, bool, error) {
	switch event.Type {
	case TypeUserMessage, TypeAssistantMessage:
		var payload MessagePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return message.Message{}, false, fmt.Errorf("decode rollout message event %s: %w", event.ID, err)
		}
		msg := payload.Message.Clone()
		if len(msg.Content) == 0 && payload.Text != "" {
			switch event.Type {
			case TypeUserMessage:
				msg = message.Text(message.RoleUser, payload.Text)
			case TypeAssistantMessage:
				msg = message.Text(message.RoleAssistant, payload.Text)
			}
		}
		if len(msg.Content) == 0 {
			return message.Message{}, false, nil
		}
		return msg, true, nil
	case TypeToolResult:
		var payload ToolResultPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return message.Message{}, false, fmt.Errorf("decode rollout tool_result event %s: %w", event.ID, err)
		}
		return message.New(message.RoleTool, message.ToolResultBlock(payload.ToolUseID, payload.Output, payload.IsError)), true, nil
	case TypeSummary:
		var payload SummaryPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return message.Message{}, false, fmt.Errorf("decode rollout summary event %s: %w", event.ID, err)
		}
		if payload.Text == "" {
			return message.Message{}, false, nil
		}
		return SummaryMessage(payload.Text), true, nil
	default:
		return message.Message{}, false, nil
	}
}

// Usage creates a usage event.
func Usage(sessionID string, turn, inputTokens, outputTokens int) (Event, error) {
	return NewEvent(sessionID, turn, TypeUsage, UsagePayload{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	})
}

// Error creates an error event.
func Error(sessionID string, turn int, err error) (Event, error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return NewEvent(sessionID, turn, TypeError, ErrorPayload{Message: msg})
}

// NewID returns a compact random identifier with a stable prefix.
func NewID(prefix string) string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%020d_%016x%016x", prefix, time.Now().UnixNano(), mathrand.Uint64(), mathrand.Uint64())
	}
	return fmt.Sprintf("%s_%020d_%s", prefix, time.Now().UnixNano(), hex.EncodeToString(b[:]))
}
