package navigation

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/icons"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
)

func TestExplorerTrianglesAndIndent(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{})
	m.SetSize(40, 20)
	m.SetFocused(true)
	bucket := model.Resource{ID: "projects/p/buckets/my-app-bucket", Name: "my-app-bucket", Service: model.ServiceCloudStorage, Status: model.StatusHealthy}
	m.SetRows([]resources.Row{
		{Kind: resources.RowResource, Resource: bucket, Expandable: true, Expanded: true},
		{Kind: resources.RowFacet, Facet: model.Facet{ID: "objects", Label: "Objects"}, Depth: 1, Expandable: true, Expanded: true},
		{Kind: resources.RowResource, Resource: model.PrefixResource(bucket, "logos/", "logos", ""), Depth: 2, Expandable: true},
		{Kind: resources.RowResource, Resource: model.Resource{Name: "logo.png", Service: model.ServiceCloudStorage, Status: model.StatusHealthy}, Depth: 3},
	})
	view := stripANSI(m.View())
	if strings.Contains(view, "Explorer") {
		t.Fatalf("explorer label should not be drawn:\n%s", view)
	}
	if !strings.Contains(view, "▼") || !strings.Contains(view, "▶") {
		t.Fatalf("missing expand triangles:\n%s", view)
	}
	for _, g := range []string{"├──", "└──"} {
		if strings.Contains(view, g) {
			t.Fatalf("must not draw tree lines %q:\n%s", g, view)
		}
	}
	if !strings.Contains(view, "my-app-bucket") || !strings.Contains(view, "logos") {
		t.Fatalf("missing names:\n%s", view)
	}
	bucketAt, logosAt := -1, -1
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.Contains(line, "my-app-bucket") && bucketAt < 0 {
			bucketAt = i
		}
		if strings.Contains(line, "logos") && logosAt < 0 {
			logosAt = i
		}
	}
	if bucketAt < 0 || logosAt < 0 {
		t.Fatalf("rows:\n%s", view)
	}
	if strings.Index(lines[logosAt], "▶") <= strings.Index(lines[bucketAt], "▼") {
		t.Fatalf("logos should be indented under bucket:\n%s\n%s", lines[bucketAt], lines[logosAt])
	}
	if !strings.Contains(lines[bucketAt], "🪣 my-app-bucket") || !strings.Contains(lines[logosAt], "📂 logos") {
		t.Fatalf("a bucket and a folder in it want different icons:\n%s\n%s", lines[bucketAt], lines[logosAt])
	}
}

func TestHitTriangle(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{})
	m.SetSize(40, 20)
	m.SetRows([]resources.Row{
		{Kind: resources.RowResource, Resource: model.Resource{ID: "a", Name: "bucket"}, Expandable: true},
		{Kind: resources.RowResource, Resource: model.Resource{ID: "b", Name: "other"}},
	})
	y := m.HeaderRows()
	idx, tri, ok := m.Hit(4, y)
	if !ok || idx != 0 || !tri {
		t.Fatalf("triangle hit idx=%d tri=%v ok=%v", idx, tri, ok)
	}
	idx, tri, ok = m.Hit(12, y)
	if !ok || idx != 0 || tri {
		t.Fatalf("name hit idx=%d tri=%v ok=%v", idx, tri, ok)
	}
	idx, tri, ok = m.Hit(12, y+1)
	if !ok || idx != 1 || tri {
		t.Fatalf("second row idx=%d tri=%v ok=%v", idx, tri, ok)
	}
}

func TestSelectKey(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{})
	m.SetRows([]resources.Row{
		{Kind: resources.RowResource, Resource: model.Resource{ID: "a", Name: "a"}},
		{Kind: resources.RowResource, Resource: model.Resource{ID: "b", Name: "b"}},
	})
	m.SelectKey("b")
	r, ok := m.Selected()
	if !ok || r.ID != "b" {
		t.Fatalf("%+v ok=%v", r, ok)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	skip := false
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			skip = true
			continue
		}
		if skip {
			if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
				skip = false
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestProblemMarker(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{})
	m.SetSize(80, 12)
	owner := model.Resource{ID: "projects/p", Name: "demo-project", Service: model.ServiceProject}
	m.SetRows([]resources.Row{
		{Kind: resources.RowResource, Resource: owner, Expandable: true, Problems: true},
	})
	view := stripANSI(m.View())
	if !strings.Contains(view, "demo-project "+problemMark) {
		t.Fatalf("missing problem mark:\n%s", view)
	}
}

func TestNoBrandInPane(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{})
	m.SetSize(40, 12)
	m.SetRows([]resources.Row{{Kind: resources.RowResource, Resource: model.Resource{ID: "p", Name: "demo-project", Service: model.ServiceProject}}})
	lines := strings.Split(stripANSI(m.View()), "\n")
	if strings.Contains(strings.Join(lines, "\n"), "Google Cloud") || !strings.Contains(lines[m.HeaderRows()], "demo-project") {
		t.Fatalf("first row should follow the top border:\n%s", strings.Join(lines, "\n"))
	}
}

func TestLoadingMarker(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		row     resources.Row
		wantMrk bool
	}{
		{"loading scope", resources.Row{Kind: resources.RowResource, Resource: model.Resource{ID: "p", Name: "demo-project"}, Expandable: true, Loading: true}, true},
		{"loading facet", resources.Row{Kind: resources.RowFacet, Resource: model.Resource{ID: "b"}, Facet: model.Facet{ID: "objects", Label: "Objects"}, Loading: true}, true},
		{"idle", resources.Row{Kind: resources.RowResource, Resource: model.Resource{ID: "p", Name: "demo-project"}, Expandable: true}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := New(icons.TextRenderer{})
			m.SetSize(40, 10)
			m.SetRows([]resources.Row{tc.row})
			view := stripANSI(m.View())
			name := tc.row.Resource.Name
			if tc.row.Kind == resources.RowFacet {
				name = tc.row.Facet.Label
			}
			if got := strings.Contains(view, name+" "+loadingMark); got != tc.wantMrk {
				t.Fatalf("marker=%v, want %v:\n%s", got, tc.wantMrk, view)
			}
		})
	}
}

func TestNoteRowIsMutedAndNotSelectable(t *testing.T) {
	t.Parallel()
	bucket := model.Resource{ID: "b", Name: "bucket", Service: model.ServiceCloudStorage}
	objects := model.Facet{ID: "objects", Label: "Objects"}
	rows := []resources.Row{
		{Kind: resources.RowResource, Resource: bucket, Expandable: true, Expanded: true},
		{Kind: resources.RowFacet, Resource: bucket, Facet: objects, Depth: 1, Expandable: true, Expanded: true},
		{Kind: resources.RowResource, Resource: model.Resource{ID: "b/o1", Name: "o1"}, Depth: 2},
		{Kind: resources.RowNote, Resource: bucket, Facet: objects, Depth: 2, Note: "… more not shown (limit 200)"},
		{Kind: resources.RowFacet, Resource: bucket, Facet: model.Facet{ID: "perm", Label: "Permissions"}, Depth: 1, Expandable: true},
	}
	newModel := func() Model {
		m := New(icons.TextRenderer{})
		m.SetSize(60, 20)
		m.SetRows(rows)
		return m
	}

	if view := stripANSI(newModel().View()); !strings.Contains(view, "        … more not shown (limit 200)") {
		t.Fatalf("note row missing or misindented:\n%s", view)
	}

	tests := []struct {
		name string
		act  func(*Model)
		want int
	}{
		{"down skips note", func(m *Model) { m.SelectIndex(2); m.Move(1) }, 4},
		{"up skips note", func(m *Model) { m.SelectIndex(4); m.Move(-1) }, 2},
		{"click on note ignored", func(m *Model) { m.SelectIndex(2); m.SelectIndex(3) }, 2},
		{"select key ignores note", func(m *Model) { m.SelectIndex(0); m.SelectKey(rows[3].Key()) }, 0},
		{"bottom", func(m *Model) { m.GoBottom() }, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newModel()
			tc.act(&m)
			if m.Cursor() != tc.want {
				t.Fatalf("cursor=%d want %d", m.Cursor(), tc.want)
			}
		})
	}

	// A trailing note is never the landing row when the list shrinks.
	m := newModel()
	m.GoBottom()
	m.SetRows(rows[:4])
	if m.Cursor() != 2 {
		t.Fatalf("cursor=%d after shrink, want 2", m.Cursor())
	}
}

func TestChoicesRenderAndHit(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{})
	m.SetSize(60, 20)
	m.SetRows([]resources.Row{
		{Kind: resources.RowResource, Resource: model.Resource{ID: "c", Name: "Role Bindings", Service: model.ServiceIAMBinding}, Expandable: true, Choices: []string{"role", "principal"}, Chosen: 1},
	})
	view := stripANSI(m.View())
	if !strings.Contains(view, "Role Bindings  ○ role ◉ principal") {
		t.Fatalf("choices missing:\n%s", view)
	}
	y := m.HeaderRows()
	line := ""
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "◉") {
			line = l
		}
	}
	for want, label := range []string{"○ role", "◉ principal"} {
		x := strings.Index(line, label)
		x = lipgloss.Width(line[:x])
		if i, tri, ok := m.Hit(x, y); !ok || i != 0 || tri {
			t.Fatalf("hit %d %v %v", i, tri, ok)
		}
		if got, ok := m.ChoiceAt(0, x); !ok || got != want {
			t.Errorf("%s at x=%d: choice %d ok=%v", label, x, got, ok)
		}
	}
	if _, ok := m.ChoiceAt(0, strings.Index(line, "IAM Role")); ok {
		t.Error("the name is not a choice")
	}
}

func TestChoicesDropWhenNarrow(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{})
	m.SetSize(24, 20)
	m.SetRows([]resources.Row{
		{Kind: resources.RowResource, Resource: model.Resource{ID: "c", Name: "Role Bindings", Service: model.ServiceIAMBinding}, Choices: []string{"role", "principal"}},
	})
	if view := stripANSI(m.View()); strings.Contains(view, "○") || strings.Contains(view, "◉") {
		t.Fatalf("choices should not squeeze the name:\n%s", view)
	}
	if _, ok := m.ChoiceAt(0, 20); ok {
		t.Error("hidden choices are not clickable")
	}
}

func TestPrincipalRowsHaveNoIcon(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{})
	m.SetSize(60, 20)
	p := model.PrincipalResource(model.Resource{ID: "c"}, "user:a@x", nil)
	m.SetRows([]resources.Row{{Kind: resources.RowResource, Resource: p}})
	view := stripANSI(m.View())
	if strings.Contains(view, "RB") || !strings.Contains(view, "  user:a@x") {
		t.Fatalf("principal row:\n%s", view)
	}
}

func TestBadgesRightAlignedAndColored(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{})
	m.SetSize(50, 20)
	vuln := func(id, sev string, st model.ResourceStatus) resources.Row {
		return resources.Row{Kind: resources.RowResource, Depth: 1, Resource: model.Resource{
			ID: id, Name: id, Service: model.ServiceVulnerability, Status: st, Badge: sev,
		}}
	}
	m.SetRows([]resources.Row{
		vuln("CVE-2024-45337", "CRITICAL", model.StatusError),
		vuln("CVE-2011-3374", "LOW", model.StatusWarning),
	})
	raw := m.View()
	var ends []int
	for _, line := range strings.Split(stripANSI(raw), "\n") {
		for _, sev := range []string{"CRITICAL", "LOW"} {
			if i := strings.Index(line, sev); i >= 0 {
				if !strings.Contains(line, "CVE-") {
					t.Fatalf("badge on the wrong line: %q", line)
				}
				ends = append(ends, len([]rune(line[:i]))+len(sev))
			}
		}
	}
	if len(ends) != 2 || ends[0] != ends[1] {
		t.Fatalf("badges should end in the same column: %v\n%s", ends, stripANSI(raw))
	}
	for _, st := range []model.ResourceStatus{model.StatusError, model.StatusWarning} {
		badge := map[model.ResourceStatus]string{model.StatusError: "CRITICAL", model.StatusWarning: "LOW"}[st]
		if want := styles.Status(string(st)).Render(badge); !strings.Contains(raw, want) {
			t.Errorf("%s badge not rendered with the %s style", badge, st)
		}
	}

	// Too narrow: the name keeps its room and the badge is dropped.
	m.SetSize(24, 20)
	if view := stripANSI(m.View()); strings.Contains(view, "CRITICAL") {
		t.Fatalf("badge should not squeeze the name:\n%s", view)
	}
}

func TestInheritedRowsShowOrigin(t *testing.T) {
	t.Parallel()
	row := func(name string, origin model.ServiceType) resources.Row {
		return resources.Row{Kind: resources.RowResource, Depth: 1, Resource: model.Resource{
			ID: name + string(origin), Name: name, Service: model.ServiceIAMBinding, Status: model.StatusHealthy, Origin: origin,
		}}
	}
	for _, r := range []icons.Renderer{icons.TextRenderer{}, icons.NoopRenderer{}} {
		m := New(r)
		m.SetSize(50, 20)
		m.SetRows([]resources.Row{
			row("owner", ""),
			row("editor", model.ServiceFolder),
			row("billing.admin", model.ServiceOrganization),
		})
		lines := strings.Split(stripANSI(m.View()), "\n")
		var ends []int
		folder, org := "↑FLD", "↑ORG"
		if r.Name() == "text" {
			folder, org = "↑📁", "↑🏢"
		}
		for _, want := range []struct{ name, mark string }{{"owner", ""}, {"editor", folder}, {"billing.admin", org}} {
			var line string
			for _, l := range lines {
				if strings.Contains(l, " "+want.name) {
					line = l
				}
			}
			if line == "" {
				t.Fatalf("%s: no %s row", r.Name(), want.name)
			}
			if want.mark == "" {
				if strings.Contains(line, inheritMark) {
					t.Errorf("%s: own binding marked inherited: %q", r.Name(), line)
				}
				continue
			}
			i := strings.Index(line, want.mark)
			if i < 0 {
				t.Errorf("%s: %s row lacks %s: %q", r.Name(), want.name, want.mark, line)
				continue
			}
			ends = append(ends, lipgloss.Width(line[:i])+lipgloss.Width(want.mark))
		}
		if len(ends) == 2 && ends[0] != ends[1] {
			t.Errorf("%s: origin marks should end in the same column: %v", r.Name(), ends)
		}
	}
}

func TestTitleInBorder(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{})
	m.SetSize(30, 10)
	if top := strings.Split(stripANSI(m.View()), "\n")[0]; !strings.HasPrefix(top, "╭─ "+Title+" ─") {
		t.Fatalf("top %q", top)
	}
}

// graphicsStub draws every icon as a two-column placeholder, as Kitty does.
type graphicsStub struct{ icons.TextRenderer }

func (graphicsStub) Name() string { return "kitty" }

func (graphicsStub) Cols(icons.Size) int { return 2 }

func (graphicsStub) RenderAt(svc model.ServiceType, _ icons.Size, row int) string {
	if row != 0 {
		return ""
	}
	if svc == icons.Cursor {
		return "@@"
	}
	return "ii"
}

func TestCursor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		r    icons.Renderer
		want string
	}{
		{icons.TextRenderer{}, "> "},
		{icons.NoopRenderer{}, "> "},
		{graphicsStub{}, "@@"},
	} {
		m := New(tc.r)
		m.SetSize(40, 10)
		m.SetRows([]resources.Row{
			{Kind: resources.RowResource, Resource: model.Resource{ID: "a", Name: "first"}},
			{Kind: resources.RowResource, Resource: model.Resource{ID: "b", Name: "second"}},
		})
		lines := strings.Split(stripANSI(m.View()), "\n")
		if got := []rune(lines[1])[2:4]; string(got) != tc.want {
			t.Errorf("%s: cursor %q, want %q: %q", tc.r.Name(), string(got), tc.want, lines[1])
		}
		if got := []rune(lines[2])[2:4]; string(got) != "  " {
			t.Errorf("%s: unselected row %q", tc.r.Name(), lines[2])
		}
	}
}

func TestPinnedRow(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{})
	m.SetSize(40, 10)
	m.SetRows([]resources.Row{
		{Kind: resources.RowResource, Resource: model.Resource{ID: "org", Name: "example-org", Service: model.ServiceOrganization}, Expandable: true, Expanded: true, Pinned: true},
		{Kind: resources.RowResource, Resource: model.Resource{ID: "f", Name: "platform", Service: model.ServiceFolder}, Depth: 1, Expandable: true},
	})
	lines := strings.Split(stripANSI(m.View()), "\n")
	if strings.ContainsAny(lines[1], "▼▶") {
		t.Errorf("pinned row draws a triangle: %q", lines[1])
	}
	// The child's triangle sits right after the cursor column.
	if got := strings.Index(lines[2], "▶"); got != strings.Index(lines[1], ">")+cursorCols {
		t.Errorf("child not shifted left: %q", lines[2])
	}
	y := m.HeaderRows()
	if _, tri, ok := m.Hit(4, y); !ok || tri {
		t.Errorf("pinned row hit tri=%v ok=%v", tri, ok)
	}
	if idx, tri, ok := m.Hit(4, y+1); !ok || idx != 1 || !tri {
		t.Errorf("child triangle hit idx=%d tri=%v ok=%v", idx, tri, ok)
	}
}
