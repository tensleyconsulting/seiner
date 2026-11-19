package gcp

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/api/googleapi"

	"github.com/tensleyconsulting/seiner/internal/gcp/fake"
	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestFakeCatalogListsAll(t *testing.T) {
	t.Parallel()
	res := FakeCatalog().List(context.Background(), "projects/demo-project")
	if len(res.Errors) != 0 {
		t.Fatalf("errors: %v", res.Errors)
	}
	want := 0
	for _, r := range fake.ResourcesFor("projects/demo-project") {
		if r.ParentID == "" {
			want++
		}
	}
	inherited := map[string]bool{}
	for _, r := range res.Resources {
		if from := r.Metadata[model.MetaInheritedFrom]; from != "" {
			inherited[from] = true
		}
	}
	// demo-project inherits bindings from every ancestor that has one.
	for _, from := range []string{"folders/prod-apps", "folders/prod", "folders/platform", "organizations/example-org"} {
		if !inherited[from] {
			t.Errorf("no binding inherited from %s", from)
		}
	}
	if got := len(res.Resources) - countInherited(res.Resources); got != want {
		t.Fatalf("got %d own resources want %d", got, want)
	}
	for _, r := range res.Resources {
		if err := r.Validate(); err != nil {
			t.Errorf("%s: %v", r.Name, err)
		}
	}
}

func countInherited(rs []model.Resource) int {
	n := 0
	for _, r := range rs {
		if r.Origin != "" {
			n++
		}
	}
	return n
}

func TestCatalogPartialFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("permission denied")
	cat := NewCatalog(
		fake.Static{
			Svc:   model.ServiceCloudStorage,
			Items: []model.Resource{{ID: "b", Name: "bucket", Service: model.ServiceCloudStorage}},
		},
		fake.Static{Svc: model.ServiceGKE, Err: boom},
	)
	res := cat.List(context.Background(), "p")
	if len(res.Resources) != 1 || res.Resources[0].Name != "bucket" {
		t.Fatalf("resources=%+v", res.Resources)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("errors=%v", res.Errors)
	}
	if !errors.Is(res.Errors[0], boom) {
		t.Fatalf("unwrap: %v", res.Errors[0])
	}
}

func TestCatalogClassifiesProviderErrors(t *testing.T) {
	t.Parallel()
	denied := &googleapi.Error{Code: 403, Message: "Required 'container.clusters.list' permission"}
	cat := NewCatalog(
		fake.Static{Svc: model.ServiceCloudStorage, Items: []model.Resource{{ID: "b", Name: "bucket", Service: model.ServiceCloudStorage}}},
		fake.Static{Svc: model.ServiceGKE, Err: denied},
	)
	var mu sync.Mutex
	got := map[model.ServiceType]error{}
	cat.ListEach(context.Background(), "p", func(r model.ProviderResult) {
		mu.Lock()
		defer mu.Unlock()
		got[r.Service] = r.Err
	})
	if got[model.ServiceCloudStorage] != nil {
		t.Fatalf("storage err: %v", got[model.ServiceCloudStorage])
	}
	var pe model.ProviderError
	if !errors.As(got[model.ServiceGKE], &pe) {
		t.Fatalf("GKE err %T is not a ProviderError", got[model.ServiceGKE])
	}
	want := model.ProviderError{Service: model.ServiceGKE, Kind: model.ErrKindPermissionDenied, Message: "missing container.clusters.list", Err: denied}
	if pe != want {
		t.Fatalf("got %+v, want %+v", pe, want)
	}
	res := cat.List(context.Background(), "p")
	if len(res.Errors) != 1 || res.Errors[0] != want {
		t.Fatalf("List errors %+v", res.Errors)
	}
}

func TestCatalogSkipsInvalid(t *testing.T) {
	t.Parallel()
	cat := NewCatalog(fake.Static{
		Svc: model.ServicePubSub,
		Items: []model.Resource{
			{ID: "ok", Name: "events", Service: model.ServicePubSub},
			{Name: "no-id", Service: model.ServicePubSub},
		},
	})
	res := cat.List(context.Background(), "p")
	if len(res.Resources) != 1 || res.Resources[0].Name != "events" {
		t.Fatalf("resources=%+v", res.Resources)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("errors=%v", res.Errors)
	}
}

func TestCatalogCancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := FakeCatalog().List(ctx, "p")
	if len(res.Errors) == 0 {
		t.Fatal("expected cancellation errors")
	}
}

func TestFakeCatalogScopeIsolation(t *testing.T) {
	t.Parallel()
	cat := FakeCatalog()
	org := cat.List(context.Background(), "organizations/example-org")
	if len(org.Resources) == 0 {
		t.Fatal("org should have IAM/policy fixtures")
	}
	for _, r := range org.Resources {
		if r.ScopeID != "organizations/example-org" {
			t.Fatalf("leaked %s scope=%s", r.Name, r.ScopeID)
		}
	}
	proj := cat.List(context.Background(), "projects/demo-project")
	for _, r := range proj.Resources {
		if r.ScopeID != "projects/demo-project" {
			t.Fatalf("leaked %s scope=%s", r.Name, r.ScopeID)
		}
	}
}

func TestListRelatedBucketFacets(t *testing.T) {
	t.Parallel()
	cat := FakeCatalog()
	var parent model.Resource
	for _, r := range cat.List(context.Background(), "projects/demo-project").Resources {
		if r.ID == "projects/demo-project/buckets/my-app-bucket" {
			parent = r
		}
	}
	if parent.ID == "" {
		t.Fatal("my-app-bucket not listed")
	}
	objs := cat.ListRelated(context.Background(), parent, model.FacetObjects)
	if len(objs.Errors) != 0 || len(objs.Resources) != 2 {
		t.Fatalf("objects %+v errors %v", objs.Resources, objs.Errors)
	}
	perms := cat.ListRelated(context.Background(), parent, model.FacetPermissions)
	if len(perms.Errors) != 0 || len(perms.Resources) != 2 {
		t.Fatalf("perms %+v errors %v", perms.Resources, perms.Errors)
	}
}

// sleepy is a provider that waits before answering, or until ctx is done.
type sleepy struct {
	svc   model.ServiceType
	delay time.Duration // zero blocks until ctx is done
}

func (s sleepy) Service() model.ServiceType { return s.svc }

func (s sleepy) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	var wait <-chan time.Time
	if s.delay > 0 {
		wait = time.After(s.delay)
	}
	select {
	case <-wait:
		return []model.Resource{{ID: string(s.svc) + "/1", Name: string(s.svc), Service: s.svc, ScopeID: scopeID}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s sleepy) Get(context.Context, string) (model.Resource, error) {
	return model.Resource{}, model.ErrNotFound
}

func TestListEachDeliversFastProvidersFirst(t *testing.T) {
	t.Parallel()
	cat := NewCatalog(
		sleepy{svc: model.ServiceGKE, delay: 100 * time.Millisecond},
		sleepy{svc: model.ServiceCloudStorage, delay: time.Nanosecond},
	)
	var (
		mu    sync.Mutex
		order []model.ServiceType
	)
	start := time.Now()
	cat.ListEach(context.Background(), "p", func(r model.ProviderResult) {
		mu.Lock()
		defer mu.Unlock()
		if r.Err != nil || len(r.Resources) != 1 {
			t.Errorf("%s: %+v", r.Service, r)
		}
		order = append(order, r.Service)
	})
	want := []model.ServiceType{model.ServiceCloudStorage, model.ServiceGKE}
	if !slices.Equal(order, want) {
		t.Fatalf("order=%v want %v", order, want)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("took %s", elapsed)
	}

	// The aggregate keeps provider order regardless of arrival order.
	res := cat.List(context.Background(), "p")
	if len(res.Resources) != 2 || res.Resources[0].Service != model.ServiceGKE {
		t.Fatalf("List=%+v", res.Resources)
	}
}

func TestListEachReportsEmptyProviders(t *testing.T) {
	t.Parallel()
	cat := NewCatalog(fake.Static{Svc: model.ServiceBigQuery}, fake.EnabledAPIs())
	var (
		mu  sync.Mutex
		got []model.ServiceType
	)
	cat.ListEach(context.Background(), "p", func(r model.ProviderResult) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, r.Service)
	})
	slices.Sort(got)
	want := []model.ServiceType{model.ServiceBigQuery, model.ServiceUsage}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestListRelatedEachSkipsNonRelatedProviders(t *testing.T) {
	t.Parallel()
	cat := NewCatalog(sleepy{svc: model.ServiceGKE, delay: time.Nanosecond}, fake.Static{Svc: model.ServiceCloudStorage})
	var calls atomic.Int32
	cat.ListRelatedEach(context.Background(), model.Resource{ID: "b"}, model.FacetObjects, func(r model.ProviderResult) {
		calls.Add(1)
		if r.Service != model.ServiceCloudStorage {
			t.Errorf("unexpected %s", r.Service)
		}
	})
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

// Not parallel: it lowers the package-level providerTimeout.
func TestListEachTimesOutHungProvider(t *testing.T) {
	prev := providerTimeout
	providerTimeout = 50 * time.Millisecond
	t.Cleanup(func() { providerTimeout = prev })

	cat := NewCatalog(
		sleepy{svc: model.ServiceGKE},
		sleepy{svc: model.ServiceCloudStorage, delay: time.Nanosecond},
	)
	var (
		mu  sync.Mutex
		got = map[model.ServiceType]model.ProviderResult{}
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		cat.ListEach(context.Background(), "p", func(r model.ProviderResult) {
			mu.Lock()
			defer mu.Unlock()
			got[r.Service] = r
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ListEach did not return")
	}
	if err := got[model.ServiceGKE].Err; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hung provider err=%v", err)
	}
	if r := got[model.ServiceCloudStorage]; r.Err != nil || len(r.Resources) != 1 {
		t.Fatalf("fast provider=%+v", r)
	}
}

// counting wraps a provider and counts List calls.
type counting struct {
	fake.Static
	calls *atomic.Int32
}

func (c counting) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	c.calls.Add(1)
	return c.Static.List(ctx, scopeID)
}

func TestListEachCachesWithinTTL(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	cat := NewCatalog(counting{
		Static: fake.Static{Svc: model.ServiceCloudStorage, Items: []model.Resource{{ID: "b", Name: "bucket", Service: model.ServiceCloudStorage}}},
		calls:  &calls,
	})

	first := cat.List(context.Background(), "projects/p")
	second := cat.List(context.Background(), "projects/p")
	if n := calls.Load(); n != 1 {
		t.Fatalf("List calls=%d want 1 (second call should hit the session cache)", n)
	}
	if len(first.Resources) != 1 || len(second.Resources) != 1 {
		t.Fatalf("first=%+v second=%+v", first.Resources, second.Resources)
	}

	cat.Invalidate("projects/p")
	cat.List(context.Background(), "projects/p")
	if n := calls.Load(); n != 2 {
		t.Fatalf("List calls=%d want 2 after Invalidate", n)
	}
}

type recordInvalidator struct{ scopes *[]string }

func (r recordInvalidator) Invalidate(scopeID string) { *r.scopes = append(*r.scopes, scopeID) }

func TestInvalidateReachesInvalidators(t *testing.T) {
	t.Parallel()
	var a, b []string
	base := NewCatalog()
	cat := base.WithInvalidators(recordInvalidator{&a}, recordInvalidator{&b})
	cat.Invalidate("projects/p")
	base.Invalidate("projects/q")
	if !slices.Equal(a, []string{"projects/p"}) || !slices.Equal(b, []string{"projects/p"}) {
		t.Fatalf("invalidated %v and %v, want projects/p each (and the original catalog untouched)", a, b)
	}
}

func TestInvalidateLeavesSiblingScopes(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	cat := NewCatalog(counting{
		Static: fake.Static{Svc: model.ServiceCloudStorage, Items: []model.Resource{{ID: "b", Name: "bucket", Service: model.ServiceCloudStorage}}},
		calls:  &calls,
	})

	cat.List(context.Background(), "projects/p")
	cat.List(context.Background(), "projects/p2")
	cat.Invalidate("projects/p")
	cat.List(context.Background(), "projects/p2")
	if n := calls.Load(); n != 2 {
		t.Fatalf("List calls=%d want 2 (projects/p2 must stay cached)", n)
	}
	cat.List(context.Background(), "projects/p")
	if n := calls.Load(); n != 3 {
		t.Fatalf("List calls=%d want 3 (projects/p was invalidated)", n)
	}
}

// partialProvider lists one load balancer beside a partial error.
type partialProvider struct{ calls *atomic.Int32 }

func (partialProvider) Service() model.ServiceType { return model.ServiceLoadBalancer }

func (p partialProvider) List(context.Context, string) ([]model.Resource, error) {
	p.calls.Add(1)
	r := model.Resource{ID: "projects/p/global/urlMaps/web", Name: "web", Service: model.ServiceLoadBalancer, ScopeID: "projects/p"}
	return []model.Resource{r}, model.Partial(errors.New("ssl proxies denied"))
}

// A partial error keeps the provider's items, marks the error Partial, and
// is not cached.
func TestListEachKeepsPartialResults(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	cat := NewCatalog(partialProvider{calls: &calls})
	res := cat.List(context.Background(), "projects/p")
	if len(res.Resources) != 1 || len(res.Errors) != 1 || !res.Errors[0].Partial {
		t.Fatalf("resources=%d errors=%+v; want 1 and one partial error", len(res.Resources), res.Errors)
	}
	cat.List(context.Background(), "projects/p")
	if n := calls.Load(); n != 2 {
		t.Fatalf("List calls=%d want 2 (a partial result must not be cached)", n)
	}
}

func TestListEachDoesNotCacheErrors(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	cat := NewCatalog(counting{Static: fake.Static{Svc: model.ServiceGKE, Err: errors.New("boom")}, calls: &calls})

	cat.List(context.Background(), "projects/p")
	cat.List(context.Background(), "projects/p")
	if n := calls.Load(); n != 2 {
		t.Fatalf("List calls=%d want 2 (a failing provider must not be cached)", n)
	}
}

// countingRelated wraps a RelatedLister and counts ListRelated calls.
type countingRelated struct {
	fake.Static
	calls *atomic.Int32
}

func (c countingRelated) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	c.calls.Add(1)
	return c.Static.ListRelated(ctx, parent, facetID)
}

func TestListRelatedEachCachesWithinTTL(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	item := model.Resource{
		ID: "projects/p/buckets/b/objects/o", ParentID: "projects/p/buckets/b",
		FacetID: model.FacetObjects, Service: model.ServiceCloudStorage, Name: "o",
	}
	cat := NewCatalog(countingRelated{
		Static: fake.Static{Svc: model.ServiceCloudStorage, Items: []model.Resource{item}},
		calls:  &calls,
	})
	parent := model.Resource{ID: "projects/p/buckets/b"}

	cat.ListRelated(context.Background(), parent, model.FacetObjects)
	cat.ListRelated(context.Background(), parent, model.FacetObjects)
	if n := calls.Load(); n != 1 {
		t.Fatalf("ListRelated calls=%d want 1 (second call should hit the session cache)", n)
	}

	cat.Invalidate("projects/p")
	cat.ListRelated(context.Background(), parent, model.FacetObjects)
	if n := calls.Load(); n != 2 {
		t.Fatalf("ListRelated calls=%d want 2 after Invalidate", n)
	}
}

// capped is a RelatedLister that returns one child and reports whether it
// stopped at a limit.
type capped struct {
	svc       model.ServiceType
	truncated bool
}

func (c capped) Service() model.ServiceType { return c.svc }

func (c capped) List(context.Context, string) ([]model.Resource, error) { return nil, nil }

func (c capped) Get(context.Context, string) (model.Resource, error) {
	return model.Resource{}, model.ErrNotFound
}

func (c capped) ListRelated(_ context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	r := model.Resource{ID: parent.ID + "/" + string(c.svc), Name: string(c.svc), Service: c.svc, ParentID: parent.ID, FacetID: facetID}
	return []model.Resource{r}, c.truncated, nil
}

func TestListRelatedTruncated(t *testing.T) {
	t.Parallel()
	parent := model.Resource{ID: "projects/p/buckets/b"}
	cases := []struct {
		name      string
		providers []model.ResourceProvider
		want      bool
	}{
		{"none", []model.ResourceProvider{capped{svc: model.ServiceCloudStorage}, capped{svc: model.ServiceIAM}}, false},
		{"one of two", []model.ResourceProvider{capped{svc: model.ServiceCloudStorage, truncated: true}, capped{svc: model.ServiceIAM}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cat := NewCatalog(tc.providers...)
			res := cat.ListRelated(context.Background(), parent, model.FacetObjects)
			if res.Truncated != tc.want || len(res.Resources) != 2 {
				t.Fatalf("truncated=%v resources=%d", res.Truncated, len(res.Resources))
			}
			var mu sync.Mutex
			flagged := map[model.ServiceType]bool{}
			cat.ListRelatedEach(context.Background(), parent, model.FacetObjects, func(r model.ProviderResult) {
				mu.Lock()
				defer mu.Unlock()
				flagged[r.Service] = r.Truncated
			})
			for _, p := range tc.providers {
				if flagged[p.Service()] != p.(capped).truncated {
					t.Errorf("%s truncated=%v", p.Service(), flagged[p.Service()])
				}
			}
		})
	}
}
