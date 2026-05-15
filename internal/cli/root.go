// Package cli wires the cobra command tree for the ub binary.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/feimingxliu/ub/internal/config"
	logx "github.com/feimingxliu/ub/internal/log"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	_ "github.com/feimingxliu/ub/internal/provider/anthropic"
	_ "github.com/feimingxliu/ub/internal/provider/compat"
	_ "github.com/feimingxliu/ub/internal/provider/fake"
	_ "github.com/feimingxliu/ub/internal/provider/ollama"
	_ "github.com/feimingxliu/ub/internal/provider/openai"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/store"
	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

// Execute runs the root command. It exits the process on failure.
func Execute() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}

// Run executes the CLI with injected streams and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	return runWithFactory(args, stdout, stderr, newRootCmd)
}

func runWithFactory(args []string, stdout, stderr io.Writer, cmdFactory func() *cobra.Command) (code int) {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "panic: %v\n%s", r, debug.Stack())
			code = 1
		}
	}()

	logger, cleanup, err := logx.SetupFromEnv(stderr)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	defer func() {
		if cleanup == nil {
			return
		}
		if err := cleanup(); err != nil && code == 0 {
			fmt.Fprintf(stderr, "error: close log: %v\n", err)
			code = 1
		}
	}()

	logger.Debug("cli command start", "args", args)
	cmd := cmdFactory()
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	var opts runtimeOptions
	root := &cobra.Command{
		Use:           "ub",
		Short:         "ub — Ulimited Blade, a coding agent in your terminal",
		Long:          "ub is a terminal-based coding agent. Run `ub run` to start an interactive session.",
		Version:       Version(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.PersistentFlags().StringVar(&opts.profile, "profile", "", "configuration profile to apply")
	root.PersistentFlags().BoolVar(&opts.dev, "dev", false, "use the dev profile")
	root.PersistentFlags().StringVar(&opts.mode, "mode", "", "execution mode: default, plan, or agent-approve")

	root.AddCommand(newRunCmd())
	root.AddCommand(newChatCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newSessionsCmd())

	return root
}

type runtimeOptions struct {
	profile string
	dev     bool
	mode    string
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run an agent session (TUI by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("I-22 (TUI) / I-21 (headless agent)")
		},
	}
}

func newChatCmd() *cobra.Command {
	var providerName string
	var model string
	var sessionID string
	var forceNew bool

	cmd := &cobra.Command{
		Use:   "chat [prompt|-]",
		Short: "Send one prompt to a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChat(cmd, args[0], providerName, model, chatOptions{
				SessionID: sessionID,
				New:       forceNew,
			})
		},
	}
	cmd.Flags().StringVar(&providerName, "provider", "", "provider config name")
	cmd.Flags().StringVar(&model, "model", "", "model id override")
	cmd.Flags().StringVar(&sessionID, "session", "", "continue an existing session id")
	cmd.Flags().BoolVar(&forceNew, "new", false, "force creation of a new session")
	return cmd
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or manage configuration",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the merged effective configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigForCommand(cmd)
			if err != nil {
				return err
			}
			redacted := config.Redact(cfg)
			out, err := yaml.Marshal(redacted)
			if err != nil {
				return fmt.Errorf("marshal config: %w", err)
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "List configuration files used in the current invocation",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, files, err := loadConfigForCommand(cmd)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "(no config files loaded; using built-in defaults)")
				return err
			}
			for _, file := range files {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), file); err != nil {
					return err
				}
			}
			return nil
		},
	})
	return cmd
}

func loadConfigForCommand(cmd *cobra.Command) (*config.Config, []string, error) {
	opts, err := loadOptionsForCommand(cmd)
	if err != nil {
		return nil, nil, err
	}
	return config.LoadWithOptions(opts)
}

func loadOptionsForCommand(cmd *cobra.Command) (config.LoadOptions, error) {
	root := cmd.Root()
	profile, err := root.PersistentFlags().GetString("profile")
	if err != nil {
		return config.LoadOptions{}, err
	}
	dev, err := root.PersistentFlags().GetBool("dev")
	if err != nil {
		return config.LoadOptions{}, err
	}
	mode, err := root.PersistentFlags().GetString("mode")
	if err != nil {
		return config.LoadOptions{}, err
	}
	return config.LoadOptions{
		Profile:       profile,
		Dev:           dev,
		ExecutionMode: mode,
	}, nil
}

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage agent sessions",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List sessions in the current workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsLS(cmd)
		},
	})
	return cmd
}

type chatOptions struct {
	SessionID string
	New       bool
}

type chatSessionState struct {
	store     *store.Store
	rollout   *rollout.SQLite
	session   store.Session
	history   []message.Message
	nextTurn  int
	sessionID string
}

func runChat(cmd *cobra.Command, promptArg, providerFlag, modelFlag string, opts chatOptions) error {
	if opts.New && strings.TrimSpace(opts.SessionID) != "" {
		return fmt.Errorf("cannot use --new with --session")
	}
	prompt, err := readChatPrompt(cmd, promptArg)
	if err != nil {
		return err
	}

	cfg, _, err := loadConfigForCommand(cmd)
	if err != nil {
		return err
	}
	providerName, model, err := selectChatProvider(cfg, providerFlag, modelFlag)
	if err != nil {
		return err
	}
	providerCfg, ok := cfg.Providers[providerName]
	if !ok {
		return fmt.Errorf("provider %q not configured; check `ub config show`", providerName)
	}
	p, err := provider.New(providerName, providerCfg)
	if err != nil {
		return fmt.Errorf("create provider %q: %w", providerName, err)
	}

	state, err := startChatRollout(cmd, prompt, model, opts)
	if err != nil {
		return err
	}
	defer state.store.Close()

	userMsg := message.Text(message.RoleUser, prompt)
	event, err := rollout.UserMessage(state.sessionID, state.nextTurn, userMsg)
	if err != nil {
		return err
	}
	if err := state.rollout.Append(cmd.Context(), event); err != nil {
		return err
	}

	requestMessages := append(cloneMessages(state.history), userMsg)
	stream, err := p.Chat(cmd.Context(), provider.Request{
		Model:    model,
		Messages: requestMessages,
	})
	if err != nil {
		return recordChatError(cmd, state, fmt.Errorf("provider %q chat: %w", providerName, err))
	}
	defer stream.Close()

	var assistant strings.Builder
	for {
		event, err := stream.Next(cmd.Context())
		if err == io.EOF {
			if err := recordAssistantMessage(cmd, state.rollout, state.sessionID, state.nextTurn, assistant.String()); err != nil {
				return err
			}
			if err := finishChatSession(cmd, state, prompt, model); err != nil {
				return err
			}
			return nil
		}
		if err != nil {
			if recordErr := recordAssistantMessage(cmd, state.rollout, state.sessionID, state.nextTurn, assistant.String()); recordErr != nil {
				return recordErr
			}
			return recordChatError(cmd, state, fmt.Errorf("provider %q stream: %w", providerName, err))
		}
		switch event.Type {
		case provider.EventTextDelta:
			if _, err := io.WriteString(cmd.OutOrStdout(), event.Text); err != nil {
				return recordChatError(cmd, state, err)
			}
			assistant.WriteString(event.Text)
		case provider.EventUsage:
			if event.Usage != nil {
				usageEvent, err := rollout.Usage(state.sessionID, state.nextTurn, event.Usage.InputTokens, event.Usage.OutputTokens)
				if err != nil {
					return err
				}
				if err := state.rollout.Append(cmd.Context(), usageEvent); err != nil {
					return err
				}
			}
			continue
		case provider.EventDone:
			if err := recordAssistantMessage(cmd, state.rollout, state.sessionID, state.nextTurn, assistant.String()); err != nil {
				return err
			}
			if err := finishChatSession(cmd, state, prompt, model); err != nil {
				return err
			}
			return nil
		case provider.EventToolCall:
			var toolErr error
			if event.ToolName == "" {
				toolErr = fmt.Errorf("ub chat does not execute tool calls yet")
			} else {
				toolErr = fmt.Errorf("ub chat does not execute tool calls yet: received %q", event.ToolName)
			}
			return recordChatError(cmd, state, toolErr)
		case provider.EventError:
			var eventErr error
			if event.Err != nil {
				eventErr = event.Err
			} else {
				eventErr = fmt.Errorf("provider returned error event")
			}
			return recordChatError(cmd, state, eventErr)
		default:
			return recordChatError(cmd, state, fmt.Errorf("provider returned unsupported event type %q", event.Type))
		}
	}
}

func startChatRollout(cmd *cobra.Command, prompt, model string, opts chatOptions) (*chatSessionState, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	ro, err := rollout.New(st)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("get cwd: %w", err)
	}

	if sessionID := strings.TrimSpace(opts.SessionID); sessionID != "" {
		sess, err := st.GetSession(cmd.Context(), sessionID)
		if errors.Is(err, store.ErrNotFound) {
			_ = st.Close()
			return nil, fmt.Errorf("session %q not found", sessionID)
		}
		if err != nil {
			_ = st.Close()
			return nil, err
		}
		history, nextTurn, err := readChatHistory(cmd, ro, sessionID)
		if err != nil {
			_ = st.Close()
			return nil, err
		}
		return &chatSessionState{
			store:     st,
			rollout:   ro,
			session:   *sess,
			history:   history,
			nextTurn:  nextTurn,
			sessionID: sessionID,
		}, nil
	}

	sessionID := rollout.NewID("sess")
	now := time.Now().UTC()
	sess := store.Session{
		ID:        sessionID,
		Workspace: cwd,
		Title:     chatTitle(prompt),
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.CreateSession(cmd.Context(), sess); err != nil {
		_ = st.Close()
		return nil, err
	}
	return &chatSessionState{
		store:     st,
		rollout:   ro,
		session:   sess,
		nextTurn:  1,
		sessionID: sessionID,
	}, nil
}

func readChatHistory(cmd *cobra.Command, ro *rollout.SQLite, sessionID string) ([]message.Message, int, error) {
	var history []message.Message
	maxTurn := 0
	if err := ro.ForEach(cmd.Context(), sessionID, func(event rollout.Event) error {
		if event.Turn > maxTurn {
			maxTurn = event.Turn
		}
		if event.Type != rollout.TypeUserMessage && event.Type != rollout.TypeAssistantMessage {
			return nil
		}
		var payload rollout.MessagePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode rollout message event %s: %w", event.ID, err)
		}
		msg := payload.Message.Clone()
		if len(msg.Content) == 0 && payload.Text != "" {
			switch event.Type {
			case rollout.TypeUserMessage:
				msg = message.Text(message.RoleUser, payload.Text)
			case rollout.TypeAssistantMessage:
				msg = message.Text(message.RoleAssistant, payload.Text)
			}
		}
		if len(msg.Content) > 0 {
			history = append(history, msg)
		}
		return nil
	}); err != nil {
		return nil, 0, err
	}
	return history, maxTurn + 1, nil
}

func finishChatSession(cmd *cobra.Command, state *chatSessionState, prompt, model string) error {
	state.session.Model = model
	state.session.UpdatedAt = time.Now().UTC()
	if state.session.Title == "" {
		state.session.Title = chatTitle(prompt)
	}
	return state.store.UpdateSession(cmd.Context(), state.session)
}

func recordAssistantMessage(cmd *cobra.Command, ro *rollout.SQLite, sessionID string, turn int, text string) error {
	if text == "" {
		return nil
	}
	event, err := rollout.AssistantMessage(sessionID, turn, message.Text(message.RoleAssistant, text))
	if err != nil {
		return err
	}
	return ro.Append(cmd.Context(), event)
}

func recordChatError(cmd *cobra.Command, state *chatSessionState, chatErr error) error {
	event, err := rollout.Error(state.sessionID, state.nextTurn, chatErr)
	if err != nil {
		return fmt.Errorf("record rollout error payload: %v; original error: %w", err, chatErr)
	}
	if err := state.rollout.Append(cmd.Context(), event); err != nil {
		return fmt.Errorf("record rollout error: %v; original error: %w", err, chatErr)
	}
	state.session.UpdatedAt = time.Now().UTC()
	if err := state.store.UpdateSession(cmd.Context(), state.session); err != nil {
		return fmt.Errorf("update session after chat error: %v; original error: %w", err, chatErr)
	}
	return chatErr
}

func cloneMessages(messages []message.Message) []message.Message {
	if messages == nil {
		return nil
	}
	out := make([]message.Message, len(messages))
	for i, msg := range messages {
		out[i] = msg.Clone()
	}
	return out
}

func chatTitle(prompt string) string {
	title := strings.TrimSpace(strings.Join(strings.Fields(prompt), " "))
	if title == "" {
		return "(empty prompt)"
	}
	const max = 60
	runes := []rune(title)
	if len(runes) <= max {
		return title
	}
	return string(runes[:max-3]) + "..."
}

func readChatPrompt(cmd *cobra.Command, promptArg string) (string, error) {
	if promptArg != "-" {
		return promptArg, nil
	}
	raw, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("read stdin prompt: %w", err)
	}
	return string(raw), nil
}

func selectChatProvider(cfg *config.Config, providerFlag, modelFlag string) (string, string, error) {
	providerName := strings.TrimSpace(providerFlag)
	model := strings.TrimSpace(modelFlag)
	if cfg != nil {
		if model == "" {
			model = strings.TrimSpace(cfg.DefaultModel)
		}
		if providerName == "" {
			providerName = strings.TrimSpace(cfg.DefaultProvider)
		}
		if providerName == "" {
			providerName = firstConfiguredProvider(cfg.Providers)
		}
	}
	if providerName == "" {
		return "", "", fmt.Errorf("provider required: set --provider, default_provider, or configure at least one provider")
	}
	return providerName, model, nil
}

func firstConfiguredProvider(providers map[string]config.ProviderConfig) string {
	for _, name := range sortedProviderNames(providers) {
		if strings.TrimSpace(providers[name].Type) != "" {
			return name
		}
	}
	return ""
}

func runSessionsLS(cmd *cobra.Command) error {
	path, err := store.DefaultPath()
	if err != nil {
		return fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}
	sessions, err := st.ListSessions(cmd.Context(), cwd, 20)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "no sessions")
		return err
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tUPDATED\tTITLE\tMODEL"); err != nil {
		return err
	}
	for _, sess := range sessions {
		title := sess.Title
		if title == "" {
			title = "(untitled)"
		}
		model := sess.Model
		if model == "" {
			model = "-"
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			sess.ID,
			sess.UpdatedAt.Local().Format(time.RFC3339),
			title,
			model,
		); err != nil {
			return err
		}
	}
	return w.Flush()
}

func notImplemented(iteration string) error {
	return fmt.Errorf("not implemented yet — scheduled for %s (see docs/roadmap.md)", iteration)
}
