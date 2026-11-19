package navigation

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/icons"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
	"github.com/tensleyconsulting/seiner/internal/ui/text"
)

// Title is set into the pane's top border.
const Title = "Resources"

// Model is the left-pane hierarchical explorer.
type Model struct {
	rows     []resources.Row
	cursor   int
	offset   int
	width    int
	height   int
	focused  bool
	renderer icons.Renderer
}

func New(renderer icons.Renderer) Model {
	return Model{renderer: renderer}
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.ensureVisible()
}

func (m *Model) SetFocused(v bool) { m.focused = v }

func (m *Model) SetRows(rows []resources.Row) {
	sel := ""
	if cur, ok := m.SelectedRow(); ok {
		sel = cur.Key()
	}
	m.rows = rows
	if sel != "" {
		for i, r := range m.rows {
			if r.Key() == sel {
				m.cursor = i
				m.ensureVisible()
				return
			}
		}
	}
	if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}
	m.settle(-1)
	m.ensureVisible()
}

// selectable reports whether the cursor may rest on row i. Notes are not.
func (m Model) selectable(i int) bool {
	return i >= 0 && i < len(m.rows) && m.rows[i].Kind != resources.RowNote
}

// settle moves the cursor off a non-selectable row, first in direction dir
// and then the other way.
func (m *Model) settle(dir int) {
	if m.selectable(m.cursor) {
		return
	}
	for _, d := range []int{dir, -dir} {
		for i := m.cursor + d; i >= 0 && i < len(m.rows); i += d {
			if m.selectable(i) {
				m.cursor = i
				return
			}
		}
	}
}

func (m Model) Rows() []resources.Row { return m.rows }

func (m Model) Cursor() int { return m.cursor }

func (m Model) Offset() int { return m.offset }

func (m Model) Width() int { return m.width }

func (m *Model) SelectIndex(i int) {
	if !m.selectable(i) {
		return
	}
	m.cursor = i
	m.ensureVisible()
}

// HeaderRows is the number of terminal rows from the pane top to the first
// explorer item: the top border, which holds the title.
func (m Model) HeaderRows() int { return 1 }

// Hit reports which explorer row contains pane-local (x, y), and whether the
// click landed on the expand triangle.
func (m Model) Hit(x, y int) (index int, triangle bool, ok bool) {
	if m.width <= 0 || m.height <= 0 {
		return 0, false, false
	}
	if x <= 0 || x >= m.width-1 || y <= 0 || y >= m.height-1 {
		return 0, false, false
	}
	first := m.HeaderRows()
	if y < first {
		return 0, false, false
	}
	i := m.offset + (y - first)
	if i < m.offset || i >= len(m.rows) || i >= m.offset+m.pageSize() {
		return 0, false, false
	}
	innerX := x - 2 // left border + padding
	if innerX < 0 {
		return i, false, true
	}
	row := m.rows[i]
	triStart := cursorCols + 2*m.indent(row)
	onTri := row.Expandable && !row.Pinned && innerX >= triStart && innerX < triStart+2
	return i, onTri, true
}

func (m Model) SelectedRow() (resources.Row, bool) {
	if len(m.rows) == 0 {
		return resources.Row{}, false
	}
	return m.rows[m.cursor], true
}

func (m Model) Selected() (model.Resource, bool) {
	row, ok := m.SelectedRow()
	if !ok {
		return model.Resource{}, false
	}
	return row.Resource, true
}

func (m *Model) SelectKey(key string) {
	if key == "" {
		return
	}
	for i, r := range m.rows {
		if r.Key() == key && m.selectable(i) {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
}

// Move steps the cursor by delta rows, clamped to the list. Landing on a
// row the cursor may not rest on continues in the same direction.
func (m *Model) Move(delta int) {
	if len(m.rows) == 0 || delta == 0 {
		return
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.rows)-1)
	m.settle(dir)
	m.ensureVisible()
}

func (m *Model) GoTop() {
	m.cursor = 0
	m.settle(1)
	m.ensureVisible()
}

func (m *Model) GoBottom() {
	if len(m.rows) > 0 {
		m.cursor = len(m.rows) - 1
	}
	m.settle(-1)
	m.ensureVisible()
}

func (m *Model) ensureVisible() {
	page := m.pageSize()
	if page <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+page {
		m.offset = m.cursor - page + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m Model) pageSize() int {
	// less the top and bottom borders
	return max(1, m.height-2)
}

func (m Model) View() string {
	innerW := m.width - 4
	if innerW < 8 {
		innerW = 8
	}

	var b strings.Builder
	if len(m.rows) == 0 {
		b.WriteString(styles.MutedStyle().Render("No resources."))
	} else {
		page := m.pageSize()
		end := min(len(m.rows), m.offset+page)
		for i := m.offset; i < end; i++ {
			b.WriteString(m.renderRow(i, innerW))
			if i != end-1 {
				b.WriteString("\n")
			}
		}
	}

	body := strings.TrimRight(b.String(), "\n")
	return styles.TitledPane(Title, m.focused, m.width, m.height, body)
}

func (m Model) renderRow(i, innerW int) string {
	row := m.rows[i]
	cursor := strings.Repeat(" ", cursorCols)
	if i == m.cursor {
		cursor = m.cursorCell()
	}
	indent := strings.Repeat("  ", m.indent(row))
	glyph := ""
	if !row.Pinned {
		glyph = expandGlyph(row.Expandable, row.Expanded)
	}
	if row.Kind == resources.RowNote {
		avail := max(4, innerW-lipgloss.Width(cursor)-lipgloss.Width(indent)-lipgloss.Width(glyph))
		return cursor + indent + glyph + styles.MutedStyle().Render(text.Truncate(row.Note, avail))
	}

	icon := ""
	if row.Kind == resources.RowResource {
		icon = m.iconCell(row.Resource)
	}
	lay := m.layout(row, innerW)
	name := text.Truncate(rowName(row), lay.nameW)

	selected := i == m.cursor && m.focused
	if selected {
		name = styles.SelectedRow().Render(name)
	} else if row.Kind == resources.RowFacet {
		name = styles.MutedStyle().Render(name)
	}

	var b strings.Builder
	b.WriteString(cursor)
	b.WriteString(indent)
	b.WriteString(glyph)
	if icon != "" {
		b.WriteString(icon)
		b.WriteString(" ")
	}
	b.WriteString(name)
	if row.Loading {
		b.WriteString(" " + styles.MutedStyle().Render(loadingMark))
	}
	if row.Problems {
		b.WriteString(" " + styles.Status("Warning").Render(problemMark))
	}
	if lay.badge {
		// Right-align the badge so badges line up down the list.
		tag, w := m.trailer(row)
		pad := innerW - lipgloss.Width(b.String()) - w
		b.WriteString(strings.Repeat(" ", max(lipgloss.Width(badgeGap), pad)))
		b.WriteString(tag)
	}
	if lay.choices {
		b.WriteString(choiceGap)
		for j, c := range row.Choices {
			if j > 0 {
				b.WriteString(" ")
			}
			b.WriteString(styles.Choice(j == row.Chosen).Render(choiceText(c, j == row.Chosen)))
		}
	}
	return b.String()
}

// rowName is the text a resource or facet row shows after its icon.
func rowName(row resources.Row) string {
	if row.Kind == resources.RowFacet {
		return row.Facet.Label
	}
	return row.Resource.Name
}

// rowLayout is where a resource or facet row's parts sit.
type rowLayout struct {
	prefixW int  // cursor, indent, glyph, and icon with its gap
	nameW   int  // columns for the (truncated) name
	choices bool // whether the choices fit after the name
	badge   bool // whether the resource's badge fits at the end of the row
}

// layout measures row in innerW columns. The name gives up room to the
// loading mark and choices, but keeps at least 4 columns; choices that
// would squeeze it further are not drawn.
func (m Model) layout(row resources.Row, innerW int) rowLayout {
	prefix := cursorCols + 2*m.indent(row)
	if !row.Pinned {
		prefix += 2 // expand triangle
	}
	if row.Kind == resources.RowResource {
		if w := lipgloss.Width(m.iconCell(row.Resource)); w > 0 {
			prefix += w + 1
		}
	}
	avail := innerW - prefix
	if row.Loading {
		avail -= 1 + lipgloss.Width(loadingMark)
	}
	if row.Problems {
		avail -= 1 + lipgloss.Width(problemMark)
	}
	out := rowLayout{prefixW: prefix}
	if cw := choicesWidth(row.Choices); cw > 0 && avail-cw >= 4 {
		avail -= cw
		out.choices = true
	} else if bw := m.badgeWidth(row); bw > 0 && avail-bw >= 4 {
		avail -= bw
		out.badge = true
	}
	name := lipgloss.Width(rowName(row))
	out.nameW = min(name, max(4, avail))
	if !out.choices {
		out.nameW = max(4, avail)
	}
	return out
}

// choiceGap separates a row's name from its choices.
const choiceGap = "  "

// badgeGap is the least space between a row's name and its badge.
const badgeGap = "  "

// badgeWidth is the columns a resource row's trailer takes, gap included;
// 0 when it has none.
func (m Model) badgeWidth(row resources.Row) int {
	if _, w := m.trailer(row); w > 0 {
		return lipgloss.Width(badgeGap) + w
	}
	return 0
}

// inheritMark precedes the origin icon of an inherited row.
const inheritMark = "↑"

// trailer is what a resource row draws at its end, and its width: the
// badge in its status color, or for a row inherited from another node, an
// up arrow and that node's icon (its abbreviation when the renderer draws
// none).
func (m Model) trailer(row resources.Row) (string, int) {
	r := row.Resource
	if row.Kind != resources.RowResource {
		return "", 0
	}
	if r.Badge != "" {
		return styles.Status(string(r.Status)).Render(r.Badge), lipgloss.Width(r.Badge)
	}
	if r.Origin == "" {
		return "", 0
	}
	mark := styles.MutedStyle().Render(inheritMark)
	if m.renderer != nil {
		if s := m.renderer.RenderAt(r.Origin, icons.SizeList, 0); s != "" {
			if !icons.Graphics(m.renderer) {
				s = styles.Icon().Render(s)
			}
			return mark + s, lipgloss.Width(inheritMark) + m.renderer.Cols(icons.SizeList)
		}
	}
	abbrev := r.Origin.Abbrev()
	return mark + styles.Icon().Render(abbrev), lipgloss.Width(inheritMark) + lipgloss.Width(abbrev)
}

func choiceText(label string, chosen bool) string {
	if chosen {
		return "◉ " + label
	}
	return "○ " + label
}

// choicesWidth is the columns choices take after the name, gap included;
// 0 when there are none.
func choicesWidth(choices []string) int {
	if len(choices) == 0 {
		return 0
	}
	w := lipgloss.Width(choiceGap) + len(choices) - 1
	for _, c := range choices {
		w += lipgloss.Width(choiceText(c, false))
	}
	return w
}

// ChoiceAt reports which of row i's choices covers pane-local x, if any.
func (m Model) ChoiceAt(i, x int) (int, bool) {
	if i < 0 || i >= len(m.rows) {
		return 0, false
	}
	row := m.rows[i]
	lay := m.layout(row, max(8, m.width-4))
	if !lay.choices {
		return 0, false
	}
	col := x - 2 // left border + padding
	start := lay.prefixW + lipgloss.Width(text.Truncate(rowName(row), lay.nameW)) + lipgloss.Width(choiceGap)
	if row.Loading {
		start += 1 + lipgloss.Width(loadingMark)
	}
	if row.Problems {
		start += 1 + lipgloss.Width(problemMark)
	}
	for j, c := range row.Choices {
		w := lipgloss.Width(choiceText(c, false))
		if col >= start && col < start+w {
			return j, true
		}
		start += w + 1
	}
	return 0, false
}

// loadingMark follows the name of a row whose load is in flight.
const loadingMark = "…"

// problemMark follows the name of a row whose load reported errors. The
// errors themselves are in the details and messages panes.
const problemMark = "!"

// cursorCols is the width of the cursor column.
const cursorCols = 2

// cursorCell marks the row under the cursor: the cursor icon with graphics,
// "> " without.
func (m Model) cursorCell() string {
	if icons.Graphics(m.renderer) && m.renderer.Cols(icons.SizeList) == cursorCols {
		if s := m.renderer.RenderAt(icons.Cursor, icons.SizeList, 0); s != "" {
			return s
		}
	}
	return styles.Cursor().Render("> ")
}

// indent is how many levels row is indented. Rows under a pinned row sit
// one level left, since the pinned row has no triangle to line up under.
func (m Model) indent(row resources.Row) int {
	if row.Pinned || len(m.rows) == 0 || !m.rows[0].Pinned {
		return row.Depth
	}
	return max(0, row.Depth-1)
}

func expandGlyph(expandable, expanded bool) string {
	if !expandable {
		return "  "
	}
	if expanded {
		return "▼ "
	}
	return "▶ "
}

// iconCell is r's list icon. Principal nodes and URL list entries are plain
// values, not products, so they have none.
func (m Model) iconCell(r model.Resource) string {
	if m.renderer == nil || r.IsPrincipal() || r.IsURLListItem() {
		return ""
	}
	s := m.renderer.RenderAt(icons.For(r), icons.SizeList, 0)
	if icons.Graphics(m.renderer) {
		return s
	}
	cols := m.renderer.Cols(icons.SizeList)
	if s == "" {
		if cols > 0 {
			return strings.Repeat(" ", cols)
		}
		return ""
	}
	return styles.Icon().Width(cols).Render(s)
}
