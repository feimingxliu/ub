// Package contextwindow resolves a provider/model context window from static
// metadata and learned runtime observations.
package contextwindow

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Source identifies where a resolved context window came from.
type Source string

const (
	SourceUnknown         Source = "unknown"
	SourceConfig          Source = "config"
	SourceLearnedOverflow Source = "learned_overflow"
	SourceModelInfo       Source = "model_info"
	SourceProviderCaps    Source = "provider_caps"
	SourceLearnedUsage    Source = "learned_usage"
)

// Confidence describes how precisely MaxTokens represents the backend limit.
type Confidence string

const (
	ConfidenceUnknown Confidence = "unknown"
	ConfidenceLow     Confidence = "low"
	ConfidenceMedium  Confidence = "medium"
	ConfidenceHigh    Confidence = "high"
	ConfidenceExact   Confidence = "exact"
)

// Key isolates learned observations by configured provider endpoint and model.
type Key struct {
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model"`
}

// NewKey returns a normalized, persistence-safe cache key. Endpoint userinfo,
// query parameters, and fragments are deliberately discarded.
func NewKey(providerName, endpoint, model string) Key {
	return Key{
		Provider: strings.ToLower(strings.TrimSpace(providerName)),
		Endpoint: normalizeEndpoint(endpoint),
		Model:    strings.TrimSpace(model),
	}
}

// Resolution is the effective context window used by Agent decisions.
type Resolution struct {
	MaxTokens  int        `json:"max_tokens"`
	Source     Source     `json:"source"`
	Confidence Confidence `json:"confidence"`
}

// Observation is the derived state persisted for one cache key.
type Observation struct {
	Version                  int       `json:"version"`
	Key                      Key       `json:"key"`
	ExactMaxTokens           int       `json:"exact_max_tokens,omitempty"`
	ExactObservedAt          time.Time `json:"exact_observed_at,omitempty"`
	OverflowUpperBoundTokens int       `json:"overflow_upper_bound_tokens,omitempty"`
	OverflowObservedAt       time.Time `json:"overflow_observed_at,omitempty"`
	AcceptedInputTokens      int       `json:"accepted_input_tokens,omitempty"`
	AcceptedObservedAt       time.Time `json:"accepted_observed_at,omitempty"`
	UpdatedAt                time.Time `json:"updated_at"`
}

// Store persists learned observations. Implementations must isolate values by
// Key and may treat a missing value as found=false, err=nil.
type Store interface {
	Load(Key) (Observation, bool, error)
	Save(Key, Observation) error
}

// Options configures a Resolver. ConfiguredTokens is an explicit user
// override; ModelInfoTokens and ProviderTokens are non-authoritative fallbacks.
type Options struct {
	Key              Key
	ConfiguredTokens int
	ModelInfoTokens  int
	ProviderTokens   int
	Store            Store
	Now              func() time.Time
}

// Resolver combines static candidates with learned usage and overflow state.
// It is safe for concurrent use by a parent Agent and its child agents.
type Resolver struct {
	mu               sync.RWMutex
	key              Key
	configuredTokens int
	modelInfoTokens  int
	providerTokens   int
	observation      Observation
	store            Store
	now              func() time.Time
}

// New constructs a resolver and loads any cached observation.
func New(opts Options) (*Resolver, error) {
	key := NewKey(opts.Key.Provider, opts.Key.Endpoint, opts.Key.Model)
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	r := &Resolver{
		key:              key,
		configuredTokens: positive(opts.ConfiguredTokens),
		modelInfoTokens:  positive(opts.ModelInfoTokens),
		providerTokens:   positive(opts.ProviderTokens),
		store:            opts.Store,
		now:              now,
		observation: Observation{
			Version: 1,
			Key:     key,
		},
	}
	if opts.Store == nil {
		return r, nil
	}
	observation, found, err := opts.Store.Load(key)
	if err != nil {
		return r, err
	}
	if found {
		r.observation = normalizeObservation(key, observation)
	}
	return r, nil
}

// Key returns the normalized observation key.
func (r *Resolver) Key() Key {
	if r == nil {
		return Key{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.key
}

// Resolve returns the current effective window.
func (r *Resolver) Resolve() Resolution {
	if r == nil {
		return Resolution{Source: SourceUnknown, Confidence: ConfidenceUnknown}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return resolve(
		r.configuredTokens,
		r.modelInfoTokens,
		r.providerTokens,
		r.observation,
	)
}

// ObserveAccepted records a successful provider input-token usage. Persistence
// errors are returned after the in-memory observation has been updated.
func (r *Resolver) ObserveAccepted(inputTokens int) error {
	if r == nil || inputTokens <= 0 {
		return nil
	}
	now := r.now().UTC()
	r.mu.Lock()
	if inputTokens > r.observation.AcceptedInputTokens {
		r.observation.AcceptedInputTokens = inputTokens
		r.observation.AcceptedObservedAt = now
	}
	r.observation.UpdatedAt = now
	r.mu.Unlock()
	return r.persist()
}

// ObserveOverflow records a context overflow. When the error contains a
// recognizable maximum context value it is stored as a high-confidence exact
// observation; otherwise estimatedTokens is retained as a conservative upper
// bound.
func (r *Resolver) ObserveOverflow(err error, estimatedTokens int) error {
	if r == nil || err == nil {
		return nil
	}
	now := r.now().UTC()
	limit := ParseOverflowLimit(err)
	r.mu.Lock()
	if limit > 0 {
		r.observation.ExactMaxTokens = limit
		r.observation.ExactObservedAt = now
	} else if estimatedTokens > 0 {
		r.observation.OverflowUpperBoundTokens = estimatedTokens
		r.observation.OverflowObservedAt = now
	}
	r.observation.UpdatedAt = now
	r.mu.Unlock()
	return r.persist()
}

func (r *Resolver) persist() error {
	if r == nil || r.store == nil {
		return nil
	}
	r.mu.RLock()
	local := r.observation
	key := r.key
	r.mu.RUnlock()

	if stored, found, err := r.store.Load(key); err != nil {
		return err
	} else if found {
		local = mergeObservations(key, stored, local)
	}
	if err := r.store.Save(key, local); err != nil {
		return err
	}
	r.mu.Lock()
	r.observation = mergeObservations(key, r.observation, local)
	r.mu.Unlock()
	return nil
}

func resolve(configured, modelInfo, providerTokens int, observation Observation) Resolution {
	if configured > 0 {
		return Resolution{MaxTokens: configured, Source: SourceConfig, Confidence: ConfidenceExact}
	}
	accepted := positive(observation.AcceptedInputTokens)
	if exact := positive(observation.ExactMaxTokens); exact > 0 && exact >= accepted {
		return Resolution{MaxTokens: exact, Source: SourceLearnedOverflow, Confidence: ConfidenceHigh}
	}

	resolution := Resolution{Source: SourceUnknown, Confidence: ConfidenceUnknown}
	if modelInfo > 0 {
		resolution = Resolution{MaxTokens: modelInfo, Source: SourceModelInfo, Confidence: ConfidenceHigh}
	} else if providerTokens > 0 {
		resolution = Resolution{MaxTokens: providerTokens, Source: SourceProviderCaps, Confidence: ConfidenceMedium}
	}
	upper := positive(observation.OverflowUpperBoundTokens)
	if upper > accepted && (resolution.MaxTokens == 0 || upper < resolution.MaxTokens) {
		resolution = Resolution{MaxTokens: upper, Source: SourceLearnedOverflow, Confidence: ConfidenceLow}
	}
	if accepted > resolution.MaxTokens {
		resolution = Resolution{MaxTokens: accepted, Source: SourceLearnedUsage, Confidence: ConfidenceLow}
	}
	return resolution
}

func mergeObservations(key Key, left, right Observation) Observation {
	left = normalizeObservation(key, left)
	right = normalizeObservation(key, right)
	out := left
	if right.AcceptedInputTokens > out.AcceptedInputTokens {
		out.AcceptedInputTokens = right.AcceptedInputTokens
		out.AcceptedObservedAt = right.AcceptedObservedAt
	}
	if right.ExactMaxTokens > 0 && (out.ExactMaxTokens <= 0 || right.ExactObservedAt.After(out.ExactObservedAt)) {
		out.ExactMaxTokens = right.ExactMaxTokens
		out.ExactObservedAt = right.ExactObservedAt
	}
	if right.OverflowUpperBoundTokens > 0 && (out.OverflowUpperBoundTokens <= 0 || right.OverflowObservedAt.After(out.OverflowObservedAt)) {
		out.OverflowUpperBoundTokens = right.OverflowUpperBoundTokens
		out.OverflowObservedAt = right.OverflowObservedAt
	}
	if right.UpdatedAt.After(out.UpdatedAt) {
		out.UpdatedAt = right.UpdatedAt
	}
	return out
}

func normalizeObservation(key Key, observation Observation) Observation {
	observation.Version = 1
	observation.Key = key
	observation.ExactMaxTokens = positive(observation.ExactMaxTokens)
	observation.OverflowUpperBoundTokens = positive(observation.OverflowUpperBoundTokens)
	observation.AcceptedInputTokens = positive(observation.AcceptedInputTokens)
	return observation
}

func positive(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

var overflowLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)maximum\s+context(?:\s+(?:length|window))?\s*(?:is|of|:|=)?\s*([0-9][0-9_,]*)`),
	regexp.MustCompile(`(?i)context(?:\s+(?:length|window))?\s+(?:limit|maximum|max)\s*(?:is|of|:|=)?\s*([0-9][0-9_,]*)`),
	regexp.MustCompile(`(?i)context(?:\s+(?:length|window))?\s*(?:is|of|:|=)\s*([0-9][0-9_,]*)\s*tokens?`),
	regexp.MustCompile(`(?i)tokens?\s*(?:>|exceeds?)\s*([0-9][0-9_,]*)\s*(?:tokens?\s*)?(?:maximum|max)`),
	regexp.MustCompile(`(?i)([0-9][0-9_,]*)\s*tokens?\s+(?:maximum|max)(?:\s+context)?`),
	regexp.MustCompile(`(?i)context[^\n]{0,48}\blimit\s*(?:is|of|:|=)\s*([0-9][0-9_,]*)\s*tokens?`),
}

// ParseOverflowLimit extracts a maximum context token count from common
// provider error wording. It intentionally ignores bare token counts that are
// not adjacent to maximum/context/limit wording.
func ParseOverflowLimit(err error) int {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 0
	}
	text := err.Error()
	for _, pattern := range overflowLimitPatterns {
		match := pattern.FindStringSubmatch(text)
		if len(match) != 2 {
			continue
		}
		raw := strings.NewReplacer(",", "", "_", "").Replace(match[1])
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr == nil && value > 0 && value <= int64(maxInt()) {
			return int(value)
		}
	}
	return 0
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

func normalizeEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err == nil {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.ForceQuery = false
		parsed.Fragment = ""
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.Path = strings.TrimRight(parsed.Path, "/")
		return strings.TrimRight(parsed.String(), "/")
	}
	if before, _, ok := strings.Cut(raw, "?"); ok {
		raw = before
	}
	if before, _, ok := strings.Cut(raw, "#"); ok {
		raw = before
	}
	if schemeAt := strings.Index(raw, "://"); schemeAt >= 0 {
		prefix := raw[:schemeAt+3]
		rest := raw[schemeAt+3:]
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		raw = prefix + rest
	}
	return strings.TrimRight(raw, "/")
}
