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
	TypeUserMessage         Type = "user_message"
	TypeAssistantMessage    Type = "assistant_message"
	TypeToolResult          Type = "tool_result"
	TypeSummary             Type = "summary"
	TypeUsage               Type = "usage"
	TypeError               Type = "error"
	TypeActivity            Type = "activity"
	TypeMemoryWrite         Type = "memory_write"
	TypeFileHistorySnapshot Type = "file_history_snapshot"
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
	InputTokens      int `json:"input_tokens,omitempty"`
	OutputTokens     int `json:"output_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// ErrorPayload stores a user-readable error.
type ErrorPayload struct {
	Message string `json:"message"`
}

// ToolResultPayload stores one executed tool result.
type ToolResultPayload struct {
	ToolUseID      string            `json:"tool_use_id"`
	ToolName       string            `json:"tool_name,omitempty"`
	Output         string            `json:"output"`
	IsError        bool              `json:"is_error,omitempty"`
	Files          []tool.FileChange `json:"files,omitempty"`
	Truncated      bool              `json:"truncated,omitempty"`
	OriginalBytes  int               `json:"original_bytes,omitempty"`
	FullOutputPath string            `json:"full_output_path,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// ActivityPayload stores a display-only activity event for TUI restoration.
type ActivityPayload struct {
	ActivityKind    string `json:"activity_kind,omitempty"`
	ToolUseID       string `json:"tool_use_id,omitempty"`
	ToolName        string `json:"tool_name,omitempty"`
	ParentToolUseID string `json:"parent_tool_use_id,omitempty"`
	SubagentID      string `json:"subagent_id,omitempty"`
	Status          string `json:"status,omitempty"`
	Summary         string `json:"summary,omitempty"`
	Content         string `json:"content,omitempty"`
	Decision        string `json:"decision,omitempty"`
	Source          string `json:"source,omitempty"`
	Reason          string `json:"reason,omitempty"`
	Allowed         bool   `json:"allowed,omitempty"`
	IsError         bool   `json:"is_error,omitempty"`
}

// ContextMaintenance stores metadata about a context decision without storing
// prompt text or original tool-result output. It accompanies both summaries
// and prune-only context maintenance events.
type ContextMaintenance struct {
	Decision            string   `json:"decision,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	TokensBefore        int      `json:"tokens_before,omitempty"`
	TokensAfter         int      `json:"tokens_after,omitempty"`
	CutoffMessages      int      `json:"cutoff_messages,omitempty"`
	PrunedToolUseIDs    []string `json:"pruned_tool_use_ids,omitempty"`
	ProtectedToolUseIDs []string `json:"protected_tool_use_ids,omitempty"`
	SummaryModel        string   `json:"summary_model,omitempty"`
	DurationMillis      int64    `json:"duration_millis,omitempty"`
	Retry               bool     `json:"retry,omitempty"`
}

// SummaryPayload stores an automatic context summary or a prune-only context
// maintenance checkpoint. Messages is the provider-facing context to restore
// when the session resumes.
type SummaryPayload struct {
	Text               string              `json:"text"`
	CompressedMessages int                 `json:"compressed_messages,omitempty"`
	KeptMessages       int                 `json:"kept_messages,omitempty"`
	EstimatedTokens    int                 `json:"estimated_tokens,omitempty"`
	Messages           []message.Message   `json:"messages,omitempty"`
	Maintenance        *ContextMaintenance `json:"context_maintenance,omitempty"`
}

// MemoryWritePayload stores one durable memory write for audit/debugging.
type MemoryWritePayload struct {
	Scope           string `json:"scope"`
	Category        string `json:"category"`
	Text            string `json:"text"`
	Path            string `json:"path,omitempty"`
	Heading         string `json:"heading,omitempty"`
	Source          string `json:"source,omitempty"`
	Action          string `json:"action,omitempty"`
	DroppedExpired  int    `json:"dropped_expired,omitempty"`
	DroppedOverflow int    `json:"dropped_overflow,omitempty"`
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
		ToolUseID:      toolUseID,
		ToolName:       toolName,
		Output:         result.Content,
		IsError:        result.IsError,
		Files:          append([]tool.FileChange(nil), result.Files...),
		Truncated:      result.Truncated,
		OriginalBytes:  result.OriginalBytes,
		FullOutputPath: result.FullOutputPath,
		Metadata:       cloneStringMap(result.Metadata),
	})
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Activity creates a display-only activity event.
func Activity(sessionID string, turn int, payload ActivityPayload) (Event, error) {
	return NewEvent(sessionID, turn, TypeActivity, payload)
}

// Summary creates a summary event.
func Summary(sessionID string, turn int, text string, compressedMessages, keptMessages, estimatedTokens int) (Event, error) {
	return SummaryWithMessages(sessionID, turn, text, nil, compressedMessages, keptMessages, estimatedTokens)
}

// SummaryWithMessages creates a summary event with the complete compacted
// provider context used after summarization. The messages normally contain the
// summary system message followed by the kept recent turn suffix.
func SummaryWithMessages(sessionID string, turn int, text string, messages []message.Message, compressedMessages, keptMessages, estimatedTokens int) (Event, error) {
	return SummaryWithMessagesAndMaintenance(sessionID, turn, text, messages, compressedMessages, keptMessages, estimatedTokens, nil)
}

// SummaryWithMessagesAndMaintenance creates a summary event with the complete
// provider context and optional safe-to-persist maintenance audit metadata.
func SummaryWithMessagesAndMaintenance(sessionID string, turn int, text string, messages []message.Message, compressedMessages, keptMessages, estimatedTokens int, maintenance *ContextMaintenance) (Event, error) {
	cloned := make([]message.Message, 0, len(messages))
	for _, msg := range messages {
		if len(msg.Content) == 0 {
			continue
		}
		cloned = append(cloned, msg.Clone())
	}
	payload := SummaryPayload{
		Text:               text,
		CompressedMessages: compressedMessages,
		KeptMessages:       keptMessages,
		EstimatedTokens:    estimatedTokens,
		Messages:           cloned,
	}
	if maintenance != nil {
		copy := *maintenance
		copy.PrunedToolUseIDs = append([]string(nil), maintenance.PrunedToolUseIDs...)
		copy.ProtectedToolUseIDs = append([]string(nil), maintenance.ProtectedToolUseIDs...)
		payload.Maintenance = &copy
	}
	return NewEvent(sessionID, turn, TypeSummary, payload)
}

// MemoryWrite creates a durable memory audit event.
func MemoryWrite(sessionID string, turn int, payload MemoryWritePayload) (Event, error) {
	return NewEvent(sessionID, turn, TypeMemoryWrite, payload)
}

// FileHistorySnapshot stores one file checkpoint metadata entry.
func FileHistorySnapshot(sessionID string, turn int, payload any) (Event, error) {
	return NewEvent(sessionID, turn, TypeFileHistorySnapshot, payload)
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
		msgs, ok, err := SummaryMessagesFromEvent(event)
		if err != nil {
			return message.Message{}, false, err
		}
		if !ok || len(msgs) == 0 {
			return message.Message{}, false, nil
		}
		return msgs[0], true, nil
	default:
		return message.Message{}, false, nil
	}
}

// SummaryMessagesFromEvent extracts the full compacted provider context from a
// summary event. Older events do not have Messages, so they fall back to the
// single summary system message.
func SummaryMessagesFromEvent(event Event) ([]message.Message, bool, error) {
	if event.Type != TypeSummary {
		return nil, false, nil
	}
	var payload SummaryPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return nil, false, fmt.Errorf("decode rollout summary event %s: %w", event.ID, err)
	}
	if len(payload.Messages) > 0 {
		out := make([]message.Message, 0, len(payload.Messages))
		for _, msg := range payload.Messages {
			if len(msg.Content) == 0 {
				continue
			}
			out = append(out, msg.Clone())
		}
		if len(out) > 0 {
			return out, true, nil
		}
	}
	if payload.Text == "" {
		return nil, false, nil
	}
	return []message.Message{SummaryMessage(payload.Text)}, true, nil
}

// ActivityFromEvent extracts a display-only activity payload.
func ActivityFromEvent(event Event) (ActivityPayload, bool, error) {
	if event.Type != TypeActivity {
		return ActivityPayload{}, false, nil
	}
	var payload ActivityPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ActivityPayload{}, false, fmt.Errorf("decode rollout activity event %s: %w", event.ID, err)
	}
	if payload.ActivityKind == "" {
		return ActivityPayload{}, false, nil
	}
	return payload, true, nil
}

// Usage creates a usage event.
func Usage(sessionID string, turn, inputTokens, outputTokens int) (Event, error) {
	return UsageWithDetails(sessionID, turn, UsagePayload{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	})
}

// UsageWithDetails creates a usage event with all provider-reported token
// fields that are available.
func UsageWithDetails(sessionID string, turn int, usage UsagePayload) (Event, error) {
	return NewEvent(sessionID, turn, TypeUsage, UsagePayload{
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
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
