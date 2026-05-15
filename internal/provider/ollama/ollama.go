// Package ollama implements ub's Ollama provider adapter.
package ollama

import (
	"bufio"
	"bytes"
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
)

const defaultBaseURL = "http://localhost:11434"

// Provider adapts Ollama /api/chat to provider.Provider.
type Provider struct {
	name    string
	baseURL string
	headers map[string]string
	client  *http.Client
}

func init() {
	provider.Register("ollama", NewFromConfig)
}

// NewFromConfig creates an Ollama provider from one config entry.
func NewFromConfig(name string, cfg config.ProviderConfig) (provider.Provider, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Provider{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		headers: cloneHeaders(cfg.Headers),
		client:  &http.Client{Timeout: effectiveTimeout(cfg.Timeout)},
	}, nil
}

// Name returns the configured provider name.
func (p *Provider) Name() string {
	return p.name
}

// Caps returns Ollama capabilities available in I-12.
func (p *Provider) Caps() provider.Caps {
	return provider.Caps{
		SupportsTools:     false,
		SupportsStreaming: true,
		MaxContextTokens:  32_000,
		SupportsVision:    false,
	}
}

// Chat creates a streaming Ollama /api/chat request.
func (p *Provider) Chat(ctx context.Context, req provider.Request) (provider.Stream, error) {
	if strings.TrimSpace(req.Model) == "" {
		return nil, errors.New("ollama model is required")
	}
	body, err := toChatRequest(req)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal ollama chat request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("create ollama chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range p.headers {
		httpReq.Header.Set(key, value)
	}
	res, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama chat request: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		defer res.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("ollama chat request failed: status %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return newStream(res.Body), nil
}

type chatRequest struct {
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func toChatRequest(req provider.Request) (chatRequest, error) {
	out := chatRequest{
		Model:  req.Model,
		Stream: true,
	}
	for _, msg := range req.Messages {
		content, err := textContent(msg)
		if err != nil {
			return chatRequest{}, err
		}
		switch msg.Role {
		case message.RoleSystem, message.RoleUser, message.RoleAssistant:
			out.Messages = append(out.Messages, chatMessage{
				Role:    string(msg.Role),
				Content: content,
			})
		default:
			return chatRequest{}, fmt.Errorf("ollama provider does not support role %q", msg.Role)
		}
	}
	if len(out.Messages) == 0 {
		return chatRequest{}, errors.New("ollama request requires at least one message")
	}
	return out, nil
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
	return fmt.Errorf("ollama text provider does not support content block %q", blockType)
}

func effectiveTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return 120 * time.Second
}

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type streamResponse struct {
	Message         chatMessage `json:"message"`
	Done            bool        `json:"done"`
	PromptEvalCount int         `json:"prompt_eval_count"`
	EvalCount       int         `json:"eval_count"`
	Error           string      `json:"error"`
}

type stream struct {
	body     io.ReadCloser
	scanner  *bufio.Scanner
	queue    []provider.Event
	doneSent bool
	closed   bool
}

func newStream(body io.ReadCloser) *stream {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(nil, bufio.MaxScanTokenSize<<4)
	return &stream{body: body, scanner: scanner}
}

func (s *stream) Next(ctx context.Context) (provider.Event, error) {
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
	for s.scanner.Scan() {
		var item streamResponse
		if err := json.Unmarshal(s.scanner.Bytes(), &item); err != nil {
			return provider.Event{}, fmt.Errorf("decode ollama stream: %w", err)
		}
		if item.Error != "" {
			return provider.Event{}, errors.New(item.Error)
		}
		if item.Message.Content != "" {
			return provider.Event{Type: provider.EventTextDelta, Text: item.Message.Content}, nil
		}
		if item.Done {
			if item.PromptEvalCount != 0 || item.EvalCount != 0 {
				s.queue = append(s.queue, provider.Event{
					Type: provider.EventUsage,
					Usage: &provider.Usage{
						InputTokens:  item.PromptEvalCount,
						OutputTokens: item.EvalCount,
					},
				})
			}
			s.queue = append(s.queue, provider.Event{Type: provider.EventDone})
			return s.Next(ctx)
		}
		if err := ctx.Err(); err != nil {
			_ = s.Close()
			return provider.Event{}, err
		}
	}
	if err := s.scanner.Err(); err != nil {
		return provider.Event{}, err
	}
	return provider.Event{}, io.EOF
}

func (s *stream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.body == nil {
		return nil
	}
	return s.body.Close()
}
