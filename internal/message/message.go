// Package message defines ub's provider-neutral message model.
package message

import (
	"encoding/json"
	"strings"
)

// Role identifies who authored a message.
type Role string

const (
	// RoleUser is a user-authored message.
	RoleUser Role = "user"
	// RoleAssistant is an assistant-authored message.
	RoleAssistant Role = "assistant"
	// RoleSystem is an instruction or system message.
	RoleSystem Role = "system"
	// RoleTool is a tool-authored message.
	RoleTool Role = "tool"
)

// BlockType identifies the content block variant.
type BlockType string

const (
	// BlockText contains plain text.
	BlockText BlockType = "text"
	// BlockImage references an image by URL.
	BlockImage BlockType = "image"
	// BlockReasoning contains hidden provider reasoning that may need replay.
	BlockReasoning BlockType = "reasoning"
	// BlockToolUse asks the runtime to invoke a tool.
	BlockToolUse BlockType = "tool_use"
	// BlockToolResult contains the result of a tool invocation.
	BlockToolResult BlockType = "tool_result"
)

// Message is ub's provider-neutral message representation.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock is one ordered piece of message content.
type ContentBlock struct {
	Type               BlockType       `json:"type"`
	Text               string          `json:"text,omitempty"`
	Reasoning          string          `json:"reasoning,omitempty"`
	ImageURL           string          `json:"image_url,omitempty"`
	ToolUseID          string          `json:"tool_use_id,omitempty"`
	ToolName           string          `json:"tool_name,omitempty"`
	Input              json.RawMessage `json:"input,omitempty"`
	Output             string          `json:"output,omitempty"`
	IsError            bool            `json:"is_error,omitempty"`
	ReasoningSignature string          `json:"reasoning_signature,omitempty"`
}

// New constructs a message with cloned content blocks.
func New(role Role, content ...ContentBlock) Message {
	return Message{
		Role:    role,
		Content: cloneBlocks(content),
	}
}

// Text constructs a text-only message.
func Text(role Role, text string) Message {
	return New(role, TextBlock(text))
}

// TextBlock constructs a plain text content block.
func TextBlock(text string) ContentBlock {
	return ContentBlock{
		Type: BlockText,
		Text: text,
	}
}

// ReasoningBlock constructs a hidden reasoning content block.
func ReasoningBlock(reasoning, signature string) ContentBlock {
	return ContentBlock{
		Type:               BlockReasoning,
		Reasoning:          reasoning,
		ReasoningSignature: signature,
	}
}

// ImageBlock constructs an image URL content block.
func ImageBlock(url string) ContentBlock {
	return ContentBlock{
		Type:     BlockImage,
		ImageURL: url,
	}
}

// ToolUseBlock constructs a tool_use content block.
func ToolUseBlock(id, name string, input json.RawMessage) ContentBlock {
	return ContentBlock{
		Type:      BlockToolUse,
		ToolUseID: id,
		ToolName:  name,
		Input:     cloneRaw(input),
	}
}

// ToolResultBlock constructs a tool_result content block.
func ToolResultBlock(id, output string, isError bool) ContentBlock {
	return ContentBlock{
		Type:      BlockToolResult,
		ToolUseID: id,
		Output:    output,
		IsError:   isError,
	}
}

// Text returns the text from all text blocks, joined by newlines.
func (m Message) Text() string {
	parts := make([]string, 0, len(m.Content))
	for _, block := range m.Content {
		if block.Type == BlockText {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Append returns a copy of m with content appended.
func (m Message) Append(content ...ContentBlock) Message {
	out := m.Clone()
	out.Content = append(out.Content, cloneBlocks(content)...)
	return out
}

// Clone returns a deep copy of m.
func (m Message) Clone() Message {
	return Message{
		Role:    m.Role,
		Content: cloneBlocks(m.Content),
	}
}

func cloneBlocks(blocks []ContentBlock) []ContentBlock {
	if blocks == nil {
		return nil
	}
	out := make([]ContentBlock, len(blocks))
	for i, block := range blocks {
		out[i] = block
		out[i].Input = cloneRaw(block.Input)
	}
	return out
}

func cloneRaw(in json.RawMessage) json.RawMessage {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return json.RawMessage(out)
}
