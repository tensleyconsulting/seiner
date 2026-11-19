package acm

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/vpcsc"
	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	bindings []Binding
	sa       []Binding
	err      error // returned from ListBindings when set
	saErr    error
	groups   map[string]Group
	groupErr error
	levels   map[string][]vpcsc.Level
	getErr   error // returned from GetBinding and ListLevels when set
	mu       *sync.Mutex
	calls    *[]string
}

func (f fakeAPI) called(name string) {
	if f.calls != nil {
		f.mu.Lock()
		*f.calls = append(*f.calls, name)
		f.mu.Unlock()
	}
}

func (f fakeAPI) ListBindings(_ context.Context, org string) ([]Binding, error) {
	f.called("bindings " + org)
	if f.err != nil {
		return nil, f.err
	}
	return f.bindings, nil
}

func (f fakeAPI) ListServiceAccountBindings(_ context.Context, org string) ([]Binding, error) {
	f.called("sa " + org)
	if f.saErr != nil {
		return nil, f.saErr
	}
	return f.sa, nil
}

func (f fakeAPI) GetBinding(_ context.Context, org, name string) (Binding, error) {
	f.called("get " + org + "/" + name)
	if f.getErr != nil {
		return Binding{}, f.getErr
	}
	for _, b := range slices.Concat(f.bindings, f.sa) {
		if b.Name == name {
			return b, nil
		}
	}
	return Binding{}, model.ErrNotFound
}

func (f fakeAPI) ListLevels(_ context.Context, policy string) ([]vpcsc.Level, error) {
	f.called("levels " + policy)
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.levels[policy], nil
}

func (f fakeAPI) Group(_ context.Context, key string) (Group, error) {
	f.called("group " + key)
	if f.groupErr != nil {
		return Group{}, f.groupErr
	}
	g, ok := f.groups[key]
	if !ok {
		return Group{}, errors.New("404 group not found")
	}
	return g, nil
}

func sample() fakeAPI {
	return fakeAPI{
		bindings: []Binding{
			{
				Name: "b1", GroupKey: "01abc", Policy: "100", Levels: []string{"corp_devices"},
				Session: &Session{Length: "28800s", LengthEnabled: true, ReauthMethod: "LOGIN", MaxInactivity: "1800s"},
				Scoped: []Scoped{
					{App: "Google Cloud SDK", Levels: []string{"corp_network"}},
					{Project: "projects/42", DryRunLevels: []string{"corp_devices"}},
				},
			},
			{Name: "b2", GroupKey: "02def", Policy: "100", Levels: []string{"corp_network"}, DryRunLevels: []string{"corp_devices"}},
			{Name: "b3", GroupKey: "01abc", Levels: []string{"break_glass"}},
			{Name: "b4", GroupKey: "03ghi", Levels: []string{"corp_network"}},
		},
		sa: []Binding{
			{Name: "s1", ServiceAccount: "tf@seed.iam.gserviceaccount.com", Levels: []string{"corp_network"}},
			{Name: "s2", SAProject: "42", Levels: []string{"corp_network"}},
			{Name: "b4", GroupKey: "03ghi"}, // returned by both lists
		},
		groups: map[string]Group{
			"01abc": {Email: "eng@example.com", DisplayName: "Engineering"},
			"02def": {Email: "contractors@example.com", DisplayName: "contractors@example.com"},
		},
		levels: map[string][]vpcsc.Level{
			"100": {
				{Name: "corp_network", Conditions: []vpcsc.Condition{{IPSubnetworks: []string{"203.0.113.0/24"}}}},
				{Name: "corp_devices", Conditions: []vpcsc.Condition{{Device: &vpcsc.Device{Screenlock: true}}}},
				{Name: "break_glass", Expression: "levels.corp_network && request.auth.claims.sec"},
			},
		},
		mu: &sync.Mutex{},
	}
}

func withCalls(f fakeAPI) (fakeAPI, *[]string) {
	var calls []string
	f.calls = &calls
	return f, &calls
}

func names(rs []model.Resource) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func TestListScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope     string
		wantN     int
		wantPerms []string
	}{
		{"organizations/9", 6, []string{"accesscontextmanager.gcpUserAccessBindings.list"}},
		{"folders/7", 0, nil},
		{"projects/p", 0, nil},
		{"", 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			api, calls := withCalls(sample())
			p := NewBindings(api)
			got, err := p.List(context.Background(), tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.wantN {
				t.Fatalf("len=%d, want %d: %v", len(got), tc.wantN, names(got))
			}
			if tc.wantN == 0 && len(*calls) != 0 {
				t.Fatalf("calls %v for a scope it does not serve", *calls)
			}
			if perms := p.ListPermissions(tc.scope); !slices.Equal(perms, tc.wantPerms) {
				t.Fatalf("perms %v, want %v", perms, tc.wantPerms)
			}
		})
	}
}

func TestBindingMapping(t *testing.T) {
	t.Parallel()
	got, err := NewBindings(sample()).List(context.Background(), "organizations/9")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"eng@example.com", "contractors@example.com", "eng@example.com", "03ghi",
		"tf@seed.iam.gserviceaccount.com", "service accounts of projects/42"}
	if !slices.Equal(names(got), want) {
		t.Fatalf("names %q, want %q", names(got), want)
	}
	eng := got[0]
	if eng.ID != "organizations/9/gcpUserAccessBindings/b1" || eng.Service != model.ServiceAccessBinding ||
		eng.ScopeID != "organizations/9" || eng.Location != "global" {
		t.Fatalf("eng %+v", eng)
	}
	if p, ok := model.ParseName(eng.ID); !ok || p.Scope != eng.ScopeID {
		t.Fatalf("ParseName(%s) = %+v", eng.ID, p)
	}
	md := eng.Metadata
	if md["principalType"] != "group" || md["group"] != "eng@example.com" || md["groupId"] != "01abc" ||
		md["displayName"] != "Engineering" || md["accessLevels"] != "corp_devices" ||
		md["session"] != "reauth LOGIN every 8h0m0s, idle 30m0s" || md["scopedAccess"] != "2" || md["binding"] != "b1" {
		t.Fatalf("eng metadata %v", md)
	}
	if want := []string{"app Google Cloud SDK: levels corp_network", "project projects/42: dry-run levels corp_devices"}; len(eng.Lists) != 1 || !slices.Equal(eng.Lists[0].Items, want) {
		t.Fatalf("scoped %+v, want %q", eng.Lists, want)
	}
	// A dry-run level in a scoped setting marks the binding.
	if eng.Badge != "DRY-RUN" || eng.Status != model.StatusWarning {
		t.Fatalf("eng badge %q status %v", eng.Badge, eng.Status)
	}
	keys := make([]string, 0, 3)
	for _, f := range eng.Fields[:3] {
		keys = append(keys, f.Key)
	}
	if want := []string{"principalType", "group", "groupId"}; !slices.Equal(keys, want) {
		t.Fatalf("field order %v, want %v", keys, want)
	}

	contractors := got[1]
	if contractors.Badge != "DRY-RUN" || contractors.Metadata["dryRunAccessLevels"] != "corp_devices" {
		t.Fatalf("contractors %+v", contractors)
	}
	if _, ok := contractors.Metadata["displayName"]; ok {
		t.Fatal("display name equal to email repeated")
	}
	plain := got[2]
	if plain.Badge != "" || plain.Status != "" || plain.Lists != nil {
		t.Fatalf("plain %+v", plain)
	}
	unresolved := got[3]
	if unresolved.Metadata["groupId"] != "03ghi" || unresolved.Metadata["group"] != "" {
		t.Fatalf("unresolved %v", unresolved.Metadata)
	}
	sa, saProject := got[4], got[5]
	if sa.Metadata["principalType"] != "service account" || sa.Metadata["principal"] != "tf@seed.iam.gserviceaccount.com" {
		t.Fatalf("sa %v", sa.Metadata)
	}
	if saProject.Metadata["principalType"] != "service accounts of project" || saProject.Metadata["principal"] != "projects/42" {
		t.Fatalf("sa project %v", saProject.Metadata)
	}
	fed := bindingResource("organizations/9", Binding{Name: "f1", Federated: "principalSet://iam.googleapis.com/x"}, nil)
	if fed.Name != "principalSet://iam.googleapis.com/x" || fed.Metadata["principalType"] != "federated" {
		t.Fatalf("federated %+v", fed)
	}
}

func TestGroupsLookedUpOnce(t *testing.T) {
	t.Parallel()
	api, calls := withCalls(sample())
	if _, err := NewBindings(api).List(context.Background(), "organizations/9"); err != nil {
		t.Fatal(err)
	}
	var lookups []string
	for _, c := range *calls {
		if len(c) > 6 && c[:6] == "group " {
			lookups = append(lookups, c)
		}
	}
	slices.Sort(lookups)
	if want := []string{"group 01abc", "group 02def", "group 03ghi"}; !slices.Equal(lookups, want) {
		t.Fatalf("lookups %v, want %v", lookups, want)
	}
}

func TestGroupLookupFailureListsByID(t *testing.T) {
	t.Parallel()
	api := sample()
	api.groupErr = errors.New("403 cloudidentity disabled")
	got, err := NewBindings(api).List(context.Background(), "organizations/9")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "01abc" || got[0].Metadata["groupId"] != "01abc" {
		t.Fatalf("got %+v", got[0])
	}
}

func TestServiceAccountListFailureKeepsGroups(t *testing.T) {
	t.Parallel()
	api := sample()
	api.saErr = errors.New("boom")
	got, err := NewBindings(api).List(context.Background(), "organizations/9")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("names %v, want the 4 group bindings", names(got))
	}
}

func TestListErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	api := sample()
	api.err = boom
	if _, err := NewBindings(api).List(context.Background(), "organizations/9"); !errors.Is(err, boom) {
		t.Fatalf("err=%v, want boom", err)
	}
	if _, err := NewBindings(nil).List(context.Background(), "organizations/9"); err == nil {
		t.Fatal("no error without an API")
	}
}

func TestSessionText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    *Session
		want string
	}{
		{"nil", nil, ""},
		{"empty", &Session{}, ""},
		{"length off", &Session{Length: "3600s"}, ""},
		{"reauth", &Session{Length: "3600s", LengthEnabled: true, ReauthMethod: "SECURITY_KEY"}, "reauth SECURITY_KEY every 1h0m0s"},
		{"no method", &Session{Length: "3600s", LengthEnabled: true, ReauthMethod: "SESSION_REAUTH_METHOD_UNSPECIFIED"}, "reauth every 1h0m0s"},
		{"idle and oidc", &Session{MaxInactivity: "900s", UseOIDCMaxAge: true}, "idle 15m0s, OIDC max_age"},
		{"odd duration", &Session{MaxInactivity: "soon"}, "idle soon"},
	}
	for _, tc := range cases {
		if got := sessionText(tc.s); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestScopedText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s    Scoped
		want string
	}{
		{Scoped{App: "Console"}, "app Console"},
		{Scoped{Project: "projects/42", Levels: []string{"a", "b"}, Session: &Session{MaxInactivity: "60s"}}, "project projects/42: levels a, b; idle 1m0s"},
		{Scoped{App: "x", DryRunSession: &Session{MaxInactivity: "60s"}}, "app x: dry-run idle 1m0s"},
		{Scoped{}, "unscoped"},
	}
	for _, tc := range cases {
		if got := scopedText(tc.s); got != tc.want {
			t.Errorf("scopedText(%+v) = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestBindingAccessLevels(t *testing.T) {
	t.Parallel()
	p := NewBindings(sample())
	parent := bindingResource("organizations/9", sample().bindings[0], nil)
	if len(parent.Facets) != 1 || parent.Facets[0].ID != model.FacetAccessLevels || !parent.Facets[0].Inline {
		t.Fatalf("facets %+v", parent.Facets)
	}
	got, truncated, err := p.ListRelated(context.Background(), parent, model.FacetAccessLevels)
	if err != nil || truncated {
		t.Fatalf("err=%v truncated=%v", err, truncated)
	}
	if want := []string{"corp_devices", "corp_network"}; !slices.Equal(names(got), want) {
		t.Fatalf("levels %v, want %v", names(got), want)
	}
	devices, network := got[0], got[1]
	if devices.ID != "organizations/9/accessPolicies/100/accessLevels/corp_devices" || devices.ParentID != parent.ID ||
		devices.FacetID != model.FacetAccessLevels || devices.ScopeID != "organizations/9" || devices.Service != model.ServiceAccessLevel {
		t.Fatalf("devices %+v", devices)
	}
	if devices.Metadata["usedBy"] != "binding; project projects/42 dry-run" || network.Metadata["usedBy"] != "app Google Cloud SDK" {
		t.Fatalf("usedBy %q, %q", devices.Metadata["usedBy"], network.Metadata["usedBy"])
	}
	if devices.Lists[0].Items[0] != "device screen lock" {
		t.Fatalf("devices conditions %v", devices.Lists)
	}
}

func TestBindingAccessLevelsIgnoreOthers(t *testing.T) {
	t.Parallel()
	api, calls := withCalls(sample())
	p := NewBindings(api)
	b := bindingResource("organizations/9", sample().bindings[0], nil)
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: b.ID, Service: model.ServicePerimeter}, model.FacetAccessLevels},
		{"undeclared facet", b, model.FacetPermissions},
		{"malformed id", model.Resource{ID: "organizations/9/bindings/b1", Service: model.ServiceAccessBinding}, model.FacetAccessLevels},
	}
	for _, tc := range cases {
		got, truncated, err := p.ListRelated(context.Background(), tc.parent, tc.facet)
		if got != nil || truncated || err != nil {
			t.Errorf("%s: got %v %v %v", tc.name, got, truncated, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("calls %v", *calls)
	}
	// A binding that names no levels declares no facet and lists none.
	if r := bindingResource("organizations/9", Binding{Name: "x", GroupKey: "g"}, nil); r.Facets != nil {
		t.Fatalf("facets without levels %+v", r.Facets)
	}
}

func TestBindingAccessLevelsErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	api := sample()
	api.getErr = boom
	parent := bindingResource("organizations/9", sample().bindings[0], nil)
	if _, _, err := NewBindings(api).ListRelated(context.Background(), parent, model.FacetAccessLevels); !errors.Is(err, boom) {
		t.Fatalf("err=%v, want boom", err)
	}
}

func TestLevelUses(t *testing.T) {
	t.Parallel()
	got := levelUses(Binding{
		Levels:       []string{"a"},
		DryRunLevels: []string{"a", "b"},
		Scoped:       []Scoped{{App: "SDK", Levels: []string{"b"}}, {Levels: []string{"c"}}, {App: "SDK", Levels: []string{"b"}}},
	})
	want := map[string][]string{"a": {"binding", "dry-run"}, "b": {"dry-run", "app SDK"}, "c": {"unscoped"}}
	if len(got) != len(want) {
		t.Fatalf("uses %v, want %v", got, want)
	}
	for k, v := range want {
		if !slices.Equal(got[k], v) {
			t.Errorf("%s: %v, want %v", k, got[k], v)
		}
	}
}

func TestParseBindingID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		id        string
		org, name string
		ok        bool
	}{
		{"organizations/9/gcpUserAccessBindings/b1", "organizations/9", "b1", true},
		{"organizations/9/gcpUserAccessBindings/", "", "", false},
		{"folders/9/gcpUserAccessBindings/b1", "", "", false},
		{"organizations/9/gcpUserAccessBindings/b1/x", "", "", false},
	} {
		org, name, ok := parseBindingID(tc.id)
		if org != tc.org || name != tc.name || ok != tc.ok {
			t.Errorf("parseBindingID(%q) = %q %q %v", tc.id, org, name, ok)
		}
	}
}
