package app

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

const (
	wsOrg     = "organizations/example-org"
	wsProduct = wsOrg + "/products/" + string(model.ServiceWorkspace)
)

// namesUnder names the rows directly under the row keyed key.
func namesUnder(m Model, key string) []string {
	rows := m.nav.Rows()
	i := slices.IndexFunc(rows, func(r resources.Row) bool { return r.Key() == key })
	if i < 0 {
		return nil
	}
	var out []string
	for _, r := range rowsUnder(m, key) {
		if r.Depth == rows[i].Depth+1 {
			out = append(out, r.Resource.Name)
		}
	}
	return out
}

// TestWorkspaceUsersInBuckets checks that the demo directory's users list
// by org unit, that the crowded root org unit opens to letter buckets,
// and that Esc walks back out one bucket at a time.
func TestWorkspaceUsersInBuckets(t *testing.T) {
	t.Parallel()
	m := expandID(sized(newTestModel()), wsProduct)
	if got, want := namesUnder(m, wsProduct), []string{"Users", "Groups"}; !slices.Equal(got, want) {
		t.Fatalf("Workspace children %q, want %q", got, want)
	}
	users := model.CollectionID(wsProduct, model.ServiceWorkspaceUser)
	m = expandID(m, users)
	if got, want := namesUnder(m, users), []string{"Contractors", "Engineering", "Finance", "Sales", "/"}; !slices.Equal(got, want) {
		t.Fatalf("org units %q, want %q", got, want)
	}
	var root resources.Row
	for _, r := range rowsUnder(m, users) {
		if !r.Resource.IsBucket() || r.Expanded {
			t.Fatalf("org unit row %+v should be a closed bucket", r)
		}
		if r.Resource.Name == "/" {
			root = r
		}
	}
	if root.Resource.Badge != "55" {
		t.Fatalf("root org unit badge %q, want 55", root.Resource.Badge)
	}

	// Engineering holds its Platform folder, then its own users.
	eng := mustRow(t, m, "Engineering")
	m = expandID(m, eng.Resource.ID)
	if got, want := namesUnder(m, eng.Key()), []string{"Platform", "alice@example.com", "bob@example.com"}; !slices.Equal(got, want) {
		t.Fatalf("Engineering %q, want %q", got, want)
	}

	// The root org unit has 55 users, so it opens to letter buckets.
	m = expandID(m, root.Resource.ID)
	letters := namesUnder(m, root.Key())
	if len(letters) < 2 || letters[0] != "A" {
		t.Fatalf("root org unit buckets %q", letters)
	}
	a := mustRow(t, m, "A")
	m = expandID(m, a.Resource.ID)
	a = mustRow(t, m, "A")
	kids := rowsUnder(m, a.Key())
	if len(kids) != 8 || a.Resource.Badge != "8" || kids[0].Resource.Name != "aaron@example.com" || kids[0].Resource.Service != model.ServiceWorkspaceUser {
		t.Fatalf("bucket A (%s) holds %+v", a.Resource.Badge, kids)
	}

	// Esc on a user closes its bucket; on a closed bucket, what holds it.
	m.nav.SelectKey(kids[0].Key())
	for _, want := range []string{a.Key(), root.Key(), users} {
		m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
		if row := mustSelected(t, m); row.Key() != want || row.Expanded {
			t.Fatalf("after esc selected %q (open %v), want %q closed", row.Key(), row.Expanded, want)
		}
	}
}

// TestWorkspaceMembersInBuckets checks that a small group list stays flat
// and that a group with many members opens to letter buckets of members.
func TestWorkspaceMembersInBuckets(t *testing.T) {
	t.Parallel()
	m := expandID(sized(newTestModel()), wsProduct)
	groups := model.CollectionID(wsProduct, model.ServiceWorkspaceGroup)
	m = expandID(m, groups)
	got := namesUnder(m, groups)
	if len(got) != 7 || got[0] != "all@example.com" {
		t.Fatalf("groups %q", got)
	}
	all := mustRow(t, m, "all@example.com")
	m = expandID(m, all.Resource.ID)
	all = mustRow(t, m, "all@example.com")
	buckets := rowsUnder(m, all.Key())
	if len(buckets) < 2 {
		t.Fatalf("all@ members %+v", buckets)
	}
	for _, b := range buckets {
		if !b.Resource.IsBucket() || b.Resource.Service != model.ServiceWorkspaceMember || b.Depth != all.Depth+1 {
			t.Fatalf("member row %+v should be a member bucket", b)
		}
	}
	// Esc on a closed bucket closes the group's members.
	m.nav.SelectKey(buckets[0].Key())
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if row := mustSelected(t, m); row.Key() != all.Key() || row.Expanded {
		t.Fatalf("after esc selected %+v", row)
	}

	// A small group lists its members flat.
	m = expandID(m, mustRow(t, m, "engineering@example.com").Resource.ID)
	if got := namesUnder(m, mustRow(t, m, "engineering@example.com").Key()); len(got) != 5 || got[0] != "alice@example.com" {
		t.Fatalf("engineering@ members %q", got)
	}
}

func TestSearchOpensBuckets(t *testing.T) {
	t.Parallel()
	m := expandID(sized(newTestModel()), wsProduct)
	m = expandID(m, model.CollectionID(wsProduct, model.ServiceWorkspaceUser))
	m.search.Query = "zoe@"
	m.rebuildOutline()
	var path []string
	for _, r := range m.nav.Rows() {
		path = append(path, r.Resource.Name)
	}
	if n := len(path); n < 3 || path[n-3] != "/" || path[n-2] != "Z" || path[n-1] != "zoe@example.com" {
		t.Fatalf("search rows %q", path)
	}
}

// mustRow is the first explorer row whose resource is named name.
func mustRow(t *testing.T, m Model, name string) resources.Row {
	t.Helper()
	for _, r := range m.nav.Rows() {
		if r.Kind == resources.RowResource && r.Resource.Name == name {
			return r
		}
	}
	t.Fatalf("no row %s", name)
	return resources.Row{}
}
