package tooloutput

import (
	"strconv"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func BenchmarkLimitResultLargeInlineOnly(b *testing.B) {
	full := strings.Repeat("large tool output line with enough text to matter\n", 8000)
	limits := Limits{
		InlineMaxBytes:   12 * 1024,
		InlineMaxLines:   400,
		SpilloverEnabled: false,
	}
	b.SetBytes(int64(len(full)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := LimitResult(tool.Result{Content: full}, LimitOptions{
			SessionID: "sess",
			ToolUseID: "tool",
			Limits:    limits,
		})
		if err != nil {
			b.Fatal(err)
		}
		if !result.Truncated || result.FullContent != "" {
			b.Fatalf("unexpected result: %#v", result)
		}
	}
}

func BenchmarkLimitResultLargeSpillover(b *testing.B) {
	full := strings.Repeat("large tool output line with enough text to matter\n", 8000)
	stateRoot := b.TempDir()
	limits := Limits{
		InlineMaxBytes:   12 * 1024,
		InlineMaxLines:   400,
		SpilloverEnabled: true,
	}
	b.SetBytes(int64(len(full)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := LimitResult(tool.Result{Content: full}, LimitOptions{
			SessionID: "sess",
			ToolUseID: "tool-" + strconv.Itoa(i),
			StateRoot: stateRoot,
			Limits:    limits,
		})
		if err != nil {
			b.Fatal(err)
		}
		if !result.Truncated || result.FullOutputPath == "" {
			b.Fatalf("unexpected result: %#v", result)
		}
	}
}
