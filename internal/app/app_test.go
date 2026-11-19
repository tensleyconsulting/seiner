package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/gcp"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/messages"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

func testIdent() model.Identity {
	return model.Identity{Account: "test@example.com", Source: model.IdentityADC}
}

func newTestModel() Model {
	return load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", gcp.FakeCatalog(), testIdent()))
}

func load(m Model) Model {
	return drain(m, m.Init())
}

func drain(m Model, cmd tea.Cmd) Model {
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return m
		}
		switch msg := msg.(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				m = drain(m, c)
			}
			return m
		default:
			var next tea.Model
			next, cmd = m.Update(msg)
			m = next.(Model)
		}
	}
	return m
}

func jumpTo(m Model, name string) Model {
	m.focus = PaneNav
	m.syncLayout()
	m.nav.GoTop()
	for i := 0; i < 400; i++ {
		row, ok := m.nav.SelectedRow()
		if ok && (row.Resource.Name == name || row.Facet.Label == name) {
			m.syncDetails()
			return m
		}
		m.nav.Move(1)
	}
	m.syncDetails()
	return m
}

func expandNamed(m Model, name string) Model {
	m = jumpTo(m, name)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		m = drain(m, cmd)
	}
	return m
}

// jumpToID is jumpTo for the row whose resource has id.
func jumpToID(m Model, id string) Model {
	m.focus = PaneNav
	m.syncLayout()
	m.nav.GoTop()
	for i := 0; i < 400; i++ {
		if row, ok := m.nav.SelectedRow(); ok && row.Resource.ID == id {
			break
		}
		m.nav.Move(1)
	}
	m.syncDetails()
	return m
}

// expandID is expandNamed for the row whose resource has id, for names
// that repeat across scopes (every CRM node has an IAM node) or across a
// product and its collection (VPC Networks).
func expandID(m Model, id string) Model {
	m = jumpToID(m, id)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		m = drain(m, cmd)
	}
	return m
}

// demoIAM is demo-project's IAM node.
const demoIAM = "projects/demo-project/products/iam"

func openBucket(m Model) Model {
	return jumpTo(expandNamed(m, "Cloud Storage"), "my-app-bucket")
}

func press(m Model, k tea.KeyPressMsg) Model {
	next, _ := m.Update(k)
	return next.(Model)
}

func keyText(s string) tea.KeyPressMsg {
	r := []rune(s)
	return tea.KeyPressMsg{Text: s, Code: r[0]}
}

func TestLayoutMode(t *testing.T) {
	t.Parallel()
	if layoutMode(200) != LayoutLarge {
		t.Fatal("200")
	}
	if layoutMode(100) != LayoutMedium {
		t.Fatal("100")
	}
	if layoutMode(40) != LayoutSmall {
		t.Fatal("40")
	}
}

func TestQuit(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	_, cmd := m.Update(keyText("q"))
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}

	m = newTestModel()
	m.search.Activate()
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c should quit even while searching")
	}
}

func TestHelpToggle(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m = press(m, keyText("?"))
	if !m.showHelp {
		t.Fatal("help should open")
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.showHelp {
		t.Fatal("help should close")
	}
}

// TestHelpOtherKeysCloseSilently pins the help overlay's key handling: any
// key other than ?, esc, q, and ctrl+c closes help and does nothing else.
func TestHelpOtherKeysCloseSilently(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"down", keyText("j")},
		{"search", keyText("/")},
		{"copy", keyText("c")},
		{"refresh", keyText("r")},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}},
		{"tab", tea.KeyPressMsg{Code: tea.KeyTab}},
		{"space", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := sized(newTestModel())
			m = press(m, keyText("?"))
			if !m.showHelp {
				t.Fatal("help should open")
			}
			before := m.nav.View()
			cursor, focus, flash := m.nav.Cursor(), m.focus, m.status.Flash
			next, cmd := m.Update(tt.key)
			m = next.(Model)
			if m.showHelp {
				t.Fatal("help should close")
			}
			if cmd != nil {
				t.Fatal("closing help must not return a command")
			}
			if m.search.Active || m.showCopy || len(m.outline.inflight) != 0 {
				t.Fatalf("key leaked through help: search=%v copy=%v inflight=%d", m.search.Active, m.showCopy, len(m.outline.inflight))
			}
			if m.nav.Cursor() != cursor || m.focus != focus || m.status.Flash != flash || m.nav.View() != before {
				t.Fatal("closing help must not change the explorer, focus, or flash")
			}
		})
	}
}

func TestHelpQClosesWithoutQuitting(t *testing.T) {
	t.Parallel()
	m := press(newTestModel(), keyText("?"))
	next, cmd := m.Update(keyText("q"))
	if next.(Model).showHelp || cmd != nil {
		t.Fatal("q in help should close help, not quit")
	}
	m = press(newTestModel(), keyText("?"))
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("ctrl+c in help should quit")
	}
}

func TestPaneFocus(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.width = 160
	m.height = 40
	m.syncLayout()
	if m.focus != PaneNav {
		t.Fatalf("start focus %s", m.focus)
	}
	m = press(m, keyText("l"))
	if m.focus != PaneDetail {
		t.Fatalf("after l: %s", m.focus)
	}
	m = press(m, keyText("h"))
	if m.focus != PaneNav {
		t.Fatalf("after h: %s", m.focus)
	}
}

func TestExplorerMovesSelection(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.width = 160
	m.height = 40
	m.syncLayout()
	m.focus = PaneNav
	m.syncLayout()
	first, ok := m.nav.Selected()
	if !ok || first.Name != "demo-project" {
		t.Fatalf("first %+v ok=%v", first, ok)
	}
	m.nav.GoTop()
	view := m.nav.View()
	if !strings.Contains(view, "example-org") || !strings.Contains(view, "demo-project") {
		t.Fatalf("crm tree:\n%s", view)
	}
	if !strings.Contains(view, "Cloud Storage") || !strings.Contains(ansi.Strip(view), "🆔 IAM") {
		t.Fatalf("project resources should be grouped:\n%s", view)
	}
	if strings.Contains(view, "my-app-bucket") {
		t.Fatalf("buckets should stay collapsed under Cloud Storage:\n%s", view)
	}
}

func TestSearch(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.width = 160
	m.height = 40
	m.syncLayout()
	m = press(m, keyText("/"))
	if !m.search.Active {
		t.Fatal("search should activate")
	}
	for _, r := range []rune("postgres") {
		m = press(m, keyText(string(r)))
	}
	if m.search.Query != "postgres" {
		t.Fatalf("query %q", m.search.Query)
	}
	view := m.nav.View()
	if !strings.Contains(view, "postgres-prod") {
		t.Fatalf("search missed postgres:\n%s", view)
	}
	if strings.Contains(view, "my-app-bucket") {
		t.Fatalf("search should hide non-matches:\n%s", view)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.search.Query != "" || m.search.Active {
		t.Fatal("esc should clear search")
	}
	if !strings.Contains(m.nav.View(), "Cloud Storage") {
		t.Fatal("cleared search should restore the tree")
	}
}

func TestSearchPaste(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.width = 160
	m.height = 40
	m.syncLayout()
	m = press(m, keyText("/"))
	next, _ := m.Update(tea.PasteMsg{Content: "postgres\n"})
	m = next.(Model)
	if m.search.Query != "postgres " {
		t.Fatalf("query %q", m.search.Query)
	}
	if view := m.nav.View(); !strings.Contains(view, "postgres-prod") || strings.Contains(view, "my-app-bucket") {
		t.Fatalf("paste should filter the explorer:\n%s", view)
	}
}

func TestSearchOpensLoadedCollapsedScope(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandNamed(m, "checkout-project") // load it
	if !m.outline.complete[scopeKey("projects/checkout-project")] {
		t.Fatal("checkout-project should be loaded")
	}
	m = expandNamed(m, "checkout-project") // collapse it
	if strings.Contains(m.nav.View(), "checkout-assets") {
		t.Fatalf("collapsed project should hide its bucket:\n%s", m.nav.View())
	}

	m = press(m, keyText("/"))
	for _, r := range "checkout-assets" {
		m = press(m, keyText(string(r)))
	}
	if !strings.Contains(m.nav.View(), "checkout-assets") {
		t.Fatalf("search should find a bucket in a loaded, collapsed project:\n%s", m.nav.View())
	}

	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(m.nav.View(), "checkout-assets") {
		t.Fatalf("clearing search should restore the collapsed project:\n%s", m.nav.View())
	}
}

func TestSearchDoesNotLoadScopes(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	key := scopeKey("projects/playground-project")
	if m.outline.loaded(key) {
		t.Fatal("playground-project should not be loaded at startup")
	}
	m = press(m, keyText("/"))
	for _, r := range "toy-vm" {
		next, cmd := m.Update(keyText(string(r)))
		m = next.(Model)
		if cmd != nil {
			t.Fatalf("typing %q returned a command", r)
		}
	}
	if m.outline.loaded(key) || m.outline.loading(key) {
		t.Fatal("search must not load a scope")
	}
	if strings.Contains(m.nav.View(), "toy-vm") {
		t.Fatalf("an unloaded project has nothing to match:\n%s", m.nav.View())
	}
}

func TestViewContainsResourceName(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.width = 160
	m.height = 40
	m.syncLayout()
	s := m.render()
	if !strings.Contains(s, "Cloud Storage") {
		t.Fatalf("missing collection in view")
	}
	if !strings.Contains(s, "example-org") {
		t.Fatal("organization should appear in the explorer")
	}
	if !strings.Contains(s, "Project") {
		t.Fatal("selected project should show in details")
	}
}

func TestResizeDoesNotPanic(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	for _, w := range []int{40, 90, 160} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
		m = next.(Model)
		_ = m.render()
	}
}

func sized(m Model) Model {
	m.width = 160
	m.height = 40
	m.syncLayout()
	return m
}

func clipboardText(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected clipboard cmd")
	}
	return fmt.Sprint(cmd())
}

func TestCopyName(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = openBucket(m)
	m = press(m, keyText("c"))
	if !m.showCopy {
		t.Fatal("c should open copy overlay")
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.showCopy {
		t.Fatal("enter should close overlay")
	}
	if got := clipboardText(t, cmd); got != "my-app-bucket" {
		t.Fatalf("clipboard %q", got)
	}
	if !strings.Contains(m.status.Flash, "copied name") {
		t.Fatalf("flash=%q", m.status.Flash)
	}
}

func TestCopyJSONOmitsRaw(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = openBucket(m)
	m = press(m, keyText("c"))
	m = press(m, keyText("G"))
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	got := clipboardText(t, cmd)
	if !strings.Contains(got, `"id"`) || !strings.Contains(got, "my-app-bucket") {
		t.Fatalf("json %s", got)
	}
	if strings.Contains(got, `"raw"`) {
		t.Fatal("raw leaked")
	}
	if !strings.Contains(m.status.Flash, "copied json") {
		t.Fatalf("flash=%q", m.status.Flash)
	}
}

func TestCopyEscCancels(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = press(m, keyText("c"))
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.showCopy {
		t.Fatal("esc should close overlay")
	}
}

func TestOpenConsoleCopiesURL(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = openBucket(m)
	next, cmd := m.Update(keyText("o"))
	m = next.(Model)
	got := clipboardText(t, cmd)
	if !strings.Contains(got, "console.cloud.google.com/storage/browser/my-app-bucket") {
		t.Fatalf("url %q", got)
	}
	if !strings.Contains(got, "project=demo-project") {
		t.Fatalf("missing project: %q", got)
	}
	if !strings.Contains(m.status.Flash, "copied console URL") {
		t.Fatalf("flash=%q", m.status.Flash)
	}
}

func TestDetailsKeyAndEsc(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.width = 90
	m.height = 24
	m.syncLayout()
	if layoutMode(m.width) != LayoutMedium {
		t.Fatal("want medium")
	}
	m = press(m, keyText("d"))
	if m.focus != PaneDetail {
		t.Fatalf("focus %s", m.focus)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.focus != PaneNav {
		t.Fatalf("esc should return to explorer, got %s", m.focus)
	}
}

func TestSearchSwallowsCopy(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = press(m, keyText("/"))
	m = press(m, keyText("c"))
	if m.showCopy {
		t.Fatal("c while searching must not open copy")
	}
	if m.search.Query != "c" {
		t.Fatalf("query %q", m.search.Query)
	}
}

func TestDataSourceLiveInStatus(t *testing.T) {
	t.Parallel()
	m := load(New(config.Config{Project: "demo-project", IconMode: config.IconText, DataSource: "live"}, "test", gcp.FakeCatalog(), testIdent()))
	if m.status.Catalog != "live" {
		t.Fatalf("catalog %q", m.status.Catalog)
	}
}

func TestADCProjectDoesNotSwitchFixtureScope(t *testing.T) {
	t.Parallel()
	ident := model.Identity{
		Account:    "sa@real-prod.iam.gserviceaccount.com",
		ADCProject: "real-prod",
		Source:     model.IdentityADC,
	}
	m := load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", gcp.FakeCatalog(), ident))
	m = sized(m)
	if m.scope.ID != "projects/demo-project" {
		t.Fatalf("scope %s", m.scope.ID)
	}
	if m.status.Project != "demo-project" {
		t.Fatalf("status project %q want the short name", m.status.Project)
	}
	view := m.render()
	if !strings.Contains(view, "sa@real-prod.iam.gserviceaccount.com") {
		t.Fatalf("missing account:\n%s", view)
	}
	if strings.Contains(view, "auth:") {
		t.Fatalf("unexpected banner:\n%s", view)
	}
}

func TestStartOnInitialResource(t *testing.T) {
	t.Parallel()
	cases := []struct {
		start, name string
	}{
		{"organizations/example-org", "example-org"},
		{"folders/prod-apps", "prod-apps"},
		{"projects/checkout-project", "checkout-project"},
	}
	for _, tc := range cases {
		cfg := config.Config{Project: "demo-project", InitialResource: tc.start, IconMode: config.IconText}
		m := sized(load(New(cfg, "test", gcp.FakeCatalog(), testIdent())))
		if m.scope.ID != tc.start || m.status.Project != tc.name {
			t.Fatalf("%s: scope %s, status %q", tc.start, m.scope.ID, m.status.Project)
		}
		if row := mustSelected(t, m); row.Resource.ID != tc.start {
			t.Fatalf("%s: selected %s", tc.start, row.Resource.ID)
		}
		if !m.outline.complete[scopeKey(tc.start)] {
			t.Fatalf("%s should be loaded", tc.start)
		}
		if m.outline.loaded(scopeKey("projects/demo-project")) {
			t.Fatalf("%s: the default project should not load", tc.start)
		}
	}

	// A resource the tree does not have falls back to its first project.
	cfg := config.Config{Project: "demo-project", InitialResource: "folders/999", IconMode: config.IconText}
	m := sized(load(New(cfg, "test", gcp.FakeCatalog(), testIdent())))
	if m.scope.Kind != model.KindProject {
		t.Fatalf("fallback scope %+v", m.scope)
	}
}

func TestDetailsShowsActions(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = openBucket(m)
	view := m.details.View()
	if !strings.Contains(view, "open console") {
		t.Fatalf("details should list open console:\n%s", view)
	}
	if !strings.Contains(view, "copy") {
		t.Fatalf("details should list copy:\n%s", view)
	}
	if !strings.Contains(view, "Permissions") || !strings.Contains(view, "Objects") {
		t.Fatalf("details should list facets:\n%s", view)
	}
}

func TestMouseSelectsExplorerRow(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m.nav.GoTop()
	y := m.bodyTop() + m.nav.HeaderRows()
	next, _ := m.Update(tea.MouseClickMsg{X: 10, Y: y, Button: tea.MouseLeft})
	m = next.(Model)
	got, ok := m.nav.Selected()
	if !ok || got.Name != "example-org" {
		t.Fatalf("click selected %+v ok=%v", got, ok)
	}
	if !strings.Contains(m.details.View(), "Organization") {
		t.Fatal("details should follow explorer selection")
	}
}

func TestMouseTriangleToggles(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = openBucket(m)
	row, _ := m.nav.SelectedRow()
	y := m.bodyTop() + m.nav.HeaderRows() + (m.nav.Cursor() - m.nav.Offset())
	// Rows under the pinned organization sit one level left.
	x := 4 + 2*(row.Depth-1)
	next, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("expanding facets is local")
	}
	view := m.nav.View()
	if !strings.Contains(view, "Permissions") || !strings.Contains(view, "Objects") {
		t.Fatalf("triangle click should expand:\n%s", view)
	}
}

func TestDragSplit(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m.syncLayout()
	start := m.nav.Width()
	next, _ := m.Update(tea.MouseClickMsg{X: start - 1, Y: m.bodyTop() + 2, Button: tea.MouseLeft})
	m = next.(Model)
	if m.drag != dragSplit {
		t.Fatal("click on split should start drag")
	}
	next, _ = m.Update(tea.MouseMotionMsg{X: 70, Y: m.bodyTop() + 2, Button: tea.MouseLeft})
	m = next.(Model)
	if m.split != 70 {
		t.Fatalf("split %d", m.split)
	}
	next, _ = m.Update(tea.MouseReleaseMsg{X: 70, Y: m.bodyTop() + 2, Button: tea.MouseLeft})
	m = next.(Model)
	if m.drag != dragNone {
		t.Fatal("release should end drag")
	}
	if m.nav.Width() != 70 {
		t.Fatalf("nav width %d want 70", m.nav.Width())
	}
}

func TestDragMessagesDivider(t *testing.T) {
	t.Parallel()
	m := showMessages(sized(newTestModel()))
	sizes := m.paneLayout()
	if sizes.msgH != defaultMsgHeight || sizes.detailH+sizes.msgH != sizes.bodyH {
		t.Fatalf("default split %+v", sizes)
	}
	x := m.nav.Width() + 5
	edge := m.bodyTop() + sizes.detailH
	next, _ := m.Update(tea.MouseClickMsg{X: x, Y: edge, Button: tea.MouseLeft})
	m = next.(Model)
	if m.drag != dragMessages {
		t.Fatal("click on the messages border should start a drag")
	}
	next, _ = m.Update(tea.MouseMotionMsg{X: x, Y: edge - 10, Button: tea.MouseLeft})
	m = next.(Model)
	next, _ = m.Update(tea.MouseReleaseMsg{X: x, Y: edge - 10, Button: tea.MouseLeft})
	m = next.(Model)
	if m.drag != dragNone {
		t.Fatal("release should end drag")
	}
	if got := m.paneLayout().msgH; got != defaultMsgHeight+10 {
		t.Fatalf("messages height %d want %d", got, defaultMsgHeight+10)
	}
	if h := lipgloss.Height(m.render()); h != m.height {
		t.Fatalf("render height %d want %d", h, m.height)
	}

	// Dragging past either end clamps.
	m.drag = dragMessages
	m = m.dragTo(x, 0)
	if got := m.paneLayout(); got.detailH != minDetailHeight {
		t.Fatalf("drag to top: %+v", got)
	}
	m = m.dragTo(x, m.height)
	if got := m.paneLayout().msgH; got != minMsgHeight {
		t.Fatalf("drag to bottom: msgH %d", got)
	}
}

// showMessages turns the messages pane on, as the m key does.
func showMessages(m Model) Model {
	m.showMessages = true
	m.syncLayout()
	return m
}

func TestMessagesPaneFocus(t *testing.T) {
	t.Parallel()
	m := showMessages(sized(newTestModel()))
	m = press(m, tea.KeyPressMsg{Text: "l", Code: 'l'})
	m = press(m, tea.KeyPressMsg{Text: "l", Code: 'l'})
	if m.focus != PaneMessages {
		t.Fatalf("focus %v", m.focus)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.focus != PaneNav {
		t.Fatalf("esc focus %v", m.focus)
	}
	sizes := m.paneLayout()
	next, _ := m.Update(tea.MouseClickMsg{X: m.nav.Width() + 5, Y: m.bodyTop() + sizes.detailH + 2, Button: tea.MouseLeft})
	if m = next.(Model); m.focus != PaneMessages {
		t.Fatalf("click focus %v", m.focus)
	}

	// A body too short for messages hides the pane and moves focus off it.
	next, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 10})
	if m = next.(Model); m.paneLayout().msgH != 0 || m.focus != PaneDetail {
		t.Fatalf("short body: %+v focus %v", m.paneLayout(), m.focus)
	}
}

func TestAllocateTwoPanes(t *testing.T) {
	t.Parallel()
	s := allocatePanes(160, 40, 0, 0, true, LayoutLarge, false, 0)
	if s.navW+s.detailW != 160 {
		t.Fatalf("%+v", s)
	}
	if s.navW < minNavWidth || s.detailW < minDetailWidth {
		t.Fatalf("%+v", s)
	}
	s = allocatePanes(160, 40, 80, 0, true, LayoutLarge, false, 0)
	if s.navW != 80 {
		t.Fatalf("split %d", s.navW)
	}
}

func mustSelected(t *testing.T, m Model) resources.Row {
	t.Helper()
	row, ok := m.nav.SelectedRow()
	if !ok {
		t.Fatal("no selection")
	}
	return row
}

// childNames lists the names of the rows directly under row i.
func childNames(rows []resources.Row, i int) []string {
	var out []string
	for j := i + 1; j < len(rows) && rows[j].Depth > rows[i].Depth; j++ {
		if rows[j].Depth == rows[i].Depth+1 {
			out = append(out, rows[j].Resource.Name)
		}
	}
	return out
}

func rowNamed(t *testing.T, m Model, name string) (int, resources.Row) {
	t.Helper()
	for i, r := range m.nav.Rows() {
		if r.Resource.Name == name {
			return i, r
		}
	}
	t.Fatalf("no row %q:\n%s", name, m.nav.View())
	return 0, resources.Row{}
}

// rowsUnder returns the rows nested under the row keyed key, stopping at the
// first row that is not deeper.
func rowsUnder(m Model, key string) []resources.Row {
	rows := m.nav.Rows()
	for i, r := range rows {
		if r.Key() != key {
			continue
		}
		var out []resources.Row
		for _, c := range rows[i+1:] {
			if c.Depth <= r.Depth {
				break
			}
			out = append(out, c)
		}
		return out
	}
	return nil
}

func TestHeaderIDFollowsSelection(t *testing.T) {
	t.Parallel()
	const bucket = "projects/demo-project/buckets/my-app-bucket"
	m := sized(newTestModel())
	m = jumpTo(expandNamed(m, "Cloud Storage"), "Cloud Storage")
	coll, _ := m.nav.Selected()
	if !coll.IsCollection() || coll.ParentID == "" {
		t.Fatalf("expected the Cloud Storage collection, got %+v", coll)
	}
	if got := m.selectedID(); got != coll.ParentID {
		t.Errorf("collection: ID line %q, want its owner %q", got, coll.ParentID)
	}

	m = expandNamed(m, "my-app-bucket")
	if got := m.selectedID(); got != bucket {
		t.Errorf("bucket: ID line %q, want %q", got, bucket)
	}
	header := strings.Split(ansi.Strip(m.render()), "\n")[2]
	if !strings.Contains(header, "ID "+bucket) {
		t.Errorf("third header line should show the ID: %q", header)
	}

	for _, name := range []string{"Objects", "logos"} {
		m = expandNamed(m, name)
		row, _ := m.nav.SelectedRow()
		if row.Kind != resources.RowFacet && !row.Resource.IsPrefix() {
			t.Fatalf("%s: cursor on %+v", name, row)
		}
		if got := m.selectedID(); got != bucket {
			t.Errorf("%s: ID line %q, want the bucket %q", name, got, bucket)
		}
	}
	m = jumpTo(m, "logo.png")
	obj, _ := m.nav.Selected()
	if obj.Name != "logo.png" || m.selectedID() != obj.ID {
		t.Errorf("object: ID line %q, want %q", m.selectedID(), obj.ID)
	}
}

// TestMessagesPaneToggle checks that the messages pane starts hidden, that
// m toggles it, and that an error turns it on.
func TestMessagesPaneToggle(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	if m.paneLayout().msgH != 0 {
		t.Fatal("messages pane should start hidden")
	}
	m = press(m, keyText("m"))
	if m.paneLayout().msgH == 0 {
		t.Fatal("m should show the messages pane")
	}
	m = press(m, keyText("m"))
	if m.paneLayout().msgH != 0 {
		t.Fatal("m should hide the messages pane again")
	}
	m.logf(messages.Info, "fyi")
	if m.paneLayout().msgH != 0 {
		t.Fatal("an info message should not show the pane")
	}
	m.logf(messages.Error, "boom")
	if m.paneLayout().msgH == 0 {
		t.Fatal("an error should show the pane")
	}
}
