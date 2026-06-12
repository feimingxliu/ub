package tui

import (
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/app/ub/tui/tuitheme"
)

func TestExpandedLargeToolDetailIsDisplayTruncatedWithoutMutatingDetail(t *testing.T) {
	longDetail := strings.Repeat("0123456789", maxExpandedDetailRunes/10+100)
	var list messageList
	list.appendOrUpdateActivity(Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_task",
		ToolName:     "task",
		Status:       "done",
		Summary:      "large detail",
		Content:      longDetail,
	})
	list.items[0].collapsed = false
	list.invalidateRender()

	view := list.view(100, 2000, 0, tuitheme.Plain())
	if !strings.Contains(view, "display truncated") {
		t.Fatalf("expanded large detail should show display truncation marker")
	}
	if got := list.items[0].detail; got != longDetail {
		t.Fatalf("stored detail was mutated: got len %d, want %d", len(got), len(longDetail))
	}
}

func TestMessageRenderCacheIsBounded(t *testing.T) {
	var list messageList
	list.append(userRole, "hello")
	for width := 80; width < 90; width++ {
		_ = list.render(width, tuitheme.Plain())
	}
	if got := len(list.renderCache); got > maxRenderCacheEntries {
		t.Fatalf("render cache size = %d, want <= %d", got, maxRenderCacheEntries)
	}
}

func TestTextRenderCacheSurvivesAssistantStreamingInvalidation(t *testing.T) {
	list := newMessageList()
	styles := tuitheme.Plain()
	list.append(userRole, strings.Repeat("hello ", 20))
	list.append(assistantRole, strings.Repeat("response ", 30))

	firstKey := textRenderCacheKey(list.items[0], 100, styles)
	_ = list.render(100, styles)
	if _, ok := list.itemRenderCache[firstKey]; !ok {
		t.Fatalf("missing initial text render cache entry")
	}

	list.appendAssistantDelta("tail")
	_ = list.render(100, styles)
	if _, ok := list.itemRenderCache[firstKey]; !ok {
		t.Fatalf("streaming invalidation dropped unchanged text render cache entry")
	}
}

func TestItemRenderCacheSurvivesAssistantStreamingInvalidation(t *testing.T) {
	list := newMessageList()
	styles := tuitheme.Plain()
	list.appendOrUpdateActivity(Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_read",
		ToolName:     "read",
		Status:       "done",
		Summary:      "read file",
		Content:      strings.Repeat("tool output ", 20),
	})
	list.items[0].collapsed = false
	list.invalidateRender()
	list.append(assistantRole, strings.Repeat("response ", 30))

	toolKey := itemRenderCacheKey(list.items[0], false, 100, styles)
	_ = list.render(100, styles)
	if _, ok := list.itemRenderCache[toolKey]; !ok {
		t.Fatalf("missing initial tool render cache entry")
	}

	list.appendAssistantDelta("tail")
	_ = list.render(100, styles)
	if _, ok := list.itemRenderCache[toolKey]; !ok {
		t.Fatalf("streaming invalidation dropped unchanged tool render cache entry")
	}
}
