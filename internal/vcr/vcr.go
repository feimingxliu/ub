// Package vcr records and replays HTTP interactions using JSONL cassettes.
package vcr

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const redactedValue = "***"

// Mode controls how a Recorder handles requests.
type Mode string

const (
	// ModeDisabled passes requests through to the base transport.
	ModeDisabled Mode = "disabled"
	// ModeRecord sends real requests and appends responses to a cassette.
	ModeRecord Mode = "record"
	// ModeReplay returns responses from a cassette without network access.
	ModeReplay Mode = "replay"
)

// Recorder is an http.RoundTripper that records or replays HTTP interactions.
type Recorder struct {
	mode         Mode
	cassettePath string
	base         http.RoundTripper
	records      []cassetteRecord
	next         int
}

type cassetteRecord struct {
	Request  cassetteRequest  `json:"request"`
	Response cassetteResponse `json:"response"`
}

type cassetteRequest struct {
	Method     string      `json:"method"`
	URL        string      `json:"url"`
	Headers    http.Header `json:"headers"`
	BodySHA256 string      `json:"body_sha256"`
}

type cassetteResponse struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	Body       string      `json:"body"`
}

// ParseMode parses a mode string.
func ParseMode(raw string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ModeDisabled:
		return ModeDisabled, nil
	case ModeRecord:
		return ModeRecord, nil
	case ModeReplay:
		return ModeReplay, nil
	default:
		return "", fmt.Errorf("invalid vcr mode %q (expected disabled, record, or replay)", raw)
	}
}

// ModeFromEnv returns the VCR mode selected by UB_VCR.
func ModeFromEnv() Mode {
	mode, err := ParseMode(os.Getenv("UB_VCR"))
	if err != nil {
		return ModeDisabled
	}
	return mode
}

// New constructs a Recorder.
func New(cassettePath, mode string, base http.RoundTripper) (*Recorder, error) {
	parsed, err := ParseMode(mode)
	if err != nil {
		return nil, err
	}
	if base == nil {
		base = http.DefaultTransport
	}
	r := &Recorder{
		mode:         parsed,
		cassettePath: cassettePath,
		base:         base,
	}
	switch parsed {
	case ModeRecord, ModeReplay:
		if cassettePath == "" {
			return nil, errors.New("vcr cassette path is empty")
		}
	}
	if parsed == ModeReplay {
		records, err := readCassette(cassettePath)
		if err != nil {
			return nil, err
		}
		r.records = records
	}
	return r, nil
}

// RoundTrip implements http.RoundTripper.
func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	if r == nil {
		return nil, errors.New("nil vcr recorder")
	}
	switch r.mode {
	case ModeDisabled:
		return r.base.RoundTrip(req)
	case ModeRecord:
		return r.record(req)
	case ModeReplay:
		return r.replay(req)
	default:
		return nil, fmt.Errorf("unsupported vcr mode %q", r.mode)
	}
}

func (r *Recorder) record(req *http.Request) (*http.Response, error) {
	body, hash, err := captureRequestBody(req)
	if err != nil {
		return nil, err
	}
	resp, err := r.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	respBody, err := captureResponseBody(resp)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	record := cassetteRecord{
		Request: cassetteRequest{
			Method:     req.Method,
			URL:        req.URL.String(),
			Headers:    redactHeaders(req.Header),
			BodySHA256: hash,
		},
		Response: cassetteResponse{
			StatusCode: resp.StatusCode,
			Headers:    redactHeaders(resp.Header),
			Body:       base64.StdEncoding.EncodeToString(respBody),
		},
	}
	if err := appendCassette(r.cassettePath, record); err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	resp.ContentLength = int64(len(respBody))
	return resp, nil
}

func (r *Recorder) replay(req *http.Request) (*http.Response, error) {
	if r.next >= len(r.records) {
		return nil, fmt.Errorf("vcr cassette exhausted: no record for actual %s %s", req.Method, req.URL.String())
	}
	_, hash, err := captureRequestBody(req)
	if err != nil {
		return nil, err
	}
	actual := cassetteRequest{
		Method:     req.Method,
		URL:        req.URL.String(),
		BodySHA256: hash,
	}
	record := r.records[r.next]
	if err := matchRequest(record.Request, actual); err != nil {
		return nil, err
	}
	r.next++
	body, err := base64.StdEncoding.DecodeString(record.Response.Body)
	if err != nil {
		return nil, fmt.Errorf("decode cassette response body at record %d: %w", r.next, err)
	}
	return &http.Response{
		StatusCode:    record.Response.StatusCode,
		Status:        fmt.Sprintf("%d %s", record.Response.StatusCode, http.StatusText(record.Response.StatusCode)),
		Header:        cloneHeader(record.Response.Headers),
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}, nil
}

func matchRequest(expected, actual cassetteRequest) error {
	var mismatches []string
	if expected.Method != actual.Method {
		mismatches = append(mismatches, "method")
	}
	if expected.URL != actual.URL {
		mismatches = append(mismatches, "url")
	}
	if expected.BodySHA256 != actual.BodySHA256 {
		mismatches = append(mismatches, "body_sha256")
	}
	if len(mismatches) == 0 {
		return nil
	}
	return fmt.Errorf(
		"vcr request mismatch (%s): expected %s %s body_sha256=%s, actual %s %s body_sha256=%s",
		strings.Join(mismatches, ", "),
		expected.Method, expected.URL, expected.BodySHA256,
		actual.Method, actual.URL, actual.BodySHA256,
	)
}

func captureRequestBody(req *http.Request) ([]byte, string, error) {
	if req.Body == nil {
		return nil, bodyHash(nil), nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read request body: %w", err)
	}
	if err := req.Body.Close(); err != nil {
		return nil, "", fmt.Errorf("close request body: %w", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return body, bodyHash(body), nil
}

func captureResponseBody(resp *http.Response) ([]byte, error) {
	if resp.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if err := resp.Body.Close(); err != nil {
		return nil, fmt.Errorf("close response body: %w", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return body, nil
}

func bodyHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func appendCassette(path string, record cassetteRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create cassette directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open cassette for append: %w", err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(record); err != nil {
		return fmt.Errorf("write cassette record: %w", err)
	}
	return nil
}

func readCassette(path string) ([]cassetteRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cassette: %w", err)
	}
	defer file.Close()

	var records []cassetteRecord
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record cassetteRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("decode cassette line %d: %w", lineNo, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read cassette: %w", err)
	}
	return records, nil
}

func redactHeaders(in http.Header) http.Header {
	out := make(http.Header, len(in))
	for key, values := range in {
		copied := append([]string(nil), values...)
		if isSensitiveHeader(key) {
			for i := range copied {
				copied[i] = redactedValue
			}
		}
		out[key] = copied
	}
	return out
}

func cloneHeader(in http.Header) http.Header {
	out := make(http.Header, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func isSensitiveHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "x-api-key", "api-key", "x-auth-token", "x-api-token", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}
