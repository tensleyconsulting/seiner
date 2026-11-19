package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"google.golang.org/api/googleapi"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/gcp"
	"github.com/tensleyconsulting/seiner/internal/gcp/fake"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/header"
	"github.com/tensleyconsulting/seiner/internal/ui/messages"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

func TestPartialProviderError(t *testing.T) {
	t.Parallel()
	boom := errors.New("permission denied")
	src := gcp.NewCatalog(
		fake.Static{
			Svc: model.ServiceCloudStorage,
			Items: []model.Resource{
				{ID: "b", Name: "my-app-bucket", Service: model.ServiceCloudStorage},
			},
		},
		fake.Static{Svc: model.ServiceGKE, Err: boom},
	)
	m := load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", src, testIdent()))
	if len(m.catalog) != 1 || m.catalog[0].Name != "my-app-bucket" {
		t.Fatalf("catalog=%+v", m.catalog)
	}
	if !strings.Contains(m.status.Flash, "permission denied") {
		t.Fatalf("flash=%q", m.status.Flash)
	}
}

func TestRefreshReloads(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	next, cmd := m.Update(keyText("r"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("refresh should return a load cmd")
	}
	if !m.outline.loading(scopeKey(m.scope.ID)) {
		t.Fatal("scope should be in flight")
	}
	m = drain(m, cmd)
	if len(m.outline.inflight) != 0 {
		t.Fatalf("inflight after drain: %v", m.outline.inflight)
	}
	m.nav.GoTop()
	if !strings.Contains(m.nav.View(), "Cloud Storage") {
		t.Fatalf("refresh should keep project collections:\n%s", m.nav.View())
	}
}

// Without credentials cmd passes no catalog: the TUI starts with the
// banner and shows no resources, demo or otherwise.
func TestAuthBannerDoesNotCrash(t *testing.T) {
	t.Parallel()
	ident := model.Identity{Err: errors.New("could not find default credentials")}
	m := sized(load(New(config.Config{IconMode: config.IconText, Intro: true}, "test", nil, ident)))
	view := m.render()
	if !strings.Contains(view, "auth:") {
		t.Fatalf("missing banner:\n%s", view)
	}
	if strings.Contains(view, "Cloud Storage") || strings.Contains(view, "demo-project") {
		t.Fatalf("no catalog must show no resources:\n%s", view)
	}
	if !m.intro.Done() {
		t.Fatal("no catalog has nothing to load, so no intro")
	}
	if strings.Contains(view, "loading") || strings.Contains(view, "unavailable") {
		t.Fatalf("no catalog must not report loads:\n%s", view)
	}
	if m.status.Account != "not signed in" {
		t.Fatalf("account %q", m.status.Account)
	}
}

func TestNoticeLineCountsInLayout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		ident model.Identity
		want  []string
	}{
		{"none", testIdent(), nil},
		{"notice", model.Identity{Account: "sa@x", Notice: model.NoticeNoProject}, []string{"no project set"}},
		{"banner", model.Identity{Err: errors.New("no adc")}, []string{"auth:"}},
		{"both", model.Identity{Err: errors.New("no adc"), Notice: model.NoticeNoProject}, []string{"auth:", "no project set"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := sized(load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", gcp.FakeCatalog(), tt.ident)))
			if got := m.bodyTop(); got != header.Rows+len(tt.want) {
				t.Fatalf("bodyTop %d, want %d", got, header.Rows+len(tt.want))
			}
			lines := strings.Split(ansi.Strip(m.render()), "\n")
			if len(lines) != m.height {
				t.Fatalf("rendered %d lines, want %d", len(lines), m.height)
			}
			for i, w := range tt.want {
				if !strings.Contains(lines[header.Rows+i], w) {
					t.Fatalf("line %d %q, want %q", header.Rows+i, lines[header.Rows+i], w)
				}
			}
			if strings.Contains(strings.Join(lines[:header.Rows], "\n"), "no project set") {
				t.Fatal("notice must not be in the header")
			}
		})
	}
}

// TestRejectedCredentialsAreOneBanner checks that when Google rejects the
// live credentials, every failing service adds up to one banner and one
// message, not one message per service.
func TestRejectedCredentialsAreOneBanner(t *testing.T) {
	t.Parallel()
	expired := &googleapi.Error{Code: 401, Message: "Request had invalid authentication credentials."}
	src := gcp.NewCatalog(
		fake.Static{
			Svc: model.ServiceCloudStorage,
			Items: []model.Resource{
				{ID: "projects/demo-project/buckets/b", Name: "my-app-bucket", Service: model.ServiceCloudStorage, ScopeID: "projects/demo-project"},
			},
		},
		fake.Static{Svc: model.ServiceGKE, Err: expired},
		fake.Static{Svc: model.ServiceCloudSQL, Err: expired},
		fake.Static{Svc: model.ServiceSecretManager, Err: expired},
	).WithHierarchy(fake.Hierarchy())
	m := sized(load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", src, testIdent())))

	var errs []string
	for _, e := range m.messages.Entries() {
		if e.Level == messages.Error {
			errs = append(errs, e.Text)
		}
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "credentials rejected") {
		t.Fatalf("error messages %q, want one credentials message", errs)
	}
	if !strings.HasPrefix(m.status.Flash, "auth: credentials rejected") {
		t.Fatalf("flash %q", m.status.Flash)
	}
	if got := m.bodyTop(); got != header.Rows+1 {
		t.Fatalf("bodyTop %d, want the banner counted", got)
	}
	lines := strings.Split(ansi.Strip(m.render()), "\n")
	if len(lines) != m.height {
		t.Fatalf("rendered %d lines, want %d", len(lines), m.height)
	}
	if !strings.Contains(lines[header.Rows], "auth: credentials rejected") {
		t.Fatalf("banner line %q", lines[header.Rows])
	}
	m.nav.GoTop()
	if !strings.Contains(m.nav.View(), "Cloud Storage") {
		t.Fatalf("services that loaded must stay:\n%s", m.nav.View())
	}
}

func TestRejectedCredentialsFromHierarchy(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	pe := model.ProviderError{Service: model.ServiceOrganization, Kind: model.ErrKindUnauthenticated, Message: "sign-in expired"}
	next, _ := m.Update(hierarchyLoadedMsg{err: pe})
	m = next.(Model)
	if m.authErr == nil || !m.hierFailed {
		t.Fatalf("authErr=%v hierFailed=%v", m.authErr, m.hierFailed)
	}
	if bs := m.banners(); len(bs) != 1 || !strings.Contains(ansi.Strip(bs[0]), "sign-in expired") {
		t.Fatalf("banners %q", bs)
	}
}

func TestBannerStaysOneLineWhenNarrow(t *testing.T) {
	t.Parallel()
	ident := model.Identity{Err: errors.New(strings.Repeat("very long credentials problem ", 10)), Notice: model.NoticeNoProject}
	m := load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", gcp.FakeCatalog(), ident))
	m.width, m.height = 60, 30
	m.syncLayout()
	bs := m.banners()
	if len(bs) != 2 {
		t.Fatalf("banners %d, want 2", len(bs))
	}
	for _, b := range bs {
		if h := lipgloss.Height(b); h != 1 {
			t.Fatalf("banner is %d lines:\n%s", h, b)
		}
	}
}

func TestProductGroupDetailsShowProblems(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m.applyLoaded(m.scope.ID, model.ProviderResult{
		Service:   model.ServiceVPC,
		Resources: []model.Resource{{ID: "projects/demo-project/global/networks/n", Name: "n", Service: model.ServiceVPC, ScopeID: "projects/demo-project"}},
		Err:       model.ProviderError{Service: model.ServiceVPC, Kind: model.ErrKindTimeout, Message: "timed out"},
	})
	m.applyLoaded(m.scope.ID, model.ProviderResult{
		Service: model.ServicePubSub,
		Err:     model.ProviderError{Service: model.ServicePubSub, Kind: model.ErrKindQuota, Message: "quota"},
	})
	m.finishLoad(m.scope.ID)
	m = jumpToID(m, demoVPC)
	if got := m.problems(mustSelected(t, m)); len(got) != 1 || got[0].Service != model.ServiceVPC {
		t.Fatalf("product problems %+v", got)
	}
	m = jumpToID(expandID(m, demoVPC), demoVPC+"/collections/vpc")
	if got := m.problems(mustSelected(t, m)); len(got) != 1 || got[0].Service != model.ServiceVPC {
		t.Fatalf("nested collection problems %+v", got)
	}
}

func TestProvidersRenderAsTheyArrive(t *testing.T) {
	t.Parallel()
	var providers []model.ResourceProvider
	for _, p := range fake.Providers() {
		if svc := p.Service(); svc == model.ServiceCloudStorage || svc == model.ServicePubSub {
			providers = append(providers, p)
		}
	}
	if len(providers) != 2 {
		t.Fatalf("fixture providers=%d", len(providers))
	}
	src := gcp.NewCatalog(providers...).WithHierarchy(fake.Hierarchy())
	m := sized(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", src, testIdent()))

	first, ok := m.startScope(m.scope.ID, true)().(providerLoadedMsg)
	if !ok {
		t.Fatal("first message should be one provider's result")
	}
	next, cmd := m.Update(first)
	m = next.(Model)
	if cmd == nil {
		t.Fatal("providerLoadedMsg should return the next read")
	}
	if m.outline.complete[scopeKey(m.scope.ID)] {
		t.Fatal("scope complete after one provider")
	}
	m.nav.GoTop()
	if view := m.nav.View(); !strings.Contains(view, first.result.Service.CollectionLabel()) {
		t.Fatalf("first provider %s should render before the rest:\n%s", first.result.Service, view)
	}
	if strings.Contains(m.status.Flash, "loaded") {
		t.Fatalf("per-provider arrival should not flash: %q", m.status.Flash)
	}

	m = drain(m, cmd)
	if !m.outline.complete[scopeKey(m.scope.ID)] {
		t.Fatal("scope should be complete after drain")
	}
	m.nav.GoTop()
	view := m.nav.View()
	for _, label := range []string{"Cloud Storage", "Pub/Sub Topics"} {
		if !strings.Contains(view, label) {
			t.Fatalf("missing %s:\n%s", label, view)
		}
	}
	if !strings.HasPrefix(m.status.Flash, "loaded ") {
		t.Fatalf("flash=%q", m.status.Flash)
	}
}

// TestHierarchyArrivesAfterStartup checks that the explorer starts out
// re-rooted on just the selected project (so startup never waits on
// Resource Manager) and re-roots onto the full CRM tree once
// hierarchyLoadedMsg arrives, keeping the selected project and its
// already-loaded resources.
func TestHierarchyArrivesAfterStartup(t *testing.T) {
	t.Parallel()
	src := gcp.NewCatalog(
		fake.Static{
			Svc: model.ServiceCloudStorage,
			Items: []model.Resource{
				{ID: "projects/demo-project/buckets/b", Name: "my-app-bucket", Service: model.ServiceCloudStorage, ScopeID: "projects/demo-project"},
			},
		},
	).WithHierarchy(fake.Hierarchy())

	m := sized(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", src, testIdent()))

	// Apply only the resource-load messages, not the hierarchy load that
	// Init also batches.
	m = drain(m, m.startup)

	if m.tree.ID != model.ProjectScope("demo-project") {
		t.Fatalf("tree should still be the single-project stub: %+v", m.tree)
	}
	m.nav.GoTop()
	if view := m.nav.View(); !strings.Contains(view, "demo-project") || strings.Contains(view, "example-org") {
		t.Fatalf("expected only the project node before the hierarchy arrives:\n%s", view)
	}

	root, err := src.Hierarchy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	next, cmd := m.Update(hierarchyLoadedMsg{root: root})
	m = next.(Model)
	if cmd != nil {
		m = drain(m, cmd)
	}

	if m.scope.ID != model.ProjectScope("demo-project") {
		t.Fatalf("selected project should survive the re-root: %+v", m.scope)
	}
	m.nav.GoTop()
	view := m.nav.View()
	for _, name := range []string{"example-org", "platform", "prod-apps", "demo-project"} {
		if !strings.Contains(view, name) {
			t.Fatalf("missing %s after hierarchy arrives:\n%s", name, view)
		}
	}
	if !strings.Contains(view, "Cloud Storage") {
		t.Fatalf("project resources loaded before the re-root should survive:\n%s", view)
	}
}

// flakyHierarchy fails its first Hierarchy call, as a Resource Manager
// timeout would, then defers to the embedded catalog.
type flakyHierarchy struct {
	gcp.Catalog
	calls *int
}

func (f flakyHierarchy) Hierarchy(ctx context.Context) (model.HierarchyNode, error) {
	*f.calls++
	if *f.calls == 1 {
		return model.HierarchyNode{}, context.DeadlineExceeded
	}
	return f.Catalog.Hierarchy(ctx)
}

func TestRefreshRetriesFailedHierarchy(t *testing.T) {
	t.Parallel()
	calls := 0
	src := flakyHierarchy{Catalog: gcp.FakeCatalog(), calls: &calls}
	m := sized(load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", src, testIdent())))
	if calls != 1 || !m.hierFailed || m.tree.ID != model.ProjectScope("demo-project") {
		t.Fatalf("calls=%d hierFailed=%v tree=%q; want the single-project stub after a failed load", calls, m.hierFailed, m.tree.ID)
	}

	refresh := func(m Model) Model {
		next, cmd := m.Update(keyText("r"))
		return drain(next.(Model), cmd)
	}
	m = refresh(m)
	if calls != 2 || m.hierFailed {
		t.Fatalf("calls=%d hierFailed=%v; refresh should retry the hierarchy", calls, m.hierFailed)
	}
	if _, ok := m.tree.Find(model.ProjectScope("demo-project")); !ok || m.tree.ID == model.ProjectScope("demo-project") {
		t.Fatalf("tree not re-rooted on the CRM tree: %q", m.tree.ID)
	}

	m = refresh(m)
	if calls != 2 {
		t.Fatalf("calls=%d; refresh should not rebuild a loaded hierarchy", calls)
	}
}

// partialHierarchyCatalog returns its tree with a partial error on the
// first Hierarchy call, as a Resource Manager outage on one folder would,
// then the tree alone.
type partialHierarchyCatalog struct {
	gcp.Catalog
	calls *int
}

func (p partialHierarchyCatalog) Hierarchy(ctx context.Context) (model.HierarchyNode, error) {
	*p.calls++
	root, err := p.Catalog.Hierarchy(ctx)
	if *p.calls == 1 && err == nil {
		err = model.ProviderError{Service: model.ServiceOrganization, Kind: model.ErrKindQuota, Message: "quota exceeded", Partial: true}
	}
	return root, err
}

// A partial tree is shown with a warning, and refresh rebuilds it.
func TestPartialHierarchyWarnsAndRetries(t *testing.T) {
	t.Parallel()
	calls := 0
	src := partialHierarchyCatalog{Catalog: gcp.FakeCatalog(), calls: &calls}
	m := sized(load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", src, testIdent())))
	if calls != 1 || !m.hierFailed {
		t.Fatalf("calls=%d hierFailed=%v; want a partial load marked for retry", calls, m.hierFailed)
	}
	if _, ok := m.tree.Find(model.ProjectScope("demo-project")); !ok || m.tree.ID == model.ProjectScope("demo-project") {
		t.Fatalf("partial tree not applied: %q", m.tree.ID)
	}
	want := "Resource hierarchy may be incomplete: quota exceeded; refresh to retry"
	if got := warnMessages(m); !slices.Contains(got, want) {
		t.Fatalf("warnings %q, want %q", got, want)
	}

	next, cmd := m.Update(keyText("r"))
	m = drain(next.(Model), cmd)
	if calls != 2 || m.hierFailed {
		t.Fatalf("calls=%d hierFailed=%v; refresh should rebuild the partial tree", calls, m.hierFailed)
	}
}

func TestProviderErrorKeepsOtherServices(t *testing.T) {
	t.Parallel()
	boom := errors.New("permission denied")
	m := sized(newTestModel())
	before := len(m.catalog)
	m.applyLoaded(m.scope.ID, model.ProviderResult{Service: model.ServiceGKE, Err: boom})
	m.finishLoad(m.scope.ID)
	if !strings.Contains(m.status.Flash, "permission denied") {
		t.Fatalf("flash=%q", m.status.Flash)
	}
	for _, r := range m.catalog {
		if r.Service == model.ServiceGKE && r.ScopeID == "projects/demo-project" {
			t.Fatalf("errored service kept stale rows: %s", r.ID)
		}
	}
	if len(m.catalog) == 0 || len(m.catalog) >= before {
		t.Fatalf("catalog %d → %d", before, len(m.catalog))
	}
	m.nav.GoTop()
	if !strings.Contains(m.nav.View(), "Cloud Storage") {
		t.Fatalf("other services must stay:\n%s", m.nav.View())
	}
}

// deniedGKE is a catalog whose GKE provider fails with a 403 naming the
// missing permission; storage succeeds.
func deniedGKE() gcp.Catalog {
	return gcp.NewCatalog(
		fake.Static{
			Svc: model.ServiceCloudStorage,
			Items: []model.Resource{
				{ID: "projects/demo-project/buckets/b", Name: "my-app-bucket", Service: model.ServiceCloudStorage, ScopeID: "projects/demo-project"},
			},
		},
		fake.Static{Svc: model.ServiceGKE, Err: &googleapi.Error{Code: 403, Message: "Required 'container.clusters.list' permission"}},
	).WithHierarchy(fake.Hierarchy())
}

// scopeRow returns the explorer row for m.scope.
func scopeRow(t *testing.T, m Model) resources.Row {
	t.Helper()
	for _, r := range m.nav.Rows() {
		if r.Kind == resources.RowResource && r.Resource.ID == m.scope.ID {
			return r
		}
	}
	t.Fatalf("no row for scope %q", m.scope.ID)
	return resources.Row{}
}

// errorMessages returns the text of each error in the messages pane.
func errorMessages(m Model) []string {
	var out []string
	for _, e := range m.messages.Entries() {
		if e.Level == messages.Error {
			out = append(out, e.Text)
		}
	}
	return out
}

func TestPermissionErrorGoesToMessages(t *testing.T) {
	t.Parallel()
	m := sized(load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", deniedGKE(), testIdent())))

	if !scopeRow(t, m).Problems {
		t.Fatal("scope row should carry the problem mark")
	}
	if !strings.Contains(m.status.Flash, "missing container.clusters.list") {
		t.Fatalf("flash=%q", m.status.Flash)
	}
	want := "demo-project · Kubernetes Engine: missing container.clusters.list"
	if msgs := errorMessages(m); !slices.Contains(msgs, want) {
		t.Fatalf("messages %q, want %q", msgs, want)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 240, Height: 40})
	m = next.(Model)
	m.nav.GoTop()
	nav := ansi.Strip(m.nav.View())
	if !strings.Contains(nav, "Cloud Storage") || strings.Contains(nav, "container.clusters.list") {
		t.Fatalf("explorer should list resources but not errors:\n%s", nav)
	}
	if v := ansi.Strip(m.render()); !strings.Contains(v, "Messages (") || !strings.Contains(v, "Kubernetes Engine: missing") {
		t.Fatalf("messages pane missing the error:\n%s", v)
	}

	m.focus = PaneNav
	m.nav.SelectKey(m.scope.ID)
	m.syncDetails()
	if d := m.details.View(); !strings.Contains(d, "Problems") || !strings.Contains(d, "missing container.clusters.list") {
		t.Fatalf("details:\n%s", d)
	}
}

// partialLB lists a load balancer beside a denied proxy kind.
type partialLB struct{}

func (partialLB) Service() model.ServiceType { return model.ServiceLoadBalancer }

func (partialLB) List(_ context.Context, scopeID string) ([]model.Resource, error) {
	r := model.Resource{ID: scopeID + "/global/urlMaps/web", Name: "web-lb", Service: model.ServiceLoadBalancer, ScopeID: scopeID}
	return []model.Resource{r}, model.Partial(&googleapi.Error{Code: 403, Message: "Required 'compute.targetSslProxies.list' permission"})
}

// A partial list keeps its items and is a warning, not an error.
func TestPartialListIsAWarning(t *testing.T) {
	t.Parallel()
	cat := gcp.NewCatalog(partialLB{}).WithHierarchy(fake.Hierarchy())
	m := sized(load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", cat, testIdent())))

	if !slices.ContainsFunc(m.catalog, func(r model.Resource) bool { return r.Name == "web-lb" }) {
		t.Fatal("partial list dropped its items")
	}
	want := "demo-project · Load Balancers: partial list: missing compute.targetSslProxies.list"
	if msgs := warnMessages(m); !slices.Contains(msgs, want) {
		t.Fatalf("warnings %q, want %q", msgs, want)
	}
	if msgs := errorMessages(m); len(msgs) != 0 {
		t.Fatalf("errors %q, want none", msgs)
	}
	if !strings.Contains(m.status.Flash, "partial list") {
		t.Fatalf("flash=%q", m.status.Flash)
	}
}

func TestProviderErrorPersistsUntilProviderRecovers(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	denied := model.ProviderError{Service: model.ServiceGKE, Kind: model.ErrKindPermissionDenied, Message: "missing container.clusters.list"}
	m.applyLoaded(m.scope.ID, model.ProviderResult{Service: model.ServiceGKE, Err: denied})
	m.finishLoad(m.scope.ID)
	m.rebuildOutline()
	if !scopeRow(t, m).Problems {
		t.Fatal("error should persist after the load completes")
	}
	m.applyLoaded(m.scope.ID, model.ProviderResult{Service: model.ServiceGKE, Err: denied})
	if got := len(m.outline.errs[scopeKey(m.scope.ID)]); got != 1 {
		t.Fatalf("a repeat error should replace, not duplicate: %d", got)
	}
	m.applyLoaded(m.scope.ID, model.ProviderResult{Service: model.ServiceGKE})
	m.finishLoad(m.scope.ID)
	if scopeRow(t, m).Problems {
		t.Fatal("recovered provider kept its problem mark")
	}
	if !strings.HasPrefix(m.status.Flash, "loaded ") {
		t.Fatalf("flash=%q", m.status.Flash)
	}
}

func TestFlashCountsSeveralErrors(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	for _, svc := range []model.ServiceType{model.ServiceGKE, model.ServicePubSub, model.ServiceBigQuery} {
		m.applyLoaded(m.scope.ID, model.ProviderResult{Service: svc, Err: model.ProviderError{Service: svc, Kind: model.ErrKindQuota, Message: "quota exceeded"}})
	}
	m.finishLoad(m.scope.ID)
	if m.status.Flash != "3 provider errors; see messages" {
		t.Fatalf("flash=%q", m.status.Flash)
	}
	if got := errorMessages(m); len(got) != 3 {
		t.Fatalf("error messages %q", got)
	}
}

func TestCollectionDetailsShowProblems(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m.applyLoaded(m.scope.ID, model.ProviderResult{
		Service: model.ServiceCloudStorage,
		Resources: []model.Resource{
			{ID: "projects/demo-project/buckets/b", Name: "my-app-bucket", Service: model.ServiceCloudStorage, ScopeID: "projects/demo-project"},
		},
		Err: model.ProviderError{Service: model.ServiceCloudStorage, Kind: model.ErrKindTimeout, Message: "timed out"},
	})
	m.finishLoad(m.scope.ID)
	m = jumpTo(m, "Cloud Storage")
	if got := m.problems(mustSelected(t, m)); len(got) != 1 || got[0].Kind != model.ErrKindTimeout {
		t.Fatalf("collection problems %+v", got)
	}
	m = jumpTo(m, "Pub/Sub Topics")
	if got := m.problems(mustSelected(t, m)); len(got) != 0 {
		t.Fatalf("other collection problems %+v", got)
	}
	if strings.Contains(m.details.View(), "Problems") {
		t.Fatalf("details should hide Problems:\n%s", m.details.View())
	}
}

func TestFacetError(t *testing.T) {
	t.Parallel()
	m := openBucket(sized(newTestModel()))
	bucket := mustSelected(t, m).Resource
	m.outline.showFacets[bucket.ID] = true
	m.outline.related[facetLoadKey(bucket.ID, model.FacetObjects)] = relatedReq{parent: bucket, facet: model.Facet{ID: model.FacetObjects, Label: "Objects"}}
	denied := model.ProviderError{Service: model.ServiceCloudStorage, Kind: model.ErrKindPermissionDenied, Message: "missing storage.objects.list"}
	m.applyRelated(relatedLoadedMsg{parentID: bucket.ID, facetID: model.FacetObjects, result: model.ProviderResult{Service: model.ServiceCloudStorage, Err: denied}})
	m.finishRelated(relatedDoneMsg{parentID: bucket.ID, facetID: model.FacetObjects})
	if m.status.Flash != denied.Error() {
		t.Fatalf("flash=%q", m.status.Flash)
	}
	want := bucket.Name + " › Objects · Cloud Storage: missing storage.objects.list"
	if msgs := errorMessages(m); !slices.Contains(msgs, want) {
		t.Fatalf("messages %q, want %q", msgs, want)
	}
	m.focus = PaneNav
	m.nav.SelectKey(facetRowKey(bucket.ID, model.FacetObjects))
	row := mustSelected(t, m)
	if row.Kind != resources.RowFacet || !row.Problems || row.Expandable {
		t.Fatalf("failed facet row %+v", row)
	}
	if got := m.problems(row); len(got) != 1 || got[0] != denied {
		t.Fatalf("facet problems %+v", got)
	}
	before := len(m.nav.Rows())
	m.status.SetFlash("")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.nav.Rows()) != before || m.status.Flash != denied.Error() {
		t.Fatalf("Enter on a failed facet: %d → %d rows, flash %q", before, len(m.nav.Rows()), m.status.Flash)
	}
}

func TestTruncatedFacetShowsNoteRow(t *testing.T) {
	t.Parallel()
	m := openBucket(sized(newTestModel()))
	bucket := mustSelected(t, m).Resource
	m.outline.showFacets[bucket.ID] = true
	objs := model.Children(fake.Resources(), bucket.ID, model.FacetObjects)
	deliver := func(m Model, truncated bool) Model {
		m.applyRelated(relatedLoadedMsg{parentID: bucket.ID, facetID: model.FacetObjects, result: model.ProviderResult{Service: model.ServiceCloudStorage, Resources: objs, Truncated: truncated}})
		m.finishRelated(relatedDoneMsg{parentID: bucket.ID, facetID: model.FacetObjects})
		return m
	}
	notes := func(m Model) (idx []int, last int) {
		last = -1
		for i, r := range m.nav.Rows() {
			if r.Kind == resources.RowNote {
				idx = append(idx, i)
			}
			if r.Depth > 0 && (r.Resource.FacetID == model.FacetObjects || r.Resource.IsPrefix()) {
				last = i
			}
		}
		return idx, last
	}

	m = deliver(m, true)
	idx, lastChild := notes(m)
	if len(idx) != 1 || idx[0] != lastChild+1 {
		t.Fatalf("note rows %v, last child %d", idx, lastChild)
	}
	note := m.nav.Rows()[idx[0]]
	if note.Resource.ID != bucket.ID || note.Facet.ID != model.FacetObjects {
		t.Fatalf("note owner %+v", note)
	}
	if want := fmt.Sprintf("… more not shown (limit %d)", len(objs)); note.Note != want {
		t.Fatalf("note %q want %q", note.Note, want)
	}

	m = deliver(m, false)
	if idx, _ := notes(m); len(idx) != 0 {
		t.Fatalf("note should clear on an untruncated reload: %v", idx)
	}
}

func TestRefreshTwiceKeepsOneLoadPerKey(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m = expandNamed(m, "Enabled Services")
	enKey := facetLoadKey(model.EnabledServicesResource(model.Resource{ID: m.scope.ID}).ID, model.FacetEnabledServices)
	if !m.outline.loaded(enKey) {
		t.Fatalf("enabled services should be loaded under %v", enKey)
	}

	m = press(m, keyText("r"))
	first := maps.Clone(m.outline.inflight)
	if _, ok := first[scopeKey(m.scope.ID)]; !ok {
		t.Fatal("refresh should reload the scope")
	}
	if _, ok := first[enKey]; !ok {
		t.Fatal("refresh should reload the enabled services facet")
	}

	next, cmd := m.Update(keyText("r"))
	m = next.(Model)
	if len(m.outline.inflight) != len(first) {
		t.Fatalf("inflight after second refresh=%d, want %d", len(m.outline.inflight), len(first))
	}
	for key, old := range first {
		cur, ok := m.outline.inflight[key]
		if !ok {
			t.Fatalf("%v dropped by second refresh", key)
		}
		if cur.gen == old.gen {
			t.Fatalf("%v should restart with a new generation", key)
		}
		if old.ctx.Err() == nil {
			t.Fatalf("%v: first load should be canceled", key)
		}
		if cur.ctx.Err() != nil {
			t.Fatalf("%v: second load should be running", key)
		}
	}

	stale := first[scopeKey(m.scope.ID)].gen
	next, _ = m.Update(scopeLoadDoneMsg{scopeID: m.scope.ID, gen: stale})
	m = next.(Model)
	if !m.outline.loading(scopeKey(m.scope.ID)) {
		t.Fatal("a canceled load's done message must not end the current load")
	}

	m = drain(m, cmd)
	if len(m.outline.inflight) != 0 {
		t.Fatalf("inflight after drain: %v", m.outline.inflight)
	}
}

func TestExpandWhileLoadingDoesNotDuplicate(t *testing.T) {
	t.Parallel()
	m := sized(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", gcp.FakeCatalog(), testIdent()))
	key := scopeKey(m.scope.ID)
	gen := m.outline.inflight[key].gen
	if cmd := m.startScope(m.scope.ID, false); cmd != nil {
		t.Fatal("a second load for an in-flight scope should be dropped")
	}
	if got := m.outline.inflight[key].gen; got != gen {
		t.Fatalf("gen=%d, want %d", got, gen)
	}
}

func TestLoadingRowUntilDone(t *testing.T) {
	t.Parallel()
	m := sized(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", gcp.FakeCatalog(), testIdent()))
	scopeRow := func() resources.Row {
		t.Helper()
		for _, r := range m.nav.Rows() {
			if r.Kind == resources.RowResource && r.Resource.ID == m.scope.ID {
				return r
			}
		}
		t.Fatalf("no row for %s", m.scope.ID)
		return resources.Row{}
	}
	if !scopeRow().Loading {
		t.Fatal("scope row should show loading before any result")
	}
	if !strings.Contains(ansi.Strip(m.nav.View()), "demo-project …") {
		t.Fatalf("explorer should mark the loading scope:\n%s", m.nav.View())
	}

	gen := m.outline.inflight[scopeKey(m.scope.ID)].gen
	cmd := m.loadCmd(context.Background(), gen, m.scope.ID)
	for {
		msg := cmd()
		if _, done := msg.(scopeLoadDoneMsg); done {
			if !scopeRow().Loading {
				t.Fatal("scope row should stay loading until its done message")
			}
			next, _ := m.Update(msg)
			m = next.(Model)
			break
		}
		next, c := m.Update(msg)
		m = next.(Model)
		cmd = c
	}
	if scopeRow().Loading {
		t.Fatal("scope row should stop loading after its done message")
	}
}

func TestQuitCancelsLoads(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		prep func(Model) Model
		key  tea.KeyPressMsg
	}{
		{"q", func(m Model) Model { return m }, keyText("q")},
		{"ctrl+c", func(m Model) Model { return m }, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}},
		{"ctrl+c in help", func(m Model) Model { return press(m, keyText("?")) }, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}},
		{"ctrl+c in copy", func(m Model) Model { return press(m, keyText("c")) }, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := tc.prep(press(sized(newTestModel()), keyText("r")))
			started := maps.Clone(m.outline.inflight)
			if len(started) == 0 {
				t.Fatal("refresh should leave loads in flight")
			}
			next, cmd := m.Update(tc.key)
			m = next.(Model)
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("expected quit")
			}
			if len(m.outline.inflight) != 0 {
				t.Fatalf("inflight after quit: %v", m.outline.inflight)
			}
			for key, l := range started {
				if l.ctx.Err() == nil {
					t.Fatalf("%v not canceled", key)
				}
			}
		})
	}
}

// denyAll is a permission tester that grants nothing.
type denyAll struct{}

func (denyAll) TestPermissions(context.Context, string, []string) ([]string, error) { return nil, nil }

// A provider skipped for missing permissions is a details section only:
// no explorer row and no error.
func TestMissingPermissionIsQuiet(t *testing.T) {
	t.Parallel()
	cat := gcp.NewCatalog(
		fake.Static{
			Svc: model.ServiceCloudStorage,
			Items: []model.Resource{
				{ID: "projects/demo-project/buckets/b", Name: "my-app-bucket", Service: model.ServiceCloudStorage, ScopeID: "projects/demo-project"},
			},
		},
		fake.Static{Svc: model.ServiceGKE, Permissions: []string{"container.clusters.list"}},
	).WithHierarchy(fake.Hierarchy()).WithPermissionTester(denyAll{})
	m := sized(load(New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", cat, testIdent())))

	if scopeRow(t, m).Problems {
		t.Fatal("a missing permission should not mark the scope")
	}
	if msgs := errorMessages(m); len(msgs) != 0 {
		t.Fatalf("messages %q, want none", msgs)
	}
	for _, r := range m.nav.Rows() {
		if r.Kind == resources.RowNote {
			t.Fatalf("explorer note %q, want none", r.Note)
		}
	}

	m.focus = PaneNav
	m.nav.SelectKey(m.scope.ID)
	m.syncDetails()
	d := ansi.Strip(m.details.View())
	if !strings.Contains(d, "No access") || !strings.Contains(d, "missing container.clusters.list") || strings.Contains(d, "Problems") {
		t.Fatalf("details:\n%s", d)
	}
}

// A later report without Missing clears the denial, as after a refresh
// once access is granted.
func TestMissingPermissionClearsOnRecovery(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m.applyLoaded(m.scope.ID, model.ProviderResult{Service: model.ServiceGKE, Missing: []string{"container.clusters.list"}})
	m.rebuildOutline()
	if got := m.outline.denials(scopeKey(m.scope.ID)); len(got) != 1 || got[0].Service != model.ServiceGKE {
		t.Fatalf("denials %+v", got)
	}
	m.applyLoaded(m.scope.ID, model.ProviderResult{Service: model.ServiceGKE})
	m.rebuildOutline()
	if got := m.outline.denials(scopeKey(m.scope.ID)); got != nil {
		t.Fatalf("denials %+v after recovery", got)
	}
}

// The fixture catalog denies Cloud SQL and Secrets on checkout-project.
// The denial shows in the details pane, not as an explorer row.
func TestFixtureCheckoutProjectNoAccess(t *testing.T) {
	t.Parallel()
	m := expandID(sized(newTestModel()), "projects/checkout-project")
	for _, r := range m.nav.Rows() {
		if r.Kind == resources.RowNote && r.Resource.ID == "projects/checkout-project" {
			t.Fatalf("explorer note %q, want none", r.Note)
		}
	}
	var got []model.ServiceType
	for _, d := range m.outline.denials(scopeKey("projects/checkout-project")) {
		got = append(got, d.Service)
	}
	if want := []model.ServiceType{model.ServiceCloudSQL, model.ServiceSecretManager}; !slices.Equal(got, want) {
		t.Fatalf("denials %v, want %v", got, want)
	}
}

// warnMessages returns the text of each warning in the messages pane.
func warnMessages(m Model) []string {
	var out []string
	for _, e := range m.messages.Entries() {
		if e.Level == messages.Warn {
			out = append(out, e.Text)
		}
	}
	return out
}

// An API off in the quota project is one warning with the fix, however
// many scopes report it, and marks no scope.
func TestQuotaProjectAPIIsOneWarning(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	before := len(errorMessages(m))
	pe := model.ProviderError{
		Service: model.ServiceServiceAccount, Kind: model.ErrKindAPIDisabled,
		Message: "enable iam.googleapis.com in project ops", API: "iam.googleapis.com", Project: "ops", QuotaProject: true,
	}
	for _, scope := range []string{m.scope.ID, "projects/checkout-project"} {
		m.applyLoaded(scope, model.ProviderResult{Service: model.ServiceServiceAccount, Err: pe})
		m.finishLoad(scope)
	}
	var quota []string
	for _, w := range warnMessages(m) {
		if strings.Contains(w, "quota project") {
			quota = append(quota, w)
		}
	}
	want := "iam.googleapis.com is not enabled in your quota project ops, so Service Accounts are hidden. " +
		"Enable it there, or bill another project: gcloud auth application-default set-quota-project PROJECT"
	if !slices.Equal(quota, []string{want}) {
		t.Fatalf("warnings %q, want %q", quota, want)
	}
	if got := len(errorMessages(m)); got != before {
		t.Fatalf("error messages %d, want %d", got, before)
	}
	if scopeRow(t, m).Problems {
		t.Fatal("a quota project problem should not mark the scope")
	}
}
