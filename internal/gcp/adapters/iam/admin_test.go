package iam

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAdmin struct {
	accounts map[string][]ServiceAccount    // by project
	roles    map[string][]Role              // by parent
	role     map[string]Role                // by name, for GetRole
	keys     map[string][]ServiceAccountKey // by email
	policies map[string]Policy              // by email
	err      error
	calls    *int
}

func (f fakeAdmin) ListServiceAccounts(_ context.Context, project string) ([]ServiceAccount, error) {
	if f.calls != nil {
		*f.calls++
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.accounts[project], nil
}

func (f fakeAdmin) ListRoles(_ context.Context, parent string) ([]Role, error) {
	if f.calls != nil {
		*f.calls++
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.roles[parent], nil
}

func (f fakeAdmin) GetRole(_ context.Context, name string) (Role, error) {
	if f.calls != nil {
		*f.calls++
	}
	if f.err != nil {
		return Role{}, f.err
	}
	r, ok := f.role[name]
	if !ok {
		return Role{}, model.ErrNotFound
	}
	return r, nil
}

func (f fakeAdmin) ListServiceAccountKeys(_ context.Context, _, email string) ([]ServiceAccountKey, error) {
	if f.calls != nil {
		*f.calls++
	}
	if f.err != nil {
		return nil, f.err
	}
	return slices.Clone(f.keys[email]), nil
}

func (f fakeAdmin) GetServiceAccountPolicy(_ context.Context, _, email string) (Policy, error) {
	if f.calls != nil {
		*f.calls++
	}
	if f.err != nil {
		return Policy{}, f.err
	}
	return f.policies[email], nil
}

func TestServiceAccountsScopes(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"folders/1", "organizations/1", "", "p"} {
		calls := 0
		got, err := NewServiceAccounts(fakeAdmin{calls: &calls}, nil).List(context.Background(), scope)
		if err != nil || got != nil || calls != 0 {
			t.Errorf("%q: %v %v, %d calls", scope, got, err, calls)
		}
	}
}

func TestServiceAccountMapping(t *testing.T) {
	t.Parallel()
	p := NewServiceAccounts(fakeAdmin{accounts: map[string][]ServiceAccount{"p": {
		{Email: "deployer@p.iam.gserviceaccount.com", DisplayName: "Deployer", Description: "CI", UniqueID: "101", OAuth2ClientID: "101"},
		{Email: "123-compute@developer.gserviceaccount.com", Disabled: true},
	}}}, nil)
	got, err := p.List(context.Background(), "projects/p")
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	cases := []struct {
		r                  model.Resource
		id, name, state    string
		status             model.ResourceStatus
		desc               string
		fields             []string
		uniqueID, consoleq string
	}{
		{got[0], "projects/p/serviceAccounts/deployer@p.iam.gserviceaccount.com", "deployer", "ENABLED", model.StatusHealthy, "CI",
			[]string{"email", "displayName", model.MetaUniqueID, "oauth2ClientId"}, "101", "/iam-admin/serviceaccounts/details/101?project=p"},
		{got[1], "projects/p/serviceAccounts/123-compute@developer.gserviceaccount.com", "123-compute@developer.gserviceaccount.com", "DISABLED", model.StatusWarning, "",
			[]string{"email"}, "", "/iam-admin/serviceaccounts?project=p"},
	}
	for _, tc := range cases {
		r := tc.r
		if r.ID != tc.id || r.Name != tc.name || r.Service != model.ServiceServiceAccount || r.ScopeID != "projects/p" {
			t.Errorf("identity: %s %s %s %s", r.ID, r.Name, r.Service, r.ScopeID)
		}
		if r.State != tc.state || r.Status != tc.status || r.Description != tc.desc {
			t.Errorf("%s: state %s status %s desc %q", r.Name, r.State, r.Status, r.Description)
		}
		var keys []string
		for _, f := range r.Fields {
			keys = append(keys, f.Key)
		}
		if !slices.Equal(keys, tc.fields) {
			t.Errorf("%s: fields %v, want %v", r.Name, keys, tc.fields)
		}
		if got := model.Normalize(r).ConsoleURL(); got != "https://console.cloud.google.com"+tc.consoleq {
			t.Errorf("%s: console %s", r.Name, got)
		}
	}
}

func TestServiceAccountKeys(t *testing.T) {
	t.Parallel()
	day := func(d int) time.Time { return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC) }
	email := "deployer@p.iam.gserviceaccount.com"
	admin := fakeAdmin{
		accounts: map[string][]ServiceAccount{"p": {{Email: email}}},
		keys: map[string][]ServiceAccountKey{email: {
			{ID: "sys1", Type: "SYSTEM_MANAGED", Origin: "GOOGLE_PROVIDED", Algorithm: "KEY_ALG_RSA_2048", ValidAfter: day(2), ValidBefore: time.Now().AddDate(0, 0, 10)},
			{ID: "old", Type: "USER_MANAGED", Origin: "GOOGLE_PROVIDED", Algorithm: "KEY_ALG_RSA_2048", ValidAfter: day(1), ValidBefore: day(3)},
			{ID: "new", Type: "USER_MANAGED", Origin: "USER_PROVIDED", Algorithm: "KEY_ALG_RSA_4096", ValidAfter: day(5), ValidBefore: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)},
			{ID: "off", Type: "USER_MANAGED", ValidAfter: day(4), Disabled: true},
		}},
	}
	p := NewServiceAccounts(admin, nil)
	accounts, err := p.List(context.Background(), "projects/p")
	if err != nil || len(accounts) != 1 {
		t.Fatalf("%+v %v", accounts, err)
	}
	acct := accounts[0]
	if len(acct.Facets) != 2 || acct.Facets[0].ID != model.FacetKeys || acct.Facets[1].ID != model.FacetPermissions {
		t.Fatalf("account facets: %+v", acct.Facets)
	}
	got, truncated, err := p.ListRelated(context.Background(), acct, model.FacetKeys)
	if err != nil || truncated || len(got) != 4 {
		t.Fatalf("%+v %v %v", got, truncated, err)
	}
	var order []string
	for _, k := range got {
		order = append(order, k.Name+":"+k.Badge)
	}
	if want := []string{"new:USER", "off:USER", "old:EXPIRED", "sys1:"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want user-managed first, newest first: %v", order, want)
	}
	k := got[0]
	if k.ID != acct.ID+"/keys/new" || k.Service != model.ServiceServiceAccountKey || k.ParentID != acct.ID || k.FacetID != model.FacetKeys ||
		k.ScopeID != "projects/p" || k.Status != model.StatusHealthy || k.State != "ENABLED" || !k.Created.Equal(day(5)) ||
		k.Metadata["expires"] != "never" || k.Metadata["algorithm"] != "RSA_4096" || k.Metadata["origin"] != "USER_PROVIDED" {
		t.Fatalf("user key: %+v", k)
	}
	if off := got[1]; off.Status != model.StatusWarning || off.State != "DISABLED" {
		t.Fatalf("disabled key: %+v", off)
	}
	if old := got[2]; old.Status != model.StatusWarning || old.Metadata["expires"] != "2026-01-03 00:00 UTC" {
		t.Fatalf("expired key: %+v", old)
	}
}

func TestServiceAccountKeysIgnoresOthers(t *testing.T) {
	t.Parallel()
	calls := 0
	p := NewServiceAccounts(fakeAdmin{calls: &calls}, nil)
	agent := agentResource("p", "service-1@gcp-sa-foo.iam.gserviceaccount.com", nil)
	if len(agent.Facets) != 0 {
		t.Fatalf("a service agent has no keys facet: %+v", agent.Facets)
	}
	for _, tc := range []struct {
		parent model.Resource
		facet  string
	}{
		{model.Resource{ID: "projects/p/serviceAccounts/a@p.iam.gserviceaccount.com", Service: model.ServiceCustomRole}, model.FacetKeys},
		{model.Resource{ID: "projects/p/serviceAccounts/a@p.iam.gserviceaccount.com", Service: model.ServiceServiceAccount}, model.FacetVersions},
		{model.Resource{ID: "projects/p/locations/global/keyRings/r", Service: model.ServiceServiceAccount}, model.FacetKeys},
	} {
		if got, truncated, err := p.ListRelated(context.Background(), tc.parent, tc.facet); got != nil || truncated || err != nil {
			t.Errorf("%s %s: %v %v %v", tc.parent.ID, tc.facet, got, truncated, err)
		}
	}
	if calls != 0 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestServiceAgents(t *testing.T) {
	t.Parallel()
	admin := fakeAdmin{accounts: map[string][]ServiceAccount{"p": {
		{Email: "deployer@p.iam.gserviceaccount.com"},
		{Email: "123-compute@developer.gserviceaccount.com"},
	}}}
	policy := fakeAPI{policies: map[string]Policy{"projects/p": {Bindings: []Binding{
		{Role: "roles/editor", Members: []string{
			"serviceAccount:123@cloudservices.gserviceaccount.com",
			"serviceAccount:123-compute@developer.gserviceaccount.com", // listed already
			"serviceAccount:deployer@p.iam.gserviceaccount.com",        // listed already
			"serviceAccount:terraform@seed.iam.gserviceaccount.com",    // another project's
			"user:alice@example.com",
		}},
		{Role: "roles/container.serviceAgent", Members: []string{"serviceAccount:service-123@container-engine-robot.iam.gserviceaccount.com"}},
		{Role: "roles/compute.serviceAgent", Members: []string{"serviceAccount:123@cloudservices.gserviceaccount.com"}},
		{Role: "roles/editor", Condition: "x", Members: []string{"serviceAccount:123@cloudservices.gserviceaccount.com"}},
		{Role: "roles/viewer", Members: []string{"deleted:serviceAccount:service-9@gcp-sa-x.iam.gserviceaccount.com?uid=1"}},
	}}}}

	got, err := NewServiceAccounts(admin, policy).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range got {
		names = append(names, r.Name)
	}
	want := []string{"deployer", "123-compute@developer.gserviceaccount.com",
		"123@cloudservices.gserviceaccount.com", "service-123@container-engine-robot.iam.gserviceaccount.com"}
	if !slices.Equal(names, want) {
		t.Fatalf("names %v, want %v", names, want)
	}
	agent := got[2]
	if agent.ID != "projects/p/serviceAccounts/123@cloudservices.gserviceaccount.com" || agent.ScopeID != "projects/p" || !agent.IsGoogleServiceAccount() {
		t.Errorf("agent %+v", agent)
	}
	if roles := agent.List(model.ListRoles); !slices.Equal(roles, []string{"roles/compute.serviceAgent", "roles/editor"}) {
		t.Errorf("roles %v", roles)
	}
	if got := model.Normalize(agent).ConsoleURL(); got != "https://console.cloud.google.com/iam-admin/serviceaccounts?project=p" {
		t.Errorf("console %s", got)
	}
}

func TestServiceAgentsPolicyErrors(t *testing.T) {
	t.Parallel()
	admin := fakeAdmin{accounts: map[string][]ServiceAccount{"p": {{Email: "deployer@p.iam.gserviceaccount.com"}}}}
	got, err := NewServiceAccounts(admin, fakeAPI{err: errors.New("permission denied")}).List(context.Background(), "projects/p")
	if err != nil || len(got) != 1 {
		t.Fatalf("an unreadable policy should only drop agents: %v %v", got, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewServiceAccounts(admin, cancelingAPI{cancel}).agents(ctx, "p", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}

// cancelingAPI fails GetPolicy as a load whose context ended does.
type cancelingAPI struct{ cancel context.CancelFunc }

func (a cancelingAPI) GetPolicy(ctx context.Context, _ string) (Policy, error) {
	a.cancel()
	return Policy{}, ctx.Err()
}

func (a cancelingAPI) GetNode(context.Context, string) (Node, error) { return Node{}, nil }

func TestCustomRolesScopes(t *testing.T) {
	t.Parallel()
	roles := map[string][]Role{
		"projects/p":      {{Name: "projects/p/roles/deployer"}},
		"organizations/1": {{Name: "organizations/1/roles/reviewer"}},
	}
	cases := []struct {
		scope string
		calls int
		ids   []string
	}{
		{"projects/p", 1, []string{"projects/p/roles/deployer"}},
		{"organizations/1", 1, []string{"organizations/1/roles/reviewer"}},
		{"folders/1", 0, nil},
		{"", 0, nil},
	}
	for _, tc := range cases {
		calls := 0
		got, err := NewCustomRoles(NewRoles(fakeAdmin{roles: roles, calls: &calls})).List(context.Background(), tc.scope)
		if err != nil || calls != tc.calls {
			t.Errorf("%q: err %v, %d calls", tc.scope, err, calls)
		}
		var ids []string
		for _, r := range got {
			ids = append(ids, r.ID)
			if r.ScopeID != tc.scope {
				t.Errorf("%s: scope %s", r.ID, r.ScopeID)
			}
		}
		if !slices.Equal(ids, tc.ids) {
			t.Errorf("%q: ids %v, want %v", tc.scope, ids, tc.ids)
		}
	}
}

func TestCustomRoleMapping(t *testing.T) {
	t.Parallel()
	perms := []string{"run.services.get", "run.services.update"}
	r := roleResource("projects/p", Role{Name: "projects/p/roles/deployer", Title: "Deployer", Description: "Ships", Stage: "GA", Permissions: perms})
	if r.Name != "deployer" || r.Service != model.ServiceCustomRole || r.State != "GA" || r.Status != model.StatusUnknown || r.Description != "Ships" {
		t.Errorf("%+v", r)
	}
	want := []model.Field{{Key: "title", Value: "Deployer"}, {Key: "role", Value: "projects/p/roles/deployer"}, {Key: "permissions", Value: "2"}}
	if !slices.Equal(r.Fields, want) {
		t.Errorf("fields %v", r.Fields)
	}
	if f, ok := r.InlineFacet(); !ok || f.ID != model.FacetRolePermissions {
		t.Errorf("facets %+v", r.Facets)
	}
	if len(r.Lists) != 0 {
		t.Errorf("lists %+v; permissions list under the role instead", r.Lists)
	}
}

func TestRolePermissions(t *testing.T) {
	t.Parallel()
	api := fakeAdmin{
		roles: map[string][]Role{"projects/p": {{Name: "projects/p/roles/deployer", Permissions: []string{"run.services.get"}}}},
		role:  map[string]Role{"roles/viewer": {Name: "roles/viewer", Title: "Viewer", Description: "Read access", Permissions: []string{"compute.instances.get", "compute.instances.list"}}},
	}
	binding := model.BindingResource("projects/p", "projects/p", "roles/viewer", []string{"user:a@example.com"}, "")
	custom := roleResource("projects/p", Role{Name: "projects/p/roles/deployer"})
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
		want   []string
		calls  int
	}{
		{"binding", binding, model.FacetRolePermissions, []string{"compute.instances.get", "compute.instances.list"}, 1},
		{"custom role, already listed", custom, model.FacetRolePermissions, []string{"run.services.get"}, 0},
		{"other facet", binding, model.FacetPermissions, nil, 0},
		{"other service", model.Resource{ID: "projects/p/buckets/b", Service: model.ServiceCloudStorage}, model.FacetRolePermissions, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			api := api
			api.calls = &calls
			roles := NewRoles(api)
			if _, err := NewCustomRoles(roles).List(context.Background(), "projects/p"); err != nil {
				t.Fatal(err)
			}
			calls = 0
			p := NewRolePermissions(roles)
			for range 2 { // the second read comes from the cache
				got, truncated, err := p.ListRelated(context.Background(), tc.parent, tc.facet)
				if err != nil || truncated {
					t.Fatalf("%v %v", err, truncated)
				}
				var names []string
				for _, r := range got {
					names = append(names, r.Name)
					if r.Service != model.ServiceIAMPermission || r.ParentID != tc.parent.ID || r.FacetID != model.FacetRolePermissions || r.ScopeID != tc.parent.ScopeID {
						t.Errorf("%+v", r)
					}
					if tc.name == "binding" && (r.Metadata[model.MetaRoleTitle] != "Viewer" || r.Metadata[model.MetaRoleDescription] != "Read access") {
						t.Errorf("role details %v", r.Metadata)
					}
				}
				if !slices.Equal(names, tc.want) {
					t.Errorf("got %v, want %v", names, tc.want)
				}
			}
			if calls != tc.calls {
				t.Errorf("%d API calls, want %d", calls, tc.calls)
			}
		})
	}
}

func TestRolePermissionsErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("permission denied")
	binding := model.BindingResource("projects/p", "projects/p", "roles/viewer", nil, "")
	if _, _, err := NewRolePermissions(NewRoles(fakeAdmin{err: boom})).ListRelated(context.Background(), binding, model.FacetRolePermissions); !errors.Is(err, boom) {
		t.Errorf("got %v", err)
	}
	if _, _, err := NewRolePermissions(nil).ListRelated(context.Background(), binding, model.FacetRolePermissions); err == nil {
		t.Error("no admin API: want an error")
	}
}

func TestRoleStatus(t *testing.T) {
	t.Parallel()
	cases := map[string]model.ResourceStatus{
		"GA": model.StatusUnknown, "BETA": model.StatusUnknown, "ALPHA": model.StatusUnknown, "EAP": model.StatusUnknown,
		"DEPRECATED": model.StatusWarning, "DISABLED": model.StatusWarning, "": model.StatusUnknown, "NEW": model.StatusUnknown,
	}
	for stage, want := range cases {
		if got := roleStatus(stage); got != want {
			t.Errorf("%q: %s, want %s", stage, got, want)
		}
	}
}

func TestAdminErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("permission denied")
	api := fakeAdmin{err: boom}
	if _, err := NewServiceAccounts(api, nil).List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Errorf("service accounts: %v", err)
	}
	if _, err := NewCustomRoles(NewRoles(api)).List(context.Background(), "organizations/1"); !errors.Is(err, boom) {
		t.Errorf("custom roles: %v", err)
	}
}
