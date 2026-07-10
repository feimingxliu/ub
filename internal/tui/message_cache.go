package tui

import (
	"hash/fnv"

	"github.com/feimingxliu/ub/internal/tui/theme"
)

type itemRenderCacheKeyValue struct {
	width       int
	plain       bool
	styleName   string
	kind        messageKind
	role        string
	key         string
	collapsed   bool
	focused     bool
	copyIndex   int
	version     uint64
	fingerprint uint64
}

func (l messageList) renderItemCached(item message, focused bool, width int, styles tuitheme.Styles) []string {
	if item.kind == activityGroupMessage || l.itemRenderCache == nil {
		return l.renderItem(item, focused, width, styles)
	}
	cacheKey := itemRenderCacheKey(item, focused, width, styles)
	if lines, ok := l.itemRenderCache[cacheKey]; ok {
		return lines
	}
	lines := l.renderItem(item, focused, width, styles)
	if len(l.itemRenderCache) >= maxItemRenderCacheEntries {
		for key := range l.itemRenderCache {
			delete(l.itemRenderCache, key)
			break
		}
	}
	l.itemRenderCache[cacheKey] = lines
	return lines
}

func itemRenderCacheKey(item message, focused bool, width int, styles tuitheme.Styles) itemRenderCacheKeyValue {
	if item.kind == textMessage {
		return textRenderCacheKey(item, width, styles)
	}
	version, fingerprint := itemRenderContentID(item)
	return itemRenderCacheKeyValue{
		width:       width,
		plain:       styles.Plain,
		styleName:   itemRenderStyleName(styles),
		kind:        item.kind,
		role:        item.role,
		key:         item.key,
		collapsed:   item.collapsed,
		focused:     focused,
		copyIndex:   item.copyIndex,
		version:     version,
		fingerprint: fingerprint,
	}
}

func textRenderCacheKey(item message, width int, styles tuitheme.Styles) itemRenderCacheKeyValue {
	version, fingerprint := itemRenderContentID(item)
	return itemRenderCacheKeyValue{
		width:       width,
		plain:       styles.Plain,
		styleName:   itemRenderStyleName(styles),
		kind:        textMessage,
		role:        item.role,
		copyIndex:   item.copyIndex,
		version:     version,
		fingerprint: fingerprint,
	}
}

func itemRenderStyleName(styles tuitheme.Styles) string {
	styleName := styles.Markdown.StyleName
	if styles.Plain {
		return "plain"
	}
	if styleName == "" {
		return "dark"
	}
	return styleName
}

func itemRenderContentID(item message) (uint64, uint64) {
	if item.version != 0 {
		return item.version, 0
	}
	h := fnv.New64a()
	writeHashField(h, string(item.kind))
	writeHashField(h, item.role)
	writeHashField(h, item.text)
	writeHashField(h, item.title)
	writeHashField(h, item.name)
	writeHashField(h, item.status)
	writeHashField(h, item.detail)
	return 0, h.Sum64()
}

type hashWriter interface {
	Write([]byte) (int, error)
}

func writeHashField(h hashWriter, value string) {
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{0})
}
