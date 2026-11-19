package gcp

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"google.golang.org/api/googleapi"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/serviceusage"
	"github.com/tensleyconsulting/seiner/internal/gcp/fake"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// stubLister is a Service Usage stand-in that reports a fixed enabled set.
type stubLister struct {
	fake.Static
	apis  []string
	err   error
	calls *atomic.Int32
}

func (s stubLister) EnabledAPIs(_ context.Context, project string) ([]string, error) {
	s.calls.Add(1)
	if project != "p" {
		return nil, errors.New("unexpected project " + project)
	}
	return s.apis, s.err
}

// enabledAPI is a Service Usage API whose enabled set the test can change.
type enabledAPI struct {
	mu    sync.Mutex
	svcs  []serviceusage.APIService
	calls int
}

func (a *enabledAPI) ListEnabled(context.Context, string) ([]serviceusage.APIService, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	return a.svcs, nil
}

func (a *enabledAPI) set(names ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.svcs = nil
	for _, n := range names {
		a.svcs = append(a.svcs, serviceusage.APIService{Name: n})
	}
}

// Refresh drops the project's cached enabled set, so an API enabled since
// the last list ungates its provider; a re-list within the TTL does not.
func TestInvalidateRereadsEnabledAPIs(t *testing.T) {
	t.Parallel()
	api := &enabledAPI{}
	api.set("storage.googleapis.com")
	var gkeCalls atomic.Int32
	cat := NewCatalog(
		counting{Static: fake.Static{Svc: model.ServiceGKE}, calls: &gkeCalls},
		serviceusage.New(api),
	)

	steps := []struct {
		name       string
		enable     bool // enable container.googleapis.com first
		invalidate string
		wantGKE    int32
		wantLookup int
	}{
		{name: "disabled API is gated", wantGKE: 0, wantLookup: 1},
		{name: "enabled set is cached", enable: true, wantGKE: 0, wantLookup: 1},
		{name: "other project leaves it cached", invalidate: "projects/p2", wantGKE: 0, wantLookup: 1},
		{name: "refresh rereads it", invalidate: "projects/p", wantGKE: 1, wantLookup: 2},
	}
	for _, s := range steps {
		if s.enable {
			api.set("storage.googleapis.com", "container.googleapis.com")
		}
		if s.invalidate != "" {
			cat.Invalidate(s.invalidate)
		}
		cat.List(context.Background(), "projects/p")
		if n := gkeCalls.Load(); n != s.wantGKE {
			t.Fatalf("%s: GKE List calls=%d want %d", s.name, n, s.wantGKE)
		}
		api.mu.Lock()
		lookups := api.calls
		api.mu.Unlock()
		if lookups != s.wantLookup {
			t.Fatalf("%s: ListEnabled calls=%d want %d", s.name, lookups, s.wantLookup)
		}
	}
}

func TestListEachGatesDisabledAPIs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		scope      string
		noLister   bool
		listerErr  error
		wantGKE    int32
		wantLookup int32
	}{
		{name: "project skips disabled", scope: "projects/p", wantGKE: 0, wantLookup: 1},
		{name: "bare project id skips disabled", scope: "p", wantGKE: 0, wantLookup: 1},
		{name: "org is not gated", scope: "organizations/1", wantGKE: 1, wantLookup: 0},
		{name: "folder is not gated", scope: "folders/2", wantGKE: 1, wantLookup: 0},
		{name: "lookup error fails open", scope: "projects/p", listerErr: errors.New("403"), wantGKE: 1, wantLookup: 1},
		{name: "no lister fails open", scope: "projects/p", noLister: true, wantGKE: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gcsCalls, gkeCalls, iamCalls, lookups atomic.Int32
			providers := []model.ResourceProvider{
				counting{Static: fake.Static{Svc: model.ServiceCloudStorage}, calls: &gcsCalls},
				counting{Static: fake.Static{Svc: model.ServiceGKE}, calls: &gkeCalls},
				counting{Static: fake.Static{Svc: model.ServiceIAM}, calls: &iamCalls},
			}
			if !tc.noLister {
				providers = append(providers, stubLister{
					Static: fake.Static{Svc: model.ServiceUsage},
					apis:   []string{"storage.googleapis.com"},
					err:    tc.listerErr,
					calls:  &lookups,
				})
			}
			var (
				mu  sync.Mutex
				got = map[model.ServiceType]model.ProviderResult{}
			)
			NewCatalog(providers...).ListEach(context.Background(), tc.scope, func(r model.ProviderResult) {
				mu.Lock()
				defer mu.Unlock()
				got[r.Service] = r
			})
			if n := gkeCalls.Load(); n != tc.wantGKE {
				t.Errorf("GKE List calls=%d want %d", n, tc.wantGKE)
			}
			if n := gcsCalls.Load(); n != 1 {
				t.Errorf("storage List calls=%d want 1", n)
			}
			if n := iamCalls.Load(); n != 1 {
				t.Errorf("IAM List calls=%d want 1", n)
			}
			if n := lookups.Load(); n != tc.wantLookup {
				t.Errorf("EnabledAPIs calls=%d want %d", n, tc.wantLookup)
			}
			// A gated provider still reports, with no rows and no error.
			if r, ok := got[model.ServiceGKE]; !ok || r.Err != nil || len(r.Resources) != 0 {
				t.Errorf("GKE result=%+v reported=%v", r, ok)
			}
			if _, ok := got[model.ServiceUsage]; !ok && !tc.noLister {
				t.Error("Service Usage provider did not report")
			}
		})
	}
}

func TestFakeCatalogGatingKeepsFixtures(t *testing.T) {
	t.Parallel()
	root, err := FakeCatalog().Hierarchy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gated := FakeCatalog()
	ungated := NewCatalog(fake.Providers()...)
	var walk func(model.HierarchyNode)
	walk = func(n model.HierarchyNode) {
		want := ungated.List(context.Background(), n.ID)
		got := gated.List(context.Background(), n.ID)
		if len(got.Resources) != len(want.Resources) || len(got.Errors) != 0 {
			t.Errorf("%s: gated %d resources (errors %v), ungated %d", n.ID, len(got.Resources), got.Errors, len(want.Resources))
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}

// stubTester grants the permissions in granted and records each check.
type stubTester struct {
	granted []string
	err     error
	mu      *sync.Mutex
	asked   *[][]string
}

func (s stubTester) TestPermissions(ctx context.Context, scopeID string, perms []string) ([]string, error) {
	s.mu.Lock()
	*s.asked = append(*s.asked, slices.Clone(perms))
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return slices.DeleteFunc(slices.Clone(perms), func(p string) bool { return !slices.Contains(s.granted, p) }), nil
}

func TestListEachGatesMissingPermissions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		scope       string
		noTester    bool
		testerErr   error
		wantGKE     int32
		wantMissing []string
		wantAsked   [][]string
	}{
		{name: "missing permission skips", scope: "projects/p", wantGKE: 0, wantMissing: []string{"container.clusters.list"},
			wantAsked: [][]string{{"container.clusters.list", "storage.buckets.list"}}},
		// The project-only declarations are nil on a folder: nothing to check.
		{name: "nothing declared", scope: "folders/2", wantGKE: 1},
		{name: "check error fails open", scope: "projects/p", testerErr: errors.New("403"), wantGKE: 1,
			wantAsked: [][]string{{"container.clusters.list", "storage.buckets.list"}}},
		{name: "no tester fails open", scope: "projects/p", noTester: true, wantGKE: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gcsCalls, gkeCalls, plainCalls atomic.Int32
			var mu sync.Mutex
			var asked [][]string
			cat := NewCatalog(
				counting{Static: fake.Static{Svc: model.ServiceCloudStorage, Permissions: []string{"storage.buckets.list"}}, calls: &gcsCalls},
				counting{Static: fake.Static{Svc: model.ServiceGKE, Permissions: []string{"container.clusters.list"}}, calls: &gkeCalls},
				counting{Static: fake.Static{Svc: model.ServiceIAM}, calls: &plainCalls},
			)
			if !tc.noTester {
				cat = cat.WithPermissionTester(stubTester{granted: []string{"storage.buckets.list"}, err: tc.testerErr, mu: &mu, asked: &asked})
			}
			got := map[model.ServiceType]model.ProviderResult{}
			cat.ListEach(context.Background(), tc.scope, func(r model.ProviderResult) {
				mu.Lock()
				defer mu.Unlock()
				got[r.Service] = r
			})
			if n := gkeCalls.Load(); n != tc.wantGKE {
				t.Errorf("GKE List calls=%d want %d", n, tc.wantGKE)
			}
			if gcsCalls.Load() != 1 || plainCalls.Load() != 1 {
				t.Errorf("storage calls=%d undeclared calls=%d, want 1 each", gcsCalls.Load(), plainCalls.Load())
			}
			r := got[model.ServiceGKE]
			if !slices.Equal(r.Missing, tc.wantMissing) || r.Err != nil {
				t.Errorf("GKE result %+v, want missing %v", r, tc.wantMissing)
			}
			if !slices.EqualFunc(asked, tc.wantAsked, slices.Equal) {
				t.Errorf("asked %v, want %v", asked, tc.wantAsked)
			}
		})
	}
}

// A provider gated off for a disabled API is not part of the check.
func TestPermissionCheckSkipsDisabledAPIs(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var asked [][]string
	var lookups atomic.Int32
	NewCatalog(
		fake.Static{Svc: model.ServiceCloudStorage, Permissions: []string{"storage.buckets.list"}},
		fake.Static{Svc: model.ServiceGKE, Permissions: []string{"container.clusters.list"}},
		stubLister{Static: fake.Static{Svc: model.ServiceUsage}, apis: []string{"storage.googleapis.com"}, calls: &lookups},
	).WithPermissionTester(stubTester{mu: &mu, asked: &asked}).
		ListEach(context.Background(), "projects/p", func(model.ProviderResult) {})
	if want := [][]string{{"storage.buckets.list"}}; !slices.EqualFunc(asked, want, slices.Equal) {
		t.Fatalf("asked %v, want %v", asked, want)
	}
}

// A scope served from the cache makes no permission check; a skipped
// provider is cached like any result.
func TestPermissionCheckOnlyOnCacheMiss(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var asked [][]string
	cat := NewCatalog(
		fake.Static{Svc: model.ServiceGKE, Permissions: []string{"container.clusters.list"}},
	).WithPermissionTester(stubTester{mu: &mu, asked: &asked})
	for range 2 {
		var missing []string
		cat.ListEach(context.Background(), "projects/p", func(r model.ProviderResult) { missing = r.Missing })
		if !slices.Equal(missing, []string{"container.clusters.list"}) {
			t.Fatalf("missing %v", missing)
		}
	}
	if len(asked) != 1 {
		t.Fatalf("checks %d, want 1", len(asked))
	}
	cat.Invalidate("projects/p")
	cat.ListEach(context.Background(), "projects/p", func(model.ProviderResult) {})
	if len(asked) != 2 {
		t.Fatalf("checks %d after refresh, want 2", len(asked))
	}
}

// Org Policy calls its own API, so a project that has not enabled it lists
// nothing and reports no error. Org and folder scopes are not gated.
func TestOrgPolicyGatedOnItsAPI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		scope string
		want  int32
	}{
		{"projects/p", 0},
		{"organizations/1", 1},
	} {
		var calls, lookups atomic.Int32
		var errs []error
		var mu sync.Mutex
		NewCatalog(
			counting{Static: fake.Static{Svc: model.ServiceOrgPolicy}, calls: &calls},
			stubLister{Static: fake.Static{Svc: model.ServiceUsage}, apis: []string{"storage.googleapis.com"}, calls: &lookups},
		).ListEach(context.Background(), tc.scope, func(r model.ProviderResult) {
			mu.Lock()
			defer mu.Unlock()
			if r.Err != nil {
				errs = append(errs, r.Err)
			}
		})
		if n := calls.Load(); n != tc.want {
			t.Errorf("%s: org policy List calls=%d want %d", tc.scope, n, tc.want)
		}
		if len(errs) != 0 {
			t.Errorf("%s: errors %v", tc.scope, errs)
		}
	}
}

// IAM lists in projects whose enabled services lack iam.googleapis.com, so
// service accounts and custom roles are not gated on it.
func TestIAMNotGatedOnItsAPI(t *testing.T) {
	t.Parallel()
	for _, svc := range []model.ServiceType{model.ServiceServiceAccount, model.ServiceCustomRole, model.ServiceIAMBinding} {
		var calls, lookups atomic.Int32
		NewCatalog(
			counting{Static: fake.Static{Svc: svc}, calls: &calls},
			stubLister{Static: fake.Static{Svc: model.ServiceUsage}, apis: []string{"storage.googleapis.com"}, calls: &lookups},
		).ListEach(context.Background(), "projects/p", func(model.ProviderResult) {})
		if n := calls.Load(); n != 1 {
			t.Errorf("%s List calls=%d want 1", svc, n)
		}
	}
}

// disabledErr is an API-disabled error for api naming project, as Google
// words it.
func disabledErr(api, project string) error {
	return &googleapi.Error{Code: 403, Message: "API has not been used in project " + project +
		" before or it is disabled. Enable it by visiting https://console.developers.google.com/apis/api/" + api + "/overview?project=" + project + " then retry."}
}

// An API-disabled error naming the listed project lists nothing quietly;
// one naming the quota project is a setup error marked QuotaProject; any
// other stays an ordinary error. A client-based API names the quota
// project even when it is also the listed project.
func TestListEachPlacesDisabledAPIs(t *testing.T) {
	t.Parallel()
	tree := model.HierarchyNode{ID: "organizations/1", Kind: model.KindOrg, Children: []model.HierarchyNode{
		{ID: "projects/p", Kind: model.KindProject, Number: "111"},
		{ID: "projects/q", Kind: model.KindProject, Number: "222"},
	}}
	cases := []struct {
		name      string
		svc       model.ServiceType
		api       string
		named     string
		quota     string
		wantErr   bool
		wantQuota bool
	}{
		{name: "listed project by id", svc: model.ServiceGKE, api: "container.googleapis.com", named: "p", quota: "q"},
		{name: "listed project by number", svc: model.ServiceGKE, api: "container.googleapis.com", named: "111", quota: "q"},
		{name: "quota project", svc: model.ServiceGKE, api: "container.googleapis.com", named: "q", quota: "q", wantErr: true, wantQuota: true},
		{name: "quota project by number", svc: model.ServiceServiceAccount, api: "iam.googleapis.com", named: "222", quota: "q", wantErr: true, wantQuota: true},
		{name: "client-based in shared project", svc: model.ServiceServiceAccount, api: "iam.googleapis.com", named: "p", quota: "p", wantErr: true, wantQuota: true},
		{name: "resource-based in shared project", svc: model.ServiceGKE, api: "container.googleapis.com", named: "p", quota: "p"},
		{name: "unknown quota project", svc: model.ServiceServiceAccount, api: "iam.googleapis.com", named: "p"},
		{name: "another project", svc: model.ServiceGKE, api: "container.googleapis.com", named: "other", quota: "q", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got model.ProviderResult
			NewCatalog(fake.Static{Svc: tc.svc, Err: disabledErr(tc.api, tc.named)}).
				WithHierarchy(tree).
				WithQuotaProject(tc.quota).
				ListEach(context.Background(), "projects/p", func(r model.ProviderResult) { got = r })
			if (got.Err != nil) != tc.wantErr {
				t.Fatalf("err %v, want error %v", got.Err, tc.wantErr)
			}
			if got.Err == nil {
				return
			}
			var pe model.ProviderError
			if !errors.As(got.Err, &pe) || pe.QuotaProject != tc.wantQuota || pe.API != tc.api || pe.Project != tc.named {
				t.Fatalf("error %+v, want QuotaProject %v", pe, tc.wantQuota)
			}
		})
	}
}

// An API an organization policy denies in the listed project (or on an
// enclosing folder/org without naming a project) lists nothing quietly, as a
// disabled one does.
func TestListEachHidesRestrictedAPIs(t *testing.T) {
	t.Parallel()
	folderRestricted := &googleapi.Error{
		Code:    403,
		Message: "Request is disallowed by organization's constraints/gcp.restrictServiceUsage constraint for 'folders/123' attempting to use service 'bigquery.googleapis.com'.",
	}
	for _, err := range []error{
		restrictedErr(t, "bigquery.googleapis.com", "p"),
		folderRestricted,
	} {
		var got model.ProviderResult
		NewCatalog(fake.Static{Svc: model.ServiceBigQueryDataset, Err: err}).
			ListEach(context.Background(), "projects/p", func(r model.ProviderResult) { got = r })
		if got.Err != nil || len(got.Resources) != 0 {
			t.Fatalf("got %+v, want an empty result and no error", got)
		}
	}
}

// A quietly empty result is cached like a gated one, so a second expand
// makes no call.
func TestDisabledInScopeIsCached(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	cat := NewCatalog(counting{Static: fake.Static{Svc: model.ServiceGKE, Err: disabledErr("container.googleapis.com", "p")}, calls: &calls})
	for range 2 {
		cat.ListEach(context.Background(), "projects/p", func(model.ProviderResult) {})
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("List calls=%d want 1", n)
	}
}
