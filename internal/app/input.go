package app

import (
	"log/slog"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/actions"
	"github.com/tensleyconsulting/seiner/internal/ui/messages"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !m.intro.Done() {
		// Any key skips the intro; ctrl+c still quits outright.
		if key.Matches(msg, m.keys.ForceQuit) {
			cmd := m.quit()
			return m, cmd
		}
		slog.Debug("intro skipped")
		m.intro.Skip()
		return m, nil
	}
	if m.showCopy {
		return m.handleCopyKey(msg)
	}
	if m.showHelp {
		// Any key closes help; ctrl+c still quits outright.
		if key.Matches(msg, m.keys.ForceQuit) {
			cmd := m.quit()
			return m, cmd
		}
		m.showHelp = false
		return m, nil
	}

	if m.search.Active {
		if m.search.Handle(msg) {
			m.rebuildOutline()
			m.syncLayout()
			return m, nil
		}
	}

	switch {
	case key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.ForceQuit):
		cmd := m.quit()
		return m, cmd
	case key.Matches(msg, m.keys.Help):
		m.showHelp = true
		return m, nil
	case key.Matches(msg, m.keys.Search):
		m.search.Activate()
		m.syncLayout()
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		if m.needsReopen() {
			cmd := m.reopenCmd()
			return m, cmd
		}
		m.status.SetFlash("loading…")
		cmd := m.refresh()
		m.rebuildOutline()
		resend := m.scheduleResend(resendRefresh)
		return m, tea.Batch(cmd, resend)
	case key.Matches(msg, m.keys.Copy):
		return m.openCopy()
	case key.Matches(msg, m.keys.Open):
		return m.copyConsoleURL()
	case key.Matches(msg, m.keys.View):
		if m.focus != PaneNav {
			return m, nil
		}
		return m.chooseView(-1)
	case key.Matches(msg, m.keys.Messages):
		m.showMessages = !m.showMessages
		m.syncLayout()
		if m.showMessages && m.paneLayout().msgH == 0 {
			m.status.SetFlash("window too short for messages")
		}
		return m, nil
	case key.Matches(msg, m.keys.Details):
		if _, ok := m.selectedResource(); !ok {
			m.status.SetFlash("select a resource")
			return m, nil
		}
		m.focus = PaneDetail
		m.syncLayout()
		return m, nil
	case key.Matches(msg, m.keys.Back):
		if m.search.Query != "" {
			m.search.Deactivate(true)
			m.rebuildOutline()
			m.syncLayout()
			return m, nil
		}
		if m.focus != PaneNav {
			m.focus = PaneNav
			m.syncLayout()
			return m, nil
		}
		if row, ok := m.nav.SelectedRow(); ok {
			if row.Kind == resources.RowFacet || row.Depth > 0 || row.Expanded || m.outline.showFacets[row.Resource.ID] {
				return m.collapseOutline()
			}
		}
		return m, nil
	case key.Matches(msg, m.keys.Left):
		m.moveFocus(-1)
		m.syncLayout()
		m.syncDetails()
		return m, nil
	case key.Matches(msg, m.keys.Right):
		m.moveFocus(1)
		m.syncLayout()
		m.syncDetails()
		return m, nil
	case key.Matches(msg, m.keys.Select):
		if m.focus == PaneNav {
			return m.toggleOutline()
		}
		if m.focus == PaneDetail {
			return m.expandFromDetails()
		}
		return m, nil
	case key.Matches(msg, m.keys.Top):
		m.moveCursor(0, true, false)
		return m, nil
	case key.Matches(msg, m.keys.Bottom):
		m.moveCursor(0, false, true)
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m.moveCursor(-1, false, false)
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.moveCursor(1, false, false)
		return m, nil
	}
	return m, nil
}

func (m Model) selectedResource() (model.Resource, bool) {
	return m.nav.Selected()
}

func (m Model) openCopy() (Model, tea.Cmd) {
	r, ok := m.selectedResource()
	if !ok {
		m.status.SetFlash("select a resource")
		return m, nil
	}
	m.copyRes = r
	m.copyMenu = actions.NewCopy(r, m.keys)
	m.showCopy = true
	return m, nil
}

func (m Model) copyConsoleURL() (Model, tea.Cmd) {
	r, ok := m.selectedResource()
	if !ok {
		m.status.SetFlash("select a resource")
		return m, nil
	}
	u := r.ConsoleURL()
	m.status.SetFlash("copied console URL")
	slog.Info("copied console url", "url", u, "resource", r.ID)
	return m, tea.SetClipboard(u)
}

func (m Model) handleCopyKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		cmd := m.quit()
		return m, cmd
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Copy):
		m.showCopy = false
		return m, nil
	case key.Matches(msg, m.keys.Select):
		return m.confirmCopy()
	case key.Matches(msg, m.keys.Top):
		m.copyMenu.GoTop()
		return m, nil
	case key.Matches(msg, m.keys.Bottom):
		m.copyMenu.GoBottom()
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m.copyMenu.Move(-1)
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.copyMenu.Move(1)
		return m, nil
	}
	return m, nil
}

func (m Model) confirmCopy() (Model, tea.Cmd) {
	m.showCopy = false
	target := m.copyMenu.Selected()
	label := actions.Label(m.copyRes, target)
	text, err := actions.Payload(m.copyRes, target)
	if err != nil {
		m.status.SetFlash("copy failed")
		m.logf(messages.Error, "Copy %s failed: %v", label, err)
		slog.Warn("copy failed", "target", label, "err", err)
		return m, nil
	}
	m.status.SetFlash("copied " + label)
	slog.Info("copied", "target", label, "bytes", len(text), "resource", m.copyRes.ID)
	return m, tea.SetClipboard(text)
}

func (m *Model) moveFocus(delta int) {
	next := int(m.focus) + delta
	if next < int(PaneNav) {
		next = int(PaneNav)
	}
	last := PaneDetail
	if m.paneLayout().msgH > 0 {
		last = PaneMessages
	}
	if next > int(last) {
		next = int(last)
	}
	m.focus = Pane(next)
}

func (m *Model) moveCursor(delta int, top, bottom bool) {
	switch m.focus {
	case PaneNav:
		switch {
		case top:
			m.nav.GoTop()
		case bottom:
			m.nav.GoBottom()
		default:
			m.nav.Move(delta)
		}
		m.syncDetails()
	case PaneDetail:
		switch {
		case top:
			m.details.GoFacetTop()
		case bottom:
			m.details.GoFacetBottom()
		default:
			m.details.MoveFacet(delta)
		}
	case PaneMessages:
		switch {
		case top:
			m.messages.GoTop()
		case bottom:
			m.messages.GoBottom()
		default:
			m.messages.Scroll(delta)
		}
	}
}
