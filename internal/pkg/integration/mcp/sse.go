package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// sseTransport implements the MCP SSE transport: a long-lived GET connection
// receives server-pushed messages as Server-Sent Events, while JSON-RPC requests
// are sent via separate POST calls to an endpoint URL advertised by the server
// in its initial "endpoint" event. The transport serializes calls via a mutex
// because the SSE protocol does not natively support multiplexing.
type sseTransport struct {
	baseURL  string
	endpoint string
	headers  map[string]string
	client   *http.Client
	cancel   context.CancelFunc
	body     io.ReadCloser
	messages chan []byte
	errs     chan error
	mu       sync.Mutex
}

// newSSETransport establishes the SSE connection and waits for the server's
// endpoint announcement. It blocks until the endpoint event arrives, an error
// occurs on the stream, or ctx is cancelled. The returned transport is ready
// for Call/Notify immediately.
func newSSETransport(ctx context.Context, cfg SSEConfig) (*sseTransport, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("mcp sse: url is required")
	}
	streamCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, strings.TrimSpace(cfg.URL), nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("mcp sse: connect: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		cancel()
		return nil, fmt.Errorf("mcp sse: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	t := &sseTransport{
		baseURL:  strings.TrimSpace(cfg.URL),
		headers:  cloneHeaders(cfg.Headers),
		client:   http.DefaultClient,
		cancel:   cancel,
		body:     resp.Body,
		messages: make(chan []byte, 32),
		errs:     make(chan error, 1),
	}
	endpoint := make(chan string, 1)
	go t.readLoop(endpoint)
	select {
	case ep := <-endpoint:
		t.endpoint, err = resolveEndpoint(t.baseURL, ep)
		if err != nil {
			_ = t.Close()
			return nil, err
		}
		return t, nil
	case err := <-t.errs:
		_ = t.Close()
		return nil, err
	case <-ctx.Done():
		_ = t.Close()
		return nil, ctx.Err()
	}
}

// Call sends a JSON-RPC request via POST and waits for the matching response
// on the SSE message channel. Responses are matched by request ID; non-matching
// messages are skipped. The mutex ensures only one call is in-flight at a time.
func (t *sseTransport) Call(ctx context.Context, req rpcRequest) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := t.post(ctx, payload); err != nil {
		return nil, err
	}
	for {
		select {
		case raw := <-t.messages:
			var resp rpcResponse
			if err := json.Unmarshal(raw, &resp); err != nil {
				return nil, fmt.Errorf("mcp sse: decode message: %w", err)
			}
			if resp.ID == nil || *resp.ID != req.ID {
				continue
			}
			if resp.Error != nil {
				return nil, resp.Error
			}
			return resp.Result, nil
		case err := <-t.errs:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (t *sseTransport) Notify(ctx context.Context, method string, params any) error {
	payload, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return err
	}
	return t.post(ctx, payload)
}

// Close cancels the SSE stream context and closes the response body,
// stopping the readLoop goroutine. It is idempotent.
func (t *sseTransport) Close() error {
	if t == nil {
		return nil
	}
	t.cancel()
	if t.body != nil {
		return t.body.Close()
	}
	return nil
}

func (t *sseTransport) post(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("mcp sse: post: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("mcp sse: read post response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mcp sse: post status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// readLoop reads SSE events from the response body line by line, dispatching
// "endpoint" events to the endpoint channel (once) and "message" events to
// the messages channel. On scanner error it sends the error to errs; on clean
// EOF it sends io.EOF so Call unblocks. The messages channel is closed on exit
// so range loops in Call can detect stream termination.
func (t *sseTransport) readLoop(endpoint chan<- string) {
	defer close(t.messages)
	scanner := bufio.NewScanner(t.body)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	event := ""
	var data []string
	announcedEndpoint := false
	dispatch := func() {
		if len(data) == 0 {
			event = ""
			return
		}
		payload := strings.Join(data, "\n")
		switch event {
		case "endpoint":
			if !announcedEndpoint {
				announcedEndpoint = true
				endpoint <- payload
			}
		case "", "message":
			select {
			case t.messages <- []byte(payload):
			default:
			}
		}
		event = ""
		data = nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			dispatch()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch key {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
	dispatch()
	if err := scanner.Err(); err != nil {
		select {
		case t.errs <- fmt.Errorf("mcp sse: read stream: %w", err):
		default:
		}
		return
	}
	select {
	case t.errs <- io.EOF:
	default:
	}
}

// resolveEndpoint resolves the server-advertised endpoint URL against the
// base SSE URL. If the endpoint is absolute it is used as-is; otherwise it is
// resolved as a relative reference against the base URL.
func resolveEndpoint(base, endpoint string) (string, error) {
	ep, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", fmt.Errorf("mcp sse: parse endpoint: %w", err)
	}
	if ep.IsAbs() {
		return ep.String(), nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("mcp sse: parse base url: %w", err)
	}
	return baseURL.ResolveReference(ep).String(), nil
}
