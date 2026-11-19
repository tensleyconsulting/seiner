package crm

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	iampb "cloud.google.com/go/iam/apiv1/iampb"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A non-CRM scope fails before any client is used.
func TestTestPermissionsRejectsNonScope(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"", "p", "projects/p/buckets/b", "billingAccounts/1"} {
		if _, err := (liveAPI{}).TestPermissions(context.Background(), scope, []string{"storage.buckets.list"}); err == nil {
			t.Errorf("%q: expected error", scope)
		}
	}
}

var created = time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)

// SearchProjects follows page tokens to the last page, keeps only active
// projects with a project id, and maps each field.
func TestLiveSearchProjects(t *testing.T) {
	t.Parallel()
	f := &fakeRM{pages: [][]*resourcemanagerpb.Project{
		{
			{
				Name: "projects/101", ProjectId: "prj-a", DisplayName: "Project A", Parent: "folders/7",
				State: resourcemanagerpb.Project_ACTIVE, CreateTime: timestamppb.New(created),
				Labels: map[string]string{"env": "prod", "team": "infra"},
			},
			{Name: "projects/102", ProjectId: "prj-gone", State: resourcemanagerpb.Project_DELETE_REQUESTED},
			{Name: "projects/103", State: resourcemanagerpb.Project_ACTIVE},
		},
		{}, // an empty page in the middle does not end the listing
		{
			{Name: "projects/104", ProjectId: "prj-b", Parent: "organizations/1", State: resourcemanagerpb.Project_ACTIVE},
		},
	}}
	api := serveRM(t, f)

	got, err := api.SearchProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Project{
		{ID: "prj-a", Number: "101", DisplayName: "Project A", Parent: "folders/7", Created: created,
			Labels: map[string]string{"env": "prod", "team": "infra"}},
		{ID: "prj-b", Number: "104", Parent: "organizations/1"},
	}
	if !slices.EqualFunc(got, want, func(a, b Project) bool {
		return a.ID == b.ID && a.Number == b.Number && a.DisplayName == b.DisplayName &&
			a.Parent == b.Parent && a.Created.Equal(b.Created) && maps.Equal(a.Labels, b.Labels)
	}) {
		t.Errorf("projects %+v, want %+v", got, want)
	}

	var tokens []string
	for _, r := range f.requests() {
		req, ok := r.req.(*resourcemanagerpb.SearchProjectsRequest)
		if !ok {
			t.Fatalf("unexpected request %T", r.req)
		}
		if req.GetQuery() != "" {
			t.Errorf("query %q, want empty (every project the caller can see)", req.GetQuery())
		}
		tokens = append(tokens, req.GetPageToken())
	}
	if want := []string{"", "1", "2"}; !slices.Equal(tokens, want) {
		t.Errorf("page tokens %q, want %q", tokens, want)
	}
}

func TestLiveGetFolderAndOrg(t *testing.T) {
	t.Parallel()
	f := &fakeRM{
		folders: map[string]*resourcemanagerpb.Folder{
			"folders/7": {Name: "folders/7", DisplayName: "Platform", Parent: "organizations/1", CreateTime: timestamppb.New(created)},
		},
		orgs: map[string]*resourcemanagerpb.Organization{
			"organizations/1": {Name: "organizations/1", DisplayName: "example.com", CreateTime: timestamppb.New(created)},
		},
	}
	api := serveRM(t, f)
	ctx := context.Background()

	fo, err := api.GetFolder(ctx, "folders/7")
	if err != nil {
		t.Fatal(err)
	}
	if want := (Folder{Name: "folders/7", DisplayName: "Platform", Parent: "organizations/1", Created: created}); fo != want {
		t.Errorf("folder %+v, want %+v", fo, want)
	}
	o, err := api.GetOrg(ctx, "organizations/1")
	if err != nil {
		t.Fatal(err)
	}
	if want := (Org{Name: "organizations/1", DisplayName: "example.com", Created: created}); o != want {
		t.Errorf("org %+v, want %+v", o, want)
	}

	reqs := f.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests, want 2", len(reqs))
	}
	if r, ok := reqs[0].req.(*resourcemanagerpb.GetFolderRequest); !ok || r.GetName() != "folders/7" {
		t.Errorf("first request %v, want GetFolder folders/7", reqs[0].req)
	}
	if r, ok := reqs[1].req.(*resourcemanagerpb.GetOrganizationRequest); !ok || r.GetName() != "organizations/1" {
		t.Errorf("second request %v, want GetOrganization organizations/1", reqs[1].req)
	}
}

// TestPermissions goes to the service that owns the scope.
func TestLiveTestPermissions(t *testing.T) {
	t.Parallel()
	perms := []string{"storage.buckets.list", "compute.instances.list"}
	for _, tc := range []struct {
		scope, service string
	}{
		{"organizations/1", "organizations"},
		{"folders/7", "folders"},
		{"projects/prj-a", "projects"},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			f := &fakeRM{granted: perms[:1]}
			api := serveRM(t, f)
			got, err := api.TestPermissions(context.Background(), tc.scope, perms)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, perms[:1]) {
				t.Errorf("granted %q, want %q", got, perms[:1])
			}
			reqs := f.requests()
			if len(reqs) != 1 || reqs[0].service != tc.service {
				t.Fatalf("requests %v, want one on %s", reqs, tc.service)
			}
			req := reqs[0].req.(*iampb.TestIamPermissionsRequest)
			if req.GetResource() != tc.scope || !slices.Equal(req.GetPermissions(), perms) {
				t.Errorf("request %v, want resource %s and %q", req, tc.scope, perms)
			}
		})
	}
}

// A server error comes back as an error whose gRPC code is still
// readable, so it can be classified (permission denied, not found, …).
func TestLiveErrorsKeepGRPCCode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		call func(context.Context, liveAPI) error
	}{
		{"SearchProjects", func(ctx context.Context, a liveAPI) error { _, err := a.SearchProjects(ctx); return err }},
		{"GetFolder", func(ctx context.Context, a liveAPI) error { _, err := a.GetFolder(ctx, "folders/7"); return err }},
		{"GetOrg", func(ctx context.Context, a liveAPI) error { _, err := a.GetOrg(ctx, "organizations/1"); return err }},
		{"TestPermissions", func(ctx context.Context, a liveAPI) error {
			_, err := a.TestPermissions(ctx, "projects/p", []string{"x.y.z"})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &fakeRM{err: status.Error(codes.PermissionDenied, "caller lacks permission")}
			err := tc.call(context.Background(), serveRM(t, f))
			if err == nil {
				t.Fatal("expected error")
			}
			if c := status.Code(err); c != codes.PermissionDenied {
				t.Errorf("code %v (%v), want PermissionDenied", c, err)
			}
			if n := len(f.requests()); n != 1 {
				t.Errorf("%d requests, want 1 (no retry on PermissionDenied)", n)
			}
		})
	}
}
