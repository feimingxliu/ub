package vcr

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseModeAndModeFromEnv(t *testing.T) {
	cases := map[string]Mode{
		"":         ModeDisabled,
		"disabled": ModeDisabled,
		"record":   ModeRecord,
		"replay":   ModeReplay,
		" RECORD ": ModeRecord,
	}
	for raw, want := range cases {
		got, err := ParseMode(raw)
		if err != nil {
			t.Fatalf("ParseMode(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParseMode(%q) = %q, want %q", raw, got, want)
		}
	}
	if _, err := ParseMode("bad"); err == nil {
		t.Fatal("ParseMode(bad) returned nil error")
	}
	t.Setenv("UB_VCR", "record")
	if got := ModeFromEnv(); got != ModeRecord {
		t.Fatalf("ModeFromEnv() = %q, want %q", got, ModeRecord)
	}
	t.Setenv("UB_VCR", "")
	if got := ModeFromEnv(); got != ModeDisabled {
		t.Fatalf("ModeFromEnv empty = %q, want %q", got, ModeDisabled)
	}
}

func TestNewRejectsInvalidMode(t *testing.T) {
	if _, err := New("cassette.jsonl", "bad", nil); err == nil {
		t.Fatal("New with invalid mode returned nil error")
	}
}

func TestDisabledPassesThroughAndDoesNotCreateCassette(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return textResponse(req, http.StatusOK, "live"), nil
	})
	rec, err := New(path, string(ModeDisabled), base)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := rec.RoundTrip(newRequest(t, http.MethodGet, "https://example.test", nil))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	assertBody(t, resp, "live")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cassette should not exist, stat err=%v", err)
	}
}

func TestRecordWritesJSONLAndReadableResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cassette.jsonl")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(body); got != `{"prompt":"hi"}` {
			t.Fatalf("server body = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "session=secret")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"answer":"pong"}`))
	}))
	defer server.Close()

	rec, err := New(path, string(ModeRecord), server.Client().Transport)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := newRequest(t, http.MethodPost, server.URL+"/v1/messages", strings.NewReader(`{"prompt":"hi"}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")

	resp, err := rec.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	assertBody(t, resp, `{"answer":"pong"}`)

	lines := cassetteLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("cassette lines = %d, want 1", len(lines))
	}
	var record cassetteRecord
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("cassette line is not JSON: %v", err)
	}
	if record.Request.Method != http.MethodPost || record.Request.URL != server.URL+"/v1/messages" {
		t.Fatalf("unexpected request metadata: %+v", record.Request)
	}
	if record.Request.Headers.Get("Authorization") != redactedValue {
		t.Fatalf("authorization was not redacted: %+v", record.Request.Headers)
	}
	if record.Request.Headers.Get("Content-Type") != "application/json" {
		t.Fatalf("content-type not preserved: %+v", record.Request.Headers)
	}
	if record.Response.Headers.Get("Set-Cookie") != redactedValue {
		t.Fatalf("set-cookie was not redacted: %+v", record.Response.Headers)
	}
	if decoded, err := base64.StdEncoding.DecodeString(record.Response.Body); err != nil || string(decoded) != `{"answer":"pong"}` {
		t.Fatalf("response body base64 decode = %q, %v", decoded, err)
	}
	if strings.Contains(lines[0], "secret") {
		t.Fatalf("cassette leaked secret:\n%s", lines[0])
	}
}

func TestReplayReturnsCassetteResponseWithoutBaseTransport(t *testing.T) {
	path, url := writeRecordedCassette(t, `{"prompt":"hi"}`, `{"answer":"pong"}`)
	called := false
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})
	rec, err := New(path, string(ModeReplay), base)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := rec.RoundTrip(newRequest(t, http.MethodPost, url, strings.NewReader(`{"prompt":"hi"}`)))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if called {
		t.Fatal("base transport was called during replay")
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if got := resp.Header.Get("X-Test"); got != "yes" {
		t.Fatalf("X-Test header = %q, want yes", got)
	}
	assertBody(t, resp, `{"answer":"pong"}`)
}

func TestReplayMismatchAndExhausted(t *testing.T) {
	path, url := writeRecordedCassette(t, `{"prompt":"hi"}`, `{"answer":"pong"}`)
	rec, err := New(path, string(ModeReplay), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = rec.RoundTrip(newRequest(t, http.MethodGet, url, nil))
	if err == nil || !strings.Contains(err.Error(), "expected") || !strings.Contains(err.Error(), "actual") {
		t.Fatalf("mismatch err = %v, want expected/actual", err)
	}

	rec, err = New(path, string(ModeReplay), nil)
	if err != nil {
		t.Fatalf("New second: %v", err)
	}
	if _, err := rec.RoundTrip(newRequest(t, http.MethodPost, url, strings.NewReader(`{"prompt":"hi"}`))); err != nil {
		t.Fatalf("first replay: %v", err)
	}
	_, err = rec.RoundTrip(newRequest(t, http.MethodPost, url, strings.NewReader(`{"prompt":"hi"}`)))
	if err == nil || !strings.Contains(err.Error(), "cassette exhausted") {
		t.Fatalf("exhausted err = %v, want cassette exhausted", err)
	}
}

func TestRecordReplayRoundTripAfterServerClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "roundtrip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("recorded"))
	}))
	rec, err := New(path, string(ModeRecord), server.Client().Transport)
	if err != nil {
		t.Fatalf("New record: %v", err)
	}
	url := server.URL + "/v1/messages"
	resp, err := rec.RoundTrip(newRequest(t, http.MethodPost, url, strings.NewReader("body")))
	if err != nil {
		t.Fatalf("record RoundTrip: %v", err)
	}
	assertBody(t, resp, "recorded")
	server.Close()

	replay, err := New(path, string(ModeReplay), roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("base transport should not be called")
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("New replay: %v", err)
	}
	resp, err = replay.RoundTrip(newRequest(t, http.MethodPost, url, strings.NewReader("body")))
	if err != nil {
		t.Fatalf("replay RoundTrip: %v", err)
	}
	if resp.Header.Get("X-Test") != "roundtrip" {
		t.Fatalf("replay header = %q", resp.Header.Get("X-Test"))
	}
	assertBody(t, resp, "recorded")
}

func TestReadCassetteSkipsEmptyLinesAndReportsLineNumber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	if err := os.WriteFile(path, []byte("\n{}\n{bad}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := readCassette(path)
	if err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("readCassette err = %v, want line 3", err)
	}
}

func writeRecordedCassette(t *testing.T, requestBody, responseBody string) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	url := "https://example.test/v1/messages"
	record := cassetteRecord{
		Request: cassetteRequest{
			Method:     http.MethodPost,
			URL:        url,
			Headers:    http.Header{"Content-Type": []string{"application/json"}},
			BodySHA256: bodyHash([]byte(requestBody)),
		},
		Response: cassetteResponse{
			StatusCode: http.StatusAccepted,
			Headers:    http.Header{"X-Test": []string{"yes"}},
			Body:       base64.StdEncoding.EncodeToString([]byte(responseBody)),
		},
	}
	if err := appendCassette(path, record); err != nil {
		t.Fatalf("appendCassette: %v", err)
	}
	return path, url
}

func newRequest(t *testing.T, method, url string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

func textResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        http.StatusText(status),
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}

func assertBody(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll body: %v", err)
	}
	if string(got) != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func cassetteLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	return strings.Split(string(trimmed), "\n")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
