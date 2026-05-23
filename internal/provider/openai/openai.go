// Package openai implements ub's OpenAI provider adapter.
package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/reasoning"
	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"
)

// Provider adapts OpenAI Chat Completions to provider.Provider.
type Provider struct {
	name   string
	client sdk.Client
}

type constructorOptions struct {
	requireAPIKey  bool
	allowDummyKey  bool
	requireBaseURL bool
}

func init() {
	provider.Register("openai", NewFromConfig)
}

// NewFromConfig creates an OpenAI provider from one config entry.
func NewFromConfig(name string, cfg config.ProviderConfig) (provider.Provider, error) {
	return newFromConfig(name, cfg, constructorOptions{requireAPIKey: true})
}

// NewCompatibleFromConfig creates an OpenAI-compatible provider using the same
// Chat Completions adapter while allowing local servers without API keys.
func NewCompatibleFromConfig(name string, cfg config.ProviderConfig) (provider.Provider, error) {
	return newFromConfig(name, cfg, constructorOptions{
		allowDummyKey:  true,
		requireBaseURL: true,
	})
}

func newFromConfig(name string, cfg config.ProviderConfig, opts constructorOptions) (provider.Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		if opts.requireAPIKey {
			return nil, fmt.Errorf("openai provider %q missing api_key", name)
		}
		if opts.allowDummyKey {
			cfg.APIKey = "unused"
		}
	}
	if strings.TrimSpace(cfg.BaseURL) == "" && opts.requireBaseURL {
		return nil, fmt.Errorf("openai-compatible provider %q missing base_url", name)
	}
	return &Provider{
		name:   name,
		client: BuildClient(cfg),
	}, nil
}

// BuildClient assembles an OpenAI SDK client from a provider config. It is
// shared by NewFromConfig/NewCompatibleFromConfig and the doctor
// model-listing code so both paths resolve base URL, timeout, and credentials
// identically.
func BuildClient(cfg config.ProviderConfig) sdk.Client {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		apiKey = "unused"
	}
	requestOpts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(buildHTTPClient(cfg.Timeout)),
	}
	if base := strings.TrimSpace(cfg.BaseURL); base != "" {
		requestOpts = append(requestOpts, option.WithBaseURL(base))
	}
	for key, value := range cfg.Headers {
		requestOpts = append(requestOpts, option.WithHeader(key, value))
	}
	return sdk.NewClient(requestOpts...)
}

// Name returns the configured provider name.
func (p *Provider) Name() string {
	return p.name
}

// Caps returns OpenAI capabilities available in I-11.
func (p *Provider) Caps() provider.Caps {
	return provider.Caps{
		SupportsTools:     true,
		SupportsStreaming: true,
		MaxContextTokens:  128_000,
		SupportsVision:    false,
	}
}

// Chat creates a streaming OpenAI ChatCompletion request.
func (p *Provider) Chat(ctx context.Context, req provider.Request) (provider.Stream, error) {
	if strings.TrimSpace(req.Model) == "" {
		return nil, errors.New("openai model is required")
	}
	params, err := toChatCompletionParams(req)
	if err != nil {
		return nil, err
	}
	return newSDKStream(p.client.Chat.Completions.NewStreaming(ctx, params)), nil
}

func toChatCompletionParams(req provider.Request) (sdk.ChatCompletionNewParams, error) {
	params := sdk.ChatCompletionNewParams{
		Model: sdk.ChatModel(req.Model),
		StreamOptions: sdk.ChatCompletionStreamOptionsParam{
			IncludeUsage: sdk.Bool(true),
		},
		ParallelToolCalls: sdk.Bool(false),
	}
	if req.Reasoning != nil && req.Reasoning.Effort != "" && req.Reasoning.Effort != reasoning.EffortNone {
		params.ReasoningEffort = shared.ReasoningEffort(string(req.Reasoning.Effort))
	}
	tools, err := toToolParams(req.Tools)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, err
	}
	params.Tools = tools
	for _, msg := range req.Messages {
		converted, err := toMessageParams(msg)
		if err != nil {
			return sdk.ChatCompletionNewParams{}, err
		}
		params.Messages = append(params.Messages, converted...)
	}
	if len(params.Messages) == 0 {
		return sdk.ChatCompletionNewParams{}, errors.New("openai request requires at least one message")
	}
	return params, nil
}

func toToolParams(defs []provider.ToolDefinition) ([]sdk.ChatCompletionToolUnionParam, error) {
	if len(defs) == 0 {
		return nil, nil
	}
	tools := make([]sdk.ChatCompletionToolUnionParam, 0, len(defs))
	for _, def := range defs {
		if strings.TrimSpace(def.Name) == "" {
			return nil, errors.New("openai tool name is required")
		}
		var schema map[string]any
		if len(def.Schema) > 0 {
			if err := json.Unmarshal(def.Schema, &schema); err != nil {
				return nil, fmt.Errorf("openai tool %q schema: %w", def.Name, err)
			}
		}
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		tools = append(tools, sdk.ChatCompletionFunctionTool(
			shared.FunctionDefinitionParam{
				Name:        def.Name,
				Description: param.NewOpt(def.Description),
				Parameters:  shared.FunctionParameters(schema),
			},
		))
	}
	return tools, nil
}

func toMessageParams(msg message.Message) ([]sdk.ChatCompletionMessageParamUnion, error) {
	switch msg.Role {
	case message.RoleSystem:
		text, err := textContent(msg)
		if err != nil {
			return nil, err
		}
		return []sdk.ChatCompletionMessageParamUnion{sdk.SystemMessage(text)}, nil
	case message.RoleUser:
		text, err := textContent(msg)
		if err != nil {
			return nil, err
		}
		return []sdk.ChatCompletionMessageParamUnion{sdk.UserMessage(text)}, nil
	case message.RoleAssistant:
		text, toolCalls, err := assistantContent(msg)
		if err != nil {
			return nil, err
		}
		out := sdk.AssistantMessage(text)
		out.OfAssistant.ToolCalls = toolCalls
		return []sdk.ChatCompletionMessageParamUnion{out}, nil
	case message.RoleTool:
		var out []sdk.ChatCompletionMessageParamUnion
		for _, block := range msg.Content {
			if block.Type != message.BlockToolResult {
				return nil, unsupportedBlock(block.Type)
			}
			out = append(out, sdk.ToolMessage(block.Output, block.ToolUseID))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("openai provider does not support role %q", msg.Role)
	}
}

func assistantContent(msg message.Message) (string, []sdk.ChatCompletionMessageToolCallUnionParam, error) {
	var parts []string
	var toolCalls []sdk.ChatCompletionMessageToolCallUnionParam
	for _, block := range msg.Content {
		switch block.Type {
		case message.BlockText:
			parts = append(parts, block.Text)
		case message.BlockToolUse:
			toolCalls = append(toolCalls, sdk.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &sdk.ChatCompletionMessageFunctionToolCallParam{
					ID: block.ToolUseID,
					Function: sdk.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      block.ToolName,
						Arguments: string(block.Input),
					},
				},
			})
		default:
			return "", nil, unsupportedBlock(block.Type)
		}
	}
	return strings.Join(parts, "\n"), toolCalls, nil
}

func textContent(msg message.Message) (string, error) {
	parts := make([]string, 0, len(msg.Content))
	for _, block := range msg.Content {
		if block.Type != message.BlockText {
			return "", unsupportedBlock(block.Type)
		}
		parts = append(parts, block.Text)
	}
	return strings.Join(parts, "\n"), nil
}

func unsupportedBlock(blockType message.BlockType) error {
	return fmt.Errorf("openai text provider does not support content block %q", blockType)
}

func effectiveTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return 120 * time.Second
}

// buildHTTPClient returns an *http.Client whose timeout bounds only the wait
// for the response headers, not the body. Streaming chat completions keep the
// body open for the entire conversation turn, so using http.Client.Timeout
// (which covers headers + body) would cut the SSE stream after that duration.
// ResponseHeaderTimeout on a cloned default transport gives us "first byte"
// semantics while leaving the streamed body uncapped.
func buildHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = effectiveTimeout(timeout)
	return &http.Client{Transport: transport}
}

type sdkStream struct {
	stream   *ssestream.Stream[sdk.ChatCompletionChunk]
	queue    []provider.Event
	usage    *provider.Usage
	tools    map[int64]*toolCallDelta
	order    []int64
	doneSent bool
	closed   bool
}

type toolCallDelta struct {
	id        string
	name      string
	arguments strings.Builder
}

func newSDKStream(stream *ssestream.Stream[sdk.ChatCompletionChunk]) *sdkStream {
	return &sdkStream{stream: stream}
}

func (s *sdkStream) Next(ctx context.Context) (provider.Event, error) {
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return provider.Event{}, err
	}
	if s.closed || s.doneSent {
		return provider.Event{}, io.EOF
	}
	if len(s.queue) > 0 {
		event := s.queue[0]
		s.queue = s.queue[1:]
		if event.Type == provider.EventDone {
			s.doneSent = true
		}
		return event, nil
	}
	if s.stream == nil {
		return provider.Event{}, io.EOF
	}
	for s.stream.Next() {
		chunk := s.stream.Current()
		if chunk.JSON.Usage.Valid() || chunk.Usage.PromptTokens != 0 || chunk.Usage.CompletionTokens != 0 {
			s.usage = eventUsage(chunk.Usage)
		}
		for _, choice := range chunk.Choices {
			if len(choice.Delta.ToolCalls) > 0 {
				s.addToolDeltas(choice.Delta.ToolCalls)
			}
			if reasoning := reasoningDelta(choice.Delta); reasoning != "" {
				if choice.Delta.Content != "" {
					s.queue = append(s.queue, provider.Event{Type: provider.EventTextDelta, Text: choice.Delta.Content})
				}
				return provider.Event{Type: provider.EventReasoningDelta, Reasoning: reasoning}, nil
			}
			if choice.Delta.Content != "" {
				return provider.Event{Type: provider.EventTextDelta, Text: choice.Delta.Content}, nil
			}
			if choice.Delta.FunctionCall.Name != "" || choice.Delta.FunctionCall.Arguments != "" {
				return provider.Event{}, errors.New("openai streaming function_call is not supported")
			}
			if choice.FinishReason == "tool_calls" {
				s.enqueueToolCalls()
				return s.Next(ctx)
			}
		}
		if err := ctx.Err(); err != nil {
			_ = s.Close()
			return provider.Event{}, err
		}
	}
	if err := s.stream.Err(); err != nil {
		return provider.Event{}, err
	}
	s.enqueueToolCalls()
	if len(s.queue) > 0 {
		return s.Next(ctx)
	}
	if s.usage != nil {
		s.queue = append(s.queue, provider.Event{Type: provider.EventUsage, Usage: s.usage})
		s.usage = nil
	}
	s.queue = append(s.queue, provider.Event{Type: provider.EventDone})
	return s.Next(ctx)
}

func reasoningDelta(delta sdk.ChatCompletionChunkChoiceDelta) string {
	raw := strings.TrimSpace(delta.RawJSON())
	if raw == "" {
		return ""
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return ""
	}
	for _, key := range []string{"reasoning_content", "reasoning", "thinking"} {
		value, ok := body[key].(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *sdkStream) addToolDeltas(deltas []sdk.ChatCompletionChunkChoiceDeltaToolCall) {
	if s.tools == nil {
		s.tools = map[int64]*toolCallDelta{}
	}
	for _, delta := range deltas {
		current := s.tools[delta.Index]
		if current == nil {
			current = &toolCallDelta{}
			s.tools[delta.Index] = current
			s.order = append(s.order, delta.Index)
		}
		if delta.ID != "" {
			current.id = delta.ID
		}
		if delta.Function.Name != "" {
			current.name = delta.Function.Name
		}
		if delta.Function.Arguments != "" {
			current.arguments.WriteString(delta.Function.Arguments)
		}
	}
}

func (s *sdkStream) enqueueToolCalls() {
	if len(s.order) == 0 {
		return
	}
	for _, index := range s.order {
		call := s.tools[index]
		if call == nil {
			continue
		}
		input := json.RawMessage(call.arguments.String())
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		} else if !json.Valid(input) {
			s.queue = append(s.queue, provider.Event{
				Type: provider.EventError,
				Err:  fmt.Errorf("tool call %q arguments truncated mid-stream (likely hit max_output_tokens before tool call completed): %s", call.name, string(input)),
			})
			continue
		}
		s.queue = append(s.queue, provider.Event{
			Type:      provider.EventToolCall,
			ToolUseID: call.id,
			ToolName:  call.name,
			Input:     input,
		})
	}
	s.tools = nil
	s.order = nil
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

func eventUsage(usage sdk.CompletionUsage) *provider.Usage {
	return &provider.Usage{
		InputTokens:     int(usage.PromptTokens),
		OutputTokens:    int(usage.CompletionTokens),
		ReasoningTokens: int(usage.CompletionTokensDetails.ReasoningTokens),
		CacheReadTokens: int(usage.PromptTokensDetails.CachedTokens),
	}
}
