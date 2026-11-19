package app

import "github.com/tensleyconsulting/seiner/internal/ui/header"

// LayoutMode selects how many panes are visible.
type LayoutMode int

const (
	LayoutSmall LayoutMode = iota
	LayoutMedium
	LayoutLarge
)

const (
	largeMinWidth  = 120
	mediumMinWidth = 80
	minNavWidth    = 22
	minDetailWidth = 24
	// The messages pane sits under details, hidden until toggled on or an
	// error arrives. It starts at defaultMsgHeight
	// rows (borders and three lines) and is dragged between minMsgHeight
	// and whatever leaves details minDetailHeight.
	defaultMsgHeight = 5
	minMsgHeight     = 3
	minDetailHeight  = 8
)

func layoutMode(width int) LayoutMode {
	switch {
	case width >= largeMinWidth:
		return LayoutLarge
	case width >= mediumMinWidth:
		return LayoutMedium
	default:
		return LayoutSmall
	}
}

func (m LayoutMode) String() string {
	switch m {
	case LayoutLarge:
		return "large"
	case LayoutMedium:
		return "medium"
	default:
		return "small"
	}
}

type paneSizes struct {
	navW, detailW int
	bodyH         int
	// detailH and msgH split bodyH in the right column; msgH is 0 when
	// the body is too short for the messages pane.
	detailH, msgH int
}

// splitRight divides the right column's height between details and the
// messages pane. msgH is the requested messages height; 0 is the default.
// show is false while the pane is hidden, which gives details the column.
func splitRight(bodyH, msgH int, show bool) (detailH, msgsH int) {
	if !show || bodyH < minDetailHeight+minMsgHeight {
		return bodyH, 0
	}
	if msgH <= 0 {
		msgH = defaultMsgHeight
	}
	msgH = min(max(msgH, minMsgHeight), bodyH-minDetailHeight)
	return bodyH - msgH, msgH
}

// allocatePanes sizes the panes. banners is the number of persistent lines
// between the header and the body. msgH is the requested messages height
// and showMsgs whether the pane is on.
func allocatePanes(width, height, split, msgH int, showMsgs bool, mode LayoutMode, searchOpen bool, banners int) paneSizes {
	headerH := header.Rows + banners
	footerH := 1
	if searchOpen {
		footerH = 2
	}
	bodyH := height - headerH - footerH
	if bodyH < 3 {
		bodyH = 3
	}

	s := paneSizes{bodyH: bodyH}
	s.detailH, s.msgH = splitRight(bodyH, msgH, showMsgs)
	if mode == LayoutSmall || width < minNavWidth+minDetailWidth {
		s.navW = width
		s.detailW = width
		return s
	}

	def := max(minNavWidth, width*36/100)
	navW := def
	if split > 0 {
		navW = split
	}
	maxNav := width - minDetailWidth
	if maxNav < minNavWidth {
		maxNav = minNavWidth
	}
	if navW < minNavWidth {
		navW = minNavWidth
	}
	if navW > maxNav {
		navW = maxNav
	}
	s.navW = navW
	s.detailW = width - navW
	return s
}

func clampSplit(width, split int) int {
	if width < minNavWidth+minDetailWidth {
		return 0
	}
	maxNav := width - minDetailWidth
	if split < minNavWidth {
		return minNavWidth
	}
	if split > maxNav {
		return maxNav
	}
	return split
}
