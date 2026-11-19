package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/gcp"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// crumbBySearch is crumb as first written: it searches the tree for the
// scope and then again for its path. The walk tests hold crumb to it.
func crumbBySearch(m Model) string {
	id := m.scope.ID
	if r, ok := m.nav.Selected(); ok {
		if r.IsScope() {
			id = r.ID
		} else if r.ScopeID != "" {
			id = r.ScopeID
		}
	}
	if m.tree.ID == "" {
		return id
	}
	if n, ok := m.tree.FindScope(id); ok {
		if path := m.tree.PathTo(n.ID); len(path) > 0 {
			return model.Breadcrumb(path)
		}
	}
	if path := m.tree.PathTo(id); len(path) > 0 {
		return model.Breadcrumb(path)
	}
	if m.scope.Name != "" {
		return m.scope.Name
	}
	return id
}

// openFacets is the demo outline with a bucket's facets showing.
func openFacets() Model {
	m := openBucket(sized(newTestModel()))
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	return drain(m, cmd)
}

func TestCrumb(t *testing.T) {
	t.Parallel()
	tests := []struct {
		row  string
		want string
	}{
		{"example-org", "example-org"},
		{"platform", "example-org / platform"},
		{"prod", "example-org / platform / prod"},
		{"demo-project", "example-org / platform / prod / prod-apps / demo-project"},
		{"Cloud Storage", "example-org / platform / prod / prod-apps / demo-project"},
		{"my-app-bucket", "example-org / platform / prod / prod-apps / demo-project"},
		{"Permissions", "example-org / platform / prod / prod-apps / demo-project"},
		{"Objects", "example-org / platform / prod / prod-apps / demo-project"},
	}
	base := openFacets()
	for _, tc := range tests {
		t.Run(tc.row, func(t *testing.T) {
			m := jumpTo(base, tc.row)
			if row, ok := m.nav.SelectedRow(); !ok || (row.Resource.Name != tc.row && row.Facet.Label != tc.row) {
				t.Fatalf("no row %q", tc.row)
			}
			if got := m.crumb(); got != tc.want {
				t.Fatalf("crumb %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCrumbEveryRow walks the whole outline, moving the cursor the way a
// key press does, and checks every row against a fresh search.
func TestCrumbEveryRow(t *testing.T) {
	t.Parallel()
	m := openFacets()
	m.focus = PaneNav
	m.syncLayout()
	m = press(m, keyText("g"))
	for range len(m.nav.Rows()) {
		if got, want := m.crumb(), crumbBySearch(m); got != want {
			row, _ := m.nav.SelectedRow()
			t.Fatalf("row %q: crumb %q, want %q", row.Resource.Name+row.Facet.Label, got, want)
		}
		if !m.crumbs.ok || m.crumbs.id != m.crumbScope() {
			t.Fatalf("crumb not kept for %q: %+v", m.crumbScope(), m.crumbs)
		}
		m = press(m, keyText("j"))
	}
}

func TestCrumbWithoutHierarchy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{"no project", config.Config{IconMode: config.IconText}, ""},
		{"project before the tree loads", config.Config{Project: "demo-project", IconMode: config.IconText}, "demo-project"},
		{"folder before the tree loads", config.Config{InitialResource: "folders/42", IconMode: config.IconText}, "42"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := sized(New(tc.cfg, "test", gcp.FakeCatalog(), testIdent()))
			if got := m.crumb(); got != tc.want {
				t.Fatalf("crumb %q, want %q", got, tc.want)
			}
			if got := crumbBySearch(m); got != tc.want {
				t.Fatalf("reference crumb %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCrumbFollowsHierarchy checks the kept breadcrumb is rebuilt when the
// real tree replaces the start scope, though the selected scope is the same.
func TestCrumbFollowsHierarchy(t *testing.T) {
	t.Parallel()
	m := sized(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", gcp.FakeCatalog(), testIdent()))
	if got := m.crumb(); got != "demo-project" {
		t.Fatalf("before the tree: %q", got)
	}
	m = load(m)
	m = jumpTo(m, "demo-project")
	if got, want := m.crumb(), "example-org / platform / prod / prod-apps / demo-project"; got != want {
		t.Fatalf("after the tree: %q, want %q", got, want)
	}
}
