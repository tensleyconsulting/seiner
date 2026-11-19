package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/ui/header"
)

// dragTarget is the divider a left-button drag is moving.
type dragTarget int

const (
	dragNone dragTarget = iota
	// dragSplit moves the vertical divider between explorer and details.
	dragSplit
	// dragMessages moves the horizontal divider between details and
	// messages.
	dragMessages
)

func (m Model) overlayOpen() bool {
	return m.showHelp || m.showCopy
}

func (m Model) bodyTop() int {
	return header.Rows + len(m.banners())
}

func (m Model) twoPane() bool {
	return layoutMode(m.width) != LayoutSmall && m.width >= minNavWidth+minDetailWidth
}

func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.overlayOpen() || msg.Button != tea.MouseLeft {
		return m, nil
	}
	if m.onMsgDivider(msg.X, msg.Y) {
		m.drag = dragMessages
		return m, nil
	}
	if m.twoPane() && m.onSplit(msg.X) {
		m.drag = dragSplit
		m.split = clampSplit(m.width, msg.X)
		m.syncLayout()
		return m, nil
	}
	return m.mouseSelect(msg.X, msg.Y, true)
}

func (m Model) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	return m.dragTo(msg.X, msg.Y), nil
}

func (m Model) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if m.drag == dragNone {
		return m, nil
	}
	if msg.Button == tea.MouseLeft {
		m = m.dragTo(msg.X, msg.Y)
	}
	m.drag = dragNone
	m.syncLayout()
	return m, nil
}

// dragTo moves the divider being dragged to the pointer at (x, y).
func (m Model) dragTo(x, y int) Model {
	switch m.drag {
	case dragSplit:
		m.split = clampSplit(m.width, x)
	case dragMessages:
		// The pointer row becomes the messages pane's top border.
		sizes := m.paneLayout()
		_, m.msgH = splitRight(sizes.bodyH, max(minMsgHeight, m.bodyTop()+sizes.bodyH-y), true)
	default:
		return m
	}
	m.syncLayout()
	return m
}

func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.overlayOpen() {
		return m, nil
	}
	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return m, nil
	}
	if m.inExplorer(msg.X, msg.Y) {
		m.focus = PaneNav
		m.nav.Move(delta)
		m.syncLayout()
		m.syncDetails()
		return m, nil
	}
	if m.inDetails(msg.X, msg.Y) {
		m.focus = PaneDetail
		m.details.MoveFacet(delta)
		m.syncLayout()
		return m, nil
	}
	if m.inMessages(msg.X, msg.Y) {
		m.focus = PaneMessages
		m.messages.Scroll(delta)
		m.syncLayout()
		return m, nil
	}
	return m, nil
}

func (m Model) mouseSelect(x, y int, allowToggle bool) (tea.Model, tea.Cmd) {
	if m.inDetails(x, y) {
		m.focus = PaneDetail
		m.syncLayout()
		return m, nil
	}
	if m.inMessages(x, y) {
		m.focus = PaneMessages
		m.syncLayout()
		return m, nil
	}
	if !m.inExplorer(x, y) {
		return m, nil
	}
	m.focus = PaneNav
	idx, triangle, ok := m.nav.Hit(x, y-m.bodyTop())
	if !ok {
		m.syncLayout()
		return m, nil
	}
	m.nav.SelectIndex(idx)
	m.syncLayout()
	m.syncDetails()
	if allowToggle && triangle {
		return m.toggleOutline()
	}
	if choice, ok := m.nav.ChoiceAt(idx, x); ok && allowToggle {
		return m.chooseView(choice)
	}
	return m, nil
}

func (m Model) onSplit(x int) bool {
	if !m.twoPane() {
		return false
	}
	w := m.nav.Width()
	return x == w-1 || x == w
}

// onMsgDivider reports whether (x, y) is on the border between details and
// messages: details' bottom edge or messages' top edge.
func (m Model) onMsgDivider(x, y int) bool {
	sizes := m.paneLayout()
	if sizes.msgH == 0 || !m.inRightColumn(x) {
		return false
	}
	edge := m.bodyTop() + sizes.detailH
	return y == edge-1 || y == edge
}

// inRightColumn reports whether column x shows details and messages.
func (m Model) inRightColumn(x int) bool {
	if !m.twoPane() {
		return m.focus != PaneNav
	}
	return x >= m.nav.Width()
}

func (m Model) inExplorer(x, y int) bool {
	top := m.bodyTop()
	if y < top || y >= top+m.navHeight() {
		return false
	}
	if !m.twoPane() {
		return m.focus == PaneNav
	}
	return x < m.nav.Width()
}

func (m Model) inDetails(x, y int) bool {
	top := m.bodyTop()
	if y < top || y >= top+m.paneLayout().detailH {
		return false
	}
	return m.inRightColumn(x)
}

func (m Model) inMessages(x, y int) bool {
	sizes := m.paneLayout()
	top := m.bodyTop() + sizes.detailH
	if sizes.msgH == 0 || y < top || y >= top+sizes.msgH {
		return false
	}
	return m.inRightColumn(x)
}

func (m Model) navHeight() int {
	return m.paneLayout().bodyH
}
