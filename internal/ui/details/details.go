package details

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/icons"
	"github.com/tensleyconsulting/seiner/internal/ui/keys"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
	"github.com/tensleyconsulting/seiner/internal/ui/text"
)

// Title is set into the pane's top border.
const Title = "Details"

// Model is the right-hand detail pane.
type Model struct {
	resource    *model.Resource
	width       int
	height      int
	focused     bool
	renderer    icons.Renderer
	keys        keys.Map
	facetCursor int
	problems    []model.ProviderError
	denials     []model.Denial
}

func New(renderer icons.Renderer, km keys.Map) Model {
	return Model{renderer: renderer, keys: km}
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) SetFocused(v bool) { m.focused = v }

func (m *Model) SetResource(r *model.Resource) {
	m.resource = r
	if r == nil || m.facetCursor >= len(r.Facets) {
		m.facetCursor = 0
	}
}

// SetProblems sets the provider errors shown under the resource. nil hides
// the section.
func (m *Model) SetProblems(errs []model.ProviderError) { m.problems = errs }

// SetDenials sets the services skipped for missing permissions, shown under
// the resource. nil hides the section.
func (m *Model) SetDenials(d []model.Denial) { m.denials = d }

func (m *Model) MoveFacet(delta int) {
	if m.resource == nil || len(m.resource.Facets) == 0 {
		return
	}
	m.facetCursor += delta
	if m.facetCursor < 0 {
		m.facetCursor = 0
	}
	if m.facetCursor >= len(m.resource.Facets) {
		m.facetCursor = len(m.resource.Facets) - 1
	}
}

func (m *Model) GoFacetTop() {
	m.facetCursor = 0
}

func (m *Model) GoFacetBottom() {
	if m.resource == nil || len(m.resource.Facets) == 0 {
		m.facetCursor = 0
		return
	}
	m.facetCursor = len(m.resource.Facets) - 1
}

func (m Model) SelectedFacet() (model.Facet, bool) {
	if m.resource == nil || m.facetCursor < 0 || m.facetCursor >= len(m.resource.Facets) {
		return model.Facet{}, false
	}
	return m.resource.Facets[m.facetCursor], true
}

func (m Model) View() string {
	innerW := m.width - 4
	if innerW < 8 {
		innerW = 8
	}

	if m.resource == nil {
		return styles.TitledPane(Title, m.focused, m.width, m.height, styles.MutedStyle().Render("Select a resource."))
	}

	r := *m.resource

	// Facets and actions are pinned below the fields so a long resource
	// cannot push them out of the pane.
	var foot strings.Builder
	if len(r.Facets) > 0 {
		writeDivider(&foot, "Browse", innerW)
		for i, f := range r.Facets {
			if m.focused && i == m.facetCursor {
				foot.WriteString(styles.Cursor().Render("> "))
				foot.WriteString(styles.SelectedRow().Render(f.Label))
			} else {
				foot.WriteString("  " + f.Label)
			}
			foot.WriteString("\n")
		}
	}
	foot.WriteString(styles.MutedStyle().Render(strings.Repeat("─", innerW)))
	foot.WriteString("\n")
	actions := []key.Binding{m.keys.Open, m.keys.Copy}
	if len(r.Facets) > 0 {
		actions = append(actions, m.keys.Select)
	}
	writeActions(&foot, actions, innerW)
	room := m.height - 2 - lineCount(foot.String())

	var b strings.Builder
	m.writeHeader(&b, r, innerW)
	if r.Description != "" && r.Description != r.Service.Label() {
		b.WriteString(wrap(r.Description, innerW))
		b.WriteString("\n")
	}
	writeTable(&b, summaryRows(r), innerW)
	writeLists(&b, r.Lists, innerW, room)

	if len(r.Labels) > 0 {
		writeDivider(&b, "Labels", innerW)
		labelKeys := slices.Sorted(maps.Keys(r.Labels))
		rows := make([]row, 0, len(labelKeys))
		for _, k := range labelKeys {
			rows = append(rows, row{key: k, value: r.Labels[k]})
		}
		writeTable(&b, rows, innerW)
	}

	if len(m.problems) > 0 {
		writeProblems(&b, m.problems, innerW)
	}
	if len(m.denials) > 0 {
		writeDenials(&b, m.denials, innerW)
	}

	body := clipLines(strings.TrimRight(b.String(), "\n"), room)
	return styles.TitledPane(Title, m.focused, m.width, m.height, body+"\n"+strings.TrimRight(foot.String(), "\n"))
}

// writeLists writes each non-empty list under a divider with its count,
// one wrapped item per line. clipLines keeps room lines of the body, so
// once a non-empty line lands past them nothing written after it shows:
// the remaining items are not wrapped.
func writeLists(b *strings.Builder, lists []model.List, width, room int) {
	row := strings.Count(b.String(), "\n") // index of the next line
	beyond := func(row int) bool { return room >= 2 && row >= room }
	for _, l := range lists {
		if len(l.Items) == 0 {
			continue
		}
		writeDivider(b, l.Label+" ("+strconv.Itoa(len(l.Items))+")", width)
		if beyond(row) { // a divider is never empty
			return
		}
		row++
		for _, item := range l.Items {
			v := wrap(item, width)
			b.WriteString(v)
			b.WriteString("\n")
			done := false
			for line := range strings.SplitSeq(v, "\n") {
				done = done || (line != "" && beyond(row))
				row++
			}
			if done {
				return
			}
		}
	}
}

// writeHeader writes the hero icon with the name and subtitle beside it.
// The text column takes as many rows as the icon, and at least two.
func (m Model) writeHeader(b *strings.Builder, r model.Resource, width int) {
	cols, rows := 0, 1
	if m.renderer != nil {
		cols, rows = m.renderer.Cols(icons.SizeHero), max(1, m.renderer.Rows(icons.SizeHero))
	}
	gap := 0
	if cols > 0 {
		gap = 1
	}
	textW := max(1, width-cols-gap)
	lines := []string{
		styles.TitleStyle().Render(text.Truncate(r.Name, textW)),
		styles.MutedStyle().Render(text.Truncate(subtitle(r), textW)),
	}
	for i := range max(rows, len(lines)) {
		cell := ""
		if i < rows {
			cell = m.iconCell(icons.For(r), i)
		}
		if cell == "" && cols > 0 {
			cell = strings.Repeat(" ", cols)
		}
		b.WriteString(cell)
		if i < len(lines) {
			b.WriteString(strings.Repeat(" ", gap))
			b.WriteString(lines[i])
		}
		b.WriteString("\n")
	}
}

// subtitle names what r is without repeating its name: the service label
// for real resources, the owning scope for synthetic grouping rows.
func subtitle(r model.Resource) string {
	switch {
	case r.IsCollection(), r.IsProductGroup(), r.IsEnabledGroup():
		return scopeName(r.ScopeID)
	case r.IsPrefix():
		return "Folder · " + r.Service.Label()
	case r.IsPrincipal():
		return "Principal · " + scopeName(r.ScopeID)
	case r.IsInheritedGroup():
		return "Inherited role bindings · " + scopeName(r.ScopeID)
	case r.IsSeverityGroup():
		return "Vulnerabilities by severity"
	case r.IsBucket():
		return r.Service.CollectionLabel() + " · " + scopeName(r.ScopeID)
	case r.IsURLListItem():
		return "URL pattern · " + r.Metadata["urlList"]
	}
	return r.Service.Label()
}

func scopeName(scope string) string {
	if id, ok := model.ProjectID(scope); ok {
		return id
	}
	return scope
}

// row is one key/value line in a details table.
type row struct {
	key, value string
	style      *lipgloss.Style // value style; nil is plain
}

// summaryRows lists the identity rows followed by the resource's fields.
// Synthetic rows (collections, folders) have no real ID, location, or status.
func summaryRows(r model.Resource) []row {
	var rows []row
	add := func(k, v string) {
		if v != "" {
			rows = append(rows, row{key: k, value: v})
		}
	}
	if !r.IsSynthetic() {
		add("ID", r.ID)
		if !r.IsScope() {
			add("Location", r.Location)
		}
		if !r.IsScope() && (known(r.Status) || r.State != "") {
			// Only a provider that reports health sets Status; a bare
			// State (a role's launch stage) shows on its own.
			v := r.State
			if known(r.Status) {
				v = string(r.Status)
				if r.State != "" {
					v += " · " + r.State
				}
			}
			st := styles.Status(string(r.Status))
			rows = append(rows, row{key: "Status", value: v, style: &st})
		}
		if !r.Created.IsZero() {
			add("Created", r.Created.UTC().Format("2006-01-02 15:04 UTC"))
		}
	}
	if r.IsPrefix() {
		add("Prefix", r.Metadata[model.MetaPrefix])
	}
	fields := r.Fields
	if len(fields) == 0 {
		// Synthetic rows (CRM nodes, collections, prefixes) skip Normalize.
		fields = model.FieldsOf(r.Metadata)
	}
	for _, f := range fields {
		add(f.Key, f.Value)
	}
	return rows
}

// known reports whether s is a health bucket a provider set.
func known(s model.ResourceStatus) bool {
	return s != "" && s != model.StatusUnknown
}

// writeTable writes rows as "key  value" with the keys in one muted column
// at most two fifths of width. A longer key pushes only its own value right,
// or onto the next line when too little room is left. Values wrap under
// their first character.
func writeTable(b *strings.Builder, rows []row, width int) {
	if len(rows) == 0 {
		return
	}
	limit := max(4, width*2/5)
	keyW := 0
	for _, r := range rows {
		if w := lipgloss.Width(r.key); w <= limit {
			keyW = max(keyW, w)
		}
	}
	for _, r := range rows {
		k := text.Truncate(r.key, width)
		col := max(keyW, lipgloss.Width(k)) + 2
		b.WriteString(styles.MutedStyle().Render(k))
		if width-col < minValueWidth && col > keyW+2 {
			col = keyW + 2
			b.WriteString("\n" + strings.Repeat(" ", col))
		} else {
			b.WriteString(strings.Repeat(" ", col-lipgloss.Width(k)))
		}
		for i, line := range strings.Split(wrap(r.value, max(1, width-col)), "\n") {
			if i > 0 {
				b.WriteString("\n" + strings.Repeat(" ", col))
			}
			if r.style != nil {
				line = r.style.Render(line)
			}
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

// minValueWidth is the narrowest value column worth keeping beside a long key.
const minValueWidth = 12

// writeActions writes the key hints on as few lines as fit width.
func writeActions(b *strings.Builder, bindings []key.Binding, width int) {
	used := 0
	for _, binding := range bindings {
		h := binding.Help()
		if h.Key == "" {
			continue
		}
		w := lipgloss.Width(h.Key) + 1 + lipgloss.Width(h.Desc)
		if used > 0 {
			if used+3+w > width {
				b.WriteString("\n")
				used = 0
			} else {
				b.WriteString("   ")
				used += 3
			}
		}
		b.WriteString(styles.MutedStyle().Render(h.Key))
		b.WriteString(" " + h.Desc)
		used += w
	}
	b.WriteString("\n")
}

func lineCount(s string) int { return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1 }

// clipLines keeps at most n lines of s, replacing the last kept line with a
// muted … when lines were dropped. n below 2 leaves s for MaxHeight to clip.
func clipLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n < 2 || len(lines) <= n {
		return s
	}
	lines = append(lines[:n-1], styles.MutedStyle().Render("…"))
	return strings.Join(lines, "\n")
}

// writeProblems renders errs under a "Problems" divider: a marked service
// and kind line, then the message.
func writeProblems(b *strings.Builder, errs []model.ProviderError, width int) {
	writeDivider(b, "Problems", width)
	for _, e := range errs {
		head := e.Kind.Label()
		if e.Service != "" {
			head = e.Service.Label() + " · " + head
		}
		b.WriteString(styles.Status("Error").Render("! "))
		b.WriteString(text.Truncate(head, width-2))
		b.WriteString("\n  ")
		b.WriteString(styles.MutedStyle().Render(text.Truncate(e.Summary(), width-2)))
		b.WriteString("\n")
	}
}

// writeDenials renders the services skipped for missing permissions under a
// "No access" divider: the service, then the permissions it needs. They are
// muted, not marked: a permission check, not a failure.
func writeDenials(b *strings.Builder, denials []model.Denial, width int) {
	writeDivider(b, "No access", width)
	for _, d := range denials {
		b.WriteString(text.Truncate(d.Service.CollectionLabel(), width))
		b.WriteString("\n")
		for _, perm := range d.Missing {
			b.WriteString("  ")
			b.WriteString(styles.MutedStyle().Render(text.Truncate("missing "+perm, width-2)))
			b.WriteString("\n")
		}
	}
}

// writeDivider writes a muted "label ───" rule across width.
func writeDivider(b *strings.Builder, label string, width int) {
	b.WriteString(styles.MutedStyle().Render(label + " " + strings.Repeat("─", max(0, width-lipgloss.Width(label)-1))))
	b.WriteString("\n")
}

func (m Model) iconCell(svc model.ServiceType, row int) string {
	if m.renderer == nil {
		return ""
	}
	s := m.renderer.RenderAt(svc, icons.SizeHero, row)
	if icons.Graphics(m.renderer) {
		return s
	}
	if s == "" {
		return ""
	}
	return styles.Icon().Width(m.renderer.Cols(icons.SizeHero)).Render(s)
}

// maxValueLines caps how many lines one wrapped value may take.
const maxValueLines = 4

// wrap soft-wraps s to width w, preferring breaks after spaces, then commas
// and semicolons, then slashes, so IDs and lists split at their separators.
// It ends with … when it needs more than maxValueLines lines.
func wrap(s string, w int) string {
	w = max(1, w)
	var lines []string
	// One line past the cap is all it takes to know the value is cut.
	for para := range strings.SplitSeq(s, "\n") {
		if len(lines) > maxValueLines {
			break
		}
		lines = append(lines, wrapLine(para, w, maxValueLines+1-len(lines))...)
	}
	if len(lines) <= maxValueLines {
		return strings.Join(lines, "\n")
	}
	lines = lines[:maxValueLines]
	lines[maxValueLines-1] = text.Truncate(lines[maxValueLines-1], w-1) + "…"
	if lipgloss.Width(lines[maxValueLines-1]) > w {
		lines[maxValueLines-1] = text.Truncate(lines[maxValueLines-1], w)
	}
	return strings.Join(lines, "\n")
}

// wrapLine wraps one paragraph to w columns, stopping after limit lines.
// It keeps the line's width as it goes, measuring the whole line again
// only where a token could join a grapheme or escape sequence before it.
func wrapLine(s string, w, limit int) []string {
	var lines []string
	var cur strings.Builder
	curW := 0
	flush := func() {
		lines = append(lines, strings.TrimRight(cur.String(), " "))
		cur.Reset()
		curW = 0
	}
	controls := strings.ContainsFunc(s, unicode.IsControl)
	for _, tok := range tokens(s, w) {
		tw := lipgloss.Width(strings.TrimRight(tok, " "))
		if cur.Len() > 0 && curW+tw > w {
			flush()
			if len(lines) == limit {
				return lines
			}
			tok = strings.TrimLeft(tok, " ")
		}
		if cur.Len() > 0 && (controls || !breaksBetween(cur.String(), tok)) {
			cur.WriteString(tok)
			curW = lipgloss.Width(cur.String())
			continue
		}
		cur.WriteString(tok)
		curW += lipgloss.Width(tok)
	}
	flush()
	return lines
}

// breaksBetween reports whether a grapheme always ends between a and b, so
// a+b is as wide as a and b apart: both sides are printable ASCII or Han,
// neither of which extends, prepends to, or joins a neighbor.
func breaksBetween(a, b string) bool {
	last, _ := utf8.DecodeLastRuneInString(a)
	first, _ := utf8.DecodeRuneInString(b)
	return standalone(last) && standalone(first)
}

func standalone(r rune) bool {
	return r >= ' ' && r <= '~' || unicode.Is(unicode.Han, r)
}

// tokens splits s into pieces no wider than w that keep their trailing
// separator. It splits after spaces first, then ",;", then "/", and cuts
// runes only when a piece still does not fit.
func tokens(s string, w int) []string {
	var out []string
	for _, t := range splitAfterAny(s, " ") {
		if lipgloss.Width(strings.TrimRight(t, " ")) <= w {
			out = append(out, t)
			continue
		}
		for _, u := range splitAfterAny(t, ",;") {
			if lipgloss.Width(u) <= w {
				out = append(out, u)
				continue
			}
			for _, v := range splitAfterAny(u, "/") {
				out = append(out, cut(v, w)...)
			}
		}
	}
	return out
}

// splitAfterAny splits s after each rune in seps, keeping the separator.
func splitAfterAny(s, seps string) []string {
	var out []string
	for s != "" {
		i := strings.IndexAny(s, seps)
		if i < 0 {
			return append(out, s)
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// cut hard-splits s into pieces at most w columns wide.
func cut(s string, w int) []string {
	var out []string
	var cur []rune
	curW := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if curW+rw > w && len(cur) > 0 {
			out = append(out, string(cur))
			cur, curW = nil, 0
		}
		cur = append(cur, r)
		curW += rw
	}
	return append(out, string(cur))
}
