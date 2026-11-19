package gcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/assuredworkloads"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/crm"
	"github.com/tensleyconsulting/seiner/internal/gcp/fake"
	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestFakeHierarchyDepth(t *testing.T) {
	t.Parallel()
	root, err := FakeCatalog().Hierarchy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	path := root.PathTo("projects/demo-project")
	if len(path) < 5 {
		t.Fatalf("depth=%d want >=5 names=%v", len(path), path)
	}
	if path[0].Kind != model.KindOrg || path[len(path)-1].Kind != model.KindProject {
		t.Fatalf("path kinds %+v", path)
	}
}

// countingCRM is a crm.API fake that counts SearchProjects calls so tests can
// assert the tree is built at most once across value copies of a Catalog.
type countingCRM struct {
	calls atomic.Int32
	err   error
}

func (c *countingCRM) SearchProjects(context.Context) ([]crm.Project, error) {
	c.calls.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	return []crm.Project{{ID: "demo-project", DisplayName: "demo-project"}}, nil
}

func (c *countingCRM) GetFolder(context.Context, string) (crm.Folder, error) {
	return crm.Folder{}, nil
}
func (c *countingCRM) GetOrg(context.Context, string) (crm.Org, error) { return crm.Org{}, nil }

// blockingCRM is a crm.API fake whose SearchProjects waits for release.
type blockingCRM struct {
	started chan struct{}
	release chan struct{}
}

func (b blockingCRM) SearchProjects(context.Context) ([]crm.Project, error) {
	close(b.started)
	<-b.release
	return []crm.Project{{ID: "demo-project", DisplayName: "demo-project"}}, nil
}

func (blockingCRM) GetFolder(context.Context, string) (crm.Folder, error) { return crm.Folder{}, nil }
func (blockingCRM) GetOrg(context.Context, string) (crm.Org, error)       { return crm.Org{}, nil }

// A caller waiting on another's build returns when its own ctx ends.
func TestLiveHierarchyWaiterHonorsContext(t *testing.T) {
	t.Parallel()
	api := blockingCRM{started: make(chan struct{}), release: make(chan struct{})}
	cat := NewCatalog().WithLiveHierarchy(api)
	done := make(chan error, 1)
	go func() {
		_, err := cat.Hierarchy(context.Background())
		done <- err
	}()
	<-api.started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cat.Hierarchy(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter err %v, want canceled", err)
	}
	close(api.release)
	if err := <-done; err != nil {
		t.Fatalf("builder err %v", err)
	}
}

func TestLiveHierarchyBuildsOnceAndCaches(t *testing.T) {
	t.Parallel()
	api := &countingCRM{}
	cat := NewCatalog().WithLiveHierarchy(api)

	root, err := cat.Hierarchy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := root.Find("projects/demo-project"); !ok {
		t.Fatalf("tree missing project: %+v", root)
	}

	// A value copy derived from cat must share the cached build.
	copyCat := cat
	if _, err := copyCat.Hierarchy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := api.calls.Load(); n != 1 {
		t.Fatalf("SearchProjects called %d times, want 1", n)
	}
}

func TestLiveHierarchyErrorIsClassified(t *testing.T) {
	t.Parallel()
	api := &countingCRM{err: &googleapi.Error{Code: 403, Message: "forbidden"}}
	cat := NewCatalog().WithLiveHierarchy(api)
	_, err := cat.Hierarchy(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var pe model.ProviderError
	if !errors.As(err, &pe) || pe.Kind != model.ErrKindPermissionDenied {
		t.Fatalf("err=%v", err)
	}
}

// A failed build is not cached, so a later Hierarchy call (refresh) retries.
func TestLiveHierarchyRetriesAfterError(t *testing.T) {
	t.Parallel()
	api := &countingCRM{err: context.DeadlineExceeded}
	cat := NewCatalog().WithLiveHierarchy(api)
	if _, err := cat.Hierarchy(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	api.err = nil
	root, err := cat.Hierarchy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := root.Find("projects/demo-project"); !ok {
		t.Fatalf("tree missing project: %+v", root)
	}
	if n := api.calls.Load(); n != 2 {
		t.Fatalf("SearchProjects called %d times, want 2", n)
	}
}

// orgCRM is a crm.API fake with one project in folders/10 under
// organizations/1.
type orgCRM struct{}

func (orgCRM) SearchProjects(context.Context) ([]crm.Project, error) {
	return []crm.Project{{ID: "p", Parent: "folders/10"}}, nil
}
func (orgCRM) GetFolder(context.Context, string) (crm.Folder, error) {
	return crm.Folder{Name: "folders/10", DisplayName: "eu", Parent: "organizations/1"}, nil
}
func (orgCRM) GetOrg(context.Context, string) (crm.Org, error) {
	return crm.Org{Name: "organizations/1", DisplayName: "example"}, nil
}

// awFake lists one workload owning folders/10, and cancels cancel (when set)
// as it does.
type awFake struct{ cancel context.CancelFunc }

func (f awFake) ListWorkloads(context.Context, string, string) ([]assuredworkloads.Workload, error) {
	if f.cancel != nil {
		f.cancel()
	}
	return []assuredworkloads.Workload{{
		Name:             "organizations/1/locations/us/workloads/w",
		ComplianceRegime: "US_DATA_BOUNDARY_AND_SUPPORT",
		Resources:        []assuredworkloads.Resource{{ID: "10", Type: assuredworkloads.TypeConsumerFolder}},
	}}, nil
}

func TestLiveHierarchyMarksAssuredWorkloads(t *testing.T) {
	t.Parallel()
	cat := NewCatalog().WithLiveHierarchy(orgCRM{}).WithAssuredWorkloads(awFake{}, []string{"us"})
	root, err := cat.Hierarchy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f, ok := root.Find("folders/10")
	if !ok || f.Workload == nil || f.Workload.Regime != "US_DATA_BOUNDARY_AND_SUPPORT" {
		t.Fatalf("folder %+v", f)
	}
}

func TestWithAssuredWorkloadsSkips(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cat  Catalog
	}{
		{"prebuilt tree", NewCatalog().WithHierarchy(fake.Hierarchy()).WithAssuredWorkloads(awFake{}, []string{"us"})},
		{"no locations", NewCatalog().WithLiveHierarchy(orgCRM{}).WithAssuredWorkloads(awFake{}, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.cat.hier.aw != nil {
				t.Fatal("assured workloads attached")
			}
		})
	}
}

// A tree whose workload lists were cut short by ctx is not cached.
func TestLiveHierarchyAssuredWorkloadsCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cat := NewCatalog().WithLiveHierarchy(orgCRM{}).WithAssuredWorkloads(awFake{cancel: cancel}, []string{"us"})
	if _, err := cat.Hierarchy(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want canceled", err)
	}
	if cat.hier.built {
		t.Fatal("cancelled build was cached")
	}
}

// folderErrCRM is a crm.API fake with one project under folders/1, whose
// GetFolder fails with errs[n] on its nth call (nil once errs run out).
type folderErrCRM struct {
	searches atomic.Int32
	folders  atomic.Int32
	errs     []error
}

func (f *folderErrCRM) SearchProjects(context.Context) ([]crm.Project, error) {
	f.searches.Add(1)
	return []crm.Project{{ID: "demo-project", Parent: "folders/1"}}, nil
}

func (f *folderErrCRM) GetFolder(context.Context, string) (crm.Folder, error) {
	n := int(f.folders.Add(1)) - 1
	if n < len(f.errs) && f.errs[n] != nil {
		return crm.Folder{}, f.errs[n]
	}
	return crm.Folder{Name: "folders/1", DisplayName: "platform"}, nil
}

func (f *folderErrCRM) GetOrg(context.Context, string) (crm.Org, error) { return crm.Org{}, nil }

// A denied ancestor read is a quiet stub and the tree is cached. Any other
// failure returns the stubbed tree with a partial error and is not cached,
// so the next call rebuilds.
func TestLiveHierarchyAncestorErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		err         error
		wantPartial bool
		// wantSearches is SearchProjects calls after three Hierarchy calls.
		wantSearches int32
	}{
		{name: "permission denied", err: status.Error(codes.PermissionDenied, "denied"), wantSearches: 1},
		{name: "not found", err: &googleapi.Error{Code: 404}, wantSearches: 1},
		{name: "unavailable", err: status.Error(codes.Unavailable, "try again"), wantPartial: true, wantSearches: 2},
		{name: "quota", err: status.Error(codes.ResourceExhausted, "slow down"), wantPartial: true, wantSearches: 2},
		{name: "deadline", err: status.Error(codes.DeadlineExceeded, "slow"), wantPartial: true, wantSearches: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &folderErrCRM{errs: []error{tt.err}}
			cat := NewCatalog().WithLiveHierarchy(api)

			root, err := cat.Hierarchy(context.Background())
			var pe model.ProviderError
			if tt.wantPartial {
				if !errors.As(err, &pe) || !pe.Partial || pe.Service != model.ServiceOrganization {
					t.Fatalf("err = %#v, want a partial Organization ProviderError", err)
				}
			} else if err != nil {
				t.Fatalf("err = %v, want a quiet stub", err)
			}
			// Either way the tree is kept, with folders/1 a stub.
			if root.ID != "folders/1" || root.Name != "1" {
				t.Fatalf("root %+v, want the folders/1 stub", root)
			}
			if _, ok := root.Find("projects/demo-project"); !ok {
				t.Fatalf("tree missing project: %+v", root)
			}

			// A partial tree was not cached: this call rebuilds, and now
			// reads the folder.
			root, err = cat.Hierarchy(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantPartial && root.Name != "platform" {
				t.Fatalf("rebuilt root %+v, want the folder read", root)
			}
			if _, err := cat.Hierarchy(context.Background()); err != nil {
				t.Fatal(err)
			}
			if n := api.searches.Load(); n != tt.wantSearches {
				t.Fatalf("SearchProjects called %d times, want %d", n, tt.wantSearches)
			}
		})
	}
}
