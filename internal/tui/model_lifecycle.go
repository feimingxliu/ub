package tui

import tea "charm.land/bubbletea/v2"

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{windowSizeCmd(m.width, m.height), requestWindowSize(), waitForPermission(m.permReqs), waitForAsk(m.askReqs), waitForPlanMode(m.planModeReqs), waitForLimit(m.limitReqs), refreshModelLists(m.ctx, m.runner)}
	if m.backgroundEvents != nil {
		cmds = append(cmds, waitForBackgroundEvent(m.backgroundEvents))
	}
	if m.loadMessages != nil {
		cmds = append(cmds, loadMessagesCmd(m.ctx, m.loadMessages))
	}
	return tea.Batch(cmds...)
}
