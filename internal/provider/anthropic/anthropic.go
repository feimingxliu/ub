// Package anthropic implements ub's Anthropic provider adapter.
package anthropic

import (
	"context"
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
		SupportsTools:     false,
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
	for _, msg := range req.Messages {
		switch msg.Role {
		case message.RoleSystem:
			system, err := systemTextBlocks(msg)
			if err != nil {
				return sdk.MessageNewParams{}, err
			}
			params.System = append(params.System, system...)
		case message.RoleUser:
			blocks, err := contentTextBlocks(msg)
			if err != nil {
				return sdk.MessageNewParams{}, err
			}
			params.Messages = append(params.Messages, sdk.NewUserMessage(blocks...))
		case message.RoleAssistant:
			blocks, err := contentTextBlocks(msg)
			if err != nil {
				return sdk.MessageNewParams{}, err
			}
			params.Messages = append(params.Messages, sdk.NewAssistantMessage(blocks...))
		default:
			return sdk.MessageNewParams{}, fmt.Errorf("anthropic provider does not support role %q", msg.Role)
		}
	}
	if len(params.Messages) == 0 {
		return sdk.MessageNewParams{}, errors.New("anthropic request requires at least one user or assistant message")
	}
	return params, nil
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

func contentTextBlocks(msg message.Message) ([]sdk.ContentBlockParamUnion, error) {
	blocks := make([]sdk.ContentBlockParamUnion, 0, len(msg.Content))
	for _, block := range msg.Content {
		if block.Type != message.BlockText {
			return nil, unsupportedBlock(block.Type)
		}
		blocks = append(blocks, sdk.NewTextBlock(block.Text))
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
	closed bool
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
			if event.ContentBlock.Type != "" && event.ContentBlock.Type != "text" {
				return provider.Event{}, fmt.Errorf("anthropic streaming content block %q is not supported", event.ContentBlock.Type)
			}
		case "content_block_delta":
			delta := event.AsContentBlockDelta().Delta
			if delta.Type != "text_delta" {
				return provider.Event{}, fmt.Errorf("anthropic streaming delta %q is not supported", delta.Type)
			}
			return provider.Event{Type: provider.EventTextDelta, Text: delta.Text}, nil
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
