// Package openai implements ub's OpenAI provider adapter.
package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	sdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
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
	if strings.TrimSpace(cfg.BaseURL) == "" {
		if opts.requireBaseURL {
			return nil, fmt.Errorf("openai-compatible provider %q missing base_url", name)
		}
	} else {
		cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	}
	requestOpts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithHTTPClient(&http.Client{Timeout: effectiveTimeout(cfg.Timeout)}),
	}
	if cfg.BaseURL != "" {
		requestOpts = append(requestOpts, option.WithBaseURL(cfg.BaseURL))
	}
	for key, value := range cfg.Headers {
		requestOpts = append(requestOpts, option.WithHeader(key, value))
	}
	return &Provider{
		name:   name,
		client: sdk.NewClient(requestOpts...),
	}, nil
}

// Name returns the configured provider name.
func (p *Provider) Name() string {
	return p.name
}

// Caps returns OpenAI capabilities available in I-11.
func (p *Provider) Caps() provider.Caps {
	return provider.Caps{
		SupportsTools:     false,
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
	}
	for _, msg := range req.Messages {
		text, err := textContent(msg)
		if err != nil {
			return sdk.ChatCompletionNewParams{}, err
		}
		switch msg.Role {
		case message.RoleSystem:
			params.Messages = append(params.Messages, sdk.SystemMessage(text))
		case message.RoleUser:
			params.Messages = append(params.Messages, sdk.UserMessage(text))
		case message.RoleAssistant:
			params.Messages = append(params.Messages, sdk.AssistantMessage(text))
		default:
			return sdk.ChatCompletionNewParams{}, fmt.Errorf("openai provider does not support role %q", msg.Role)
		}
	}
	if len(params.Messages) == 0 {
		return sdk.ChatCompletionNewParams{}, errors.New("openai request requires at least one message")
	}
	return params, nil
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

func eventsFromCompletion(msg *sdk.ChatCompletion) []provider.Event {
	if msg == nil {
		return []provider.Event{{Type: provider.EventDone}}
	}
	events := make([]provider.Event, 0, len(msg.Choices)+2)
	for _, choice := range msg.Choices {
		if choice.Message.Content != "" {
			events = append(events, provider.Event{
				Type: provider.EventTextDelta,
				Text: choice.Message.Content,
			})
		}
	}
	if usage := eventUsage(msg.Usage); usage != nil {
		events = append(events, provider.Event{Type: provider.EventUsage, Usage: usage})
	}
	events = append(events, provider.Event{Type: provider.EventDone})
	return events
}

func effectiveTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return 120 * time.Second
}

type sdkStream struct {
	stream   *ssestream.Stream[sdk.ChatCompletionChunk]
	queue    []provider.Event
	usage    *provider.Usage
	doneSent bool
	closed   bool
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
			if choice.Delta.Content != "" {
				return provider.Event{Type: provider.EventTextDelta, Text: choice.Delta.Content}, nil
			}
			if len(choice.Delta.ToolCalls) > 0 || choice.Delta.FunctionCall.Name != "" || choice.Delta.FunctionCall.Arguments != "" {
				return provider.Event{}, errors.New("openai streaming tool calls are not supported")
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
	if s.usage != nil {
		s.queue = append(s.queue, provider.Event{Type: provider.EventUsage, Usage: s.usage})
		s.usage = nil
	}
	s.queue = append(s.queue, provider.Event{Type: provider.EventDone})
	return s.Next(ctx)
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
		InputTokens:  int(usage.PromptTokens),
		OutputTokens: int(usage.CompletionTokens),
	}
}
