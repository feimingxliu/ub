package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

const (
	repeatedToolWindowSize = 10
	repeatedToolMaxRepeats = 5
)

type toolLoopDetector struct {
	window     []string
	windowSize int
	maxRepeats int
}

func newToolLoopDetector(windowSize, maxRepeats int) *toolLoopDetector {
	return &toolLoopDetector{
		windowSize: max(1, windowSize),
		maxRepeats: max(1, maxRepeats),
	}
}

func (d *toolLoopDetector) Record(calls []toolCall, results []tool.Result) bool {
	if d == nil {
		return false
	}
	sig := toolInteractionSignature(calls, results)
	if sig == "" {
		d.window = nil
		return false
	}
	d.window = append(d.window, sig)
	if len(d.window) > d.windowSize {
		d.window = d.window[len(d.window)-d.windowSize:]
	}
	repeats := 0
	for _, item := range d.window {
		if item == sig {
			repeats++
		}
	}
	return repeats > d.maxRepeats
}

func toolInteractionSignature(calls []toolCall, results []tool.Result) string {
	if len(calls) == 0 {
		return ""
	}
	h := sha256.New()
	for i, call := range calls {
		io.WriteString(h, call.Name)
		io.WriteString(h, "\x00")
		io.WriteString(h, string(call.Input))
		io.WriteString(h, "\x00")
		if i < len(results) {
			io.WriteString(h, results[i].Content)
			io.WriteString(h, "\x00")
			if results[i].IsError {
				io.WriteString(h, "error")
			} else {
				io.WriteString(h, "ok")
			}
		}
		io.WriteString(h, "\x00")
	}
	return hex.EncodeToString(h.Sum(nil))
}
