package shell

import (
	"strings"
	"testing"
)

func TestCapWriter_BelowCap(t *testing.T) {
	w := newCapWriter(10)
	n, err := w.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	if string(w.Bytes()) != "hello" {
		t.Fatalf("buf=%q", w.Bytes())
	}
	if w.Total() != 5 {
		t.Fatalf("total=%d", w.Total())
	}
}

func TestCapWriter_OverflowSingleWrite(t *testing.T) {
	w := newCapWriter(4)
	in := []byte("abcdefgh")
	n, err := w.Write(in)
	if err != nil || n != 8 {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	if string(w.Bytes()) != "abcd" {
		t.Fatalf("buf=%q want abcd", w.Bytes())
	}
	if w.Total() != 8 {
		t.Fatalf("total=%d want 8", w.Total())
	}
}

func TestCapWriter_OverflowMultipleWrites(t *testing.T) {
	w := newCapWriter(5)
	if _, err := w.Write([]byte("abc")); err != nil {
		t.Fatalf("write1: %v", err)
	}
	if _, err := w.Write([]byte("defghi")); err != nil {
		t.Fatalf("write2: %v", err)
	}
	if string(w.Bytes()) != "abcde" {
		t.Fatalf("buf=%q want abcde", w.Bytes())
	}
	if w.Total() != 9 {
		t.Fatalf("total=%d want 9", w.Total())
	}
}

func TestCapWriter_LargePayload(t *testing.T) {
	w := newCapWriter(32 * 1024)
	payload := strings.Repeat("x", 40*1024)
	if _, err := w.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(w.Bytes()) != 32*1024 {
		t.Fatalf("buf len=%d want %d", len(w.Bytes()), 32*1024)
	}
	if w.Total() != 40*1024 {
		t.Fatalf("total=%d", w.Total())
	}
}
