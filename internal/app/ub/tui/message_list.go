package tui

import (
	"strings"
)

const (
	userRole      = "user"
	assistantRole = "assistant"
	activityRole  = "activity"
	toolRole      = "tool"
	systemRole    = "system"
	errorRole     = "error"
)

const (
	maxThinkingSummaryRunes    = 180
	maxToolPartialPreviewRunes = 12000
	maxExpandedDetailRunes     = 20000
	maxRenderCacheEntries      = 4
	maxItemRenderCacheEntries  = 2048
)

const activityStatusPartialFailed = "partial_failed"

const (
	thinkingGroupName = "thinking"
	toolGroupName     = "tool"
)

type messageKind string

const (
	textMessage          messageKind = "text"
	thinkingMessage      messageKind = "thinking"
	todoMessage          messageKind = "todo"
	toolMessage          messageKind = "tool"
	permissionMessage    messageKind = "permission"
	activityGroupMessage messageKind = "activity_group"
	noticeMessage        messageKind = "notice"
	systemMessage        messageKind = "system"
	errorMessage         messageKind = "error"
)

type message struct {
	role      string
	text      string
	key       string
	kind      messageKind
	title     string
	name      string
	status    string
	detail    string
	collapsed bool
	entries   []message
	copyIndex int // 1-based index for /copy; 0 means not copyable
	version   uint64
}

func (m message) collapsible() bool {
	switch m.kind {
	case thinkingMessage, toolMessage, permissionMessage, activityGroupMessage:
		return true
	default:
		return false
	}
}

type messageList struct {
	items           []message
	focus           int
	entryFocus      int
	renderCache     map[string]renderedMessages
	itemRenderCache map[itemRenderCacheKeyValue][]string
	renderVersion   uint64
	nextItemVersion uint64
	batchDepth      int
	copyIndexDirty  bool
}

type renderedMessages struct {
	lines []string
	spans []messageSpan
}

type messageSpan struct {
	itemIndex  int
	entryIndex int
	entry      bool
	start      int
	end        int
	startCol   int
	endCol     int
}

type messageTarget struct {
	itemIndex  int
	entryIndex int
}

func newMessageList() messageList {
	return messageList{focus: -1, entryFocus: -1, renderCache: map[string]renderedMessages{}, itemRenderCache: map[itemRenderCacheKeyValue][]string{}}
}

func (l *messageList) beginBatch() {
	l.batchDepth++
}

func (l *messageList) endBatch() {
	if l.batchDepth > 0 {
		l.batchDepth--
	}
	if l.batchDepth == 0 && l.copyIndexDirty {
		l.reindexCopy()
	}
	l.invalidateRender()
}

func (l *messageList) invalidateRender() {
	l.renderVersion++
	if l.renderCache == nil {
		l.renderCache = map[string]renderedMessages{}
	}
	if l.itemRenderCache == nil {
		l.itemRenderCache = map[itemRenderCacheKeyValue][]string{}
	}
	for key := range l.renderCache {
		delete(l.renderCache, key)
	}
}

func (l *messageList) stampMessage(msg *message) {
	if msg == nil {
		return
	}
	l.nextItemVersion++
	if l.nextItemVersion == 0 {
		l.nextItemVersion = 1
	}
	msg.version = l.nextItemVersion
}

func (l *messageList) append(role, text string) {
	block := message{
		role:      role,
		text:      text,
		kind:      kindForRole(role),
		title:     text,
		collapsed: defaultCollapsed(kindForRole(role)),
	}
	l.stampMessage(&block)
	l.items = append(l.items, block)
	l.reindexCopy()
	l.clampFocus()
}

func (l *messageList) appendOrUpdate(role, key, text string) {
	if strings.TrimSpace(key) == "" {
		l.append(role, text)
		return
	}
	block := message{
		role:      role,
		text:      text,
		key:       key,
		kind:      kindForRoleAndKey(role, key),
		title:     text,
		collapsed: defaultCollapsed(kindForRoleAndKey(role, key)),
	}
	l.stampMessage(&block)
	l.appendOrUpdateBlock(block)
}

func (l *messageList) appendThinking(key, text string) {
	block := message{
		role:      activityRole,
		text:      text,
		key:       key,
		kind:      thinkingMessage,
		title:     text,
		status:    "running",
		collapsed: true,
	}
	l.stampMessage(&block)
	l.appendOrUpdateBlock(block)
}

func (l *messageList) startActivityGroup(key, text string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	block := message{
		role:      activityRole,
		text:      text,
		key:       key,
		kind:      activityGroupMessage,
		name:      thinkingGroupName,
		title:     text,
		status:    "running",
		collapsed: true,
		entries: []message{{
			role:      activityRole,
			text:      text,
			key:       "thinking",
			kind:      thinkingMessage,
			title:     text,
			status:    "running",
			collapsed: true,
		}},
	}
	l.stampMessage(&block)
	l.appendOrUpdateBlock(block)
}

func (l *messageList) appendOrUpdateBlock(block message) {
	if block.version == 0 {
		l.stampMessage(&block)
	}
	if strings.TrimSpace(block.key) == "" {
		l.items = append(l.items, block)
		if block.kind == textMessage {
			l.reindexCopy()
		} else {
			l.invalidateRender()
		}
		l.clampFocus()
		return
	}
	for i := len(l.items) - 1; i >= 0; i-- {
		item := &l.items[i]
		if item.role == block.role && item.key == block.key {
			copyIndexesChanged := (item.kind == textMessage) != (block.kind == textMessage)
			collapsed := item.collapsed
			block = mergeActivityMessage(*item, block)
			l.stampMessage(&block)
			*item = block
			item.collapsed = collapsed
			if copyIndexesChanged {
				l.reindexCopy()
			} else {
				l.invalidateRender()
			}
			l.clampFocus()
			return
		}
	}
	l.items = append(l.items, block)
	if block.kind == textMessage {
		l.reindexCopy()
	} else {
		l.invalidateRender()
	}
	l.clampFocus()
}

func (l *messageList) removeKey(role, key string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	for i := len(l.items) - 1; i >= 0; i-- {
		if l.items[i].role != role || l.items[i].key != key {
			continue
		}
		l.items = append(l.items[:i], l.items[i+1:]...)
		l.reindexCopy()
		l.clampFocus()
		return
	}
}

func (l *messageList) clear() {
	l.items = nil
	l.focus = -1
	l.entryFocus = -1
	l.copyIndexDirty = false
	l.itemRenderCache = map[itemRenderCacheKeyValue][]string{}
	l.invalidateRender()
}

func (l *messageList) load(messages []InitialMessage) {
	l.beginBatch()
	defer l.endBatch()
	l.items = nil
	l.focus = -1
	l.entryFocus = -1
	l.copyIndexDirty = false
	l.itemRenderCache = map[itemRenderCacheKeyValue][]string{}
	for _, msg := range messages {
		if strings.TrimSpace(msg.ActivityKind) != "" {
			event := Event{
				Type:            EventActivity,
				Text:            msg.Text,
				ToolUseID:       msg.ToolUseID,
				ToolName:        msg.ToolName,
				ParentToolUseID: msg.ParentToolUseID,
				SubagentID:      msg.SubagentID,
				Content:         msg.Content,
				ActivityKind:    msg.ActivityKind,
				Status:          msg.Status,
				Summary:         msg.Summary,
				Decision:        msg.Decision,
				Source:          msg.Source,
				Reason:          msg.Reason,
				Allowed:         msg.Allowed,
				IsError:         msg.IsError,
			}
			l.appendOrUpdateLoadedActivity(event, msg.Turn)
			continue
		}
		role := normalizeRole(msg.Role)
		if strings.TrimSpace(msg.Text) == "" {
			continue
		}
		l.append(role, msg.Text)
	}
}

func (l *messageList) startAssistant() {
	block := message{role: assistantRole, kind: textMessage}
	l.stampMessage(&block)
	l.items = append(l.items, block)
	l.reindexCopy()
	l.clampFocus()
}

// breakAssistant ensures the next assistant delta starts a new message,
// so tool/thinking items can be interleaved between text segments.
func (l *messageList) breakAssistant() {
	// If the last item is an assistant message, appending the next delta
	// will naturally start a new one. If it's not, appendAssistantDelta
	// already calls startAssistant. So this is a no-op that just ensures
	// we don't merge into the previous assistant message.
}

func (l *messageList) appendAssistantDelta(text string) {
	if len(l.items) == 0 || l.items[len(l.items)-1].role != assistantRole {
		l.startAssistant()
	}
	l.items[len(l.items)-1].text += text
	l.items[len(l.items)-1].title = l.items[len(l.items)-1].text
	l.stampMessage(&l.items[len(l.items)-1])
	l.invalidateRender()
}

func (l messageList) texts() []string {
	out := make([]string, len(l.items))
	for i, item := range l.items {
		out[i] = item.text
	}
	return out
}

func (l *messageList) clampFocus() {
	if _, ok := l.focusTarget(); ok {
		return
	}
	l.focus = -1
	l.entryFocus = -1
}

func visibleStart(totalLines, height, scroll int) int {
	if height <= 0 || height >= totalLines {
		return 0
	}
	maxScroll := totalLines - height
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	start := totalLines - height - scroll
	if start < 0 {
		start = 0
	}
	return start
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case userRole:
		return userRole
	case assistantRole:
		return assistantRole
	case activityRole:
		return activityRole
	case toolRole:
		return toolRole
	case errorRole:
		return errorRole
	default:
		return systemRole
	}
}

func kindForRole(role string) messageKind {
	switch role {
	case activityRole:
		return noticeMessage
	case toolRole:
		return toolMessage
	case systemRole:
		return systemMessage
	case errorRole:
		return errorMessage
	default:
		return textMessage
	}
}

func kindForRoleAndKey(role, key string) messageKind {
	if role == activityRole && strings.HasPrefix(key, "thinking:") {
		return thinkingMessage
	}
	return kindForRole(role)
}

func defaultCollapsed(kind messageKind) bool {
	switch kind {
	case thinkingMessage, toolMessage, permissionMessage, activityGroupMessage:
		return true
	default:
		return false
	}
}
