package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
	"github.com/feimingxliu/ub/internal/pkg/workspace/store"
	"github.com/spf13/cobra"
)

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage agent sessions",
	}
	var all bool
	lsCmd := &cobra.Command{
		Use:   "ls",
		Short: "List sessions in the current workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsLS(cmd, all)
		},
	}
	lsCmd.Flags().BoolVar(&all, "all", false, "list sessions across all workspaces")
	cmd.AddCommand(lsCmd)
	var (
		searchLimit     int
		searchWorkspace string
	)
	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search rollout text across all sessions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsSearch(cmd, args[0], searchLimit, searchWorkspace)
		},
	}
	searchCmd.Flags().IntVar(&searchLimit, "limit", 200, "max matches to return (0 = unlimited)")
	searchCmd.Flags().StringVar(&searchWorkspace, "workspace", "", "restrict to a single workspace path")
	cmd.AddCommand(searchCmd)
	cmd.AddCommand(&cobra.Command{
		Use:     "rm <session-id> [session-id...]",
		Aliases: []string{"delete", "del"},
		Short:   "Delete sessions by id",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsRM(cmd, args)
		},
	})
	var (
		yes      bool
		clearAll bool
	)
	clearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Delete all sessions in the current workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsClear(cmd, yes, clearAll)
		},
	}
	clearCmd.Flags().BoolVar(&yes, "yes", false, "confirm deletion")
	clearCmd.Flags().BoolVar(&clearAll, "all", false, "clear sessions across all workspaces")
	cmd.AddCommand(clearCmd)
	return cmd
}

func runSessionsLS(cmd *cobra.Command, all bool) error {
	path, err := store.DefaultPath()
	if err != nil {
		return fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()

	if all {
		sessions, err := st.ListAllSessions(cmd.Context())
		if err != nil {
			return err
		}
		return printAllSessions(cmd.OutOrStdout(), sessions)
	}

	cwd, err := currentWorkspace()
	if err != nil {
		return err
	}
	sessions, err := st.ListSessions(cmd.Context(), cwd, 20)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "no sessions")
		return err
	}

	return printSessionTable(cmd.OutOrStdout(), sessions)
}

func printAllSessions(out io.Writer, sessions []store.Session) error {
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(out, "no sessions")
		return err
	}
	currentWorkspace := ""
	for i, sess := range sessions {
		if sess.Workspace == currentWorkspace {
			continue
		}
		if currentWorkspace != "" {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		currentWorkspace = sess.Workspace
		if _, err := fmt.Fprintf(out, "WORKSPACE %s\n", currentWorkspace); err != nil {
			return err
		}
		groupEnd := i + 1
		for groupEnd < len(sessions) && sessions[groupEnd].Workspace == currentWorkspace {
			groupEnd++
		}
		if err := printSessionTable(out, sessions[i:groupEnd]); err != nil {
			return err
		}
	}
	return nil
}

func printSessionTable(out io.Writer, sessions []store.Session) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tWORKSPACE\tUPDATED\tTITLE\tPROVIDER\tMODEL"); err != nil {
		return err
	}
	for _, sess := range sessions {
		title := sess.Title
		if title == "" {
			title = "(untitled)"
		}
		provider := sess.Provider
		if provider == "" {
			provider = "-"
		}
		model := sess.Model
		if model == "" {
			model = "-"
		}
		if _, err := fmt.Fprintf(
			w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			sess.ID,
			shortenWorkspaceForDisplay(sess.Workspace),
			sess.UpdatedAt.Local().Format(time.RFC3339),
			title,
			provider,
			model,
		); err != nil {
			return err
		}
	}
	return w.Flush()
}

type sessionSearchMatch struct {
	Session   store.Session
	Turn      int
	Type      rollout.Type
	Time      time.Time
	Snippet   string
	Workspace string
}

func runSessionsSearch(cmd *cobra.Command, query string, limit int, workspace string) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("search query is empty")
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

	sessions, err := st.ListAllSessions(cmd.Context())
	if err != nil {
		return err
	}
	if workspace = strings.TrimSpace(workspace); workspace != "" {
		canonical, err := canonicalWorkspace(workspace)
		if err != nil {
			return err
		}
		filtered := sessions[:0]
		for _, sess := range sessions {
			if sess.Workspace == canonical {
				filtered = append(filtered, sess)
			}
		}
		sessions = filtered
	}
	ro, err := rollout.New(st)
	if err != nil {
		return err
	}
	matches, err := searchSessions(cmd.Context(), ro, sessions, query, limit)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "no matches")
		return err
	}
	return printSessionSearchMatches(cmd.OutOrStdout(), matches)
}

// errSearchLimitReached short-circuits ForEach once the caller has accumulated
// the requested number of matches; the surrounding loop swallows it.
var errSearchLimitReached = fmt.Errorf("search: match limit reached")

func searchSessions(ctx context.Context, reader rollout.Reader, sessions []store.Session, query string, limit int) ([]sessionSearchMatch, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	var matches []sessionSearchMatch
	for _, sess := range sessions {
		err := reader.ForEach(ctx, sess.ID, func(event rollout.Event) error {
			text, err := rolloutEventSearchText(event)
			if err != nil {
				return err
			}
			if !strings.Contains(strings.ToLower(text), needle) {
				return nil
			}
			matches = append(matches, sessionSearchMatch{
				Session:   sess,
				Turn:      event.Turn,
				Type:      event.Type,
				Time:      event.Time,
				Snippet:   searchSnippet(text, query, 120),
				Workspace: sess.Workspace,
			})
			if limit > 0 && len(matches) >= limit {
				return errSearchLimitReached
			}
			return nil
		})
		if errors.Is(err, errSearchLimitReached) {
			return matches, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return matches, nil
}

func printSessionSearchMatches(out io.Writer, matches []sessionSearchMatch) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "WORKSPACE\tSESSION\tTURN\tTYPE\tTIME\tTITLE\tMATCH"); err != nil {
		return err
	}
	for _, match := range matches {
		title := match.Session.Title
		if title == "" {
			title = "(untitled)"
		}
		if _, err := fmt.Fprintf(
			w,
			"%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
			match.Workspace,
			match.Session.ID,
			match.Turn,
			match.Type,
			match.Time.Local().Format(time.RFC3339),
			title,
			match.Snippet,
		); err != nil {
			return err
		}
	}
	return w.Flush()
}

func rolloutEventSearchText(event rollout.Event) (string, error) {
	if msg, ok, err := rollout.MessageFromEvent(event); err != nil {
		return "", err
	} else if ok {
		text := msg.Text()
		if event.Type == rollout.TypeToolResult {
			var payload rollout.ToolResultPayload
			if err := json.Unmarshal(event.Payload, &payload); err == nil && len(payload.Metadata) > 0 {
				var metadata []string
				for key, value := range payload.Metadata {
					metadata = append(metadata, key+"="+value)
				}
				sort.Strings(metadata)
				text = strings.TrimSpace(text + " " + strings.Join(metadata, " "))
			}
		}
		return text, nil
	}
	switch event.Type {
	case rollout.TypeError:
		var payload rollout.ErrorPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return "", fmt.Errorf("decode rollout error event %s: %w", event.ID, err)
		}
		return payload.Message, nil
	case rollout.TypeActivity:
		var payload rollout.ActivityPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return "", fmt.Errorf("decode rollout activity event %s: %w", event.ID, err)
		}
		return strings.Join([]string{payload.ActivityKind, payload.ToolName, payload.Status, payload.Summary, payload.Content, payload.Decision, payload.Source, payload.Reason}, " "), nil
	case rollout.TypeMemoryWrite:
		var payload rollout.MemoryWritePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return "", fmt.Errorf("decode rollout memory_write event %s: %w", event.ID, err)
		}
		return strings.Join([]string{payload.Scope, payload.Category, payload.Text, payload.Path, payload.Source, payload.Action}, " "), nil
	default:
		return "", nil
	}
}

func searchSnippet(text, query string, maxRunes int) string {
	text = strings.Join(strings.Fields(text), " ")
	if maxRunes <= 0 || len([]rune(text)) <= maxRunes {
		return text
	}
	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	idx := strings.Index(lowerText, lowerQuery)
	if idx < 0 {
		return trimRunes(text, maxRunes)
	}
	runeStart := utf8.RuneCountInString(text[:idx])
	queryRunes := utf8.RuneCountInString(lowerQuery)
	start := max(0, runeStart-(maxRunes-queryRunes)/2)
	return trimRunesFrom(text, start, maxRunes)
}

func trimRunes(text string, maxRunes int) string {
	return trimRunesFrom(text, 0, maxRunes)
}

func trimRunesFrom(text string, start, maxRunes int) string {
	runes := []rune(text)
	if start > len(runes) {
		start = len(runes)
	}
	end := min(len(runes), start+maxRunes)
	prefix := ""
	if start > 0 {
		prefix = "..."
	}
	suffix := ""
	if end < len(runes) {
		suffix = "..."
	}
	return prefix + string(runes[start:end]) + suffix
}

func runSessionsRM(cmd *cobra.Command, ids []string) error {
	path, err := store.DefaultPath()
	if err != nil {
		return fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()

	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("session id is empty")
		}
		if err := st.DeleteSession(cmd.Context(), id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("session %q not found", id)
			}
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", id); err != nil {
			return err
		}
	}
	return nil
}

func runSessionsClear(cmd *cobra.Command, yes, all bool) error {
	if !yes {
		return fmt.Errorf("refusing to delete sessions without --yes")
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

	var deleted int64
	if all {
		deleted, err = st.DeleteAllSessions(cmd.Context())
	} else {
		cwd, werr := currentWorkspace()
		if werr != nil {
			return werr
		}
		deleted, err = st.DeleteWorkspaceSessions(cmd.Context(), cwd)
	}
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "deleted %d sessions\n", deleted); err != nil {
		return err
	}
	return nil
}
