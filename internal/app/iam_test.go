package app

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/gcp"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

func TestIAMBindingsByRoleAndPrincipal(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	if _, ok := m.nav.Selected(); !ok {
		t.Fatal("no selection")
	}
	m = expandID(m, demoIAM)
	m = expandNamed(m, "Role Bindings")
	_, coll := rowNamed(t, m, "Role Bindings")
	if !slices.Equal(coll.Choices, []string{"role", "principal"}) || coll.Chosen != 0 {
		t.Fatalf("collection choices %+v chosen %d", coll.Choices, coll.Chosen)
	}

	// By role: a binding opens to its members.
	m = expandNamed(m, "owner")
	i, owner := rowNamed(t, m, "owner")
	if !owner.Expanded {
		t.Fatalf("owner should expand: %+v", owner)
	}
	want := []string{"user:alice@example.com", "user:bob@example.com", "group:platform@example.com"}
	if got := childNames(m.nav.Rows(), i); !slices.Equal(got, want) {
		t.Fatalf("owner members %v, want %v", got, want)
	}
	m.nav.Move(1)
	m.syncDetails()
	member := mustSelected(t, m)
	if !member.Resource.IsPrincipal() || member.Via != owner.Resource.ID {
		t.Fatalf("member row %+v", member)
	}
	if roles := member.Resource.List(model.ListRoles); !slices.Contains(roles, "storage.objectAdmin · Business hours") {
		t.Fatalf("alice's roles %v", roles)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if row := mustSelected(t, m); row.Resource.Name != "owner" || row.Expanded {
		t.Fatalf("Esc on a member should close and select its binding, got %+v", row)
	}

	// v on the collection switches to principals.
	m = jumpTo(m, "Role Bindings")
	m = press(m, keyText("v"))
	_, coll = rowNamed(t, m, "Role Bindings")
	if coll.Chosen != 1 {
		t.Fatalf("chosen %d after v", coll.Chosen)
	}
	if row := mustSelected(t, m); row.Resource.Name != "Role Bindings" {
		t.Fatalf("selection moved to %q", row.Resource.Name)
	}
	m = expandNamed(m, "group:platform@example.com")
	i, _ = rowNamed(t, m, "group:platform@example.com")
	// Only the project's own bindings; inherited ones sit under their
	// ancestor's node (TestInheritedBindingGroups).
	if got := childNames(m.nav.Rows(), i); !slices.Equal(got, []string{"container.developer", "editor", "owner"}) {
		t.Fatalf("platform roles %v", got)
	}
	// The same binding listed under two principals has two row keys.
	keys := map[string]bool{}
	for _, r := range m.nav.Rows() {
		if keys[r.Key()] {
			t.Fatalf("duplicate row key %q", r.Key())
		}
		keys[r.Key()] = true
	}

	// Clicking "role" switches back.
	ci, _ := rowNamed(t, m, "Role Bindings")
	y := m.bodyTop() + m.nav.HeaderRows() + ci - m.nav.Offset()
	x := -1
	for col := 0; col < m.nav.Width(); col++ {
		if c, ok := m.nav.ChoiceAt(ci, col); ok && c == 0 {
			x = col
			break
		}
	}
	if x < 0 {
		t.Fatalf("role choice not drawn:\n%s", m.nav.View())
	}
	next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = next.(Model)
	if _, coll = rowNamed(t, m, "Role Bindings"); coll.Chosen != 0 {
		t.Fatalf("click on role left chosen=%d", coll.Chosen)
	}
}

func TestGoogleServiceAccountsToggle(t *testing.T) {
	t.Parallel()
	const (
		demoSAs     = demoIAM + "/collections/" + string(model.ServiceServiceAccount)
		checkoutIAM = "projects/checkout-project/products/iam"
		checkoutSAs = checkoutIAM + "/collections/" + string(model.ServiceServiceAccount)
	)
	mine := []string{"deployer", "gke-nodes", "legacy-batch"}
	all := append(slices.Clone(mine), "123456789012-compute@developer.gserviceaccount.com", "123456789012@cloudservices.gserviceaccount.com")
	slices.Sort(all)
	checkoutMine := []string{"checkout-api"}
	checkoutAll := []string{"210987654321-compute@developer.gserviceaccount.com", "checkout-api"}
	accounts := func(m Model, id string) (resources.Row, []string) {
		t.Helper()
		for i, r := range m.nav.Rows() {
			if r.Resource.ID == id {
				got := childNames(m.nav.Rows(), i)
				slices.Sort(got)
				return r, got
			}
		}
		t.Fatalf("no row %q:\n%s", id, m.nav.View())
		return resources.Row{}, nil
	}
	open := func(m Model) Model {
		m = expandID(m, demoIAM)
		m = expandID(m, demoSAs)
		m = expandID(m, "projects/checkout-project")
		m = expandID(m, checkoutIAM)
		return expandID(m, checkoutSAs)
	}

	m := open(sized(newTestModel()))
	coll, got := accounts(m, demoSAs)
	if !slices.Equal(coll.Choices, []string{"yours", "all"}) || coll.Chosen != 0 {
		t.Fatalf("choices %v chosen %d", coll.Choices, coll.Chosen)
	}
	if !slices.Equal(got, mine) {
		t.Fatalf("default accounts %v, want %v", got, mine)
	}
	if n := coll.Resource.Fields[0].Value; n != "3" {
		t.Fatalf("collection count %s", n)
	}

	// v switches only the selected collection.
	m = jumpToID(m, demoSAs)
	m = press(m, keyText("v"))
	if coll, got = accounts(m, demoSAs); coll.Chosen != 1 || !slices.Equal(got, all) {
		t.Fatalf("after v: chosen %d, accounts %v", coll.Chosen, got)
	}
	if coll, got = accounts(m, checkoutSAs); coll.Chosen != 0 || !slices.Equal(got, checkoutMine) {
		t.Fatalf("other project after v: chosen %d, accounts %v", coll.Chosen, got)
	}
	m = jumpToID(m, checkoutSAs)
	m = press(m, keyText("v"))
	if coll, got = accounts(m, checkoutSAs); coll.Chosen != 1 || !slices.Equal(got, checkoutAll) {
		t.Fatalf("other project after its v: chosen %d, accounts %v", coll.Chosen, got)
	}
	m = jumpToID(m, demoSAs)
	m = press(m, keyText("v"))
	if coll, got = accounts(m, demoSAs); coll.Chosen != 0 || !slices.Equal(got, mine) {
		t.Fatalf("after second v: chosen %d, accounts %v", coll.Chosen, got)
	}
	if coll, _ = accounts(m, checkoutSAs); coll.Chosen != 1 {
		t.Fatalf("other project lost its choice: chosen %d", coll.Chosen)
	}

	// The config file sets where every collection starts.
	m = load(New(config.Config{Project: "demo-project", IconMode: config.IconText, GoogleServiceAccounts: true}, "test", gcp.FakeCatalog(), testIdent()))
	m = open(sized(m))
	for _, id := range []string{demoSAs, checkoutSAs} {
		if coll, _ = accounts(m, id); coll.Chosen != 1 {
			t.Fatalf("%s configured on: chosen %d", id, coll.Chosen)
		}
	}
	m = jumpToID(m, demoSAs)
	m = press(m, keyText("v"))
	if coll, got = accounts(m, demoSAs); coll.Chosen != 0 || !slices.Equal(got, mine) {
		t.Fatalf("configured on, then v: chosen %d, accounts %v", coll.Chosen, got)
	}
	if coll, _ = accounts(m, checkoutSAs); coll.Chosen != 1 {
		t.Fatalf("other project followed v: chosen %d", coll.Chosen)
	}
}

func TestSearchFindsBindingMembers(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = press(m, keyText("/"))
	for _, r := range "carol" {
		m = press(m, keyText(string(r)))
	}
	view := m.nav.View()
	if !strings.Contains(view, "viewer") || !strings.Contains(view, "user:carol@example.com") {
		t.Fatalf("search should show carol under her binding:\n%s", view)
	}
	if strings.Contains(view, "user:bob@example.com") {
		t.Fatalf("other members should be filtered:\n%s", view)
	}
}

func TestBucketPermissionsListMembers(t *testing.T) {
	t.Parallel()
	m := openBucket(sized(newTestModel()))
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = expandNamed(m, "Permissions")
	m = expandNamed(m, "storage.objectViewer")
	i, row := rowNamed(t, m, "storage.objectViewer")
	if !row.Expanded {
		t.Fatalf("binding should expand: %+v", row)
	}
	if got := childNames(m.nav.Rows(), i); !slices.Equal(got, []string{"allUsers"}) {
		t.Fatalf("members %v", got)
	}
}

// TestIAPAppInheritedAccess opens the Access facet of an app with no
// bindings of its own: its inherited bindings sit under one collapsed node
// per level, nearest first.
func TestIAPAppInheritedAccess(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	for _, name := range []string{"Network Security", "IAP-Secured Apps", "grafana", "Access"} {
		m = expandNamed(m, name)
	}
	access := mustSelected(t, m)
	if access.Kind != resources.RowFacet || !access.Expanded {
		t.Fatalf("Access row %+v", access)
	}
	var names []string
	for _, r := range rowsUnder(m, access.Key()) {
		if !r.Resource.IsInheritedGroup() || r.Expanded {
			t.Fatalf("Access child %+v", r)
		}
		names = append(names, r.Resource.Name)
	}
	want := []string{"From Backend Services (us-central1) (1)", "From All Web Services (1)", "From demo-project (1)", "From example-org (1)"}
	if !slices.Equal(names, want) {
		t.Fatalf("Access children = %q, want %q", names, want)
	}
	m = expandNamed(m, "From demo-project (1)")
	group := mustSelected(t, m)
	kids := rowsUnder(m, group.Key())
	if len(kids) == 0 || kids[0].Resource.Name != "iap.httpsResourceAccessor" {
		t.Fatalf("group children %+v", kids)
	}
}

// TestIAMLeadsScope keeps each CRM node's IAM row directly under it, ahead
// of child folders and projects, Enabled Services, and other products.
func TestIAMLeadsScope(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	rows := m.nav.Rows()
	for i, r := range rows {
		if !r.Resource.IsScope() || !r.Expanded {
			continue
		}
		first := i + 1
		if model.ScopeKind(r.Resource.ID) == model.KindOrg {
			// An organization's IAM leads Cloud Platform, after Workspace.
			first += 2
		}
		if first >= len(rows) || rows[first].Resource.ID != r.Resource.ID+"/products/iam" {
			next := ""
			if first < len(rows) {
				next = rows[first].Resource.Name
			}
			t.Errorf("%s: first child is %q, want IAM", r.Resource.ID, next)
		}
	}
	m = expandID(m, demoIAM)
	var kids []string
	for i, r := range m.nav.Rows() {
		if r.Resource.ID == demoIAM {
			kids = childNames(m.nav.Rows(), i)
		}
	}
	if want := []string{"Role Bindings", "Service Accounts", "Custom Roles"}; !slices.Equal(kids, want) {
		t.Errorf("demo-project IAM holds %v, want %v", kids, want)
	}
}

// TestInheritedBindingGroups keeps a project's own bindings first and puts
// inherited ones under one collapsed node per ancestor, nearest first, in
// both views.
func TestInheritedBindingGroups(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandID(m, demoIAM)
	coll := demoIAM + "/collections/iam-binding"
	m = expandID(m, coll)

	children := func(m Model, id string) []resources.Row {
		rows := m.nav.Rows()
		for i, r := range rows {
			if r.Resource.ID != id || r.Via != "" {
				continue
			}
			var out []resources.Row
			for j := i + 1; j < len(rows) && rows[j].Depth > r.Depth; j++ {
				if rows[j].Depth == r.Depth+1 {
					out = append(out, rows[j])
				}
			}
			return out
		}
		t.Fatalf("no row %s", id)
		return nil
	}
	type group struct {
		name   string
		origin model.ServiceType
	}
	wantGroups := []group{
		{"From prod-apps (1)", model.ServiceFolder},
		{"From prod (1)", model.ServiceFolder},
		{"From platform (1)", model.ServiceFolder},
		{"From example-org (2)", model.ServiceOrganization},
	}
	check := func(m Model, view string) {
		t.Helper()
		var groups []group
		seenGroup := false
		for _, r := range children(m, coll) {
			switch {
			case r.Resource.IsInheritedGroup():
				seenGroup = true
				if r.Expanded {
					t.Errorf("%s: %s starts open", view, r.Resource.Name)
				}
				groups = append(groups, group{r.Resource.Name, r.Resource.Origin})
			case seenGroup:
				t.Errorf("%s: own row %q after the inherited groups", view, r.Resource.Name)
			case r.Resource.Origin != "":
				t.Errorf("%s: inherited %q listed with the project's own", view, r.Resource.Name)
			}
		}
		if !slices.Equal(groups, wantGroups) {
			t.Errorf("%s: groups %v, want %v", view, groups, wantGroups)
		}
	}
	check(m, "by role")

	org := coll + "/inherited/" + url.PathEscape("organizations/example-org")
	m = expandID(m, org)
	var names []string
	for _, r := range children(m, org) {
		names = append(names, r.Resource.Name)
		if r.Resource.Origin != "" {
			t.Errorf("%s: origin %q repeated inside its group", r.Resource.Name, r.Resource.Origin)
		}
	}
	if want := []string{"billing.admin", "resourcemanager.organizationAdmin"}; !slices.Equal(names, want) {
		t.Errorf("org group holds %v, want %v", names, want)
	}
	if row := mustSelected(t, m); row.Resource.ID != org {
		t.Fatalf("selection moved to %s", row.Resource.ID)
	}
	if d := ansi.Strip(m.details.View()); !strings.Contains(d, "Inherited role bindings") || !strings.Contains(d, "organizations/example-org") {
		t.Errorf("group details:\n%s", d)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if row := mustSelected(t, m); row.Resource.ID != org || row.Expanded {
		t.Fatalf("Esc should close the group, got %+v", row)
	}

	// By principal: the same groups, holding that ancestor's principals.
	m = jumpTo(m, "Role Bindings")
	m = press(m, keyText("v"))
	check(m, "by principal")
	m = expandID(m, org)
	names = nil
	for _, r := range children(m, org) {
		if !r.Resource.IsPrincipal() {
			t.Errorf("by principal: %q under the org group is not a principal", r.Resource.Name)
		}
		names = append(names, r.Resource.Name)
	}
	want := []string{"group:finance@example.com", "group:gcp-org-admins@example.com", "group:security@example.com", "serviceAccount:terraform@seed-project.iam.gserviceaccount.com", "user:alice@example.com", "user:dana@example.com"}
	if !slices.Equal(names, want) {
		t.Errorf("org principals %v, want %v", names, want)
	}
}

// TestRolePermissionsUnderPrincipal checks that, by principal, a role
// opens to its permissions grouped by service, that each principal's row
// opens on its own, and that Esc closes from the inside out.
func TestRolePermissionsUnderPrincipal(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandID(m, demoIAM)
	m = expandNamed(m, "Role Bindings")
	m = press(jumpTo(m, "Role Bindings"), keyText("v"))
	m = expandNamed(m, "group:platform@example.com")
	m = expandNamed(m, "user:alice@example.com")

	// The first owner row is platform's. Its details gain the role's
	// title and description once its permissions load.
	m = jumpTo(m, "owner")
	if view := m.details.View(); strings.Contains(view, "Full access") {
		t.Fatalf("owner details before load:\n%s", view)
	}
	m = expandNamed(m, "owner")
	i, owner := rowNamed(t, m, "owner")
	if !owner.Expanded || owner.Via == "" {
		t.Fatalf("owner should expand under its principal: %+v", owner)
	}
	m.syncDetails()
	if view := m.details.View(); !strings.Contains(view, "Full access to most resources") || !strings.Contains(view, "title") {
		t.Fatalf("owner details after load:\n%s", view)
	}
	want := []string{"artifactregistry", "cloudsql", "compute", "container", "dns", "iam", "logging", "pubsub", "resourcemanager", "run", "secretmanager", "storage"}
	if got := childNames(m.nav.Rows(), i); !slices.Equal(got, want) {
		t.Fatalf("owner groups %v, want %v", got, want)
	}
	opened := 0
	for _, r := range m.nav.Rows() {
		if r.Resource.Name == "owner" && r.Expanded {
			opened++
		}
	}
	if opened != 1 {
		t.Fatalf("%d owner rows open; alice's should stay closed", opened)
	}

	// compute is large, so it splits by resource.
	m = expandNamed(m, "compute")
	i, compute := rowNamed(t, m, "compute")
	if got := childNames(m.nav.Rows(), i); len(got) == 0 || got[0] != "compute.addresses" {
		t.Fatalf("compute children %v", got)
	}
	m = jumpTo(m, "compute.addresses")
	if row := mustSelected(t, m); !row.Resource.IsPermissionGroup() || row.Resource.Badge != "2" {
		t.Fatalf("compute.addresses %+v", row)
	}
	keys := map[string]bool{}
	for _, r := range m.nav.Rows() {
		if keys[r.Key()] {
			t.Fatalf("duplicate row key %q", r.Key())
		}
		keys[r.Key()] = true
	}

	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if row := mustSelected(t, m); row.Key() != compute.Key() || row.Expanded {
		t.Fatalf("Esc in a group should close and select it, got %+v", row)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if row := mustSelected(t, m); row.Key() != owner.Key() || row.Expanded {
		t.Fatalf("Esc on a closed group should close and select its role, got %+v", row)
	}
	m = expandNamed(m, "owner")
	m = press(jumpTo(m, "owner"), tea.KeyPressMsg{Code: tea.KeyEscape})
	if row := mustSelected(t, m); row.Key() != owner.Key() || row.Expanded {
		t.Fatalf("Esc on an open role should close it, got %+v", row)
	}
}

func TestCustomRolePermissions(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandID(m, demoIAM)
	m = expandNamed(m, "Custom Roles")
	m = jumpTo(m, "deployer")
	if r := mustSelected(t, m).Resource; r.Service != model.ServiceCustomRole || len(r.Lists) != 0 {
		t.Fatalf("deployer %+v", r)
	}
	m = expandNamed(m, "deployer")
	i, role := rowNamed(t, m, "deployer")
	want := []string{"container.deployments.get", "container.deployments.update", "iam.serviceAccounts.actAs", "run.services.get", "run.services.update"}
	if got := childNames(m.nav.Rows(), i); !role.Expanded || !slices.Equal(got, want) {
		t.Fatalf("deployer permissions %v, want %v", got, want)
	}
	m = jumpTo(m, "run.services.get")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if row := mustSelected(t, m); row.Resource.Name != "deployer" || row.Expanded {
		t.Fatalf("Esc on a permission should close and select its role, got %+v", row)
	}
}

func TestSearchOpensPermissionGroups(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandID(m, demoIAM)
	m = expandNamed(m, "Role Bindings")
	m = press(jumpTo(m, "Role Bindings"), keyText("v"))
	m = expandNamed(m, "group:platform@example.com")
	m = expandNamed(m, "owner")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // closed, but loaded
	m.search.Query = "compute.instances.setmetadata"
	m.rebuildOutline()
	var path []string
	for _, r := range m.nav.Rows() {
		path = append(path, r.Resource.Name)
	}
	n := len(path)
	if n < 4 || path[n-4] != "owner" || path[n-3] != "compute" || path[n-2] != "compute.instances" || path[n-1] != "compute.instances.setMetadata" {
		t.Fatalf("search rows %q", path)
	}
}
