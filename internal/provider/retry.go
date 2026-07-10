package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"reflect"
	"time"
)

const (
	defaultMaxRetryAttempts = 3
	defaultBaseDelay        = 500 * time.Millisecond
	defaultMaxDelay         = 10 * time.Second
	defaultJitter           = 250 * time.Millisecond
)

var (
	ioEOF             = io.EOF
	retryBackoffDelay = backoffDelay
)

// RetryableStreamFactory creates a fresh Stream. It is called again when a
// transient connection error occurs.
type RetryableStreamFactory func() (Stream, error)

// NewRetryStream calls factory immediately and retries transient connection
// errors. Some SDKs store initial HTTP failures inside the returned stream and
// expose them on the first Next() call, so the returned stream also retries
// retryable first-Next errors. Once any event has been emitted, errors are
// returned as-is to avoid duplicating partial model output.
func NewRetryStream(ctx context.Context, name string, factory RetryableStreamFactory) (Stream, error) {
	stream := &retryStream{name: name, factory: factory}
	if err := stream.connect(ctx); err != nil {
		return nil, err
	}
	return stream, nil
}

func backoffDelay(attempt int) time.Duration {
	delay := defaultBaseDelay * time.Duration(1<<uint(attempt-1))
	if delay > defaultMaxDelay {
		delay = defaultMaxDelay
	}
	jitter := time.Duration(rand.Int63n(int64(defaultJitter)))
	delay += jitter
	return delay
}

type retryStream struct {
	name     string
	factory  RetryableStreamFactory
	stream   Stream
	attempts int
	emitted  bool
	lastErr  error
}

func (s *retryStream) Next(ctx context.Context) (Event, error) {
	for {
		event, err := s.stream.Next(ctx)
		if err == nil {
			s.emitted = true
			return event, nil
		}
		if s.emitted || !isRetryableProviderErr(err) {
			return Event{}, normalizeProviderErr(s.name, err)
		}
		s.lastErr = err
		if s.attempts >= defaultMaxRetryAttempts {
			return Event{}, s.exhaustedErr()
		}
		_ = s.stream.Close()
		if err := sleepBeforeRetry(ctx, s.attempts); err != nil {
			return Event{}, err
		}
		if err := s.connect(ctx); err != nil {
			return Event{}, err
		}
	}
}

func (s *retryStream) Close() error {
	if s.stream == nil {
		return nil
	}
	return s.stream.Close()
}

func (s *retryStream) connect(ctx context.Context) error {
	for s.attempts < defaultMaxRetryAttempts {
		stream, err := s.factory()
		s.attempts++
		if err == nil {
			s.stream = stream
			return nil
		}
		if !isRetryableProviderErr(err) {
			return normalizeProviderErr(s.name, err)
		}
		s.lastErr = err
		if s.attempts >= defaultMaxRetryAttempts {
			break
		}
		if err := sleepBeforeRetry(ctx, s.attempts); err != nil {
			return err
		}
	}
	return s.exhaustedErr()
}

func (s *retryStream) exhaustedErr() error {
	return fmt.Errorf("%s: failed after %d attempts: %w", s.name, defaultMaxRetryAttempts, s.lastErr)
}

func sleepBeforeRetry(ctx context.Context, attempt int) error {
	delay := retryBackoffDelay(attempt)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// isRetryableProviderErr classifies errors from stream creation and from the
// first Next() call. AuthError and non-retryable HTTP statuses are not retried.
func isRetryableProviderErr(err error) bool {
	if err == nil {
		return false
	}
	var authErr *AuthError
	if errors.As(err, &authErr) {
		return false
	}
	if status, ok := statusCode(err); ok {
		return IsRetryableStatus(status)
	}
	return IsTransientErr(err)
}

func normalizeProviderErr(name string, err error) error {
	if err == nil {
		return nil
	}
	if status, ok := statusCode(err); ok && isAuthStatus(status) {
		return &AuthError{Provider: name, Status: status}
	}
	return err
}

func isAuthStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

func statusCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	for _, current := range unwrapErrors(err) {
		if status, ok := statusCodeFromValue(current); ok {
			return status, true
		}
	}
	return 0, false
}

func unwrapErrors(err error) []error {
	if err == nil {
		return nil
	}
	out := []error{err}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range wrapped.Unwrap() {
			out = append(out, unwrapErrors(child)...)
		}
	case interface{ Unwrap() error }:
		out = append(out, unwrapErrors(wrapped.Unwrap())...)
	}
	return out
}

func statusCodeFromValue(err error) (int, bool) {
	value := reflect.ValueOf(err)
	if !value.IsValid() {
		return 0, false
	}
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return 0, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 0, false
	}
	field := value.FieldByName("StatusCode")
	if !field.IsValid() || !field.CanInt() {
		return 0, false
	}
	status := int(field.Int())
	return status, status > 0
}
