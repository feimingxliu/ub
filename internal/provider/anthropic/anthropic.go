// Package anthropic implements ub's Anthropic provider adapter.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
)

const defaultMaxTokens int64 = 1024

// Provider adapts Anthropic Messages API to provider.Provider.
type Provider struct {
	name   string
	client sdk.Client
}

func init() {
	provider.Register("anthropic", NewFromConfig)
}

// NewFromConfig creates an Anthropic provider from one config entry.
func NewFromConfig(name string, cfg config.ProviderConfig) (provider.Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("anthropic provider %q missing api_key", name)
	}
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithHTTPClient(&http.Client{Timeout: effectiveTimeout(cfg.Timeout)}),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	for key, value := range cfg.Headers {
		opts = append(opts, option.WithHeader(key, value))
	}
	return &Provider{
		name:   name,
		client: sdk.NewClient(opts...),
	}, nil
}

// Name returns the configured provider name.
func (p *Provider) Name() string {
	return p.name
}

// Caps returns Anthropic capabilities available in I-10.
func (p *Provider) Caps() provider.Caps {
	return provider.Caps{
		SupportsStreaming: true,
		SupportsTools:     true,
		MaxContextTokens:  200_000,
		SupportsVision:    false,
	}
}

// Chat creates a streaming Anthropic Messages request.
func (p *Provider) Chat(ctx context.Context, req provider.Request) (provider.Stream, error) {
	if strings.TrimSpace(req.Model) == "" {
		return nil, errors.New("anthropic model is required")
	}
	params, err := toMessageParams(req)
	if err != nil {
		return nil, err
	}
	return newSDKStream(p.client.Messages.NewStreaming(ctx, params)), nil
}

func toMessageParams(req provider.Request) (sdk.MessageNewParams, error) {
	params := sdk.MessageNewParams{
		Model:     sdk.Model(req.Model),
		MaxTokens: defaultMaxTokens,
	}
	tools, err := toToolParams(req.Tools)
	if err != nil {
		return sdk.MessageNewParams{}, err
	}
	params.Tools = tools
	for _, msg := range req.Messages {
		switch msg.Role {
		case message.RoleSystem:
			system, err := systemTextBlocks(msg)
			if err != nil {
				return sdk.MessageNewParams{}, err
			}
			params.System = append(params.System, system...)
		case message.RoleUser:
			blocks, err := contentBlocks(msg)
			if err != nil {
				return sdk.MessageNewParams{}, err
			}
			params.Messages = append(params.Messages, sdk.NewUserMessage(blocks...))
		case message.RoleAssistant:
			blocks, err := contentBlocks(msg)
			if err != nil {
				return sdk.MessageNewParams{}, err
			}
			params.Messages = append(params.Messages, sdk.NewAssistantMessage(blocks...))
		case message.RoleTool:
			blocks, err := contentBlocks(msg)
			if err != nil {
				return sdk.MessageNewParams{}, err
			}
			params.Messages = append(params.Messages, sdk.NewUserMessage(blocks...))
		default:
			return sdk.MessageNewParams{}, fmt.Errorf("anthropic provider does not support role %q", msg.Role)
		}
	}
	if len(params.Messages) == 0 {
		return sdk.MessageNewParams{}, errors.New("anthropic request requires at least one user or assistant message")
	}
	return params, nil
}

func toToolParams(defs []provider.ToolDefinition) ([]sdk.ToolUnionParam, error) {
	if len(defs) == 0 {
		return nil, nil
	}
	tools := make([]sdk.ToolUnionParam, 0, len(defs))
	for _, def := range defs {
		if strings.TrimSpace(def.Name) == "" {
			return nil, errors.New("anthropic tool name is required")
		}
		schema, err := anthropicToolSchema(def.Schema)
		if err != nil {
			return nil, fmt.Errorf("anthropic tool %q schema: %w", def.Name, err)
		}
		tool := sdk.ToolUnionParamOfTool(schema, def.Name)
		tool.OfTool.Description = sdk.String(def.Description)
		tools = append(tools, tool)
	}
	return tools, nil
}

func anthropicToolSchema(raw json.RawMessage) (sdk.ToolInputSchemaParam, error) {
	if len(raw) == 0 {
		return sdk.ToolInputSchemaParam{}, nil
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return sdk.ToolInputSchemaParam{}, err
	}
	out := sdk.ToolInputSchemaParam{}
	if props, ok := schema["properties"]; ok {
		out.Properties = props
		delete(schema, "properties")
	}
	if required, ok := schema["required"].([]any); ok {
		for _, item := range required {
			if value, ok := item.(string); ok {
				out.Required = append(out.Required, value)
			}
		}
		delete(schema, "required")
	}
	delete(schema, "type")
	if len(schema) > 0 {
		out.ExtraFields = schema
	}
	return out, nil
}

func systemTextBlocks(msg message.Message) ([]sdk.TextBlockParam, error) {
	blocks := make([]sdk.TextBlockParam, 0, len(msg.Content))
	for _, block := range msg.Content {
		if block.Type != message.BlockText {
			return nil, unsupportedBlock(block.Type)
		}
		blocks = append(blocks, sdk.TextBlockParam{Text: block.Text})
	}
	return blocks, nil
}

func contentBlocks(msg message.Message) ([]sdk.ContentBlockParamUnion, error) {
	blocks := make([]sdk.ContentBlockParamUnion, 0, len(msg.Content))
	for _, block := range msg.Content {
		switch block.Type {
		case message.BlockText:
			blocks = append(blocks, sdk.NewTextBlock(block.Text))
		case message.BlockToolUse:
			var input any
			if len(block.Input) > 0 {
				if err := json.Unmarshal(block.Input, &input); err != nil {
					return nil, fmt.Errorf("anthropic tool_use input: %w", err)
				}
			}
			if input == nil {
				input = map[string]any{}
			}
			blocks = append(blocks, sdk.NewToolUseBlock(block.ToolUseID, input, block.ToolName))
		case message.BlockToolResult:
			blocks = append(blocks, sdk.NewToolResultBlock(block.ToolUseID, block.Output, block.IsError))
		default:
			return nil, unsupportedBlock(block.Type)
		}
	}
	return blocks, nil
}

func unsupportedBlock(blockType message.BlockType) error {
	return fmt.Errorf("anthropic non-streaming text provider does not support content block %q", blockType)
}

func effectiveTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return 120 * time.Second
}

type sdkStream struct {
	stream *ssestream.Stream[sdk.MessageStreamEventUnion]
	queue  []provider.Event
	usage  *provider.Usage
	tools  map[int64]*toolUseDelta
	closed bool
}

type toolUseDelta struct {
	id       string
	name     string
	input    json.RawMessage
	partial  strings.Builder
	hasDelta bool
}

func newSDKStream(stream *ssestream.Stream[sdk.MessageStreamEventUnion]) *sdkStream {
	return &sdkStream{stream: stream}
}

func (s *sdkStream) Next(ctx context.Context) (provider.Event, error) {
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return provider.Event{}, err
	}
	if s.closed {
		return provider.Event{}, io.EOF
	}
	if len(s.queue) > 0 {
		event := s.queue[0]
		s.queue = s.queue[1:]
		return event, nil
	}
	for s.stream.Next() {
		event := s.stream.Current()
		switch event.Type {
		case "content_block_start":
			switch event.ContentBlock.Type {
			case "", "text":
				continue
			case "thinking":
				if event.ContentBlock.Thinking != "" {
					return provider.Event{Type: provider.EventReasoningDelta, Reasoning: event.ContentBlock.Thinking}, nil
				}
				continue
			case "redacted_thinking":
				continue
			case "tool_use":
				s.startToolUse(event.Index, event.ContentBlock)
			default:
				return provider.Event{}, fmt.Errorf("anthropic streaming content block %q is not supported", event.ContentBlock.Type)
			}
		case "content_block_delta":
			delta := event.AsContentBlockDelta().Delta
			switch delta.Type {
			case "text_delta":
				return provider.Event{Type: provider.EventTextDelta, Text: delta.Text}, nil
			case "input_json_delta":
				s.appendToolInput(event.Index, delta.PartialJSON)
			case "thinking_delta":
				return provider.Event{Type: provider.EventReasoningDelta, Reasoning: delta.Thinking}, nil
			case "signature_delta":
				continue
			default:
				return provider.Event{}, fmt.Errorf("anthropic streaming delta %q is not supported", delta.Type)
			}
		case "content_block_stop":
			s.enqueueToolUse(event.Index)
			if len(s.queue) > 0 {
				return s.Next(ctx)
			}
		case "message_delta":
			s.usage = &provider.Usage{
				InputTokens:  int(event.Usage.InputTokens),
				OutputTokens: int(event.Usage.OutputTokens),
			}
		case "message_stop":
			if s.usage != nil {
				s.queue = append(s.queue, provider.Event{Type: provider.EventUsage, Usage: s.usage})
				s.usage = nil
			}
			s.queue = append(s.queue, provider.Event{Type: provider.EventDone})
			return s.Next(ctx)
		}
		if err := ctx.Err(); err != nil {
			_ = s.Close()
			return provider.Event{}, err
		}
	}
	if err := s.stream.Err(); err != nil {
		return provider.Event{}, err
	}
	return provider.Event{}, io.EOF
}

func (s *sdkStream) startToolUse(index int64, block sdk.ContentBlockStartEventContentBlockUnion) {
	if s.tools == nil {
		s.tools = map[int64]*toolUseDelta{}
	}
	current := &toolUseDelta{
		id:   block.ID,
		name: block.Name,
	}
	if block.Input != nil {
		if raw, err := json.Marshal(block.Input); err == nil {
			current.input = raw
		}
	}
	s.tools[index] = current
}

func (s *sdkStream) appendToolInput(index int64, partial string) {
	if s.tools == nil {
		s.tools = map[int64]*toolUseDelta{}
	}
	current := s.tools[index]
	if current == nil {
		current = &toolUseDelta{}
		s.tools[index] = current
	}
	current.partial.WriteString(partial)
	current.hasDelta = true
}

func (s *sdkStream) enqueueToolUse(index int64) {
	current := s.tools[index]
	if current == nil {
		return
	}
	input := current.input
	if current.hasDelta {
		input = json.RawMessage(current.partial.String())
	}
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	s.queue = append(s.queue, provider.Event{
		Type:      provider.EventToolCall,
		ToolUseID: current.id,
		ToolName:  current.name,
		Input:     input,
	})
	delete(s.tools, index)
}

func (s *sdkStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.stream == nil {
		return nil
	}
	return s.stream.Close()
}
