package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/gcp"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

// Projects outside any organization sit under the root's Cloud Platform
// node; an organization stays directly under the root.
func TestRootHoldsLooseProjectsInCloudPlatform(t *testing.T) {
	t.Parallel()
	root := model.HierarchyNode{ID: model.RootID, Name: "GCP", Kind: model.KindRoot}
	for _, n := range []model.HierarchyNode{
		{ID: "organizations/1", Name: "example-org", Kind: model.KindOrg, Children: []model.HierarchyNode{
			{ID: "projects/in-org", Name: "in-org", Kind: model.KindProject, ParentID: "organizations/1"},
		}},
		{ID: "projects/alpha", Name: "alpha", Kind: model.KindProject},
		{ID: "projects/beta", Name: "beta", Kind: model.KindProject},
	} {
		n.ParentID = root.ID
		root.Children = append(root.Children, n)
	}
	cat := gcp.NewCatalog().WithHierarchy(root)
	m := sized(load(New(config.Config{Project: "alpha", IconMode: config.IconText}, "test", cat, testIdent())))

	depth := map[string]int{}
	for _, r := range m.nav.Rows() {
		if r.Kind == resources.RowResource {
			depth[r.Resource.Name] = r.Depth
		}
	}
	plat := model.ServiceCloudPlatform.NodeLabel()
	for name, want := range map[string]int{"GCP": 0, "example-org": 1, plat: 1, "alpha": 2, "beta": 2} {
		if got, ok := depth[name]; !ok || got != want {
			t.Errorf("%s depth = %d (shown %v), want %d", name, got, ok, want)
		}
	}

	// Esc on a collapsed loose project selects the Cloud Platform node.
	m.nav.SelectKey("projects/beta")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if row, _ := m.nav.SelectedRow(); row.Resource.ID != model.ProductGroupID(model.RootID, model.ServiceCloudPlatform) {
		t.Fatalf("selected %q after esc, want the root's Cloud Platform", row.Resource.ID)
	}
}
