package tui

import (
	"fmt"
	"strings"
)

func (m *Model) setModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model cannot be empty")
	}
	if runner, ok := m.runner.(ControlRunner); ok {
		if err := runner.SetModel(model); err != nil {
			return err
		}
		m.models = normalizeModels(runner.Models(), model)
	} else if !modelAllowed(m.models, model) {
		return fmt.Errorf("model %q is not available for the current provider; use /model to list candidates", model)
	}
	m.status.model = model
	m.refreshEffortFromRunner()
	return nil
}

func (m *Model) setProvider(providerName, model string) error {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return fmt.Errorf("provider cannot be empty")
	}
	if !modelAllowed(m.providers, providerName) {
		return fmt.Errorf("provider %q is not available; use /provider to list candidates", providerName)
	}
	runner, ok := m.runner.(ProviderControlRunner)
	if !ok {
		return fmt.Errorf("provider switching is unavailable in this runner")
	}
	state, err := runner.SetProvider(providerName, strings.TrimSpace(model))
	if err != nil {
		return err
	}
	if state.Provider == "" {
		state.Provider = providerName
	}
	m.status.provider = state.Provider
	m.providers = normalizeOptions(state.Providers, state.Provider)
	if state.Model != "" {
		m.status.model = state.Model
		m.models = normalizeModels(state.Models, state.Model)
	} else {
		m.status.model = "unknown"
		m.models = append([]string(nil), state.Models...)
	}
	if state.Effort != "" || len(state.Efforts) > 0 {
		m.status.effort = defaultString(state.Effort, "none")
		m.efforts = normalizeOptions(state.Efforts, m.status.effort)
	} else {
		m.refreshEffortFromRunner()
	}
	m.refreshApprovalModelFromRunner()
	m.refreshSmallModelFromRunner()
	return nil
}

func (m *Model) setEffort(effort string) error {
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return fmt.Errorf("effort cannot be empty")
	}
	if runner, ok := m.runner.(EffortControlRunner); ok {
		if err := runner.SetEffort(effort); err != nil {
			return err
		}
		m.refreshEffortFromRunner()
		return nil
	}
	if !modelAllowed(m.efforts, effort) {
		return fmt.Errorf("effort %q is not available for the current model; use /effort to list candidates", effort)
	}
	m.status.effort = effort
	m.efforts = normalizeModels(m.efforts, effort)
	return nil
}

func (m *Model) refreshEffortFromRunner() {
	runner, ok := m.runner.(EffortControlRunner)
	if !ok {
		return
	}
	effort := defaultString(runner.Effort(), "none")
	m.status.effort = effort
	m.efforts = normalizeOptions(runner.Efforts(), effort)
}

func (m *Model) refreshApprovalModelFromRunner() {
	runner, ok := m.runner.(ApprovalControlRunner)
	if !ok {
		return
	}
	model := strings.TrimSpace(runner.ApprovalModel())
	m.approvalModel = model
	m.approvalModels = normalizeModels(runner.ApprovalModels(), model)
}

func (m *Model) refreshSmallModelFromRunner() {
	runner, ok := m.runner.(SmallModelControlRunner)
	if !ok {
		return
	}
	model := strings.TrimSpace(runner.SmallModel())
	m.smallModel = model
	m.smallModels = normalizeModels(runner.SmallModels(), model)
}

func (m *Model) setApprovalModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("approval model cannot be empty")
	}
	if runner, ok := m.runner.(ApprovalControlRunner); ok {
		if err := runner.SetApprovalModel(model); err != nil {
			return err
		}
		m.approvalModels = normalizeModels(runner.ApprovalModels(), model)
	} else if !modelAllowed(m.approvalModels, model) {
		return fmt.Errorf("approval model %q is not available for the current approval provider; use /approval-model to list candidates", model)
	}
	m.approvalModel = model
	return nil
}

func (m *Model) setSmallModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("small model cannot be empty")
	}
	if runner, ok := m.runner.(SmallModelControlRunner); ok {
		if err := runner.SetSmallModel(model); err != nil {
			return err
		}
		m.smallModels = normalizeModels(runner.SmallModels(), model)
	} else if !modelAllowed(m.smallModels, model) {
		return fmt.Errorf("small model %q is not available for the current provider; use /small-model to list candidates", model)
	}
	m.smallModel = model
	return nil
}
