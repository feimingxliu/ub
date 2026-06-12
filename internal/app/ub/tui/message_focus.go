package tui

import "github.com/feimingxliu/ub/internal/app/ub/tui/tuitheme"

func (l *messageList) toggleAt(width, height, scroll, x, y int, styles tuitheme.Styles) bool {
	if y < 0 || y >= height {
		return false
	}
	rendered := l.render(width, styles)
	start := visibleStart(len(rendered.lines), height, scroll)
	line := start + y
	for _, span := range rendered.spans {
		if line < span.start || line >= span.end {
			continue
		}
		if x < span.startCol || x >= span.endCol {
			continue
		}
		if span.itemIndex < 0 || span.itemIndex >= len(l.items) {
			return false
		}
		if span.entry {
			group := &l.items[span.itemIndex]
			if group.kind != activityGroupMessage || span.entryIndex < 0 || span.entryIndex >= len(group.entries) {
				return false
			}
			entry := &group.entries[span.entryIndex]
			if !entry.collapsible() || !activityEntryHasDetail(*entry) {
				return false
			}
			l.focus = span.itemIndex
			l.entryFocus = span.entryIndex
			entry.collapsed = !entry.collapsed
			l.invalidateRender()
			return true
		}
		if !l.items[span.itemIndex].collapsible() {
			return false
		}
		l.focus = span.itemIndex
		l.entryFocus = -1
		l.items[span.itemIndex].collapsed = !l.items[span.itemIndex].collapsed
		l.invalidateRender()
		return true
	}
	return false
}

func (l *messageList) toggleLatestCollapsible() bool {
	for i := len(l.items) - 1; i >= 0; i-- {
		item := &l.items[i]
		if item.kind == activityGroupMessage {
			if !item.collapsed {
				for j := len(item.entries) - 1; j >= 0; j-- {
					entry := &item.entries[j]
					if entry.collapsible() && activityEntryHasDetail(*entry) {
						l.focus = i
						l.entryFocus = j
						entry.collapsed = !entry.collapsed
						l.invalidateRender()
						return true
					}
				}
			}
			if item.collapsible() {
				l.focus = i
				l.entryFocus = -1
				item.collapsed = !item.collapsed
				l.invalidateRender()
				return true
			}
			continue
		}
		if item.collapsible() {
			l.focus = i
			l.entryFocus = -1
			item.collapsed = !item.collapsed
			l.invalidateRender()
			return true
		}
	}
	return false
}

func (l *messageList) focusNextCollapsible() bool {
	return l.focusCollapsible(1)
}

func (l *messageList) focusPreviousCollapsible() bool {
	return l.focusCollapsible(-1)
}

func (l *messageList) focusCollapsible(delta int) bool {
	targets := l.collapsibleTargets()
	if len(targets) == 0 {
		l.focus = -1
		l.entryFocus = -1
		return false
	}
	current := -1
	for i, target := range targets {
		if target.itemIndex == l.focus && target.entryIndex == l.entryFocus {
			current = i
			break
		}
	}
	next := 0
	if current < 0 {
		if delta < 0 {
			next = len(targets) - 1
		}
	} else {
		next = (current + delta) % len(targets)
		if next < 0 {
			next += len(targets)
		}
	}
	l.focus = targets[next].itemIndex
	l.entryFocus = targets[next].entryIndex
	return true
}

func (l *messageList) toggleFocusedCollapsible() bool {
	target, ok := l.focusTarget()
	if !ok {
		return false
	}
	if target.entryIndex >= 0 {
		entry := &l.items[target.itemIndex].entries[target.entryIndex]
		entry.collapsed = !entry.collapsed
		l.invalidateRender()
		return true
	}
	l.items[target.itemIndex].collapsed = !l.items[target.itemIndex].collapsed
	if l.items[target.itemIndex].collapsed {
		l.entryFocus = -1
	}
	l.invalidateRender()
	return true
}

func (l messageList) hasFocusedCollapsible() bool {
	_, ok := l.focusTarget()
	return ok
}

func (l *messageList) clearFocus() bool {
	if l.focus < 0 && l.entryFocus < 0 {
		return false
	}
	l.focus = -1
	l.entryFocus = -1
	l.invalidateRender()
	return true
}

func (l messageList) focusTarget() (messageTarget, bool) {
	if l.focus < 0 || l.focus >= len(l.items) {
		return messageTarget{}, false
	}
	item := l.items[l.focus]
	if l.entryFocus >= 0 {
		if item.kind != activityGroupMessage || item.collapsed || l.entryFocus >= len(item.entries) {
			return messageTarget{}, false
		}
		entry := item.entries[l.entryFocus]
		if !entry.collapsible() || !activityEntryHasDetail(entry) {
			return messageTarget{}, false
		}
		return messageTarget{itemIndex: l.focus, entryIndex: l.entryFocus}, true
	}
	if !item.collapsible() {
		return messageTarget{}, false
	}
	return messageTarget{itemIndex: l.focus, entryIndex: -1}, true
}

func (l messageList) collapsibleTargets() []messageTarget {
	var targets []messageTarget
	for itemIndex, item := range l.items {
		if !item.collapsible() {
			continue
		}
		targets = append(targets, messageTarget{itemIndex: itemIndex, entryIndex: -1})
		if item.kind != activityGroupMessage || item.collapsed {
			continue
		}
		for entryIndex, entry := range item.entries {
			if entry.collapsible() && activityEntryHasDetail(entry) {
				targets = append(targets, messageTarget{itemIndex: itemIndex, entryIndex: entryIndex})
			}
		}
	}
	return targets
}

func (l messageList) focusedLine(width int, styles tuitheme.Styles) (int, int, bool) {
	target, ok := l.focusTarget()
	if !ok {
		return 0, 0, false
	}
	rendered := l.render(width, styles)
	for _, span := range rendered.spans {
		if span.itemIndex != target.itemIndex {
			continue
		}
		if target.entryIndex >= 0 && span.entry && span.entryIndex == target.entryIndex {
			return span.start, len(rendered.lines), true
		}
		if target.entryIndex < 0 && !span.entry {
			return span.start, len(rendered.lines), true
		}
	}
	return 0, len(rendered.lines), false
}
