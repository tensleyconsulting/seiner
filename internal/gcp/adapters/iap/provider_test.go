package iap

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/iam"
	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	number      string
	numberErr   error
	backends    []Backend
	settings    map[string]Settings // keyed by IAP resource name
	settingsErr error
	policies    map[string][]Binding
	err         error // returned from ListBackends and GetPolicy when set
	mu          *sync.Mutex
	calls       *[]string
}

func (f fakeAPI) called(name string) {
	if f.calls != nil {
		f.mu.Lock()
		*f.calls = append(*f.calls, name)
		f.mu.Unlock()
	}
}

func (f fakeAPI) ProjectNumber(_ context.Context, project string) (string, error) {
	f.called("number " + project)
	return f.number, f.numberErr
}

func (f fakeAPI) ListBackends(context.Context, string) ([]Backend, error) {
	f.called("backends")
	if f.err != nil {
		return nil, f.err
	}
	return f.backends, nil
}

func (f fakeAPI) GetSettings(_ context.Context, resource string) (Settings, error) {
	f.called("settings " + resource)
	if f.settingsErr != nil {
		return Settings{}, f.settingsErr
	}
	return f.settings[resource], nil
}

func (f fakeAPI) GetPolicy(_ context.Context, resource string) ([]Binding, error) {
	f.called("policy " + resource)
	if f.err != nil {
		return nil, f.err
	}
	return f.policies[resource], nil
}

var created = time.Date(2025, 2, 3, 10, 0, 0, 0, time.UTC)

func sample() fakeAPI {
	return fakeAPI{
		number: "123",
		backends: []Backend{
			{Name: "admin-backend", Description: "Internal admin UI", Scheme: "EXTERNAL_MANAGED", Protocol: "HTTPS", Created: created},
			{Name: "grafana", Region: "us-central1", Scheme: "INTERNAL_MANAGED", Protocol: "HTTP", ClientID: "123-abc.apps.googleusercontent.com"},
		},
		settings: map[string]Settings{
			"projects/123/iap_web/compute/services/admin-backend": {
				ReauthMethod: "SECURE_KEY", ReauthMaxAge: "3600s", ReauthPolicy: "MINIMUM",
				AllowedDomains: []string{"example.com", "corp.example.com"}, CORSPreflight: true,
				ProgrammaticClients: []string{"cli-client.apps.googleusercontent.com"},
				AccessDeniedPage:    "https://example.com/denied",
			},
		},
		policies: map[string][]Binding{
			"projects/123/iap_web/compute/services/admin-backend": {
				{Role: "roles/iap.httpsResourceAccessor", Members: []string{"group:platform@example.com"}},
				{Role: "roles/iap.httpsResourceAccessor", Members: []string{"group:contractors@example.com"}, Condition: "Corp devices"},
			},
		},
		mu: &sync.Mutex{},
	}
}

func TestListScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope     string
		wantCalls int
		wantN     int
	}{
		{"projects/p", 4, 2}, // backends, number, one settings read per app
		{"folders/1", 0, 0},
		{"organizations/1", 0, 0},
		{"", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			api := sample()
			var calls []string
			api.calls = &calls
			got, err := NewApps(api, nil).List(context.Background(), tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.wantN || len(calls) != tc.wantCalls {
				t.Fatalf("len=%d calls=%v, want %d and %d calls", len(got), calls, tc.wantN, tc.wantCalls)
			}
		})
	}
}

func TestNoBackendsSkipsLookups(t *testing.T) {
	t.Parallel()
	api := sample()
	api.backends = nil
	var calls []string
	api.calls = &calls
	got, err := NewApps(api, nil).List(context.Background(), "projects/p")
	if err != nil || len(got) != 0 || !slices.Equal(calls, []string{"backends"}) {
		t.Fatalf("got %v err=%v calls=%v", got, err, calls)
	}
}

func TestAppMapping(t *testing.T) {
	t.Parallel()
	got, err := NewApps(sample(), nil).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	admin, grafana := got[0], got[1]
	if admin.ID != "projects/p/iap_web/compute/services/admin-backend" || admin.Name != "admin-backend" ||
		admin.Service != model.ServiceIAPApp || admin.ScopeID != "projects/p" || admin.Location != "global" ||
		admin.Status != "" || !admin.Created.Equal(created) || admin.Description != "Internal admin UI" {
		t.Fatalf("admin %+v", admin)
	}
	want := map[string]string{
		"loadBalancingScheme": "EXTERNAL_MANAGED",
		"protocol":            "HTTPS",
		"oauthClient":         "Google-managed",
		"reauth":              "SECURE_KEY every 1h0m0s (minimum)",
		"allowedDomains":      "example.com, corp.example.com",
		"corsPreflight":       "allowed",
		"accessDeniedPage":    "https://example.com/denied",
	}
	for k, v := range want {
		if admin.Metadata[k] != v {
			t.Errorf("%s=%q want %q", k, admin.Metadata[k], v)
		}
	}
	var keys []string
	for _, f := range admin.Fields {
		keys = append(keys, f.Key)
	}
	if want := []string{"loadBalancingScheme", "protocol", "oauthClient", "reauth", "allowedDomains", "corsPreflight", "accessDeniedPage"}; !slices.Equal(keys, want) {
		t.Errorf("fields %v, want %v", keys, want)
	}
	if len(admin.Lists) != 1 || admin.Lists[0].Items[0] != "cli-client.apps.googleusercontent.com" {
		t.Errorf("lists %+v", admin.Lists)
	}
	if len(admin.Facets) != 1 || admin.Facets[0].ID != model.FacetPermissions || admin.Facets[0].Label != "Access" {
		t.Errorf("facets %+v", admin.Facets)
	}
	if grafana.ID != "projects/p/iap_web/compute-us-central1/services/grafana" || grafana.Location != "us-central1" ||
		grafana.Metadata["oauthClient"] != "123-abc.apps.googleusercontent.com" || grafana.Metadata["reauth"] != "" {
		t.Errorf("grafana %+v", grafana)
	}
}

func TestSettingsFailureStillLists(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*fakeAPI){
		"settings": func(f *fakeAPI) { f.settingsErr = errors.New("denied") },
		"number":   func(f *fakeAPI) { f.numberErr = errors.New("denied") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := sample()
			mutate(&api)
			got, err := NewApps(api, nil).List(context.Background(), "projects/p")
			if err != nil || len(got) != 2 {
				t.Fatalf("len=%d err=%v", len(got), err)
			}
			if got[0].Metadata["reauth"] != "" || got[0].Metadata["oauthClient"] != "Google-managed" {
				t.Errorf("%+v", got[0].Metadata)
			}
		})
	}
}

func TestReauth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   Settings
		want string
	}{
		{Settings{}, ""},
		{Settings{ReauthMethod: "METHOD_UNSPECIFIED", ReauthMaxAge: "60s"}, ""},
		{Settings{ReauthMethod: "LOGIN"}, "LOGIN"},
		{Settings{ReauthMethod: "LOGIN", ReauthMaxAge: "86400s", ReauthPolicy: "DEFAULT"}, "LOGIN every 24h0m0s (default)"},
		{Settings{ReauthMethod: "PASSWORD", ReauthMaxAge: "soon", ReauthPolicy: "POLICY_TYPE_UNSPECIFIED"}, "PASSWORD every soon"},
	}
	for _, tc := range cases {
		if got := reauth(tc.in); got != tc.want {
			t.Errorf("reauth(%+v)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestListRelated(t *testing.T) {
	t.Parallel()
	api := sample()
	apps, err := NewApps(api, nil).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	got, truncated, err := NewApps(api, nil).ListRelated(context.Background(), apps[0], model.FacetPermissions)
	if err != nil || truncated || len(got) != 2 {
		t.Fatalf("len=%d truncated=%v err=%v", len(got), truncated, err)
	}
	for _, r := range got {
		if r.Service != model.ServiceIAMBinding || r.ParentID != apps[0].ID || r.FacetID != model.FacetPermissions || r.ScopeID != "projects/p" {
			t.Errorf("binding %+v", r)
		}
	}
	if got[0].ID != "projects/p/iap_web/compute/services/admin-backend/iam/bindings/roles%2Fiap.httpsResourceAccessor" ||
		got[1].Metadata["condition"] != "Corp devices" {
		t.Errorf("bindings %+v", got)
	}
}

// fakeCRM serves CRM nodes and their policies.
type fakeCRM struct {
	nodes    map[string]iam.Node
	policies map[string][]iam.Binding
	failing  map[string]bool // policies that fail to read
}

func (f fakeCRM) GetPolicy(_ context.Context, name string) (iam.Policy, error) {
	if f.failing[name] {
		return iam.Policy{}, errors.New("denied")
	}
	return iam.Policy{Bindings: f.policies[name]}, nil
}

func (f fakeCRM) GetNode(_ context.Context, name string) (iam.Node, error) {
	n, ok := f.nodes[name]
	if !ok {
		return iam.Node{}, errors.New("no node " + name)
	}
	return n, nil
}

func TestListRelatedInherited(t *testing.T) {
	t.Parallel()
	api := sample()
	api.policies["projects/123/iap_web/compute-us-central1"] = []Binding{
		{Role: "roles/iap.httpsResourceAccessor", Members: []string{"group:oncall@example.com"}},
	}
	api.policies["projects/123/iap_web"] = []Binding{
		{Role: "roles/iap.httpsResourceAccessor", Members: []string{"group:staff@example.com"}},
	}
	crm := fakeCRM{
		nodes: map[string]iam.Node{
			"projects/p":      {DisplayName: "P", Parent: "folders/2"},
			"folders/2":       {DisplayName: "prod", Parent: "organizations/1"},
			"organizations/1": {DisplayName: "example.com"},
		},
		policies: map[string][]iam.Binding{
			"projects/p": {
				{Role: "roles/owner", Members: []string{"user:owner@example.com"}},
				{Role: "roles/iap.tunnelResourceAccessor", Members: []string{"group:sre@example.com"}},
				{Role: "roles/iap.httpsResourceAccessor", Members: []string{"user:bob@example.com"}},
			},
			"organizations/1": {
				{Role: "roles/iap.httpsResourceAccessor", Members: []string{"domain:example.com"}, Condition: "Managed devices"},
			},
		},
		failing: map[string]bool{"folders/2": true},
	}
	p := NewApps(api, crm)
	apps, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	grafana := apps[1]
	got, _, err := p.ListRelated(context.Background(), grafana, model.FacetPermissions)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ from, origin, member string }
	var rows []row
	for _, r := range got {
		if r.ParentID != grafana.ID || r.FacetID != model.FacetPermissions || r.ScopeID != "projects/p" || !r.IsBinding() ||
			!strings.HasPrefix(r.ID, grafana.ID+"/") {
			t.Errorf("binding %+v", r)
		}
		rows = append(rows, row{r.Metadata[model.MetaInheritedFrom], string(r.Origin), r.List(model.ListMembers)[0]})
	}
	want := []row{
		{"projects/123/iap_web/compute-us-central1", string(model.ServiceIAPApp), "group:oncall@example.com"},
		{"projects/123/iap_web", string(model.ServiceIAPApp), "group:staff@example.com"},
		{"projects/p", string(model.ServiceProject), "user:bob@example.com"},
		{"organizations/1", string(model.ServiceOrganization), "domain:example.com"},
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("rows = %+v, want %+v", rows, want)
	}

	own, groups := model.SplitInherited(grafana, got)
	if len(own) != 0 {
		t.Errorf("own = %+v", own)
	}
	var names []string
	for _, g := range groups {
		names = append(names, g.Node.Name)
	}
	if want := []string{"From Backend Services (us-central1) (1)", "From All Web Services (1)", "From p (1)", "From example.com (1)"}; !slices.Equal(names, want) {
		t.Errorf("groups = %q, want %q", names, want)
	}
}

func TestListRelatedGlobalLevel(t *testing.T) {
	t.Parallel()
	api := sample()
	api.policies["projects/123/iap_web/compute"] = []Binding{{Role: "roles/iap.settingsAdmin", Members: []string{"group:platform@example.com"}}}
	p := NewApps(api, nil)
	apps, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := p.ListRelated(context.Background(), apps[0], model.FacetPermissions)
	if err != nil || len(got) != 3 {
		t.Fatalf("got %d err=%v", len(got), err)
	}
	if b := got[2]; b.Metadata[model.MetaInheritedFrom] != "projects/123/iap_web/compute" || b.Metadata[model.MetaInheritedName] != "Backend Services" {
		t.Errorf("inherited %+v", b)
	}
}

func TestWebRole(t *testing.T) {
	t.Parallel()
	for role, want := range map[string]bool{
		"roles/iap.httpsResourceAccessor":  true,
		"roles/iap.settingsAdmin":          true,
		"roles/iap.tunnelResourceAccessor": false,
		"roles/owner":                      false,
		"projects/p/roles/iapUser":         false,
	} {
		if got := webRole(role); got != want {
			t.Errorf("webRole(%q) = %v, want %v", role, got, want)
		}
	}
}

func TestListRelatedIgnoresOthers(t *testing.T) {
	t.Parallel()
	api := fakeAPI{err: errors.New("must not be called"), numberErr: errors.New("must not be called")}
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: "projects/p/buckets/b", Service: model.ServiceCloudStorage}, model.FacetPermissions},
		{"other facet", model.Resource{ID: "projects/p/iap_web/compute/services/s", Service: model.ServiceIAPApp}, model.FacetRules},
		{"binding id", model.Resource{ID: "projects/p/iap_web/compute/services/s/iam/bindings/r", Service: model.ServiceIAPApp}, model.FacetPermissions},
		{"not compute", model.Resource{ID: "projects/p/iap_web/appengine-p/services/default", Service: model.ServiceIAPApp}, model.FacetPermissions},
		{"malformed", model.Resource{ID: "nope", Service: model.ServiceIAPApp}, model.FacetPermissions},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, truncated, err := NewApps(api, nil).ListRelated(context.Background(), tc.parent, tc.facet)
			if got != nil || truncated || err != nil {
				t.Fatalf("got %v %v %v", got, truncated, err)
			}
		})
	}
}

func TestErrorPropagation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	boom := errors.New("boom")
	api := sample()
	api.err = boom
	if _, err := NewApps(api, nil).List(ctx, "projects/p"); !errors.Is(err, boom) {
		t.Errorf("List err=%v", err)
	}
	app := appResource("p", Backend{Name: "admin-backend"}, nil)
	if _, _, err := NewApps(api, nil).ListRelated(ctx, app, model.FacetPermissions); !errors.Is(err, boom) {
		t.Errorf("ListRelated err=%v", err)
	}
	api = sample()
	api.numberErr = boom
	if _, _, err := NewApps(api, nil).ListRelated(ctx, app, model.FacetPermissions); !errors.Is(err, boom) {
		t.Errorf("ListRelated number err=%v", err)
	}
}

func TestNoAPI(t *testing.T) {
	t.Parallel()
	if _, err := NewApps(nil, nil).List(context.Background(), "projects/p"); err == nil {
		t.Fatal("want error without an API")
	}
}

func TestListPermissions(t *testing.T) {
	t.Parallel()
	if got, want := NewApps(nil, nil).ListPermissions("projects/p"), []string{"compute.backendServices.list", "resourcemanager.projects.get"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := NewApps(nil, nil).ListPermissions("folders/1"); got != nil {
		t.Errorf("folder permissions %v", got)
	}
}
