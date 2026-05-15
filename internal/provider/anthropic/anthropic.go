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

// Caps returns Anthropic capabilities available in I-08.
func (p *Provider) Caps() provider.Caps {
	return provider.Caps{
		SupportsStreaming: false,
		SupportsTools:     false,
		MaxContextTokens:  200_000,
		SupportsVision:    false,
	}
}

// Chat performs one non-streaming Anthropic Messages call and wraps the result
// in a provider stream.
func (p *Provider) Chat(ctx context.Context, req provider.Request) (provider.Stream, error) {
	if strings.TrimSpace(req.Model) == "" {
		return nil, errors.New("anthropic model is required")
	}
	params, err := toMessageParams(req)
	if err != nil {
		return nil, err
	}
	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, err
	}
	return newStream(eventsFromMessage(msg)), nil
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

func eventsFromMessage(msg *sdk.Message) []provider.Event {
	events := make([]provider.Event, 0, len(msg.Content)+2)
	for _, block := range msg.Content {
		if block.Type == "text" {
			events = append(events, provider.Event{
				Type: provider.EventTextDelta,
				Text: block.Text,
			})
		}
	}
	events = append(events, provider.Event{
		Type: provider.EventUsage,
		Usage: &provider.Usage{
			InputTokens:  int(msg.Usage.InputTokens),
			OutputTokens: int(msg.Usage.OutputTokens),
		},
	})
	events = append(events, provider.Event{Type: provider.EventDone})
	return events
}

func effectiveTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return 120 * time.Second
}

type stream struct {
	events []provider.Event
	next   int
	closed bool
}

func newStream(events []provider.Event) *stream {
	return &stream{events: cloneEvents(events)}
}

func (s *stream) Next(ctx context.Context) (provider.Event, error) {
	if err := ctx.Err(); err != nil {
		return provider.Event{}, err
	}
	if s.closed || s.next >= len(s.events) {
		return provider.Event{}, io.EOF
	}
	event := s.events[s.next]
	s.next++
	return event, nil
}

func (s *stream) Close() error {
	s.closed = true
	return nil
}

func cloneEvents(events []provider.Event) []provider.Event {
	out := make([]provider.Event, len(events))
	for i, event := range events {
		out[i] = event
		if event.Usage != nil {
			usage := *event.Usage
			out[i].Usage = &usage
		}
	}
	return out
}
