package fake

import (
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// TestFixtureVulnerabilitiesSortedBySeverity keeps fixtures in the order
// the live adapter returns them: worst first, with the severity as badge.
func TestFixtureVulnerabilitiesSortedBySeverity(t *testing.T) {
	t.Parallel()
	rank := map[string]int{"CRITICAL": 0, "HIGH": 1, "MEDIUM": 2, "LOW": 3, "MINIMAL": 4}
	last := map[string]int{}
	for _, r := range Resources() {
		if r.Service != model.ServiceVulnerability {
			continue
		}
		k, ok := rank[r.State]
		if !ok || r.Badge != r.State {
			t.Errorf("%s: state %q badge %q", r.ID, r.State, r.Badge)
		}
		if prev, seen := last[r.ParentID]; seen && k < prev {
			t.Errorf("%s: %s listed after a less severe finding", r.ID, r.State)
		}
		last[r.ParentID] = k
	}
}

func TestFixtureScopesAreCRMNames(t *testing.T) {
	t.Parallel()
	root := Hierarchy()
	for _, r := range Resources() {
		if model.ScopeKind(r.ScopeID) == "" {
			t.Errorf("%s: ScopeID %q is not a CRM name", r.ID, r.ScopeID)
			continue
		}
		if p, ok := model.ParseName(r.ID); !ok || p.Scope != r.ScopeID {
			t.Errorf("%s: ScopeID %q does not match id scope %q", r.ID, r.ScopeID, p.Scope)
		}
		if _, ok := root.Find(r.ScopeID); !ok {
			t.Errorf("%s: ScopeID %q is not in the fixture hierarchy", r.ID, r.ScopeID)
		}
	}
}

func TestHierarchyIDsAreCRMNames(t *testing.T) {
	t.Parallel()
	var walk func(model.HierarchyNode)
	walk = func(n model.HierarchyNode) {
		if model.ScopeKind(n.ID) != n.Kind {
			t.Errorf("node %q kind %q: ScopeKind=%q", n.ID, n.Kind, model.ScopeKind(n.ID))
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(Hierarchy())
}

// TestFixturesCoverEveryGroupedService guards against fixture drift as
// services are added: every grouped service should be demoable from
// projects/demo-project without credentials.
func TestFixturesCoverEveryGroupedService(t *testing.T) {
	t.Parallel()
	// Access policies, access bindings, and the Workspace directory belong
	// to an organization and never list under a project, so they are
	// demoed from organizations/example-org instead.
	orgOnly := map[model.ServiceType]bool{
		model.ServiceAccessPolicy:    true,
		model.ServiceAccessBinding:   true,
		model.ServiceWorkspaceUser:   true,
		model.ServiceWorkspaceGroup:  true,
		model.ServiceWorkspaceMember: true,
	}
	present := make(map[model.ServiceType]bool)
	inOrg := make(map[model.ServiceType]bool)
	for _, r := range Resources() {
		switch r.ScopeID {
		case "projects/demo-project":
			present[r.Service] = true
		case "organizations/example-org":
			inOrg[r.Service] = true
		}
	}
	for _, spec := range model.Specs() {
		if !spec.Grouped {
			continue
		}
		switch {
		case orgOnly[spec.Type] && present[spec.Type]:
			t.Errorf("%s: org-only service has a fixture under projects/demo-project", spec.Type)
		case orgOnly[spec.Type] && !inOrg[spec.Type]:
			t.Errorf("%s: no fixture resource under organizations/example-org", spec.Type)
		case !orgOnly[spec.Type] && !present[spec.Type]:
			t.Errorf("%s: no fixture resource under projects/demo-project", spec.Type)
		}
	}
}

// TestFixtureFacetsHaveChildren asserts every facet a fixture resource
// declares has at least one fixture child, so Enter on any facet in the
// demo shows something.
func TestFixtureFacetsHaveChildren(t *testing.T) {
	t.Parallel()
	all := Resources()
	for _, r := range all {
		for _, f := range r.Facets {
			if len(model.Children(all, r.ID, f.ID)) == 0 {
				t.Errorf("%s: facet %q has no fixture children", r.ID, f.ID)
			}
		}
	}
}

// TestFixtureResourcesValidateAndAreUnique asserts every fixture resource is
// well-formed and that no two fixtures share an id.
func TestFixtureResourcesValidateAndAreUnique(t *testing.T) {
	t.Parallel()
	// A resource may appear under several parents (a forwarding rule is in
	// Forwarding Rules and in its load balancer's Frontends), but only once
	// under each.
	type key struct{ parent, facet, id string }
	seen := make(map[key]bool)
	for _, r := range Resources() {
		if err := r.Validate(); err != nil {
			t.Errorf("%s: %v", r.ID, err)
		}
		k := key{r.ParentID, r.FacetID, r.ID}
		if seen[k] {
			t.Errorf("duplicate fixture id %q under %q", r.ID, r.ParentID)
		}
		seen[k] = true
	}
}
