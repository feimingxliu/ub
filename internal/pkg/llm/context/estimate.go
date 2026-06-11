// Package context estimates and prepares model context windows.
package context

import (
	"encoding/json"
	"math"
	"strings"
	"sync"

	"github.com/feimingxliu/ub/internal/pkg/core/message"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	tiktoken "github.com/pkoukk/tiktoken-go"
)

const (
	messageOverhead = 4
	replyOverhead   = 3
	minRatio        = 0.25
	maxRatio        = 4.0
)

type textEncoder interface {
	Encode(text string) int
}

type tiktokenEncoder struct {
	enc *tiktoken.Tiktoken
}

func (e tiktokenEncoder) Encode(text string) int {
	return len(e.enc.Encode(text, nil, nil))
}

type cachedEncoder struct {
	encoder textEncoder
	failed  bool
}

var (
	encoderMu          sync.Mutex
	encoderCache       = map[string]cachedEncoder{}
	loadOpenAIEncoder  = loadTiktokenEncoder
	calibrationMu      sync.RWMutex
	calibrationByModel = map[string]float64{}
)

// Estimate returns a non-negative token estimate for provider-neutral messages.
func Estimate(msgs []message.Message, model string) int {
	return applyCalibration(model, estimateMessagesRaw(msgs, model))
}

// EstimateRequest returns a token estimate for a provider request, including
// the message list and tool schemas that providers count as prompt input.
func EstimateRequest(msgs []message.Message, tools []provider.ToolDefinition, model string) int {
	raw := estimateMessagesRaw(msgs, model) + estimateToolsRaw(tools, model)
	return applyCalibration(model, raw)
}

// Breakdown summarizes the uncalibrated request estimate by broad source.
type Breakdown struct {
	SystemRuntime int
	ToolSchema    int
	UserAssistant int
	ToolResult    int
	Other         int
	Total         int
}

// EstimateRequestBreakdown returns a lightweight request estimate breakdown.
func EstimateRequestBreakdown(msgs []message.Message, tools []provider.ToolDefinition, model string) Breakdown {
	var b Breakdown
	for _, msg := range msgs {
		msgTokens := messageOverhead + countText(string(msg.Role), model)
		for _, block := range msg.Content {
			msgTokens += countText(blockFrame(block), model)
		}
		switch msg.Role {
		case message.RoleSystem:
			b.SystemRuntime += msgTokens
		case message.RoleUser, message.RoleAssistant:
			b.UserAssistant += msgTokens
		case message.RoleTool:
			b.ToolResult += msgTokens
		default:
			b.Other += msgTokens
		}
	}
	b.Other += replyOverhead
	b.ToolSchema = estimateToolsRaw(tools, model)
	b.Total = b.SystemRuntime + b.ToolSchema + b.UserAssistant + b.ToolResult + b.Other
	return b
}

func estimateMessagesRaw(msgs []message.Message, model string) int {
	if len(msgs) == 0 {
		return 0
	}
	total := replyOverhead
	for _, msg := range msgs {
		total += messageOverhead
		total += countText(string(msg.Role), model)
		for _, block := range msg.Content {
			total += countText(blockFrame(block), model)
		}
	}
	return total
}

func estimateToolsRaw(tools []provider.ToolDefinition, model string) int {
	if len(tools) == 0 {
		return 0
	}
	total := 0
	for _, def := range tools {
		total += messageOverhead
		total += countText("tool:"+def.Name, model)
		total += countText(def.Description, model)
		total += countText(string(def.Schema), model)
	}
	return total
}

// ObserveUsage records provider input-token usage to calibrate future estimates.
func ObserveUsage(model string, estimated int, actual int) {
	if estimated <= 0 || actual <= 0 {
		return
	}
	key := modelKey(model)
	if key == "" {
		return
	}
	ratio := clamp(float64(actual)/float64(estimated), minRatio, maxRatio)
	calibrationMu.Lock()
	defer calibrationMu.Unlock()
	if current := calibrationByModel[key]; current > 0 {
		calibrationByModel[key] = current*0.7 + ratio*0.3
		return
	}
	calibrationByModel[key] = ratio
}

func countText(text string, model string) int {
	if text == "" {
		return 0
	}
	if enc := encoderForModel(model); enc != nil {
		return enc.Encode(text)
	}
	return approximateTokens(text)
}

func encoderForModel(model string) textEncoder {
	key := modelKey(model)
	if !isOpenAIModel(key) {
		return nil
	}
	encoderMu.Lock()
	defer encoderMu.Unlock()
	if cached, ok := encoderCache[key]; ok {
		if cached.failed {
			return nil
		}
		return cached.encoder
	}
	encoder, err := loadOpenAIEncoder(key)
	if err != nil || encoder == nil {
		encoderCache[key] = cachedEncoder{failed: true}
		return nil
	}
	encoderCache[key] = cachedEncoder{encoder: encoder}
	return encoder
}

func loadTiktokenEncoder(model string) (textEncoder, error) {
	enc, err := tiktoken.EncodingForModel(model)
	if err != nil {
		enc, err = tiktoken.GetEncoding("cl100k_base")
	}
	if err != nil {
		return nil, err
	}
	return tiktokenEncoder{enc: enc}, nil
}

func blockFrame(block message.ContentBlock) string {
	switch block.Type {
	case message.BlockText:
		return "text:\n" + block.Text
	case message.BlockImage:
		return "image:\n" + block.ImageURL
	case message.BlockReasoning:
		return "reasoning:\n" + block.Reasoning + "\nsignature:" + block.ReasoningSignature
	case message.BlockToolUse:
		return "tool_use:" + block.ToolName + "\ninput:" + compactJSON(block.Input)
	case message.BlockToolResult:
		status := "ok"
		if block.IsError {
			status = "error"
		}
		return "tool_result:" + block.ToolUseID + "\nstatus:" + status + "\noutput:" + block.Output
	default:
		raw, err := json.Marshal(block)
		if err != nil {
			return string(block.Type)
		}
		return string(raw)
	}
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	compact, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(compact)
}

func approximateTokens(text string) int {
	if text == "" {
		return 0
	}
	tokens := 0
	asciiRunes := 0
	flushASCII := func() {
		if asciiRunes == 0 {
			return
		}
		tokens += (asciiRunes + 3) / 4
		asciiRunes = 0
	}
	for _, r := range text {
		if r <= 127 {
			asciiRunes++
			continue
		}
		flushASCII()
		tokens++
	}
	flushASCII()
	if tokens <= 0 {
		return 1
	}
	return tokens
}

func applyCalibration(model string, estimated int) int {
	if estimated <= 0 {
		return 0
	}
	key := modelKey(model)
	calibrationMu.RLock()
	ratio := calibrationByModel[key]
	calibrationMu.RUnlock()
	if ratio <= 0 {
		return estimated
	}
	return int(math.Ceil(float64(estimated) * ratio))
}

func modelKey(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if _, rest, ok := strings.Cut(model, "/"); ok {
		model = strings.TrimSpace(rest)
	}
	return model
}

func isOpenAIModel(model string) bool {
	return strings.HasPrefix(model, "gpt-") ||
		strings.HasPrefix(model, "o1") ||
		strings.HasPrefix(model, "o3") ||
		strings.HasPrefix(model, "o4") ||
		strings.HasPrefix(model, "chatgpt-")
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
