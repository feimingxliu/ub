package permission

import (
	"context"
	"fmt"

	"github.com/feimingxliu/ub/internal/approval"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/tool"
)

// Options configures a Manager.
type Options struct {
	Asker           Asker
	ApprovalAgent   approval.Agent
	GlobalRulesPath string
	GlobalRules     []Rule
}

// Manager applies execution mode, rules, approval agent and human decisions.
type Manager struct {
	asker           Asker
	approvalAgent   approval.Agent
	globalRulesPath string
	globalRules     []Rule
	sessionRules    []Rule
}

// NewManager constructs a permission manager and loads global rules.
func NewManager(opts Options) (*Manager, error) {
	path := opts.GlobalRulesPath
	if path == "" {
		var err error
		path, err = DefaultRulesPath()
		if err != nil {
			return nil, fmt.Errorf("locate permission rules: %w", err)
		}
	}
	globalRules := append([]Rule(nil), opts.GlobalRules...)
	loaded, err := LoadGlobalRules(path)
	if err != nil {
		return nil, err
	}
	globalRules = append(globalRules, loaded...)
	return &Manager{
		asker:           opts.Asker,
		approvalAgent:   opts.ApprovalAgent,
		globalRulesPath: path,
		globalRules:     globalRules,
	}, nil
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
		if rule, ok := matchRule(m.globalRules, req, command); ok {
			return Result{Decision: DecisionAllow, Allowed: true, Source: SourceRule, Reason: ruleReason(rule)}, nil
		}
		if rule, ok := matchRule(m.sessionRules, req, command); ok {
			return Result{Decision: DecisionAllow, Allowed: true, Source: SourceRule, Reason: ruleReason(rule)}, nil
		}
		if mode == execution.ModeAgentApprove && m.approvalAgent != nil {
			agentRes, err := m.approvalAgent.ReviewCommand(ctx, approval.Request{
				Tool:           req.Tool,
				Args:           req.Args,
				Risk:           req.Risk,
				Mode:           req.Mode,
				Command:        command,
				Cwd:            cwdFromRequest(req),
				ContextSummary: req.ContextSummary,
			})
			if err == nil && agentRes.Decision == approval.DecisionAllow {
				return Result{Decision: DecisionAllow, Allowed: true, Source: SourceApprovalAgent, Reason: agentRes.Reason}, nil
			}
		}
	}

	decision, err := m.askHuman(ctx, req)
	if err != nil {
		return Result{}, err
	}
	return m.applyHumanDecision(decision, req, command)
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
		m.sessionRules = append(m.sessionRules, Rule{Tool: req.Tool, Command: command})
		return Result{Decision: decision, Allowed: true, Source: SourceHuman}, nil
	case DecisionAlwaysTool:
		m.sessionRules = append(m.sessionRules, Rule{Tool: req.Tool})
		return Result{Decision: decision, Allowed: true, Source: SourceHuman}, nil
	case DecisionAlwaysGlobal:
		rule := Rule{Tool: req.Tool}
		if err := SaveGlobalRule(m.globalRulesPath, rule); err != nil {
			return Result{}, err
		}
		m.globalRules = append(m.globalRules, rule)
		return Result{Decision: decision, Allowed: true, Source: SourceHuman}, nil
	default:
		return Result{}, fmt.Errorf("permission: unknown decision %q", decision)
	}
}

func ruleReason(rule Rule) string {
	switch {
	case rule.Command != "":
		return "matched command rule"
	case rule.CommandPrefix != "":
		return "matched command prefix rule"
	default:
		return "matched tool rule"
	}
}
