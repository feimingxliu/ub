package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tui/theme"
)

func BenchmarkMessageListRenderLongTranscript(b *testing.B) {
	list := newMessageList()
	for i := 0; i < 300; i++ {
		list.append(userRole, fmt.Sprintf("user message %03d %s", i, strings.Repeat("hello ", 12)))
		list.append(assistantRole, fmt.Sprintf("assistant message %03d %s", i, strings.Repeat("response ", 24)))
		list.appendOrUpdateActivity(Event{
			Type:         EventActivity,
			ActivityKind: "tool",
			ToolUseID:    fmt.Sprintf("tool_%03d", i),
			ToolName:     "bash",
			Status:       "done",
			Summary:      fmt.Sprintf("bash command %03d", i),
			Content:      strings.Repeat("tool output line\n", 8),
		})
	}
	styles := tuitheme.Plain()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		list.invalidateRender()
		_ = list.render(100, styles)
	}
}

func BenchmarkMessageListViewCachedLongTranscript(b *testing.B) {
	list := newMessageList()
	for i := 0; i < 300; i++ {
		list.append(userRole, fmt.Sprintf("user message %03d %s", i, strings.Repeat("hello ", 12)))
		list.append(assistantRole, fmt.Sprintf("assistant message %03d %s", i, strings.Repeat("response ", 24)))
	}
	styles := tuitheme.Plain()
	_ = list.view(100, 40, 0, styles)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = list.view(100, 40, 0, styles)
	}
}
