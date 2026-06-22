package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	permissiondialog "github.com/feimingxliu/ub/internal/app/ub/tui/dialog/permission"
)

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case streamEventMsg:
		return m.handleStreamEvent(msg)
	case sideQuestionEventMsg:
		return m.handleSideQuestionEvent(msg)
	case backgroundEventMsg:
		return m.handleBackgroundEvent(msg)
	case permissionRequestMsg:
		return m.handlePermissionRequest(msg)
	case askRequestMsg:
		return m.handleAskRequest(msg)
	case planModeRequestMsg:
		return m.handlePlanModeRequest(msg)
	case limitRequestMsg:
		return m.handleLimitRequest(msg)
	case spinnerTickMsg:
		return m.handleSpinnerTick(msg)
	case toastExpireMsg:
		m.handleToastExpire(msg)
		return m, nil
	case doctorResultMsg:
		return m.handleDoctorResult(msg)
	case planEditFinishedMsg:
		return m.handlePlanEditFinished(msg)
	case copyResultMsg:
		return m.handleCopyResult(msg)
	case modelRefreshResultMsg:
		return m.handleModelRefreshResult(msg)
	case messagesLoadedMsg:
		return m.handleMessagesLoaded(msg)
	}
	m.clearToastForInteraction(msg)
	m.clearEscInterruptConfirmForInteraction(msg)

	if m.pendingLimit != nil {
		if mouseMsg, ok := msg.(tea.MouseWheelMsg); ok {
			switch mouseMsg.Mouse().Button {
			case tea.MouseWheelUp:
				m.scrollMessages(3)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollMessages(-3)
				return m, nil
			}
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+home":
				m.scrollToTop()
				return m, nil
			case "ctrl+end":
				m.scrollToBottom()
				return m, nil
			case "y", "Y", "enter":
				return m.resolveLimit(defaultLimitExtension)
			case "n", "N", "esc", "ctrl+c":
				return m.resolveLimit(0)
			}
		}
		return m, nil
	}

	if m.pendingAsk != nil {
		if mouseMsg, ok := msg.(tea.MouseWheelMsg); ok {
			switch mouseMsg.Mouse().Button {
			case tea.MouseWheelUp:
				m.scrollMessages(3)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollMessages(-3)
				return m, nil
			}
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				if m.askPrompt.otherMode {
					m.askPrompt.ExitOtherMode()
					return m, nil
				}
				return m.resolveAsk(true)
			case "ctrl+home":
				m.scrollToTop()
				return m, nil
			case "ctrl+end":
				m.scrollToBottom()
				return m, nil
			case "enter":
				if m.askPrompt.HandleEnter() {
					return m.resolveAsk(false)
				}
				return m, nil
			default:
				m.askPrompt.HandleKey(key)
				return m, nil
			}
		}
		return m, nil
	}

	if m.pendingPlanMode != nil {
		if mouseMsg, ok := msg.(tea.MouseWheelMsg); ok {
			switch mouseMsg.Mouse().Button {
			case tea.MouseWheelUp:
				m.scrollMessages(3)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollMessages(-3)
				return m, nil
			}
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch strings.ToLower(key.String()) {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc", "n":
				return m.resolvePlanMode(false)
			case "y", "enter":
				return m.resolvePlanMode(true)
			case "ctrl+home":
				m.scrollToTop()
				return m, nil
			case "ctrl+end":
				m.scrollToBottom()
				return m, nil
			}
		}
		return m, nil
	}

	if m.pending != nil {
		if mouseMsg, ok := msg.(tea.MouseWheelMsg); ok {
			switch mouseMsg.Mouse().Button {
			case tea.MouseWheelUp:
				m.scrollMessages(3)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollMessages(-3)
				return m, nil
			}
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				if m.confirmEscInterrupt(key) {
					m.interruptCurrent()
					return m, waitForPermission(m.permReqs)
				}
				return m, m.showToast(toastNotice, "press Esc again to interrupt")
			case "shift+tab":
				m.clearEscInterruptConfirm()
				return m.cycleMode()
			case "ctrl+home":
				m.scrollToTop()
				return m, nil
			case "ctrl+end":
				m.scrollToBottom()
				return m, nil
			case "pgup":
				m.scrollMessages(m.pageScrollLines())
				return m, nil
			case "pgdown":
				m.scrollMessages(-m.pageScrollLines())
				return m, nil
			case "d":
				m.modal = m.modal.ToggleDiff()
				return m, nil
			case "enter":
				return m.resolvePermission(m.modal.SelectedDecision())
			default:
				if m.modal.HandleKey(key.String()) {
					return m, nil
				}
				if decision, ok := permissiondialog.DecisionForKey(key.String()); ok {
					return m.resolvePermission(decision)
				}
			}
		}
		return m, nil
	}

	if m.picker != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				m.picker = nil
				return m, nil
			case "up", "k":
				m.picker.previous()
				return m, nil
			case "down", "j", "tab":
				m.picker.next()
				return m, nil
			case "enter":
				selected := m.picker.selected()
				target := m.pickerTarget
				m.picker = nil
				m.pickerTarget = ""
				if target == "approval" {
					if err := m.setApprovalModel(selected); err != nil {
						m.messages.append(systemRole, err.Error())
						return m, nil
					}
					m.messages.append(systemRole, "approval model set to "+selected)
					return m, nil
				}
				if target == "small" {
					if err := m.setSmallModel(selected); err != nil {
						m.messages.append(systemRole, err.Error())
						return m, nil
					}
					m.messages.append(systemRole, "small model set to "+selected)
					return m, nil
				}
				if target == "effort" {
					if err := m.setEffort(selected); err != nil {
						m.messages.append(systemRole, err.Error())
						return m, nil
					}
					m.messages.append(systemRole, "effort set to "+selected)
					return m, nil
				}
				if target == "provider" {
					if err := m.setProvider(selected, ""); err != nil {
						m.messages.append(systemRole, err.Error())
						return m, nil
					}
					m.messages.append(systemRole, "provider set to "+m.status.provider+" model "+m.status.model)
					return m, nil
				}
				if err := m.setModel(selected); err != nil {
					m.messages.append(systemRole, err.Error())
					return m, nil
				}
				m.messages.append(systemRole, "model set to "+selected)
				return m, nil
			}
		}
		return m, nil
	}

	if m.plans != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				m.plans = nil
				return m, nil
			case "up", "k":
				m.plans.previous()
				return m, nil
			case "down", "j", "tab":
				m.plans.next()
				return m, nil
			case "backspace", "delete":
				m.plans.backspace()
				return m, nil
			case "ctrl+u":
				m.plans.clearQuery()
				return m, nil
			case "enter":
				selected := m.plans.selected()
				if selected.ID == "" {
					return m, nil
				}
				m.plans = nil
				return m.editPlanArtifact([]string{selected.ID})
			}
			for _, r := range key.Text {
				m.plans.appendRune(r)
			}
		}
		return m, nil
	}

	if m.rewind != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				m.rewind = nil
				return m, nil
			case "up", "k":
				m.rewind.previous()
				return m, nil
			case "down", "j", "tab":
				m.rewind.next()
				return m, nil
			case "backspace", "delete":
				m.rewind.backspace()
				return m, nil
			case "ctrl+u":
				m.rewind.clearQuery()
				return m, nil
			case "enter":
				if m.rewind.phase == rewindPickerMode {
					target := m.rewind.chosen
					mode := m.rewind.selectedMode()
					m.rewind = nil
					return m.applyRewind(target, mode.revertFiles)
				}
				target := m.rewind.selectedTarget()
				if target.Turn <= 0 {
					return m, nil
				}
				if len(target.AffectedFiles) > 0 {
					m.rewind.chooseTarget(target)
					return m, nil
				}
				m.rewind = nil
				return m.applyRewind(target, false)
			}
			for _, r := range key.Text {
				m.rewind.appendRune(r)
			}
		}
		return m, nil
	}

	if m.sessions != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "esc":
				m.sessions = nil
				return m, nil
			case "up", "k":
				m.sessions.previous()
				return m, nil
			case "down", "j", "tab":
				m.sessions.next()
				return m, nil
			case "backspace", "delete":
				m.sessions.backspace()
				return m, nil
			case "ctrl+u":
				m.sessions.clearQuery()
				return m, nil
			case "enter":
				selected := m.sessions.selected()
				if selected.ID == "" {
					return m, nil
				}
				m.sessions = nil
				return m.switchSession(selected.ID)
			}
			for _, r := range key.Text {
				m.sessions.appendRune(r)
			}
		}
		return m, nil
	}

	if m.btw.visible {
		switch msg := msg.(type) {
		case tea.MouseWheelMsg:
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				m.scrollSideQuestion(sideQuestionWheelScrollLines)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollSideQuestion(-sideQuestionWheelScrollLines)
				return m, nil
			}
		case tea.MouseClickMsg, tea.MouseReleaseMsg:
			return m, nil
		case tea.KeyPressMsg:
			if updated, cmd, handled := m.handleSideQuestionKey(msg); handled {
				return updated, cmd
			}
		}
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft {
			if m.statusHelpHit(mouse.X, mouse.Y) {
				return m.showCheatsheet()
			}
			if m.toggleMessageAt(mouse.X, mouse.Y) {
				m.scrollFocusedMessageIntoView()
				return m, nil
			}
		}
	case tea.MouseWheelMsg:
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			m.scrollMessages(3)
			return m, nil
		case tea.MouseWheelDown:
			m.scrollMessages(-3)
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.status.width = msg.Width
		m.input.MaxHeight = inputMaxHeight(msg.Height)
		m.input.SetWidth(inputContentWidth(msg.Width))
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "?":
			return m.showCheatsheet()
		case "esc":
			if m.files != nil {
				m.files = nil
				m.clearEscInterruptConfirm()
				return m, nil
			}
			if !m.running && m.messages.clearFocus() {
				m.clearEscInterruptConfirm()
				return m, nil
			}
			if m.running {
				if m.confirmEscInterrupt(msg) {
					m.interruptCurrent()
				} else {
					return m, m.showToast(toastNotice, "press Esc again to interrupt")
				}
			}
			return m, nil
		case "pgup":
			m.scrollMessages(m.pageScrollLines())
			return m, nil
		case "pgdown":
			m.scrollMessages(-m.pageScrollLines())
			return m, nil
		case "ctrl+home":
			m.scrollToTop()
			return m, nil
		case "ctrl+end":
			m.scrollToBottom()
			return m, nil
		case "ctrl+o":
			if m.messages.toggleLatestCollapsible() {
				m.scrollFocusedMessageIntoView()
				return m, nil
			}
		case "ctrl+n":
			if m.messages.focusNextCollapsible() {
				m.scrollFocusedMessageIntoView()
				return m, nil
			}
		case "ctrl+p":
			if m.messages.focusPreviousCollapsible() {
				m.scrollFocusedMessageIntoView()
				return m, nil
			}
		case "up":
			if m.moveFileSelection(-1) {
				return m, nil
			}
			if m.moveSlashValueSelection(-1) {
				return m, nil
			}
			if m.moveSlashSelection(-1) {
				return m, nil
			}
			// Smart switching: when the cursor sits on the first line, Up
			// navigates prompt history / queued prompts (single-line habit);
			// on any lower line it moves the cursor up within the textarea.
			if m.input.Line() == 0 {
				if m.navigateQueuedPrompts(-1) {
					return m, nil
				}
				if m.navigatePromptHistory(-1) {
					return m, nil
				}
				return m, nil
			}
			m.input.CursorUp()
			return m, nil
		case "down":
			if m.moveFileSelection(1) {
				return m, nil
			}
			if m.moveSlashValueSelection(1) {
				return m, nil
			}
			if m.moveSlashSelection(1) {
				return m, nil
			}
			// Smart switching: when the cursor sits on the last line, Down
			// navigates prompt history / queued prompts; otherwise it moves
			// the cursor down within the textarea.
			if m.input.Line() == m.input.LineCount()-1 {
				if m.navigateQueuedPrompts(1) {
					return m, nil
				}
				if m.navigatePromptHistory(1) {
					return m, nil
				}
				return m, nil
			}
			m.input.CursorDown()
			return m, nil
		case "shift+tab":
			return m.cycleMode()
		case "tab":
			if m.running {
				// While the agent is running, TAB queues the input for the
				// next turn instead of injecting it into the current run.
				if m.queueInput() {
					return m, nil
				}
				return m, nil
			}
			if m.completeFileMention() {
				return m, nil
			}
			if m.completeSlashValue() {
				return m, nil
			}
			if m.completeSlash() {
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		case "shift+enter", "ctrl+j":
			// Insert a newline into the textarea (Enter alone submits). The
			// textarea's KeyMap binds these to InsertNewline.
			var nlCmd tea.Cmd
			m.input, nlCmd = m.input.Update(msg)
			return m, nlCmd
		case "enter":
			if m.loadingMessages {
				return m, nil
			}
			if strings.TrimSpace(m.input.Value()) == "" && m.messages.hasFocusedCollapsible() {
				if m.messages.toggleFocusedCollapsible() {
					m.scrollFocusedMessageIntoView()
					return m, nil
				}
			}
			if m.running {
				if text := strings.TrimSpace(m.input.Value()); isSideQuestionInput(text) {
					m.input.SetValue("")
					m.files = nil
					m.resetPromptHistoryNavigation()
					return m.executeSlash(text)
				}
				// If the user is navigating queued prompts (queueIdx >= 0),
				// Enter saves the edit rather than injecting.
				if m.queueIdx >= 0 {
					if m.queueInput() {
						return m, nil
					}
					return m, nil
				}
				// While the agent is running, Enter injects the input as
				// guidance into the current run. Use TAB to queue for the
				// next turn instead.
				if text := strings.TrimSpace(m.input.Value()); text != "" && !strings.HasPrefix(text, "/") && !isShellInput(text) {
					return m.injectGuidance(text)
				}
				return m, nil
			}
			if m.completeFileMention() {
				return m, nil
			}
			if updated, cmd, ok := m.acceptSlashValueSuggestion(); ok {
				return updated, cmd
			}
			if m.completeSlashOnEnter() {
				return m, nil
			}
			if text := strings.TrimSpace(m.input.Value()); text != "" {
				if strings.HasPrefix(text, "/") {
					m.input.SetValue("")
					m.files = nil
					m.resetPromptHistoryNavigation()
					return m.executeSlash(text)
				}
				if isShellInput(text) {
					return m.startShell(text, true)
				}
				return m.startPrompt(text, true)
			}
			return m, nil
		case "space":
			if strings.TrimSpace(m.input.Value()) == "" && m.messages.hasFocusedCollapsible() {
				if m.messages.toggleFocusedCollapsible() {
					m.scrollFocusedMessageIntoView()
					return m, nil
				}
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.saveQueuedPromptEdit()
	m.refreshFilePicker()
	return m, cmd
}
