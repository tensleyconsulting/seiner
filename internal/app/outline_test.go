package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/gcp"
	"github.com/tensleyconsulting/seiner/internal/gcp/fake"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

func TestCRMNodesInExplorer(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m.nav.GoTop()
	view := m.nav.View()
	for _, name := range []string{"example-org", "platform", "prod", "prod-apps", "demo-project"} {
		if !strings.Contains(view, name) {
			t.Fatalf("missing %s:\n%s", name, view)
		}
	}
	m = jumpTo(m, "example-org")
	r, ok := m.nav.Selected()
	if !ok || !r.IsScope() || r.Service != model.ServiceOrganization {
		t.Fatalf("org %+v ok=%v", r, ok)
	}
	if !strings.Contains(m.details.View(), "Organization") {
		t.Fatalf("org details:\n%s", m.details.View())
	}
	if !strings.Contains(m.render(), "example-org") {
		t.Fatal("breadcrumb should include org")
	}
}

func TestOrganizationStaysExpanded(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = jumpTo(m, "example-org")
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: tea.KeyEscape}} {
		m = press(m, k)
		if row, _ := m.nav.SelectedRow(); !row.Pinned || !row.Expanded {
			t.Fatalf("%s: org row %+v", k, row)
		}
		if !strings.Contains(m.nav.View(), "platform") {
			t.Fatalf("%s collapsed the organization:\n%s", k, m.nav.View())
		}
	}
	// Esc on a collapsed child selects Cloud Platform without closing it.
	m = jumpTo(m, "platform")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if r, _ := m.nav.Selected(); r.Name != "Cloud Platform" || !strings.Contains(m.nav.View(), "platform") {
		t.Fatalf("selected %q:\n%s", r.Name, m.nav.View())
	}
}

func TestOutlineExpandsInPlace(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = openBucket(m)
	m.focus = PaneNav
	m.syncLayout()
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("expanding facets should not replace the explorer")
	}
	view := m.nav.View()
	if !strings.Contains(view, "Permissions") || !strings.Contains(view, "Objects") {
		t.Fatalf("facets:\n%s", view)
	}
	if !strings.Contains(view, "my-app-bucket") || !strings.Contains(view, "audit-logs") {
		t.Fatalf("siblings must stay:\n%s", view)
	}
	if strings.Contains(view, "├──") || strings.Contains(view, "└──") {
		t.Fatalf("must not draw tree lines:\n%s", view)
	}
	m = press(m, keyText("j"))
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expanding permissions should load related")
	}
	m = drain(m, cmd)
	view = m.nav.View()
	if !strings.Contains(view, "storage.admin") {
		t.Fatalf("bindings:\n%s", view)
	}
	if !strings.Contains(view, "my-app-bucket") || !strings.Contains(view, "audit-logs") {
		t.Fatalf("explorer must not be replaced:\n%s", view)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	view = m.nav.View()
	if strings.Contains(view, "storage.admin") {
		t.Fatalf("esc should collapse kids:\n%s", view)
	}
	if !strings.Contains(view, "Permissions") {
		t.Fatalf("facet headers remain until parent collapses:\n%s", view)
	}
}

func TestGCSFoldersInExplorer(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = openBucket(m)
	m.focus = PaneNav
	m.syncLayout()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	// bucket → Permissions, Objects
	m = press(m, keyText("j"))
	m = press(m, keyText("j"))
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expanding objects should load related")
	}
	m = drain(m, cmd)
	view := m.nav.View()
	if !strings.Contains(view, "logos") || !strings.Contains(view, "data") {
		t.Fatalf("object prefixes should be folder nodes:\n%s", view)
	}
	if strings.Contains(view, "logos/logo.png") {
		t.Fatalf("objects should nest under folders, not show full paths:\n%s", view)
	}
	// move onto logos and expand
	for i := 0; i < 8; i++ {
		row, ok := m.nav.SelectedRow()
		if ok && row.Resource.Name == "logos" {
			break
		}
		m = press(m, keyText("j"))
	}
	row, ok := m.nav.SelectedRow()
	if !ok || row.Resource.Name != "logos" {
		t.Fatalf("expected logos folder, got %+v", row)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	view = m.nav.View()
	if !strings.Contains(view, "logo.png") {
		t.Fatalf("expanding folder should reveal objects:\n%s", view)
	}
}

func TestResourceGroupsUnderProject(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	view := m.nav.View()
	if !strings.Contains(view, "Cloud Storage") || !strings.Contains(view, "Enabled Services") {
		t.Fatalf("%s", view)
	}
	if strings.Contains(view, "Folders") {
		t.Fatalf("CRM folders must not be wrapped:\n%s", view)
	}
	if strings.Contains(view, "Subnets") && !strings.Contains(view, "VPC Networks") {
		t.Fatalf("nested types should not appear at project level:\n%s", view)
	}
	m = expandNamed(m, "Cloud Storage")
	view = m.nav.View()
	if !strings.Contains(view, "my-app-bucket") || !strings.Contains(view, "audit-logs") {
		t.Fatalf("expanded buckets:\n%s", view)
	}
}

func TestNetworkCollectionsNestUnderProducts(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	// Only demo-project's rows count: an organization also holds some of
	// these products (Logging), and its rows list first.
	const demo = "projects/demo-project"
	depth := func(m Model) map[string]int {
		out := map[string]int{}
		for _, r := range m.nav.Rows() {
			if _, ok := out[r.Resource.Name]; !ok && r.Resource.ScopeID == demo {
				out[r.Resource.Name] = r.Depth
			}
		}
		return out
	}
	expandDemo := func(m Model, name string) Model {
		for _, r := range m.nav.Rows() {
			if r.Resource.Name == name && r.Resource.ScopeID == demo {
				next, cmd := jumpToID(m, r.Resource.ID).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				return drain(next.(Model), cmd)
			}
		}
		t.Fatalf("no %s row in %s:\n%s", name, demo, m.nav.View())
		return m
	}
	products := map[string][]string{
		"Compute Engine":       {"GCE Instances", "Disks", "Instance Templates", "Instance Groups", "Health Checks"},
		"VPC Networks":         {"IP Addresses"},
		"Network Services":     {"Load Balancers", "Forwarding Rules", "Private Service Connect", "DNS Zones"},
		"Network Security":     {"Cloud Armor Policies", "SSL Policies", "TLS Inspection Policies", "IAP-Secured Apps", "Secure Web Proxy", "Certificate Manager"},
		"Network Connectivity": {"Cloud Routers"},
		"Memorystore":          {"Redis Instances", "Valkey Instances"},
		"Logging":              {"Log Sinks", "Log Buckets"},
	}
	top := depth(m)
	for _, name := range []string{"Backend Services", "Routes", "NAT Gateways", "Record Sets", "Databases", "Secret Versions", "Crypto Keys", "SWP Rules", "Certificate Map Entries", "PSC Connections"} {
		if _, ok := top[name]; ok {
			t.Fatalf("%s list only under their parent resource", name)
		}
	}
	for product, collections := range products {
		d, ok := top[product]
		if !ok {
			t.Fatalf("no %s node:\n%s", product, m.nav.View())
		}
		if d != top["Cloud Storage"] {
			t.Fatalf("%s depth %d, Cloud Storage %d", product, d, top["Cloud Storage"])
		}
		for _, name := range collections {
			if _, ok := top[name]; ok {
				t.Fatalf("%s should stay hidden until %s expands", name, product)
			}
		}
		// Models share their outline, so each product expands in a fresh one.
		e := expandDemo(sized(newTestModel()), product)
		got := depth(e)
		for _, name := range collections {
			if cd, ok := got[name]; !ok || cd != d+1 {
				t.Fatalf("%s depth=%d ok=%v, want %d under %s:\n%s", name, cd, ok, d+1, product, e.nav.View())
			}
		}
	}
	// The VPC product node and its networks collection share a name, as
	// in the Console.
	m = expandID(expandID(sized(newTestModel()), demoVPC), demoVPC+"/collections/vpc")
	if row := mustSelected(t, m); !row.Expanded || !row.Resource.IsCollection() {
		t.Fatalf("VPC Networks collection should expand: %+v", row)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if row := mustSelected(t, m); row.Resource.ID != demoVPC {
		t.Fatalf("Esc on a collapsed collection should select its product, got %q", row.Resource.ID)
	}

	// Private Service Connect nests one more level under Network Services.
	m = expandNamed(expandNamed(sized(newTestModel()), "Network Services"), "Private Service Connect")
	psc := mustSelected(t, m)
	if !psc.Resource.IsProductGroup() || !psc.Expanded {
		t.Fatalf("PSC row %+v", psc)
	}
	var kids []string
	for _, r := range rowsUnder(m, psc.Key()) {
		kids = append(kids, r.Resource.Name)
		if r.Depth != psc.Depth+1 {
			t.Fatalf("%s depth %d under PSC at %d", r.Resource.Name, r.Depth, psc.Depth)
		}
	}
	if want := []string{"Connected Endpoints", "Published Services", "Regional Endpoints", "Service Connection Policies"}; !slices.Equal(kids, want) {
		t.Fatalf("PSC children %q, want %q", kids, want)
	}

	// Secure Web Proxy nests likewise under Network Security.
	m = expandNamed(expandNamed(sized(newTestModel()), "Network Security"), "Secure Web Proxy")
	swp := mustSelected(t, m)
	if !swp.Resource.IsProductGroup() || !swp.Expanded {
		t.Fatalf("SWP row %+v", swp)
	}
	kids = nil
	for _, r := range rowsUnder(m, swp.Key()) {
		kids = append(kids, r.Resource.Name)
	}
	if want := []string{"Web Proxies", "SWP Policies", "URL Lists"}; !slices.Equal(kids, want) {
		t.Fatalf("SWP children %q, want %q", kids, want)
	}
}

// demoVPC is demo-project's VPC Networks product node.
const demoVPC = "projects/demo-project/products/vpc"

func TestEnabledServicesNode(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandNamed(m, "Enabled Services")
	rows := m.nav.Rows()
	i := slices.IndexFunc(rows, func(r resources.Row) bool { return r.Resource.IsEnabledGroup() })
	var names []string
	for _, r := range rows[i+1:] {
		if r.Depth <= rows[i].Depth {
			break
		}
		names = append(names, r.Resource.Name)
	}
	// The list outgrows the pane, so check the rows rather than the view.
	if !slices.Contains(names, "Compute Engine") {
		t.Fatalf("enabled APIs %q lack Compute Engine", names)
	}
	sorted := slices.SortedStableFunc(slices.Values(names), func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	if len(names) < 2 || !slices.Equal(names, sorted) {
		t.Fatalf("enabled APIs %q, want sorted by name", names)
	}
}

// openArtifactRepo expands demo-project's Artifact Registry collection,
// the app-images repository, and its Images facet, which stays selected.
func openArtifactRepo(t *testing.T) Model {
	t.Helper()
	m := sized(newTestModel())
	m = expandNamed(m, model.ServiceArtifactRegistry.CollectionLabel())
	m = expandNamed(m, "app-images")
	if row := mustSelected(t, m); row.Resource.Name != "app-images" || !row.Expanded {
		t.Fatalf("app-images row %+v", row)
	}
	m = expandNamed(m, "Images")
	if row := mustSelected(t, m); row.Kind != resources.RowFacet || row.Facet.ID != model.FacetPackages || !row.Expanded {
		t.Fatalf("Images row %+v", row)
	}
	return m
}

func TestInlineFacetsListChildrenDirectly(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandNamed(m, model.ServiceArtifactRegistry.CollectionLabel())
	m = expandNamed(m, "py-packages")
	m = expandNamed(m, "Packages")
	pkgs := mustSelected(t, m)

	kids := rowsUnder(m, pkgs.Key())
	if len(kids) != 1 || kids[0].Resource.Name != "example-utils" {
		t.Fatalf("packages = %+v", kids)
	}

	// Python package → versions inline, one level down with no facet row.
	m = expandNamed(m, "example-utils")
	pkg := mustSelected(t, m)
	vers := rowsUnder(m, pkg.Key())
	if len(vers) != 1 || vers[0].Resource.Name != "2.3.0" || vers[0].Depth != pkg.Depth+1 {
		t.Fatalf("package children = %+v", vers)
	}
	for _, r := range m.nav.Rows() {
		if r.Kind == resources.RowFacet && r.Resource.Service != model.ServiceArtifactRegistry {
			t.Fatalf("inline facets must not draw facet rows: %+v", r)
		}
	}

	// Esc on the version closes example-utils.
	m.nav.SelectKey(vers[0].Key())
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if row := mustSelected(t, m); row.Key() != pkg.Key() || row.Expanded {
		t.Fatalf("after esc selected %+v", row)
	}
	// Enter reopens from the cache without a load.
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || !mustSelected(t, m).Expanded {
		t.Fatal("reopening a loaded inline facet should not load again")
	}
}

func TestArtifactDockerFacets(t *testing.T) {
	t.Parallel()
	m := openArtifactRepo(t)
	images := mustSelected(t, m)

	kids := rowsUnder(m, images.Key())
	if len(kids) != 2 {
		t.Fatalf("images = %+v", kids)
	}
	for _, k := range kids {
		if k.Kind != resources.RowResource || k.Depth != images.Depth+1 || k.Resource.Service != model.ServiceArtifactPackage {
			t.Fatalf("image row %+v", k)
		}
	}

	// Docker image → Digests and Signature Tags facets.
	m = expandNamed(m, "web/frontend")
	img := mustSelected(t, m)
	pkgFacets := rowsUnder(m, img.Key())
	if len(pkgFacets) != 2 || pkgFacets[0].Facet.Label != "Digests" || pkgFacets[1].Facet.Label != "Signature Tags" {
		t.Fatalf("image facets = %+v", pkgFacets)
	}

	// Signature Tags holds the .sig tag artifact.
	m = expandNamed(m, "Signature Tags")
	sigTags := mustSelected(t, m)
	sigTagRows := rowsUnder(m, sigTags.Key())
	if len(sigTagRows) != 1 || !strings.HasSuffix(sigTagRows[0].Resource.Name, ".sig") || sigTagRows[0].Expandable {
		t.Fatalf("signature tag rows = %+v", sigTagRows)
	}

	// Digests holds the container image digest, which exposes Signatures and Vulnerabilities.
	m = expandNamed(m, "Digests")
	digests := rowsUnder(m, mustSelected(t, m).Key())
	if len(digests) != 1 || digests[0].Resource.Name != "v1.4.2, latest" {
		t.Fatalf("digests = %+v", digests)
	}
	m = expandNamed(m, "v1.4.2, latest")
	digest := mustSelected(t, m)
	verFacets := rowsUnder(m, digest.Key())
	if len(verFacets) != 2 || verFacets[0].Facet.Label != "Signatures" || verFacets[1].Facet.Label != "Vulnerabilities" {
		t.Fatalf("digest facets = %+v", verFacets)
	}

	// Signatures facet lists cosign signatures.
	m = expandNamed(m, "Signatures")
	sigsRow := mustSelected(t, m)
	sigRows := rowsUnder(m, sigsRow.Key())
	if len(sigRows) != 2 || sigRows[0].Resource.Service != model.ServiceArtifactSignature {
		t.Fatalf("signature rows = %+v", sigRows)
	}

	// Vulnerabilities facet groups findings by severity.
	m = expandNamed(m, "Vulnerabilities")
	vulnsFacet := mustSelected(t, m)
	groups := rowsUnder(m, vulnsFacet.Key())
	var names []string
	for _, g := range groups {
		if !g.Resource.IsSeverityGroup() || g.Depth != vulnsFacet.Depth+1 || g.Expanded || g.Resource.Badge != "1" {
			t.Fatalf("vuln child %+v", g)
		}
		names = append(names, g.Resource.Name)
	}
	if want := []string{"Critical", "High", "Medium"}; !slices.Equal(names, want) {
		t.Fatalf("severity groups = %v, want %v (worst first)", names, want)
	}

	// Each severity opens on its own.
	m = expandNamed(m, "Critical")
	crit := mustSelected(t, m)
	vulns := rowsUnder(m, crit.Key())
	if len(vulns) != 1 || vulns[0].Resource.Badge != "CRITICAL" || vulns[0].Depth != crit.Depth+1 {
		t.Fatalf("critical children = %+v", vulns)
	}
	if len(rowsUnder(m, groups[1].Key())) != 0 {
		t.Fatal("opening Critical should leave High closed")
	}

	// Esc on a vulnerability closes its severity; Esc again closes Vulnerabilities.
	m.nav.SelectKey(vulns[0].Key())
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if row := mustSelected(t, m); row.Key() != crit.Key() || row.Expanded {
		t.Fatalf("after esc selected %+v", row)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m.nav.SelectKey(vulnsFacet.Key())
	if row := mustSelected(t, m); row.Key() != vulnsFacet.Key() || row.Expanded {
		t.Fatalf("after second esc Vulnerabilities row = %+v", row)
	}
}

// TestArtifactRepoPermissions lists the bindings set on a repository under
// its Permissions facet, ahead of its Images.
func TestArtifactRepoPermissions(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandNamed(m, model.ServiceArtifactRegistry.CollectionLabel())
	m = expandNamed(m, "app-images")
	repo := mustSelected(t, m)
	var facets []string
	for _, r := range rowsUnder(m, repo.Key()) {
		facets = append(facets, r.Facet.Label)
	}
	if want := []string{"Permissions", "Images"}; !slices.Equal(facets, want) {
		t.Fatalf("repo facets = %v, want %v", facets, want)
	}
	m = expandNamed(m, "Permissions")
	perms := mustSelected(t, m)
	var roles []string
	for _, r := range rowsUnder(m, perms.Key()) {
		if r.Depth == perms.Depth+1 {
			roles = append(roles, r.Resource.Name)
		}
	}
	if want := []string{"artifactregistry.reader", "artifactregistry.writer"}; !slices.Equal(roles, want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
}

// TestCertificateManager opens Certificate Manager under Network Security:
// one collection per type, and a map expanding inline to its entries.
func TestCertificateManager(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandNamed(m, "Network Security")
	m = expandNamed(m, "Certificate Manager")
	cm := mustSelected(t, m)
	var colls []string
	for _, r := range rowsUnder(m, cm.Key()) {
		colls = append(colls, r.Resource.Name)
	}
	if want := []string{"Certificates", "Certificate Maps", "DNS Authorizations", "Issuance Configs", "Trust Configs"}; !slices.Equal(colls, want) {
		t.Fatalf("Certificate Manager children = %q, want %q", colls, want)
	}
	m = expandNamed(m, "Certificate Maps")
	m = expandNamed(m, "web-map")
	webMap := mustSelected(t, m)
	var entries []string
	for _, r := range rowsUnder(m, webMap.Key()) {
		if r.Kind != resources.RowResource || r.Resource.Service != model.ServiceCertMapEntry || r.Depth != webMap.Depth+1 {
			t.Fatalf("map child %+v", r)
		}
		entries = append(entries, r.Resource.Name)
	}
	slices.Sort(entries)
	if want := []string{"PRIMARY", "api.example.com", "www.example.com"}; !slices.Equal(entries, want) {
		t.Fatalf("entries = %q, want %q", entries, want)
	}
}

func TestSearchOpensSeverityGroups(t *testing.T) {
	t.Parallel()
	m := openArtifactRepo(t)
	m = expandNamed(m, "web/frontend")
	m = expandNamed(m, "Digests")
	m = expandNamed(m, "v1.4.2, latest")
	m = expandNamed(m, "Vulnerabilities")
	m.search.Query = "CVE-2023-44487"
	m.rebuildOutline()
	var path []string
	for _, r := range m.nav.Rows() {
		path = append(path, r.Resource.Name)
	}
	if n := len(path); n < 2 || path[n-2] != "High" || path[n-1] != "CVE-2023-44487" {
		t.Fatalf("search rows %q", path)
	}
}

// TestOrgPoliciesFollowIAM checks that a CRM node with policies set
// directly on it lists Organization Policies right after IAM, ahead of its
// child folders and projects, and that a node with none has no such row.
func TestOrgPoliciesFollowIAM(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	children := func(name string) []string {
		rows := m.nav.Rows()
		for i, r := range rows {
			if r.Resource.Name != name || !(r.Resource.IsScope() || r.Resource.Service == model.ServiceCloudPlatform) {
				continue
			}
			var out []string
			for _, c := range rows[i+1:] {
				if c.Depth <= r.Depth {
					break
				}
				if c.Depth == r.Depth+1 {
					out = append(out, c.Resource.Name)
				}
			}
			return out
		}
		t.Fatalf("no %s row:\n%s", name, m.nav.View())
		return nil
	}
	for _, tc := range []struct {
		node string
		want bool
	}{
		{"Cloud Platform", true},
		{"platform", true},
		{"demo-project", true},
		{"prod", false},
	} {
		kids := children(tc.node)
		if len(kids) < 2 || kids[0] != "IAM" {
			t.Fatalf("%s children = %v, want IAM first", tc.node, kids)
		}
		if got := kids[1] == "Organization Policies"; got != tc.want {
			t.Fatalf("%s children = %v, want Organization Policies second: %v", tc.node, kids, tc.want)
		}
		if !tc.want && slices.Contains(kids, "Organization Policies") {
			t.Fatalf("%s has no direct policy but lists Organization Policies: %v", tc.node, kids)
		}
	}
}

// TestDatabasesAndRecordSetsListUnderTheirParents expands a Cloud SQL
// instance's Databases facet to its databases, and a DNS zone and a
// secret inline to their record sets and versions, each one level down.
func TestDatabasesAndRecordSetsListUnderTheirParents(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path  []string
		svc   model.ServiceType
		names []string
	}{
		{[]string{"Cloud SQL", "postgres-prod", "Databases"}, model.ServiceCloudSQLDatabase, []string{"analytics", "app", "postgres"}},
		{[]string{"Network Services", "DNS Zones", "corp-internal"}, model.ServiceDNSRecord,
			[]string{"api.corp.internal.", "postgres.corp.internal.", "redis.corp.internal."}},
		{[]string{"Secrets", "db-password"}, model.ServiceSecretVersion, []string{"1", "2", "3"}},
	}
	for _, tc := range cases {
		t.Run(tc.path[len(tc.path)-1], func(t *testing.T) {
			t.Parallel()
			m := sized(newTestModel())
			for _, name := range tc.path {
				m = expandNamed(m, name)
			}
			parent := mustSelected(t, m)
			last := tc.path[len(tc.path)-1]
			if (parent.Resource.Name != last && parent.Facet.Label != last) || !parent.Expanded {
				t.Fatalf("parent row %+v", parent)
			}
			var names []string
			for _, k := range rowsUnder(m, parent.Key()) {
				if k.Kind != resources.RowResource || k.Depth != parent.Depth+1 || k.Resource.Service != tc.svc {
					t.Fatalf("child %+v", k)
				}
				names = append(names, k.Resource.Name)
			}
			slices.Sort(names)
			if !slices.Equal(names, tc.names) {
				t.Fatalf("children = %v, want %v", names, tc.names)
			}
		})
	}
}

// TestKMSKeyRingFacets walks a key ring's facets down to a key's versions,
// which keep the provider's newest-first order.
func TestKMSKeyRingFacets(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandNamed(m, "Key Rings")
	m = expandNamed(m, "app-keys")
	facetLabels := func(parent resources.Row) []string {
		var out []string
		for _, r := range rowsUnder(m, parent.Key()) {
			if r.Depth == parent.Depth+1 {
				out = append(out, r.Facet.Label)
			}
		}
		return out
	}
	ring := mustSelected(t, m)
	if got, want := facetLabels(ring), []string{"Keys", "Import Jobs", "Permissions"}; !slices.Equal(got, want) {
		t.Fatalf("ring facets = %v, want %v", got, want)
	}
	m = expandNamed(m, "Keys")
	keysRow := mustSelected(t, m)
	var keys []string
	for _, r := range rowsUnder(m, keysRow.Key()) {
		if r.Depth == keysRow.Depth+1 && r.Resource.Service == model.ServiceKMSKey {
			keys = append(keys, r.Resource.Name)
		}
	}
	slices.Sort(keys)
	if want := []string{"gcs-cmek", "release-signing", "secrets"}; !slices.Equal(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	m = expandNamed(m, "secrets")
	key := mustSelected(t, m)
	if got, want := facetLabels(key), []string{"Versions", "Permissions"}; !slices.Equal(got, want) {
		t.Fatalf("key facets = %v, want %v", got, want)
	}
	m = expandNamed(m, "Versions")
	versionsRow := mustSelected(t, m)
	var versions []string
	for _, r := range rowsUnder(m, versionsRow.Key()) {
		if r.Resource.Service != model.ServiceKMSKeyVersion {
			t.Fatalf("child %+v", r)
		}
		versions = append(versions, r.Resource.Name+":"+r.Resource.Badge)
	}
	if want := []string{"4:PRIMARY", "3:ENABLED", "2:DESTROY_SCHEDULED", "1:DESTROYED"}; !slices.Equal(versions, want) {
		t.Fatalf("versions = %v, want %v", versions, want)
	}
}

func TestEmptyInlineFacetLosesTriangle(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandNamed(m, model.ServiceArtifactRegistry.CollectionLabel())
	m = expandNamed(m, "py-packages")
	m = expandNamed(m, "Packages")
	pkg := jumpTo(m, "example-utils")
	row := mustSelected(t, pkg)
	if row.Resource.Service != model.ServiceArtifactPackage {
		t.Fatalf("selected %+v, want example-utils", row)
	}
	if !row.Expandable {
		t.Fatal("an unloaded inline facet may have children")
	}
	m = pkg
	m.applyRelated(relatedLoadedMsg{parentID: row.Resource.ID, facetID: model.FacetVersions, result: model.ProviderResult{Service: model.ServiceArtifactRegistry}})
	m.outline.related[facetLoadKey(row.Resource.ID, model.FacetVersions)] = relatedReq{parent: row.Resource, facet: row.Resource.Facets[0]}
	m.finishRelated(relatedDoneMsg{parentID: row.Resource.ID, facetID: model.FacetVersions})
	got := mustSelected(t, m)
	if got.Expandable || got.Expanded {
		t.Fatalf("empty load should drop the triangle: %+v", got)
	}
	if m.status.Flash != "no versions" {
		t.Fatalf("flash = %q", m.status.Flash)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || mustSelected(t, m).Expanded || m.status.Flash != "no versions" {
		t.Fatalf("enter on an empty inline facet: cmd=%v flash=%q", cmd != nil, m.status.Flash)
	}
}

func TestEmptyFacetRowLosesTriangle(t *testing.T) {
	t.Parallel()
	m := openBucket(sized(newTestModel()))
	bucket := mustSelected(t, m).Resource
	m.outline.showFacets[bucket.ID] = true
	m.applyRelated(relatedLoadedMsg{parentID: bucket.ID, facetID: model.FacetObjects, result: model.ProviderResult{Service: model.ServiceCloudStorage}})
	m.finishRelated(relatedDoneMsg{parentID: bucket.ID, facetID: model.FacetObjects})
	for _, r := range m.nav.Rows() {
		if r.Kind == resources.RowFacet && r.Facet.ID == model.FacetObjects {
			if r.Expandable || r.Expanded {
				t.Fatalf("empty Objects facet row %+v", r)
			}
			return
		}
	}
	t.Fatal("no Objects facet row")
}

type optionalTestProvider struct {
	pkg model.Resource
	ver model.Resource
}

func (p optionalTestProvider) Service() model.ServiceType { return model.ServiceArtifactRegistry }
func (p optionalTestProvider) List(_ context.Context, scopeID string) ([]model.Resource, error) {
	if scopeID != "projects/demo-project" {
		return nil, nil
	}
	return []model.Resource{p.pkg}, nil
}
func (p optionalTestProvider) ListRelated(_ context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.ID == p.pkg.ID && facetID == model.FacetVersions {
		return []model.Resource{p.ver}, false, nil
	}
	return nil, false, nil
}

func TestOptionalFacetsHiddenWhenEmpty(t *testing.T) {
	t.Parallel()
	// Package with Digests and empty Optional Signature Tags.
	pkg := model.Resource{
		ID:       "projects/demo-project/locations/us-central1/repositories/r/packages/empty-img",
		Name:     "empty-img",
		Service:  model.ServiceArtifactPackage,
		ScopeID:  "projects/demo-project",
		Location: "us-central1",
		Facets: []model.Facet{
			{ID: model.FacetVersions, Label: "Digests"},
			{ID: model.FacetSignatureTags, Label: "Signature Tags", Optional: true},
		},
	}
	// Version where all facets are Optional and all come back empty.
	ver := model.Resource{
		ID:       pkg.ID + "/versions/sha256:aaaa",
		Name:     "v1.0.0",
		Service:  model.ServiceArtifactVersion,
		ScopeID:  "projects/demo-project",
		Location: "us-central1",
		ParentID: pkg.ID,
		FacetID:  model.FacetVersions,
		Facets: []model.Facet{
			{ID: model.FacetSignatures, Label: "Signatures", Optional: true},
			{ID: model.FacetVulnerabilities, Label: "Vulnerabilities", Optional: true},
		},
	}
	src := gcp.NewCatalog(optionalTestProvider{pkg: pkg, ver: ver})
	m := sized(load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", src, testIdent())))
	m = expandNamed(m, model.ServiceArtifactRegistry.CollectionLabel())
	m = expandNamed(m, "empty-img")
	pkgRow := mustSelected(t, jumpToID(m, pkg.ID))
	pkgFacets := rowsUnder(m, pkgRow.Key())
	if len(pkgFacets) != 1 || pkgFacets[0].Facet.ID != model.FacetVersions {
		t.Fatalf("expected only Digests when Signature Tags is empty, got %+v", pkgFacets)
	}

	m = expandNamed(m, "Digests")
	m = expandNamed(m, "v1.0.0")
	m = jumpToID(m, ver.ID)
	vRow := mustSelected(t, m)
	if vRow.Expandable || vRow.Expanded {
		t.Fatalf("version with all empty optional facets should lose triangle: %+v", vRow)
	}
	if kids := rowsUnder(m, vRow.Key()); len(kids) != 0 {
		t.Fatalf("expected no child rows under empty version, got %+v", kids)
	}
}

func TestDetailsOpensInlineFacet(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandNamed(m, model.ServiceArtifactRegistry.CollectionLabel())
	m = expandNamed(m, "py-packages")
	m = expandNamed(m, "Packages")
	m = jumpTo(m, "example-utils")
	pkg := mustSelected(t, m)
	m.focus = PaneDetail
	m.syncLayout()
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drain(next.(Model), cmd)
	row := mustSelected(t, m)
	if m.focus != PaneNav || row.Key() != pkg.Key() || !row.Expanded || len(rowsUnder(m, pkg.Key())) != 1 {
		t.Fatalf("focus=%v selected %+v", m.focus, row)
	}
}

// TestAccessContextManagerTree checks where Access Context Manager sits:
// the organization's node lists its access bindings, then VPC Service
// Controls with the access policies, which expand to perimeters and access
// levels; a project's node holds only the perimeters that protect it.
func TestAccessContextManagerTree(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	product := func(m Model, svc model.ServiceType, scope string) resources.Row {
		for _, r := range m.nav.Rows() {
			if r.Resource.IsProductGroup() && r.Resource.Service == svc && r.Resource.ScopeID == scope {
				return r
			}
		}
		t.Fatalf("no %s row under %s:\n%s", svc, scope, m.nav.View())
		return resources.Row{}
	}
	names := func(rows []resources.Row, depth int) []string {
		var out []string
		for _, r := range rows {
			if r.Depth == depth {
				name := r.Resource.Name
				if r.Facet.Label != "" {
					name = r.Facet.Label
				}
				out = append(out, name)
			}
		}
		return out
	}
	// Both scopes have nodes of the same names, so open them by id.
	open := func(m Model, id string) Model {
		next, cmd := jumpToID(m, id).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		return drain(next.(Model), cmd)
	}
	under := func(m Model) []string {
		row := mustSelected(t, m)
		return names(rowsUnder(m, row.Key()), row.Depth+1)
	}

	const org = "organizations/example-org"
	m = open(m, product(m, model.ServiceACM, org).Resource.ID)
	if got, want := under(m), []string{"Access Bindings", "VPC Service Controls"}; !slices.Equal(got, want) {
		t.Fatalf("example-org Access Context Manager children %q, want %q", got, want)
	}
	m = expandNamed(m, "Access Bindings")
	if got, want := under(m), []string{"03x8tuzt1hmd2bz", "contractors@example.com", "engineering@example.com", "terraform@seed-project.iam.gserviceaccount.com"}; !slices.Equal(got, want) {
		t.Fatalf("bindings %q, want %q", got, want)
	}
	// A binding expands inline to the access levels it uses.
	m = expandNamed(m, "engineering@example.com")
	if got, want := under(m), []string{"corp_devices", "corp_network"}; !slices.Equal(got, want) {
		t.Fatalf("engineering levels %q, want %q", got, want)
	}
	m = open(m, product(m, model.ServiceVPCSC, org).Resource.ID)
	m = expandNamed(m, "Access Policies")
	if got, want := under(m), []string{"example default policy", "platform team policy"}; !slices.Equal(got, want) {
		t.Fatalf("policies %q, want %q", got, want)
	}
	m = expandNamed(m, "example default policy")
	if got, want := under(m), []string{"Perimeters", "Access Levels"}; !slices.Equal(got, want) {
		t.Fatalf("policy facets %q, want %q", got, want)
	}
	m = expandNamed(m, "Perimeters")
	if got, want := under(m), []string{"data_bridge", "prod_perimeter"}; !slices.Equal(got, want) {
		t.Fatalf("perimeters %q, want %q", got, want)
	}
	// With rules as well, prod_perimeter's access levels get a facet row.
	m = expandNamed(m, "prod_perimeter")
	if got, want := under(m), []string{"Access Levels", "Ingress Rules", "Egress Rules"}; !slices.Equal(got, want) {
		t.Fatalf("prod_perimeter facets %q, want %q", got, want)
	}
	m = expandNamed(m, "Ingress Rules")
	if got, want := under(m), []string{"CI deploys", "any identity from corp_network → bigquery"}; !slices.Equal(got, want) {
		t.Fatalf("ingress rules %q, want %q", got, want)
	}

	const demo = "projects/demo-project"
	m = sized(newTestModel())
	m = open(m, product(m, model.ServiceACM, demo).Resource.ID)
	if got, want := under(m), []string{"VPC Service Controls"}; !slices.Equal(got, want) {
		t.Fatalf("demo-project Access Context Manager children %q, want %q", got, want)
	}
	m = open(m, product(m, model.ServiceVPCSC, demo).Resource.ID)
	if got, want := under(m), []string{"Service Perimeters"}; !slices.Equal(got, want) {
		t.Fatalf("demo-project VPC Service Controls children %q, want %q", got, want)
	}
}

// TestScopeProductsBeforeChildScopes checks that under an organization or
// folder, the scope's own products all list before its child folders and
// projects.
func TestScopeProductsBeforeChildScopes(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	rows := m.nav.Rows()
	checked := 0
	for i, r := range rows {
		k := model.ScopeKind(r.Resource.ID)
		crm := r.Resource.ID == r.Resource.ScopeID && (k == model.KindOrg || k == model.KindFolder)
		// An organization's products and child scopes sit under Cloud Platform.
		if !r.Expanded || !(crm && k == model.KindFolder || r.Resource.Service == model.ServiceCloudPlatform) {
			continue
		}
		var kids []string
		seenScope := false
		for _, c := range rows[i+1:] {
			if c.Depth <= r.Depth {
				break
			}
			if c.Depth != r.Depth+1 {
				continue
			}
			kids = append(kids, c.Resource.Name)
			if c.Resource.IsScope() {
				seenScope = true
			} else if seenScope {
				t.Fatalf("%s: %s lists after a child scope: %v", r.Resource.Name, c.Resource.Name, kids)
			}
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no expanded organization or folder")
	}
	// The organization holds Workspace and Cloud Platform, and Cloud
	// Platform holds products past IAM and Organization Policies.
	var org, platform []string
	for _, r := range rows {
		switch {
		case r.Depth == 1:
			org = append(org, r.Resource.Name)
		case r.Depth == 2 && !r.Resource.IsScope():
			platform = append(platform, r.Resource.Name)
		}
	}
	if want := []string{"Workspace", "Cloud Platform"}; !slices.Equal(org, want) {
		t.Fatalf("example-org children %q, want %q", org, want)
	}
	if want := []string{"IAM", "Organization Policies", "Access Context Manager", "Logging"}; !slices.Equal(platform, want) {
		t.Fatalf("Cloud Platform products %q, want %q", platform, want)
	}
}

func TestChildFoldersListLast(t *testing.T) {
	t.Parallel()
	node := func(id, name string, kind model.NodeKind, kids ...model.HierarchyNode) model.HierarchyNode {
		n := model.HierarchyNode{ID: id, Name: name, Kind: kind, Children: kids}
		for i := range n.Children {
			n.Children[i].ParentID = id
		}
		return n
	}
	// As the live tree sorts them, by name with kinds mixed.
	tree := node("organizations/o", "org", model.KindOrg,
		node("folders/a", "alpha", model.KindFolder),
		node("projects/demo-project", "beta", model.KindProject),
		node("folders/g", "gamma", model.KindFolder,
			node("folders/g1", "g-inner", model.KindFolder),
			node("projects/g2", "g-project", model.KindProject),
		),
		node("projects/z", "zeta", model.KindProject),
	)
	src := gcp.NewCatalog(fake.Providers()...).WithHierarchy(tree)
	m := sized(load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", src, testIdent())))
	m = expandNamed(m, "gamma")
	under := func(name string) []string {
		rows := m.nav.Rows()
		i := slices.IndexFunc(rows, func(r resources.Row) bool { return r.Resource.Name == name })
		if i < 0 {
			t.Fatalf("no row %s", name)
		}
		var out []string
		for _, r := range rows[i+1:] {
			if r.Depth <= rows[i].Depth {
				break
			}
			if r.Depth == rows[i].Depth+1 && r.Resource.IsScope() {
				out = append(out, r.Resource.Name)
			}
		}
		return out
	}
	for _, tc := range []struct {
		parent string
		want   []string
	}{
		{"Cloud Platform", []string{"beta", "zeta", "alpha", "gamma"}},
		{"gamma", []string{"g-project", "g-inner"}},
	} {
		if got := under(tc.parent); !slices.Equal(got, tc.want) {
			t.Errorf("scopes under %s = %q, want %q", tc.parent, got, tc.want)
		}
	}
}

func TestOrgPolicyExpandsToValues(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	enter := func(m Model, id string) Model {
		m = jumpToID(m, id)
		next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		return drain(next.(Model), cmd)
	}
	m = enter(m, model.CollectionID("projects/demo-project", model.ServiceOrgPolicy))
	m = enter(m, "projects/demo-project/policies/compute.vmExternalIpAccess")
	pol := mustSelected(t, m)
	kids := rowsUnder(m, pol.Key())
	if len(kids) != 1 {
		t.Fatalf("values = %+v", kids)
	}
	if v := kids[0]; v.Kind != resources.RowResource || v.Depth != pol.Depth+1 || v.Resource.Service != model.ServiceOrgPolicyValue ||
		v.Resource.Name != "projects/demo-project/zones/us-central1-a/instances/web-1" || v.Resource.Badge != "ALLOW" {
		t.Fatalf("value row %+v", v)
	}
}

func TestWorkstationsNestWithoutCollectionRows(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	enter := func(m Model, id string) Model {
		m = jumpToID(m, id)
		next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		return drain(next.(Model), cmd)
	}
	cluster := "projects/demo-project/locations/us-central1/workstationClusters/dev-cluster"
	config := cluster + "/workstationConfigs/vscode-standard"
	m = enter(m, model.CollectionID("projects/demo-project", model.ServiceWorkstationCluster))
	m = enter(m, cluster)
	m = enter(m, config)
	coll := m.nav.Rows()[slices.IndexFunc(m.nav.Rows(), func(r resources.Row) bool {
		return r.Resource.IsCollection() && r.Resource.Name == "Cloud Workstations"
	})]
	var got []string
	for _, r := range rowsUnder(m, coll.Key()) {
		got = append(got, strings.Repeat(" ", r.Depth-coll.Depth-1)+r.Resource.Name)
	}
	want := []string{"dev-cluster", " vscode-standard", "  alice", "  bob"}
	if !slices.Equal(got, want) {
		t.Fatalf("Cloud Workstations rows %q, want %q", got, want)
	}
}

// TestCloudPlatformCollapses checks that an organization lists Workspace
// and Cloud Platform, and that closing Cloud Platform leaves only
// Workspace beneath the organization.
func TestCloudPlatformCollapses(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	plat := model.ProductGroupID("organizations/example-org", model.ServiceCloudPlatform)
	if row := mustRow(t, m, "Cloud Platform"); !row.Expanded || row.Resource.ID != plat {
		t.Fatalf("Cloud Platform row %+v should start open", row)
	}
	if got := namesUnder(m, "organizations/example-org"); !slices.Equal(got, []string{"Workspace", "Cloud Platform"}) {
		t.Fatalf("org children %q", got)
	}
	m = expandID(m, plat)
	if got := namesUnder(m, "organizations/example-org"); !slices.Equal(got, []string{"Workspace", "Cloud Platform"}) {
		t.Fatalf("closed org children %q", got)
	}
	for _, r := range m.nav.Rows() {
		if r.Resource.Name == "demo-project" || r.Resource.Name == "platform" {
			t.Fatalf("%s still listed under a closed Cloud Platform", r.Resource.Name)
		}
	}
	m = expandID(m, plat)
	if row := mustRow(t, m, "demo-project"); row.Depth != 5 {
		t.Fatalf("demo-project depth %d after reopening", row.Depth)
	}
}

func TestSWPProxyDrillsIntoAllowlistURLs(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	netSec := model.ProductGroupID("projects/demo-project", model.ServiceNetworkSecurity)
	swpGroup := model.ProductGroupID(netSec, model.ServiceSecureWebProxy)
	m = expandID(m, netSec)
	m = expandID(m, swpGroup)
	m = expandID(m, swpGroup+"/collections/swp-web-proxy")

	// Expand egress-proxy -> rules -> allow-dev-tools -> dev-tools -> URL entries.
	m = expandNamed(m, "egress-proxy")
	_, rule := rowNamed(t, m, "allow-dev-tools")
	if !rule.Expandable || rule.Via == "" {
		t.Fatalf("allow-dev-tools under proxy should be expandable with Via: %+v", rule)
	}
	if plain := mustRow(t, m, "allow-google-apis"); plain.Expandable {
		t.Fatalf("rule without inUrlList should not be expandable: %+v", plain)
	}

	m = expandNamed(m, "allow-dev-tools")
	j, ul := rowNamed(t, m, "dev-tools")
	if !ul.Expandable || ul.Via == "" {
		t.Fatalf("dev-tools under rule should be expandable with Via: %+v", ul)
	}

	m = expandNamed(m, "dev-tools")
	want := []string{"github.com", "*.github.com", "proxy.golang.org", "registry.npmjs.org"}
	if got := childNames(m.nav.Rows(), j); !slices.Equal(got, want) {
		t.Fatalf("allowlist items = %v, want %v", got, want)
	}

	// Esc on an item closes dev-tools and selects it; Esc again closes the rule.
	m.nav.Move(1)
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if cur := mustSelected(t, m); cur.Resource.Name != "dev-tools" || cur.Expanded {
		t.Fatalf("Esc on URL item left %+v", cur)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if cur := mustSelected(t, m); cur.Resource.Name != "allow-dev-tools" || cur.Expanded {
		t.Fatalf("Esc on closed URL list left %+v", cur)
	}
}
