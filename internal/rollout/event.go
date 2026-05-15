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
)

// Type identifies a rollout event kind.
type Type string

const (
	TypeUserMessage      Type = "user_message"
	TypeAssistantMessage Type = "assistant_message"
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
