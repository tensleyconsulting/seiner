package iam

import (
	"context"
	"slices"
	"testing"
	"time"

	"cloud.google.com/go/iam/admin/apiv1/adminpb"
	iampb "cloud.google.com/go/iam/apiv1/iampb"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"google.golang.org/genproto/googleapis/type/expr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPolicyRequestAsksForVersion3(t *testing.T) {
	t.Parallel()
	for _, resource := range []string{"organizations/1", "folders/2", "projects/p"} {
		req := policyRequest(resource)
		if req.GetResource() != resource {
			t.Errorf("%s: resource %q", resource, req.GetResource())
		}
		if v := req.GetOptions().GetRequestedPolicyVersion(); v != 3 {
			t.Errorf("%s: requested policy version %d, want 3", resource, v)
		}
	}
}

func TestFromPolicy(t *testing.T) {
	t.Parallel()
	pol := &iampb.Policy{Bindings: []*iampb.Binding{
		{Role: "roles/viewer", Members: []string{"user:a"}},
		{Role: "roles/viewer", Members: []string{"user:b"}, Condition: &expr.Expr{Title: "expires", Expression: "request.time < timestamp('2027-01-01T00:00:00Z')"}},
	}}
	want := []Binding{
		{Role: "roles/viewer", Members: []string{"user:a"}},
		{Role: "roles/viewer", Members: []string{"user:b"}, Condition: "expires"},
	}
	got := fromPolicy(pol).Bindings
	if !slices.EqualFunc(got, want, func(a, b Binding) bool {
		return a.Role == b.Role && a.Condition == b.Condition && slices.Equal(a.Members, b.Members)
	}) {
		t.Errorf("bindings %+v, want %+v", got, want)
	}
}

func TestFromKey(t *testing.T) {
	t.Parallel()
	got := fromKey(&adminpb.ServiceAccountKey{
		Name:            "projects/p/serviceAccounts/a@p.iam.gserviceaccount.com/keys/abc123",
		KeyType:         adminpb.ListServiceAccountKeysRequest_USER_MANAGED,
		KeyOrigin:       adminpb.ServiceAccountKeyOrigin_GOOGLE_PROVIDED,
		KeyAlgorithm:    adminpb.ServiceAccountKeyAlgorithm_KEY_ALG_RSA_2048,
		ValidAfterTime:  timestamppb.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		ValidBeforeTime: timestamppb.New(time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)),
		PrivateKeyData:  []byte("never read"),
		Disabled:        true,
	})
	want := ServiceAccountKey{ID: "abc123", Type: "USER_MANAGED", Origin: "GOOGLE_PROVIDED", Algorithm: "KEY_ALG_RSA_2048",
		ValidAfter: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ValidBefore: time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), Disabled: true}
	if got != want {
		t.Fatalf("fromKey = %+v, want %+v", got, want)
	}
}

func bindingsEqual(a, b []Binding) bool {
	return slices.EqualFunc(a, b, func(a, b Binding) bool {
		return a.Role == b.Role && a.Condition == b.Condition && slices.Equal(a.Members, b.Members)
	})
}

func rolesEqual(a, b []Role) bool {
	return slices.EqualFunc(a, b, func(a, b Role) bool {
		return a.Name == b.Name && a.Title == b.Title && a.Description == b.Description &&
			a.Stage == b.Stage && slices.Equal(a.Permissions, b.Permissions)
	})
}

// only returns the single request f received, failing t unless there was
// exactly one and it is a T.
func only[T any](t *testing.T, f *fakeGoogle) (string, T) {
	t.Helper()
	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests %v, want 1", len(reqs), reqs)
	}
	req, ok := reqs[0].req.(T)
	if !ok {
		t.Fatalf("request %T, want %T", reqs[0].req, req)
	}
	return reqs[0].service, req
}

// GetPolicy reads the policy from the service that owns the resource and
// asks for version 3, without which conditional bindings come back
// mangled.
func TestLiveGetPolicy(t *testing.T) {
	t.Parallel()
	pol := &iampb.Policy{Version: 3, Bindings: []*iampb.Binding{
		{Role: "roles/viewer", Members: []string{"user:a@example.com", "group:ops@example.com"}},
		{Role: "roles/editor", Members: []string{"user:b@example.com"}, Condition: &expr.Expr{Title: "expires", Expression: "request.time < timestamp('2027-01-01T00:00:00Z')"}},
	}}
	want := []Binding{
		{Role: "roles/viewer", Members: []string{"user:a@example.com", "group:ops@example.com"}},
		{Role: "roles/editor", Members: []string{"user:b@example.com"}, Condition: "expires"},
	}
	for _, tc := range []struct {
		resource, service string
	}{
		{"organizations/1", "organizations"},
		{"folders/7", "folders"},
		{"projects/prj-a", "projects"},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			t.Parallel()
			f := &fakeGoogle{policies: map[string]*iampb.Policy{tc.resource: pol}}
			api, _ := serve(t, f)
			got, err := api.GetPolicy(context.Background(), tc.resource)
			if err != nil {
				t.Fatal(err)
			}
			if !bindingsEqual(got.Bindings, want) {
				t.Errorf("bindings %+v, want %+v", got.Bindings, want)
			}
			service, req := only[*iampb.GetIamPolicyRequest](t, f)
			if service != tc.service {
				t.Errorf("served by %s, want %s", service, tc.service)
			}
			if req.GetResource() != tc.resource {
				t.Errorf("resource %q, want %q", req.GetResource(), tc.resource)
			}
			if v := req.GetOptions().GetRequestedPolicyVersion(); v != 3 {
				t.Errorf("requested policy version %d, want 3", v)
			}
		})
	}
}

func TestLiveGetNode(t *testing.T) {
	t.Parallel()
	data := &fakeGoogle{
		projects: map[string]*resourcemanagerpb.Project{"projects/prj-a": {Name: "projects/101", DisplayName: "Project A", Parent: "folders/7"}},
		folders:  map[string]*resourcemanagerpb.Folder{"folders/7": {Name: "folders/7", DisplayName: "Platform", Parent: "organizations/1"}},
		orgs:     map[string]*resourcemanagerpb.Organization{"organizations/1": {Name: "organizations/1", DisplayName: "example.com"}},
	}
	for _, tc := range []struct {
		name, service string
		want          Node
	}{
		{"organizations/1", "organizations", Node{DisplayName: "example.com"}},
		{"folders/7", "folders", Node{DisplayName: "Platform", Parent: "organizations/1"}},
		{"projects/prj-a", "projects", Node{DisplayName: "Project A", Parent: "folders/7"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &fakeGoogle{projects: data.projects, folders: data.folders, orgs: data.orgs}
			api, _ := serve(t, f)
			got, err := api.GetNode(context.Background(), tc.name)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("node %+v, want %+v", got, tc.want)
			}
			reqs := f.requests()
			if len(reqs) != 1 || reqs[0].service != tc.service {
				t.Fatalf("requests %v, want one on %s", reqs, tc.service)
			}
			if n := reqs[0].req.(interface{ GetName() string }).GetName(); n != tc.name {
				t.Errorf("requested %q, want %q", n, tc.name)
			}
		})
	}
}

// ListServiceAccounts names the project and follows page tokens to the
// last page.
func TestLiveListServiceAccounts(t *testing.T) {
	t.Parallel()
	f := &fakeGoogle{accountPages: [][]*adminpb.ServiceAccount{
		{{
			Name: "projects/p/serviceAccounts/deployer@p.iam.gserviceaccount.com", Email: "deployer@p.iam.gserviceaccount.com",
			DisplayName: "Deployer", Description: "CI deploys", UniqueId: "111", Oauth2ClientId: "222",
		}},
		{{Email: "old@p.iam.gserviceaccount.com", Disabled: true}},
	}}
	_, admin := serve(t, f)
	got, err := admin.ListServiceAccounts(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	want := []ServiceAccount{
		{Email: "deployer@p.iam.gserviceaccount.com", DisplayName: "Deployer", Description: "CI deploys", UniqueID: "111", OAuth2ClientID: "222"},
		{Email: "old@p.iam.gserviceaccount.com", Disabled: true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("accounts %+v, want %+v", got, want)
	}
	var tokens []string
	for _, r := range f.requests() {
		req := r.req.(*adminpb.ListServiceAccountsRequest)
		if req.GetName() != "projects/p" {
			t.Errorf("name %q, want projects/p", req.GetName())
		}
		if req.GetPageSize() != 0 {
			t.Errorf("page size %d, want the server default", req.GetPageSize())
		}
		tokens = append(tokens, req.GetPageToken())
	}
	if want := []string{"", "1"}; !slices.Equal(tokens, want) {
		t.Errorf("page tokens %q, want %q", tokens, want)
	}
}

// ListServiceAccountKeys names the account under its project and asks
// for every key type.
func TestLiveListServiceAccountKeys(t *testing.T) {
	t.Parallel()
	const account = "projects/p/serviceAccounts/deployer@p.iam.gserviceaccount.com"
	after := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := &fakeGoogle{keys: map[string][]*adminpb.ServiceAccountKey{account: {
		{
			Name: account + "/keys/k1", KeyType: adminpb.ListServiceAccountKeysRequest_USER_MANAGED,
			KeyOrigin: adminpb.ServiceAccountKeyOrigin_GOOGLE_PROVIDED, KeyAlgorithm: adminpb.ServiceAccountKeyAlgorithm_KEY_ALG_RSA_2048,
			ValidAfterTime: timestamppb.New(after),
		},
		{Name: account + "/keys/k2", KeyType: adminpb.ListServiceAccountKeysRequest_SYSTEM_MANAGED},
	}}}
	_, admin := serve(t, f)
	got, err := admin.ListServiceAccountKeys(context.Background(), "p", "deployer@p.iam.gserviceaccount.com")
	if err != nil {
		t.Fatal(err)
	}
	want := []ServiceAccountKey{
		{ID: "k1", Type: "USER_MANAGED", Origin: "GOOGLE_PROVIDED", Algorithm: "KEY_ALG_RSA_2048", ValidAfter: after},
		{ID: "k2", Type: "SYSTEM_MANAGED", Origin: "ORIGIN_UNSPECIFIED", Algorithm: "KEY_ALG_UNSPECIFIED"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("keys %+v, want %+v", got, want)
	}
	_, req := only[*adminpb.ListServiceAccountKeysRequest](t, f)
	if req.GetName() != account {
		t.Errorf("name %q, want %q", req.GetName(), account)
	}
	if len(req.GetKeyTypes()) != 0 {
		t.Errorf("key types %v, want none (every type)", req.GetKeyTypes())
	}
}

// ListRoles asks for the FULL view of parent's live roles, follows page
// tokens, and drops a deleted role the server returns anyway.
func TestLiveListRoles(t *testing.T) {
	t.Parallel()
	f := &fakeGoogle{rolePages: [][]*adminpb.Role{
		{{
			Name: "projects/p/roles/deployer", Title: "Deployer", Description: "Deploys things",
			Stage: adminpb.Role_GA, IncludedPermissions: []string{"run.services.update", "iam.serviceAccounts.actAs"},
		}},
		{
			{Name: "projects/p/roles/gone", Deleted: true},
			{Name: "projects/p/roles/beta", Stage: adminpb.Role_BETA},
		},
	}}
	_, admin := serve(t, f)
	got, err := admin.ListRoles(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	want := []Role{
		{Name: "projects/p/roles/deployer", Title: "Deployer", Description: "Deploys things", Stage: "GA",
			Permissions: []string{"run.services.update", "iam.serviceAccounts.actAs"}},
		{Name: "projects/p/roles/beta", Stage: "BETA"},
	}
	if !rolesEqual(got, want) {
		t.Errorf("roles %+v, want %+v", got, want)
	}
	var tokens []string
	for _, r := range f.requests() {
		req := r.req.(*adminpb.ListRolesRequest)
		if req.GetParent() != "projects/p" {
			t.Errorf("parent %q, want projects/p", req.GetParent())
		}
		if req.GetView() != adminpb.RoleView_FULL {
			t.Errorf("view %v, want FULL", req.GetView())
		}
		if req.GetShowDeleted() {
			t.Error("show_deleted set")
		}
		if req.GetPageSize() != 0 {
			t.Errorf("page size %d, want the server default", req.GetPageSize())
		}
		tokens = append(tokens, req.GetPageToken())
	}
	if want := []string{"", "1"}; !slices.Equal(tokens, want) {
		t.Errorf("page tokens %q, want %q", tokens, want)
	}
}

// GetRole reads a predefined or custom role by its full name.
func TestLiveGetRole(t *testing.T) {
	t.Parallel()
	for _, r := range []*adminpb.Role{
		{Name: "roles/viewer", Title: "Viewer", Stage: adminpb.Role_GA, IncludedPermissions: []string{"a.b.get", "a.b.list"}},
		{Name: "organizations/1/roles/auditor", Title: "Auditor", Stage: adminpb.Role_DEPRECATED},
	} {
		t.Run(r.GetName(), func(t *testing.T) {
			t.Parallel()
			f := &fakeGoogle{roles: map[string]*adminpb.Role{r.GetName(): r}}
			_, admin := serve(t, f)
			got, err := admin.GetRole(context.Background(), r.GetName())
			if err != nil {
				t.Fatal(err)
			}
			if want := fromRole(r); !rolesEqual([]Role{got}, []Role{want}) {
				t.Errorf("role %+v, want %+v", got, want)
			}
			if _, req := only[*adminpb.GetRoleRequest](t, f); req.GetName() != r.GetName() {
				t.Errorf("name %q, want %q", req.GetName(), r.GetName())
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
		call func(context.Context, API, AdminAPI) error
	}{
		{"GetPolicy", func(ctx context.Context, a API, _ AdminAPI) error {
			_, err := a.GetPolicy(ctx, "projects/p")
			return err
		}},
		{"GetNode", func(ctx context.Context, a API, _ AdminAPI) error { _, err := a.GetNode(ctx, "folders/7"); return err }},
		{"ListServiceAccounts", func(ctx context.Context, _ API, a AdminAPI) error {
			_, err := a.ListServiceAccounts(ctx, "p")
			return err
		}},
		{"ListServiceAccountKeys", func(ctx context.Context, _ API, a AdminAPI) error {
			_, err := a.ListServiceAccountKeys(ctx, "p", "a@p.iam.gserviceaccount.com")
			return err
		}},
		{"ListRoles", func(ctx context.Context, _ API, a AdminAPI) error {
			_, err := a.ListRoles(ctx, "projects/p")
			return err
		}},
		{"GetRole", func(ctx context.Context, _ API, a AdminAPI) error {
			_, err := a.GetRole(ctx, "roles/viewer")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &fakeGoogle{err: status.Error(codes.PermissionDenied, "caller lacks permission")}
			api, admin := serve(t, f)
			err := tc.call(context.Background(), api, admin)
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
