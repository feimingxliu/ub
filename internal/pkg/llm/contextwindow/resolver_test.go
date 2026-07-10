package contextwindow

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolverStaticPriorityAndUnknown(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want Resolution
	}{
		{
			name: "config wins",
			opts: Options{ConfiguredTokens: 200000, ModelInfoTokens: 100000, ProviderTokens: 128000},
			want: Resolution{MaxTokens: 200000, Source: SourceConfig, Confidence: ConfidenceExact},
		},
		{
			name: "model info before provider",
			opts: Options{ModelInfoTokens: 100000, ProviderTokens: 128000},
			want: Resolution{MaxTokens: 100000, Source: SourceModelInfo, Confidence: ConfidenceHigh},
		},
		{
			name: "provider fallback",
			opts: Options{ProviderTokens: 128000},
			want: Resolution{MaxTokens: 128000, Source: SourceProviderCaps, Confidence: ConfidenceMedium},
		},
		{
			name: "unknown",
			want: Resolution{Source: SourceUnknown, Confidence: ConfidenceUnknown},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolver, err := New(tc.opts)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got := resolver.Resolve(); got != tc.want {
				t.Fatalf("Resolve() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestResolverLearnsOverflowAndUsage(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	resolver, err := New(Options{
		Key:            NewKey("compat", "https://example.test/v1", "model"),
		ProviderTokens: 128000,
		Store:          newMemoryStore(),
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := resolver.ObserveAccepted(7000); err != nil {
		t.Fatalf("ObserveAccepted: %v", err)
	}
	if err := resolver.ObserveOverflow(errors.New("request failed: maximum context length is 8,192 tokens"), 9000); err != nil {
		t.Fatalf("ObserveOverflow: %v", err)
	}
	if got := resolver.Resolve(); got != (Resolution{MaxTokens: 8192, Source: SourceLearnedOverflow, Confidence: ConfidenceHigh}) {
		t.Fatalf("Resolve() = %#v", got)
	}

	// A successful observation proves that a smaller parsed maximum is stale or
	// belongs to conflicting backend behavior, so it must not shrink the window.
	if err := resolver.ObserveAccepted(10000); err != nil {
		t.Fatalf("ObserveAccepted conflict: %v", err)
	}
	if got := resolver.Resolve(); got.MaxTokens < 10000 || got.MaxTokens == 8192 {
		t.Fatalf("conflicting Resolve() = %#v", got)
	}
}

func TestResolverUsesUnparsedOverflowUpperBound(t *testing.T) {
	resolver, err := New(Options{ProviderTokens: 128000, Store: newMemoryStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := resolver.ObserveAccepted(7000); err != nil {
		t.Fatalf("ObserveAccepted: %v", err)
	}
	if err := resolver.ObserveOverflow(errors.New("context_length_exceeded"), 9000); err != nil {
		t.Fatalf("ObserveOverflow: %v", err)
	}
	want := Resolution{MaxTokens: 9000, Source: SourceLearnedOverflow, Confidence: ConfidenceLow}
	if got := resolver.Resolve(); got != want {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestResolverUsageRaisesStaleStaticCandidate(t *testing.T) {
	resolver, err := New(Options{ProviderTokens: 8192})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := resolver.ObserveAccepted(12000); err != nil {
		t.Fatalf("ObserveAccepted: %v", err)
	}
	want := Resolution{MaxTokens: 12000, Source: SourceLearnedUsage, Confidence: ConfidenceLow}
	if got := resolver.Resolve(); got != want {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestResolverConfigIgnoresLearnedWindow(t *testing.T) {
	store := newMemoryStore()
	key := NewKey("compat", "https://example.test/v1", "model")
	if err := store.Save(key, Observation{Version: 1, Key: key, ExactMaxTokens: 8192}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	resolver, err := New(Options{Key: key, ConfiguredTokens: 200000, ProviderTokens: 128000, Store: store})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := Resolution{MaxTokens: 200000, Source: SourceConfig, Confidence: ConfidenceExact}
	if got := resolver.Resolve(); got != want {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestResolverSaveFailureKeepsInMemoryObservation(t *testing.T) {
	store := newMemoryStore()
	store.saveErr = errors.New("disk full")
	resolver, err := New(Options{ProviderTokens: 8192, Store: store})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := resolver.ObserveAccepted(12000); err == nil {
		t.Fatal("ObserveAccepted error = nil, want save failure")
	}
	if got := resolver.Resolve(); got.MaxTokens != 12000 || got.Source != SourceLearnedUsage {
		t.Fatalf("Resolve() after save failure = %#v", got)
	}
}

func TestParseOverflowLimit(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{"This model's maximum context length is 8192 tokens. Your messages resulted in 9000 tokens.", 8192},
		{"context window limit: 128,000 tokens", 128000},
		{"prompt is too long: 200001 tokens > 200_000 maximum", 200000},
		{"context_length_exceeded", 0},
		{"input is 9000 tokens and max output tokens is 4096", 0},
		{"rate limited after 8192 tokens", 0},
	}
	for _, tc := range tests {
		if got := ParseOverflowLimit(errors.New(tc.text)); got != tc.want {
			t.Errorf("ParseOverflowLimit(%q) = %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestFileStoreIsolationPrivacyAndReload(t *testing.T) {
	root := filepath.Join(t.TempDir(), "context-windows")
	store := NewFileStore(root)
	keyA := NewKey("Compat", "https://user:secret@A.example/v1/?api_key=hidden#fragment", "org/model")
	keyB := NewKey("Compat", "https://b.example/v1", "org/model")
	if strings.Contains(keyA.Endpoint, "secret") || strings.Contains(keyA.Endpoint, "api_key") || strings.Contains(keyA.Endpoint, "fragment") {
		t.Fatalf("normalized endpoint leaked sensitive data: %q", keyA.Endpoint)
	}

	resolver, err := New(Options{Key: keyA, ProviderTokens: 128000, Store: store})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := resolver.ObserveOverflow(errors.New("maximum context length is 8192 tokens"), 9000); err != nil {
		t.Fatalf("ObserveOverflow: %v", err)
	}
	reloaded, err := New(Options{Key: keyA, ProviderTokens: 128000, Store: store})
	if err != nil {
		t.Fatalf("reload New: %v", err)
	}
	if got := reloaded.Resolve(); got.MaxTokens != 8192 {
		t.Fatalf("reloaded Resolve() = %#v", got)
	}
	isolated, err := New(Options{Key: keyB, ProviderTokens: 128000, Store: store})
	if err != nil {
		t.Fatalf("isolated New: %v", err)
	}
	if got := isolated.Resolve(); got.MaxTokens != 128000 || got.Source != SourceProviderCaps {
		t.Fatalf("isolated Resolve() = %#v", got)
	}

	path := store.Path(keyA)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("cache mode = %#o, want 0600", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(raw)
	for _, secret := range []string{"user", "secret", "api_key", "hidden", "fragment"} {
		if strings.Contains(content, secret) {
			t.Fatalf("cache content leaked %q: %s", secret, content)
		}
	}
}

func TestFileStoreCorruptCacheReturnsUsableResolver(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "context-windows"))
	key := NewKey("compat", "https://example.test/v1", "model")
	path := store.Path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{bad json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	resolver, err := New(Options{Key: key, ProviderTokens: 128000, Store: store})
	if err == nil {
		t.Fatal("New error = nil, want corrupt cache error")
	}
	if resolver == nil {
		t.Fatal("New returned nil resolver on cache error")
	}
	if got := resolver.Resolve(); got.MaxTokens != 128000 || got.Source != SourceProviderCaps {
		t.Fatalf("fallback Resolve() = %#v", got)
	}
}

type memoryStore struct {
	values  map[Key]Observation
	loadErr error
	saveErr error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{values: map[Key]Observation{}}
}

func (s *memoryStore) Load(key Key) (Observation, bool, error) {
	if s.loadErr != nil {
		return Observation{}, false, s.loadErr
	}
	value, ok := s.values[key]
	return value, ok, nil
}

func (s *memoryStore) Save(key Key, observation Observation) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.values[key] = observation
	return nil
}
