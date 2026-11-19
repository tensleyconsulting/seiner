package serviceusage

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	svcs []APIService
	err  error
}

func (f fakeAPI) ListEnabled(context.Context, string) ([]APIService, error) {
	return f.svcs, f.err
}

func TestListRelated(t *testing.T) {
	t.Parallel()
	p := New(fakeAPI{svcs: []APIService{
		{Name: "storage.googleapis.com", Title: "Cloud Storage"},
		{Name: "obscure.googleapis.com", Title: "Obscure"},
	}})
	parent := model.EnabledServicesResource(model.Resource{
		ID: "projects/demo-project", Name: "demo-project", Service: model.ServiceProject, ScopeID: "projects/demo-project",
	})
	got, _, err := p.ListRelated(context.Background(), parent, model.FacetEnabledServices)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	if got[0].Service != model.ServiceCloudStorage || got[0].Name != "Cloud Storage" {
		t.Fatalf("%+v", got[0])
	}
	if got[1].Service != model.ServiceUsage || got[1].Metadata["api"] != "obscure.googleapis.com" {
		t.Fatalf("%+v", got[1])
	}
}

func TestListRelatedIgnoresOtherFacets(t *testing.T) {
	t.Parallel()
	p := New(fakeAPI{svcs: []APIService{{Name: "storage.googleapis.com"}}})
	got, _, err := p.ListRelated(context.Background(), model.Resource{ID: "b"}, model.FacetObjects)
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

// countingAPI counts ListEnabled calls per project.
type countingAPI struct {
	mu    sync.Mutex
	calls map[string]int
	svcs  []APIService
	err   error
	block chan struct{} // if set, ListEnabled waits for it to close
}

func (c *countingAPI) ListEnabled(_ context.Context, project string) ([]APIService, error) {
	c.mu.Lock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[project]++
	c.mu.Unlock()
	if c.block != nil {
		<-c.block
	}
	return c.svcs, c.err
}

func (c *countingAPI) count(project string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[project]
}

func TestEnabledAPIsSharesCacheWithListRelated(t *testing.T) {
	t.Parallel()
	api := &countingAPI{svcs: []APIService{{Name: "storage.googleapis.com"}, {Name: "iam.googleapis.com"}}}
	p := New(api)
	ctx := context.Background()

	names, err := p.EnabledAPIs(ctx, "demo-project")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"storage.googleapis.com", "iam.googleapis.com"}; !slices.Equal(names, want) {
		t.Fatalf("names=%v want %v", names, want)
	}

	// A copy of the value-typed provider shares the cache.
	cp := p
	parent := model.EnabledServicesResource(model.Resource{
		ID: "projects/demo-project", Name: "demo-project", Service: model.ServiceProject, ScopeID: "projects/demo-project",
	})
	got, _, err := cp.ListRelated(ctx, parent, model.FacetEnabledServices)
	if err != nil || len(got) != 2 {
		t.Fatalf("ListRelated=%v %v", got, err)
	}
	if n := api.count("demo-project"); n != 1 {
		t.Fatalf("ListEnabled calls=%d want 1", n)
	}

	if _, err := p.EnabledAPIs(ctx, "other-project"); err != nil {
		t.Fatal(err)
	}
	if n := api.count("other-project"); n != 1 {
		t.Fatalf("other-project calls=%d want 1", n)
	}
}

func TestInvalidateEnabledRefetches(t *testing.T) {
	t.Parallel()
	api := &countingAPI{svcs: []APIService{{Name: "storage.googleapis.com"}}}
	p := New(api)
	ctx := context.Background()

	steps := []struct {
		invalidate string
		want       int
	}{
		{"", 1},
		{"", 1},
		{"other", 1},
		{"p", 2},
		{"", 2},
	}
	for i, s := range steps {
		if s.invalidate != "" {
			p.InvalidateEnabled(s.invalidate)
		}
		if _, err := p.EnabledAPIs(ctx, "p"); err != nil {
			t.Fatal(err)
		}
		if n := api.count("p"); n != s.want {
			t.Fatalf("step %d: calls=%d want %d", i, n, s.want)
		}
	}
}

func TestEnabledCacheDoesNotCacheErrors(t *testing.T) {
	t.Parallel()
	api := &countingAPI{err: errors.New("403")}
	p := New(api)
	for range 2 {
		if _, err := p.EnabledAPIs(context.Background(), "p"); err == nil {
			t.Fatal("want error")
		}
	}
	if n := api.count("p"); n != 2 {
		t.Fatalf("calls=%d want 2", n)
	}
}

func TestEnabledCacheSharesInFlightCall(t *testing.T) {
	t.Parallel()
	api := &countingAPI{svcs: []APIService{{Name: "storage.googleapis.com"}}, block: make(chan struct{})}
	p := New(api)
	const callers = 4
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			names, err := p.EnabledAPIs(context.Background(), "p")
			if err == nil && len(names) != 1 {
				err = fmt.Errorf("names=%v", names)
			}
			errs <- err
		}()
	}
	// Wait until the first call is in flight, give the others time to
	// queue behind it, then release it.
	for api.count("p") == 0 {
		runtime.Gosched()
	}
	time.Sleep(20 * time.Millisecond)
	close(api.block)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := api.count("p"); n != 1 {
		t.Fatalf("calls=%d want 1", n)
	}
}

func TestResourceFromSummaryAndDocs(t *testing.T) {
	t.Parallel()
	r := ResourceFrom("p", "projects/p/enabled-services", APIService{Name: "storage.googleapis.com", Title: "Cloud Storage API",
		Summary: " Lets you store and retrieve objects. ", DocsURL: "https://cloud.google.com/storage/docs"})
	if r.Description != "Lets you store and retrieve objects." || r.Metadata["documentation"] != "https://cloud.google.com/storage/docs" ||
		len(r.Fields) != 3 || r.Fields[2].Key != "documentation" {
		t.Fatalf("resource = %+v", r)
	}
}
