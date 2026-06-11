package job

import (
	"strings"
	"testing"
)

func TestRing_BelowCap(t *testing.T) {
	r := newRing(10)
	if _, err := r.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := string(r.Snapshot(0)); got != "hello" {
		t.Fatalf("snapshot=%q", got)
	}
	if r.Total() != 5 {
		t.Fatalf("total=%d", r.Total())
	}
}

func TestRing_OverflowKeepsTail(t *testing.T) {
	r := newRing(5)
	if _, err := r.Write([]byte("abcdefghij")); err != nil { // 10 bytes
		t.Fatalf("write: %v", err)
	}
	if got := string(r.Snapshot(0)); got != "fghij" {
		t.Fatalf("snapshot=%q want fghij", got)
	}
	if r.Total() != 10 {
		t.Fatalf("total=%d", r.Total())
	}
}

func TestRing_OverflowMultipleWrites(t *testing.T) {
	r := newRing(4)
	if _, err := r.Write([]byte("abc")); err != nil {
		t.Fatalf("w1: %v", err)
	}
	if _, err := r.Write([]byte("defg")); err != nil {
		t.Fatalf("w2: %v", err)
	}
	if got := string(r.Snapshot(0)); got != "defg" {
		t.Fatalf("snapshot=%q want defg", got)
	}
	if r.Total() != 7 {
		t.Fatalf("total=%d", r.Total())
	}
}

func TestRing_TailSmallerThanSize(t *testing.T) {
	r := newRing(10)
	if _, err := r.Write([]byte("0123456789")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := string(r.Snapshot(3)); got != "789" {
		t.Fatalf("snapshot tail=3 -> %q", got)
	}
	if got := string(r.Snapshot(20)); got != "0123456789" {
		t.Fatalf("snapshot tail>size -> %q", got)
	}
}

func TestRing_LargePayload(t *testing.T) {
	r := newRing(32 * 1024)
	payload := strings.Repeat("x", 40*1024)
	if _, err := r.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	snap := r.Snapshot(0)
	if len(snap) != 32*1024 {
		t.Fatalf("snapshot len=%d want %d", len(snap), 32*1024)
	}
	if r.Total() != 40*1024 {
		t.Fatalf("total=%d", r.Total())
	}
}
