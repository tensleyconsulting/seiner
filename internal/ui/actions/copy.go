package actions

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/keys"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
	"github.com/tensleyconsulting/seiner/internal/ui/text"
)

// Target is a copy-able field of the selected resource. Targets from
// targetCopies on are the resource's own Copies, in order.
type Target int

const (
	TargetName Target = iota
	TargetID
	TargetJSON
	targetCopies
)

// Targets lists r's copy targets: name, ID, JSON, then r.Copies.
func Targets(r model.Resource) []Target {
	out := []Target{TargetName, TargetID, TargetJSON}
	for i := range r.Copies {
		out = append(out, targetCopies+Target(i))
	}
	return out
}

// extra returns r's Copy for t, if t is one of r.Copies.
func extra(r model.Resource, t Target) (model.Copy, bool) {
	i := int(t - targetCopies)
	if i < 0 || i >= len(r.Copies) {
		return model.Copy{}, false
	}
	return r.Copies[i], true
}

// Label is the short name the status flash uses ("copied id").
func Label(r model.Resource, t Target) string {
	switch t {
	case TargetName:
		return "name"
	case TargetID:
		return "id"
	case TargetJSON:
		return "json"
	}
	if c, ok := extra(r, t); ok {
		return c.Label
	}
	return "value"
}

// Title is the overlay's row label.
func Title(r model.Resource, t Target) string {
	switch t {
	case TargetName:
		return "Name"
	case TargetID:
		return "ID"
	case TargetJSON:
		return "JSON"
	}
	if c, ok := extra(r, t); ok {
		return c.Title
	}
	return Label(r, t)
}

// Payload returns clipboard text for the target.
func Payload(r model.Resource, t Target) (string, error) {
	switch t {
	case TargetName:
		return r.Name, nil
	case TargetID:
		return r.ID, nil
	case TargetJSON:
		return r.JSON()
	}
	if c, ok := extra(r, t); ok {
		return c.Text, nil
	}
	return r.Name, nil
}

type copyRow struct {
	target  Target
	title   string
	preview string
}

// CopyModel is the centered copy-target overlay.
type CopyModel struct {
	targets []Target
	rows    []copyRow
	cursor  int
	keys    keys.Map
}

// NewCopy opens the overlay on r's targets.
func NewCopy(r model.Resource, km keys.Map) CopyModel {
	targets := Targets(r)
	return CopyModel{
		targets: targets,
		rows:    buildCopyRows(r, targets),
		keys:    km,
	}
}

func buildCopyRows(r model.Resource, targets []Target) []copyRow {
	rows := make([]copyRow, len(targets))
	for i, t := range targets {
		preview, err := Payload(r, t)
		if err != nil {
			preview = "(error)"
		} else {
			preview = firstLine(preview)
		}
		rows[i] = copyRow{
			target:  t,
			title:   Title(r, t),
			preview: preview,
		}
	}
	return rows
}

func (m CopyModel) Selected() Target {
	if m.cursor < 0 || m.cursor >= len(m.targets) {
		return TargetName
	}
	return m.targets[m.cursor]
}

func (m *CopyModel) Move(delta int) {
	m.cursor += delta
	if m.cursor >= len(m.targets) {
		m.cursor = len(m.targets) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *CopyModel) GoTop()    { m.cursor = 0 }
func (m *CopyModel) GoBottom() { m.cursor = max(0, len(m.targets)-1) }

// View renders a centered overlay listing copy targets.
func (m CopyModel) View(width, height int, r model.Resource) string {
	rows := m.rows
	if len(rows) != len(m.targets) {
		rows = buildCopyRows(r, m.targets)
	}
	innerW := min(56, max(36, width-8))
	// Titles are ASCII; pad them to the widest so previews line up.
	titleW := 0
	for _, row := range rows {
		titleW = max(titleW, len(row.title))
	}
	// The box's width includes its border (2) and padding (4); a row is
	// the cursor (2), the title, and two spaces before the preview.
	previewW := max(8, innerW-6-2-titleW-2)

	var b strings.Builder
	b.WriteString(styles.TitleStyle().Render("Copy"))
	b.WriteString("\n")
	hint := m.keys.Select.Help().Key + " copy   " + m.keys.Back.Help().Key + " cancel"
	b.WriteString(styles.MutedStyle().Render(hint))
	b.WriteString("\n\n")

	for i, row := range rows {
		line := fmt.Sprintf("%-*s  %s", titleW, row.title, text.Truncate(row.preview, previewW))
		cursor := "  "
		if i == m.cursor {
			cursor = styles.Cursor().Render("> ")
			line = styles.SelectedRow().Render(line)
		}
		b.WriteString(cursor)
		b.WriteString(line)
		b.WriteString("\n")
	}

	box := styles.HelpBox().Width(innerW).Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
