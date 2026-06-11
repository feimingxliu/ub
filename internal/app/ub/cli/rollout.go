package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/feimingxliu/ub/internal/pkg/core/message"
	"github.com/feimingxliu/ub/internal/pkg/workspace/filehistory"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
	"github.com/feimingxliu/ub/internal/pkg/workspace/store"
	"github.com/spf13/cobra"
)

type rolloutShowOptions struct {
	JSON  bool
	Turns string
}

func newRolloutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollout",
		Short: "Inspect rollout event logs",
	}
	var opts rolloutShowOptions
	showCmd := &cobra.Command{
		Use:   "show <session-id>",
		Short: "Print rollout events for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRolloutShow(cmd, args[0], opts)
		},
	}
	showCmd.Flags().BoolVar(&opts.JSON, "json", false, "print raw rollout events as JSONL")
	showCmd.Flags().StringVar(&opts.Turns, "turns", "", "filter turns, for example 5..10")
	cmd.AddCommand(showCmd)
	return cmd
}

func runRolloutShow(cmd *cobra.Command, sessionID string, opts rolloutShowOptions) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is empty")
	}
	turns, err := parseTurnFilter(opts.Turns)
	if err != nil {
		return err
	}
	path, err := store.DefaultPath()
	if err != nil {
		return fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()

	sess, err := st.GetSession(cmd.Context(), sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("session %q not found", sessionID)
	}
	if err != nil {
		return err
	}
	ro, err := rollout.New(st)
	if err != nil {
		return err
	}

	var events []rollout.Event
	if err := ro.ForEach(cmd.Context(), sessionID, func(event rollout.Event) error {
		if turns.include(event.Turn) {
			events = append(events, event)
		}
		return nil
	}); err != nil {
		return err
	}

	if opts.JSON {
		return writeRolloutJSONL(cmd.OutOrStdout(), events)
	}
	return writeRolloutPretty(cmd.OutOrStdout(), *sess, events)
}

type turnFilter struct {
	set   bool
	start int
	end   int
}

func parseTurnFilter(raw string) (turnFilter, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return turnFilter{}, nil
	}
	if !strings.Contains(raw, "..") {
		n, err := parsePositiveTurn(raw)
		if err != nil {
			return turnFilter{}, fmt.Errorf("invalid --turns %q: %w", raw, err)
		}
		return turnFilter{set: true, start: n, end: n}, nil
	}
	parts := strings.Split(raw, "..")
	if len(parts) != 2 {
		return turnFilter{}, fmt.Errorf("invalid --turns %q: expected START..END", raw)
	}
	filter := turnFilter{set: true}
	if strings.TrimSpace(parts[0]) != "" {
		n, err := parsePositiveTurn(parts[0])
		if err != nil {
			return turnFilter{}, fmt.Errorf("invalid --turns %q: %w", raw, err)
		}
		filter.start = n
	}
	if strings.TrimSpace(parts[1]) != "" {
		n, err := parsePositiveTurn(parts[1])
		if err != nil {
			return turnFilter{}, fmt.Errorf("invalid --turns %q: %w", raw, err)
		}
		filter.end = n
	}
	if filter.start == 0 && filter.end == 0 {
		return turnFilter{}, fmt.Errorf("invalid --turns %q: expected at least one bound", raw)
	}
	if filter.start > 0 && filter.end > 0 && filter.start > filter.end {
		return turnFilter{}, fmt.Errorf("invalid --turns %q: start must be <= end", raw)
	}
	return filter, nil
}

func parsePositiveTurn(raw string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("turn must be positive")
	}
	return n, nil
}

func (f turnFilter) include(turn int) bool {
	if !f.set {
		return true
	}
	if f.start > 0 && turn < f.start {
		return false
	}
	if f.end > 0 && turn > f.end {
		return false
	}
	return true
}

func writeRolloutJSONL(w io.Writer, events []rollout.Event) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, event := range events {
		if err := enc.Encode(event); err != nil {
			return err
		}
	}
	return nil
}

func writeRolloutPretty(w io.Writer, sess store.Session, events []rollout.Event) error {
	style := newRolloutStyle(w)
	if _, err := fmt.Fprintf(w, "%s %s\n", style.header("session"), sess.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "workspace: %s\n", sess.Workspace); err != nil {
		return err
	}
	if sess.Title != "" {
		if _, err := fmt.Fprintf(w, "title: %s\n", sess.Title); err != nil {
			return err
		}
	}
	if sess.Model != "" {
		if _, err := fmt.Fprintf(w, "model: %s\n", sess.Model); err != nil {
			return err
		}
	}
	if !sess.UpdatedAt.IsZero() {
		if _, err := fmt.Fprintf(w, "updated: %s\n", sess.UpdatedAt.Local().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if len(events) == 0 {
		_, err := fmt.Fprintln(w, "\n(no events)")
		return err
	}
	for _, event := range events {
		if _, err := fmt.Fprintf(
			w, "\n%s %d  %s  %s\n",
			style.header("turn"),
			event.Turn,
			style.eventType(string(event.Type)),
			event.Time.Local().Format(time.RFC3339),
		); err != nil {
			return err
		}
		if err := writeRolloutEvent(w, style, event); err != nil {
			return err
		}
	}
	return nil
}

type rolloutStyle struct {
	plain bool
	title lipgloss.Style
	typ   lipgloss.Style
	muted lipgloss.Style
	err   lipgloss.Style
}

func newRolloutStyle(w io.Writer) rolloutStyle {
	plain := true
	if os.Getenv("NO_COLOR") == "" {
		if file, ok := w.(*os.File); ok {
			if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
				plain = false
			}
		}
	}
	return rolloutStyle{
		plain: plain,
		title: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("43")),
		typ:   lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		muted: lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		err:   lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
	}
}

func (s rolloutStyle) header(text string) string {
	if s.plain {
		return text
	}
	return s.title.Render(text)
}

func (s rolloutStyle) eventType(text string) string {
	if s.plain {
		return text
	}
	return s.typ.Render(text)
}

func (s rolloutStyle) mutedText(text string) string {
	if s.plain {
		return text
	}
	return s.muted.Render(text)
}

func (s rolloutStyle) errorText(text string) string {
	if s.plain {
		return text
	}
	return s.err.Render(text)
}

func writeRolloutEvent(w io.Writer, style rolloutStyle, event rollout.Event) error {
	switch event.Type {
	case rollout.TypeUserMessage, rollout.TypeAssistantMessage:
		var payload rollout.MessagePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode %s event %s: %w", event.Type, event.ID, err)
		}
		role := string(payload.Message.Role)
		if role == "" {
			role = strings.TrimSuffix(string(event.Type), "_message")
		}
		text := payload.Text
		if text == "" {
			text = payload.Message.Text()
		}
		return writeRolloutMessage(w, style, role, payload.Message, text)
	case rollout.TypeUsage:
		var payload rollout.UsagePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode usage event %s: %w", event.ID, err)
		}
		_, err := fmt.Fprintf(w, "  usage: %s\n", formatUsage(payload))
		return err
	case rollout.TypeToolResult:
		var payload rollout.ToolResultPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode tool_result event %s: %w", event.ID, err)
		}
		status := "ok"
		if payload.IsError {
			status = style.errorText("error")
		}
		name := payload.ToolName
		if name == "" {
			name = "(unknown)"
		}
		if _, err := fmt.Fprintf(w, "  tool: %s id=%s status=%s\n", name, payload.ToolUseID, status); err != nil {
			return err
		}
		if payload.Truncated {
			if _, err := fmt.Fprintf(w, "  output: %s original_bytes=%d full_output_path=%s\n", style.mutedText("truncated"), payload.OriginalBytes, payload.FullOutputPath); err != nil {
				return err
			}
		}
		if len(payload.Metadata) > 0 {
			keys := make([]string, 0, len(payload.Metadata))
			for key := range payload.Metadata {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if _, err := fmt.Fprintf(w, "  metadata: %s=%s\n", key, payload.Metadata[key]); err != nil {
					return err
				}
			}
		}
		if err := writeIndentedBlock(w, "output:", payload.Output, style); err != nil {
			return err
		}
		for _, file := range payload.Files {
			if _, err := fmt.Fprintf(w, "  file: %s %s\n", file.Path, file.Kind); err != nil {
				return err
			}
			if err := writeIndentedLines(w, file.UnifiedDiff, "    "); err != nil {
				return err
			}
		}
		return nil
	case rollout.TypeSummary:
		var payload rollout.SummaryPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode summary event %s: %w", event.ID, err)
		}
		if _, err := fmt.Fprintf(w, "  summary: compressed=%d kept=%d estimated_tokens=%d\n", payload.CompressedMessages, payload.KeptMessages, payload.EstimatedTokens); err != nil {
			return err
		}
		return writeIndentedBlock(w, "text:", payload.Text, style)
	case rollout.TypeMemoryWrite:
		var payload rollout.MemoryWritePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode memory_write event %s: %w", event.ID, err)
		}
		action := defaultRolloutString(payload.Action, "write")
		source := defaultRolloutString(payload.Source, "agent")
		if _, err := fmt.Fprintf(w, "  memory: scope=%s category=%s action=%s source=%s\n", payload.Scope, payload.Category, action, source); err != nil {
			return err
		}
		if payload.Path != "" {
			if _, err := fmt.Fprintf(w, "  path: %s\n", payload.Path); err != nil {
				return err
			}
		}
		if payload.DroppedExpired > 0 || payload.DroppedOverflow > 0 {
			if _, err := fmt.Fprintf(w, "  pruned: expired=%d overflow=%d\n", payload.DroppedExpired, payload.DroppedOverflow); err != nil {
				return err
			}
		}
		return writeIndentedBlock(w, "text:", payload.Text, style)
	case rollout.TypeFileHistorySnapshot:
		var payload filehistory.EventPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode file_history_snapshot event %s: %w", event.ID, err)
		}
		update := ""
		if payload.IsUpdate {
			update = " update=true"
		}
		_, err := fmt.Fprintf(w, "  file checkpoint: turn=%d tracked_files=%d%s\n", payload.Snapshot.Turn, len(payload.Snapshot.TrackedFileBackups), update)
		return err
	case rollout.TypeError:
		var payload rollout.ErrorPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode error event %s: %w", event.ID, err)
		}
		_, err := fmt.Fprintf(w, "  error: %s\n", style.errorText(payload.Message))
		return err
	default:
		var payload any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode event %s payload: %w", event.ID, err)
		}
		raw, err := json.MarshalIndent(payload, "  ", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "  payload:\n%s\n", raw)
		return err
	}
}

func writeRolloutMessage(w io.Writer, style rolloutStyle, role string, msg message.Message, fallbackText string) error {
	if len(msg.Content) == 0 {
		return writeIndentedBlock(w, role+":", fallbackText, style)
	}
	if len(msg.Content) == 1 && msg.Content[0].Type == message.BlockText {
		text := msg.Content[0].Text
		if text == "" {
			text = fallbackText
		}
		return writeIndentedBlock(w, role+":", text, style)
	}
	if _, err := fmt.Fprintf(w, "  %s\n", role+":"); err != nil {
		return err
	}
	for _, block := range msg.Content {
		if err := writeRolloutMessageBlock(w, style, block); err != nil {
			return err
		}
	}
	return nil
}

func writeRolloutMessageBlock(w io.Writer, style rolloutStyle, block message.ContentBlock) error {
	switch block.Type {
	case message.BlockText:
		return writeIndentedBlockAt(w, "text:", block.Text, style, "    ", "      ")
	case message.BlockImage:
		return writeIndentedBlockAt(w, "image:", block.ImageURL, style, "    ", "      ")
	case message.BlockReasoning:
		if err := writeIndentedBlockAt(w, "reasoning:", block.Reasoning, style, "    ", "      "); err != nil {
			return err
		}
		if strings.TrimSpace(block.ReasoningSignature) != "" {
			_, err := fmt.Fprintf(w, "    reasoning_signature: %s\n", style.mutedText(block.ReasoningSignature))
			return err
		}
		return nil
	case message.BlockToolUse:
		name := defaultRolloutString(block.ToolName, "(unknown)")
		id := defaultRolloutString(block.ToolUseID, "(missing)")
		if _, err := fmt.Fprintf(w, "    tool_use: %s id=%s\n", name, id); err != nil {
			return err
		}
		return writeIndentedBlockAt(w, "input:", formatRolloutJSON(block.Input), style, "    ", "      ")
	case message.BlockToolResult:
		status := "ok"
		if block.IsError {
			status = style.errorText("error")
		}
		id := defaultRolloutString(block.ToolUseID, "(missing)")
		if _, err := fmt.Fprintf(w, "    tool_result: id=%s status=%s\n", id, status); err != nil {
			return err
		}
		return writeIndentedBlockAt(w, "output:", block.Output, style, "    ", "      ")
	default:
		raw, err := json.MarshalIndent(block, "", "  ")
		if err != nil {
			return err
		}
		return writeIndentedBlockAt(w, string(block.Type)+":", string(raw), style, "    ", "      ")
	}
}

func formatRolloutJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return string(raw)
	}
	formatted, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(formatted)
}

func defaultRolloutString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func formatUsage(payload rollout.UsagePayload) string {
	var parts []string
	appendToken := func(name string, value int) {
		if value > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", name, value))
		}
	}
	appendToken("input", payload.InputTokens)
	appendToken("output", payload.OutputTokens)
	appendToken("reasoning", payload.ReasoningTokens)
	appendToken("cache_read", payload.CacheReadTokens)
	appendToken("cache_write", payload.CacheWriteTokens)
	if len(parts) == 0 {
		return "(empty)"
	}
	return strings.Join(parts, " ")
}

func writeIndentedBlock(w io.Writer, label, text string, style rolloutStyle) error {
	return writeIndentedBlockAt(w, label, text, style, "  ", "    ")
}

func writeIndentedBlockAt(w io.Writer, label, text string, style rolloutStyle, labelPrefix, bodyPrefix string) error {
	if strings.TrimSpace(text) == "" {
		_, err := fmt.Fprintf(w, "%s%s %s\n", labelPrefix, label, style.mutedText("(empty)"))
		return err
	}
	if !strings.Contains(text, "\n") {
		_, err := fmt.Fprintf(w, "%s%s %s\n", labelPrefix, label, text)
		return err
	}
	if _, err := fmt.Fprintf(w, "%s%s\n", labelPrefix, label); err != nil {
		return err
	}
	return writeIndentedLines(w, text, bodyPrefix)
}

func writeIndentedLines(w io.Writer, text, prefix string) error {
	if text == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "%s%s\n", prefix, line); err != nil {
			return err
		}
	}
	return nil
}
