package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestIsRetryableStatus(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{408, true},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{529, true},
		{200, false},
	}
	for _, tt := range tests {
		if got := IsRetryableStatus(tt.status); got != tt.want {
			t.Errorf("IsRetryableStatus(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestIsTransientErr(t *testing.T) {
	if IsTransientErr(nil) {
		t.Error("IsTransientErr(nil) = true, want false")
	}
	if IsTransientErr(context.Canceled) {
		t.Error("IsTransientErr(context.Canceled) = true, want false")
	}
	if IsTransientErr(context.DeadlineExceeded) {
		t.Error("IsTransientErr(context.DeadlineExceeded) = true, want false")
	}
	// Generic errors should be transient.
	if !IsTransientErr(errors.New("connection reset")) {
		t.Error("IsTransientErr(generic) = false, want true")
	}
}

func TestAuthError(t *testing.T) {
	err := &AuthError{Provider: "openai", KeyEnv: "OPENAI_API_KEY", Status: http.StatusUnauthorized}
	msg := err.Error()
	if msg == "" {
		t.Fatal("AuthError.Error() returned empty string")
	}
	if !containsStr(msg, "openai") {
		t.Fatalf("AuthError message = %q, want to contain 'openai'", msg)
	}
	if !containsStr(msg, "401") {
		t.Fatalf("AuthError message = %q, want to contain '401'", msg)
	}
	if !containsStr(msg, "OPENAI_API_KEY") {
		t.Fatalf("AuthError message = %q, want to contain 'OPENAI_API_KEY'", msg)
	}
	// Without KeyEnv.
	err2 := &AuthError{Provider: "anthropic", Status: http.StatusForbidden}
	msg2 := err2.Error()
	if !containsStr(msg2, "the API key") {
		t.Fatalf("AuthError message without KeyEnv = %q, want to contain 'the API key'", msg2)
	}
}

func TestNewRetryStreamSuccessOnFirstAttempt(t *testing.T) {
	callCount := 0
	stream, err := NewRetryStream(context.Background(), "test", func() (Stream, error) {
		callCount++
		return &fakeStream{}, nil
	})
	if err != nil {
		t.Fatalf("NewRetryStream: %v", err)
	}
	if stream == nil {
		t.Fatal("expected non-nil stream")
	}
	if callCount != 1 {
		t.Fatalf("factory called %d times, want 1", callCount)
	}
}

func TestNewRetryStreamRetriesOnTransientError(t *testing.T) {
	withoutRetryDelay(t)
	callCount := 0
	stream, err := NewRetryStream(context.Background(), "test", func() (Stream, error) {
		callCount++
		if callCount < 3 {
			return nil, fmt.Errorf("connection reset")
		}
		return &fakeStream{}, nil
	})
	if err != nil {
		t.Fatalf("NewRetryStream: %v", err)
	}
	if stream == nil {
		t.Fatal("expected non-nil stream")
	}
	if callCount != 3 {
		t.Fatalf("factory called %d times, want 3", callCount)
	}
}

func TestNewRetryStreamDoesNotRetryAuthError(t *testing.T) {
	callCount := 0
	_, err := NewRetryStream(context.Background(), "test", func() (Stream, error) {
		callCount++
		return nil, &AuthError{Provider: "test", Status: 401}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if callCount != 1 {
		t.Fatalf("factory called %d times, want 1 (auth error not retried)", callCount)
	}
}

func TestNewRetryStreamExhaustsAttempts(t *testing.T) {
	withoutRetryDelay(t)
	callCount := 0
	_, err := NewRetryStream(context.Background(), "test", func() (Stream, error) {
		callCount++
		return nil, fmt.Errorf("connection reset")
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !containsStr(err.Error(), "failed after 3 attempts") {
		t.Fatalf("error = %q, want to contain 'failed after 3 attempts'", err.Error())
	}
	if callCount != 3 {
		t.Fatalf("factory called %d times, want 3", callCount)
	}
}

func TestNewRetryStreamRetriesInitialNextError(t *testing.T) {
	withoutRetryDelay(t)
	callCount := 0
	stream, err := NewRetryStream(context.Background(), "test", func() (Stream, error) {
		callCount++
		if callCount < 3 {
			return &errorStream{err: statusError{StatusCode: http.StatusTooManyRequests}}, nil
		}
		return &singleEventStream{event: Event{Type: EventTextDelta, Text: "ok"}}, nil
	})
	if err != nil {
		t.Fatalf("NewRetryStream: %v", err)
	}
	event, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if event.Text != "ok" {
		t.Fatalf("event = %#v, want ok text", event)
	}
	if callCount != 3 {
		t.Fatalf("factory called %d times, want 3", callCount)
	}
}

func TestNewRetryStreamDoesNotRetryAfterEventEmitted(t *testing.T) {
	withoutRetryDelay(t)
	callCount := 0
	stream, err := NewRetryStream(context.Background(), "test", func() (Stream, error) {
		callCount++
		return &eventThenErrorStream{
			event: Event{Type: EventTextDelta, Text: "partial"},
			err:   statusError{StatusCode: http.StatusInternalServerError},
		}, nil
	})
	if err != nil {
		t.Fatalf("NewRetryStream: %v", err)
	}
	if event, err := stream.Next(context.Background()); err != nil || event.Text != "partial" {
		t.Fatalf("first Next = %#v, %v; want partial text", event, err)
	}
	if _, err := stream.Next(context.Background()); err == nil {
		t.Fatal("second Next expected error")
	}
	if callCount != 1 {
		t.Fatalf("factory called %d times, want 1", callCount)
	}
}

func TestNewRetryStreamDoesNotRetryNonRetryableStatus(t *testing.T) {
	withoutRetryDelay(t)
	callCount := 0
	stream, err := NewRetryStream(context.Background(), "test", func() (Stream, error) {
		callCount++
		return &errorStream{err: statusError{StatusCode: http.StatusBadRequest}}, nil
	})
	if err != nil {
		t.Fatalf("NewRetryStream: %v", err)
	}
	if _, err := stream.Next(context.Background()); err == nil {
		t.Fatal("expected status error")
	}
	if callCount != 1 {
		t.Fatalf("factory called %d times, want 1", callCount)
	}
}

func TestNewRetryStreamWrapsAuthStatus(t *testing.T) {
	stream, err := NewRetryStream(context.Background(), "test-provider", func() (Stream, error) {
		return &errorStream{err: statusError{StatusCode: http.StatusUnauthorized}}, nil
	})
	if err != nil {
		t.Fatalf("NewRetryStream: %v", err)
	}
	_, err = stream.Next(context.Background())
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T %v, want AuthError", err, err)
	}
	if authErr.Provider != "test-provider" || authErr.Status != http.StatusUnauthorized {
		t.Fatalf("AuthError = %#v", authErr)
	}
}

type fakeStream struct{}

func (s *fakeStream) Next(_ context.Context) (Event, error) { return Event{}, ioEOF }
func (s *fakeStream) Close() error                          { return nil }

type errorStream struct {
	err error
}

func (s *errorStream) Next(_ context.Context) (Event, error) { return Event{}, s.err }
func (s *errorStream) Close() error                          { return nil }

type singleEventStream struct {
	event Event
	done  bool
}

func (s *singleEventStream) Next(_ context.Context) (Event, error) {
	if s.done {
		return Event{}, ioEOF
	}
	s.done = true
	return s.event, nil
}
func (s *singleEventStream) Close() error { return nil }

type eventThenErrorStream struct {
	event Event
	err   error
	next  int
}

func (s *eventThenErrorStream) Next(_ context.Context) (Event, error) {
	if s.next == 0 {
		s.next++
		return s.event, nil
	}
	return Event{}, s.err
}
func (s *eventThenErrorStream) Close() error { return nil }

type statusError struct {
	StatusCode int
}

func (e statusError) Error() string { return fmt.Sprintf("status %d", e.StatusCode) }

func withoutRetryDelay(t *testing.T) {
	t.Helper()
	old := retryBackoffDelay
	retryBackoffDelay = func(int) time.Duration { return 0 }
	t.Cleanup(func() {
		retryBackoffDelay = old
	})
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
