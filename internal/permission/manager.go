package permission

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/feimingxliu/ub/internal/approval"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/tool"
)

// Options configures a Manager.
type Options struct {
	Asker            Asker
	ApprovalAgent    approval.Agent
	ProjectRulesPath string
	AllowRules       []Rule
	AskRules         []Rule
	DenyRules        []Rule
}

// Manager applies execution mode, rules, approval agent and human decisions.
type Manager struct {
	asker            Asker
	approvalAgent    approval.Agent
	projectRulesPath string
	allowRules       []Rule
	askRules         []Rule
	denyRules        []Rule
	sessionRules     []Rule
	mu               sync.RWMutex
}

// NewManager constructs a permission manager and loads project rules.
func NewManager(opts Options) (*Manager, error) {
	projectPath := opts.ProjectRulesPath
	allowRules := append([]Rule(nil), opts.AllowRules...)
	askRules := append([]Rule(nil), opts.AskRules...)
	denyRules := append([]Rule(nil), opts.DenyRules...)
	if projectPath != "" {
		loadedAllow, loadedAsk, loadedDeny, err := LoadProjectRules(projectPath)
		if err != nil {
			return nil, err
		}
		allowRules = append(allowRules, loadedAllow...)
		askRules = append(askRules, loadedAsk...)
		denyRules = append(denyRules, loadedDeny...)
	}
	return &Manager{
		asker:            opts.Asker,
		approvalAgent:    opts.ApprovalAgent,
		projectRulesPath: projectPath,
		allowRules:       allowRules,
		askRules:         askRules,
		denyRules:        denyRules,
	}, nil
}

// SetApprovalAgent replaces the auto-mode approval agent for future requests.
func (m *Manager) SetApprovalAgent(agent approval.Agent) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.approvalAgent = agent
	m.mu.Unlock()
}

// Ask returns the permission decision for one tool call.
func (m *Manager) Ask(ctx context.Context, req Request) (Result, error) {
	mode, err := execution.ParseMode(string(req.Mode))
	if err != nil {
		return Result{}, err
	}
	req.Mode = mode
	if err := execution.Gate(mode, req.Risk); err != nil {
		return Result{Decision: DecisionDeny, Allowed: false, Source: SourceMode, Reason: err.Error()}, nil
	}
	if req.Risk != tool.RiskExec {
		return Result{Decision: DecisionAllow, Allowed: true, Source: SourceAuto}, nil
	}

	command := commandFromRequest(req)
	blacklisted := isBlacklisted(command)
	if !blacklisted {
		m.mu.RLock()
		denyRule, denyOK := matchAnyRule(m.denyRules, req, command)
		allowRule, allowOK := matchAllowRule(m.allowRules, req, command)
		askRule, askOK := matchAnyRule(m.askRules, req, command)
		sessionRule, sessionOK := matchAllowRule(m.sessionRules, req, command)
		agent := m.approvalAgent
		m.mu.RUnlock()
		if denyOK {
			return Result{Decision: DecisionDeny, Allowed: false, Source: SourceRule, Reason: ruleReason(denyRule)}, nil
		}
		if allowOK {
			return Result{Decision: DecisionAllow, Allowed: true, Source: SourceRule, Reason: ruleReason(allowRule)}, nil
		}
		if sessionOK {
			return Result{Decision: DecisionAllow, Allowed: true, Source: SourceRule, Reason: ruleReason(sessionRule)}, nil
		}
		if askOK {
			req.ApprovalReason = ruleReason(askRule)
			decision, err := m.askHuman(ctx, req)
			if err != nil {
				return Result{}, err
			}
			return m.applyHumanDecision(decision, req, command)
		}
		if mode == execution.ModeAuto {
			if agent == nil {
				slog.Info("approval agent unavailable; falling back to human approval", "tool", req.Tool, "risk", req.Risk, "mode", req.Mode)
			} else {
				agentRes, err := agent.ReviewCommand(ctx, approval.Request{
					Tool:           req.Tool,
					Args:           req.Args,
					Risk:           req.Risk,
					Mode:           req.Mode,
					Command:        command,
					Cwd:            cwdFromRequest(req),
					ContextSummary: req.ContextSummary,
				})
				if err == nil {
					slog.Info("approval agent decision", "tool", req.Tool, "risk", req.Risk, "mode", req.Mode, "decision", agentRes.Decision, "reason", agentRes.Reason)
					notifyApprovalObserver(req, agentRes.Decision, agentRes.Reason, nil)
					if agentRes.Decision == approval.DecisionAllow {
						return Result{Decision: DecisionAllow, Allowed: true, Source: SourceApprovalAgent, Reason: agentRes.Reason}, nil
					}
				} else {
					slog.Warn("approval agent review failed", "tool", req.Tool, "risk", req.Risk, "mode", req.Mode, "err", err)
					notifyApprovalObserver(req, "", "", err)
				}
				if err != nil {
					req.ApprovalReason = err.Error()
				} else if agentRes.Reason != "" {
					req.ApprovalReason = agentRes.Reason
				} else {
					req.ApprovalReason = string(agentRes.Decision)
				}
			}
		}
	}

	decision, err := m.askHuman(ctx, req)
	if err != nil {
		return Result{}, err
	}
	return m.applyHumanDecision(decision, req, command)
}

func notifyApprovalObserver(req Request, decision approval.Decision, reason string, err error) {
	if req.ApprovalObserver == nil {
		return
	}
	req.ApprovalObserver(ApprovalObservation{
		Decision: string(decision),
		Reason:   reason,
		Err:      err,
	})
}

func (m *Manager) askHuman(ctx context.Context, req Request) (Decision, error) {
	if m.asker == nil {
		return "", fmt.Errorf("permission: human asker is required")
	}
	return m.asker.Ask(ctx, req)
}

func (m *Manager) applyHumanDecision(decision Decision, req Request, command string) (Result, error) {
	switch decision {
	case DecisionAllow:
		return Result{Decision: decision, Allowed: true, Source: SourceHuman}, nil
	case DecisionDeny:
		return Result{Decision: decision, Allowed: false, Source: SourceHuman}, nil
	case DecisionAlwaysCmd:
		if command == "" {
			return Result{}, fmt.Errorf("permission: exact command rule requires command text")
		}
		m.mu.Lock()
		m.sessionRules = append(m.sessionRules, Rule{Raw: formatPermissionRule(req.Tool, command), Action: RuleAllow, Tool: permissionToolName(req.Tool), Pattern: command})
		m.mu.Unlock()
		return Result{Decision: decision, Allowed: true, Source: SourceHuman}, nil
	case DecisionAlwaysTool:
		m.mu.Lock()
		m.sessionRules = append(m.sessionRules, Rule{Raw: formatPermissionRule(req.Tool, ""), Action: RuleAllow, Tool: permissionToolName(req.Tool)})
		m.mu.Unlock()
		return Result{Decision: decision, Allowed: true, Source: SourceHuman}, nil
	case DecisionAlwaysProjectCmd:
		if command == "" {
			return Result{}, fmt.Errorf("permission: project command rule requires command text")
		}
		path, err := m.projectRulePath(req)
		if err != nil {
			return Result{}, err
		}
		raw := formatPermissionRule(req.Tool, command)
		rule, err := parsePermissionRule(raw, RuleAllow)
		if err != nil {
			return Result{}, err
		}
		if err := SaveProjectRule(path, RuleAllow, raw); err != nil {
			return Result{}, err
		}
		m.mu.Lock()
		m.projectRulesPath = path
		m.allowRules = append(m.allowRules, rule)
		m.mu.Unlock()
		return Result{Decision: decision, Allowed: true, Source: SourceHuman}, nil
	case DecisionAlwaysProjectPattern:
		rules, err := projectPatternRules(req.Tool, command)
		if err != nil {
			return Result{}, err
		}
		path, err := m.projectRulePath(req)
		if err != nil {
			return Result{}, err
		}
		rawRules := make([]string, 0, len(rules))
		for _, rule := range rules {
			rawRules = append(rawRules, rule.Raw)
		}
		if err := SaveProjectRules(path, RuleAllow, rawRules); err != nil {
			return Result{}, err
		}
		m.mu.Lock()
		m.projectRulesPath = path
		m.allowRules = append(m.allowRules, rules...)
		m.mu.Unlock()
		return Result{Decision: decision, Allowed: true, Source: SourceHuman}, nil
	default:
		return Result{}, fmt.Errorf("permission: unknown decision %q", decision)
	}
}

func (m *Manager) projectRulePath(req Request) (string, error) {
	if m.projectRulesPath != "" {
		return m.projectRulesPath, nil
	}
	return ProjectRulesPath(workspaceFromRequest(req))
}

func projectPatternRules(toolName, command string) ([]Rule, error) {
	commands := splitShellCommands(command)
	if len(commands) == 0 {
		return nil, fmt.Errorf("permission: project command pattern requires command text")
	}
	if len(commands) > 5 {
		return nil, fmt.Errorf("permission: refusing to save %d command patterns; split the command or edit .ub/permissions.yaml manually", len(commands))
	}
	rules := make([]Rule, 0, len(commands))
	for _, subcommand := range commands {
		pattern := similarCommandPattern(subcommand)
		if pattern == "" {
			return nil, fmt.Errorf("permission: cannot derive command pattern for %q", subcommand)
		}
		raw := formatPermissionRule(toolName, pattern)
		rule, err := parsePermissionRule(raw, RuleAllow)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func similarCommandPattern(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	keep := similarPatternFieldCount(fields)
	prefix := strings.Join(fields[:keep], " ")
	return prefix + ":*"
}

func similarPatternFieldCount(fields []string) int {
	if len(fields) <= 2 {
		return len(fields)
	}
	switch fields[0] {
	case "npm", "pnpm", "yarn":
		if fields[1] == "run" && len(fields) >= 3 {
			return 3
		}
		return 2
	case "make":
		return 2
	default:
		return 2
	}
}

func ruleReason(rule Rule) string {
	switch {
	case rule.Pattern != "" && strings.Contains(rule.Pattern, "*"):
		return fmt.Sprintf("matched %s rule %s", rule.Action, rule.Raw)
	case rule.Pattern != "":
		return fmt.Sprintf("matched %s exact command rule %s", rule.Action, rule.Raw)
	default:
		return fmt.Sprintf("matched %s tool rule %s", rule.Action, rule.Raw)
	}
}
