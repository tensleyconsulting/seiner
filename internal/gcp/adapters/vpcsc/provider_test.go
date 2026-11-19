package vpcsc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	project    Project
	projectErr error
	policies   []Policy
	policyErr  error
	perimeters map[string][]Perimeter // keyed by policy number
	perimErr   map[string]error       // keyed by policy number
	levels     map[string][]Level
	err        error // returned from GetPerimeter and ListLevels when set
	mu         *sync.Mutex
	calls      *[]string
}

func (f fakeAPI) called(name string) {
	if f.calls != nil {
		f.mu.Lock()
		*f.calls = append(*f.calls, name)
		f.mu.Unlock()
	}
}

func (f fakeAPI) ListPolicies(_ context.Context, org string) ([]Policy, error) {
	f.called("policies " + org)
	if f.policyErr != nil {
		return nil, f.policyErr
	}
	return f.policies, nil
}

func (f fakeAPI) ListPerimeters(_ context.Context, policy string) ([]Perimeter, error) {
	f.called("perimeters " + policy)
	if err := f.perimErr[policy]; err != nil {
		return nil, err
	}
	return f.perimeters[policy], nil
}

func (f fakeAPI) GetPerimeter(_ context.Context, policy, name string) (Perimeter, error) {
	f.called("perimeter " + policy + "/" + name)
	if f.err != nil {
		return Perimeter{}, f.err
	}
	for _, p := range f.perimeters[policy] {
		if p.Name == name {
			return p, nil
		}
	}
	return Perimeter{}, model.ErrNotFound
}

func (f fakeAPI) ListLevels(_ context.Context, policy string) ([]Level, error) {
	f.called("levels " + policy)
	if f.err != nil {
		return nil, f.err
	}
	return f.levels[policy], nil
}

func (f fakeAPI) Project(_ context.Context, project string) (Project, error) {
	f.called("project " + project)
	return f.project, f.projectErr
}

// sample is an org with a default policy (100) and a scoped one (200).
// prod protects project p (number 42) and has a dry-run spec that adds
// another project and service; bridge reaches p through a VPC network;
// other does not protect p.
func sample() fakeAPI {
	return fakeAPI{
		project: Project{Number: "42", Org: "organizations/9"},
		policies: []Policy{
			{Name: "100", Title: "default policy"},
			{Name: "200", Title: "team", Scopes: []string{"folders/7"}},
		},
		perimeters: map[string][]Perimeter{
			"100": {
				{
					Name: "prod", Title: "Production", Description: "prod data", Type: "PERIMETER_TYPE_REGULAR",
					Status: &Config{
						Resources:          []string{"projects/42", "projects/43"},
						RestrictedServices: []string{"storage.googleapis.com", "bigquery.googleapis.com"},
						AccessLevels:       []string{"corp"},
						Ingress:            []Rule{ciRule},
						Egress:             []Rule{exportRule},
						VPCRestricted:      true,
						VPCAllowed:         []string{"RESTRICTED-SERVICES"},
					},
					Spec: &Config{
						Resources:          []string{"projects/42", "projects/43", "projects/44"},
						RestrictedServices: []string{"storage.googleapis.com", "bigquery.googleapis.com", "secretmanager.googleapis.com"},
						AccessLevels:       []string{"corp", "devices"},
						Ingress:            []Rule{ciRule, anyRule},
						VPCRestricted:      true,
						VPCAllowed:         []string{"RESTRICTED-SERVICES"},
					},
				},
				{
					Name: "bridge", Title: "bridge", Type: "PERIMETER_TYPE_BRIDGE",
					Status: &Config{Resources: []string{"//compute.googleapis.com/projects/p/global/networks/default", "projects/50"}},
				},
				{Name: "other", Status: &Config{Resources: []string{"projects/60"}}},
			},
			"200": {
				{Name: "sandbox", Status: &Config{Resources: []string{"projects/70"}}, Spec: &Config{Resources: []string{"projects/70", "projects/42"}}},
			},
		},
		levels: map[string][]Level{
			"100": {
				{Name: "devices", Title: "Devices", Combining: "OR", Conditions: []Condition{
					{Device: &Device{Encryption: []string{"ENCRYPTED"}, Screenlock: true}, Members: []string{"group:eng@example.com"}},
					{RequiredLevels: []string{"corp"}, Negate: true},
				}},
				{Name: "corp", Title: "corp", Description: "office", Conditions: []Condition{{IPSubnetworks: []string{"203.0.113.0/24"}, Regions: []string{"US"}}}},
				{Name: "admins", Expression: "request.auth.claims.groups.exists(g, g == 'sec')"},
			},
		},
		mu: &sync.Mutex{},
	}
}

var (
	ciRule = Rule{
		Title:      "CI deploys",
		Identities: []string{"serviceAccount:ci@build.iam.gserviceaccount.com"},
		Sources:    []string{"corp", "projects/50"},
		Operations: []Operation{{Service: "storage.googleapis.com", Methods: []string{"google.storage.objects.create", "permission storage.objects.get"}}, {Service: "run.googleapis.com", Methods: []string{"*"}}},
		Resources:  []string{"*"},
	}
	anyRule = Rule{
		IdentityType: "ANY_IDENTITY",
		Sources:      []string{"corp"},
		Operations:   []Operation{{Service: "storage.googleapis.com"}, {Service: "bigquery.googleapis.com"}, {Service: "pubsub.googleapis.com"}},
		Resources:    []string{"projects/42"},
	}
	exportRule = Rule{
		Identities:        []string{"group:data@example.com"},
		SourceRestriction: "SOURCE_RESTRICTION_ENABLED",
		Sources:           []string{"projects/42"},
		Operations:        []Operation{{Service: "bigquery.googleapis.com", Methods: []string{"*"}}},
		Resources:         []string{"projects/99"},
		ExternalResources: []string{"s3://exports"},
		Roles:             []string{"roles/bigquery.dataViewer"},
	}
)

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

func TestPoliciesListScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope     string
		wantNames []string
		wantPerms []string
	}{
		{"organizations/9", []string{"default policy", "team"}, []string{"accesscontextmanager.policies.list"}},
		{"folders/7", nil, nil},
		{"projects/p", nil, nil},
		{"", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			api, calls := withCalls(sample())
			p := NewPolicies(api)
			got, err := p.List(context.Background(), tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(names(got), tc.wantNames) {
				t.Fatalf("names %v, want %v", names(got), tc.wantNames)
			}
			if tc.wantNames == nil && len(*calls) != 0 {
				t.Fatalf("calls %v for a scope it does not serve", *calls)
			}
			if perms := p.ListPermissions(tc.scope); !slices.Equal(perms, tc.wantPerms) {
				t.Fatalf("perms %v, want %v", perms, tc.wantPerms)
			}
		})
	}
}

func TestPolicyMapping(t *testing.T) {
	t.Parallel()
	got, err := NewPolicies(sample()).List(context.Background(), "organizations/9")
	if err != nil {
		t.Fatal(err)
	}
	org, scoped := got[0], got[1]
	if org.ID != "organizations/9/accessPolicies/100" || org.Service != model.ServiceAccessPolicy ||
		org.ScopeID != "organizations/9" || org.Location != "global" || org.Badge != "" ||
		org.Metadata["policy"] != "100" || org.Metadata["scope"] != "organization" {
		t.Fatalf("org policy %+v", org)
	}
	if scoped.Badge != "SCOPED" || scoped.Metadata["scope"] != "folders/7" {
		t.Fatalf("scoped policy %+v", scoped)
	}
	if want := []string{model.FacetPerimeters, model.FacetAccessLevels}; len(org.Facets) != 2 ||
		org.Facets[0].ID != want[0] || org.Facets[1].ID != want[1] {
		t.Fatalf("facets %+v", org.Facets)
	}
	if p, ok := model.ParseName(org.ID); !ok || p.Scope != org.ScopeID {
		t.Fatalf("ParseName(%s) = %+v", org.ID, p)
	}
	untitled := policyResource("organizations/9", Policy{Name: "300"})
	if untitled.Name != "300" {
		t.Fatalf("untitled name %q", untitled.Name)
	}
}

func TestPolicyFacets(t *testing.T) {
	t.Parallel()
	p := NewPolicies(sample())
	pols, err := p.List(context.Background(), "organizations/9")
	if err != nil {
		t.Fatal(err)
	}
	parent := pols[0]

	perims, truncated, err := p.ListRelated(context.Background(), parent, model.FacetPerimeters)
	if err != nil || truncated {
		t.Fatalf("err=%v truncated=%v", err, truncated)
	}
	if want := []string{"bridge", "other", "prod"}; !slices.Equal(names(perims), want) {
		t.Fatalf("perimeters %v, want %v (sorted)", names(perims), want)
	}
	for _, r := range perims {
		if r.ParentID != parent.ID || r.FacetID != model.FacetPerimeters || r.ScopeID != "organizations/9" || r.Service != model.ServicePerimeter {
			t.Fatalf("perimeter child %+v", r)
		}
	}
	if perims[2].ID != "organizations/9/accessPolicies/100/servicePerimeters/prod" || perims[2].Metadata["policy"] != "default policy (100)" {
		t.Fatalf("prod %s policy=%q", perims[2].ID, perims[2].Metadata["policy"])
	}

	levels, _, err := p.ListRelated(context.Background(), parent, model.FacetAccessLevels)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"admins", "corp", "devices"}; !slices.Equal(names(levels), want) {
		t.Fatalf("levels %v, want %v", names(levels), want)
	}
	if levels[1].ID != "organizations/9/accessPolicies/100/accessLevels/corp" || levels[1].FacetID != model.FacetAccessLevels {
		t.Fatalf("corp %+v", levels[1])
	}
}

func TestPolicyFacetsIgnoreOthers(t *testing.T) {
	t.Parallel()
	api, calls := withCalls(sample())
	p := NewPolicies(api)
	policy := policyResource("organizations/9", Policy{Name: "100"})
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: policy.ID, Service: model.ServiceOrgPolicy}, model.FacetPerimeters},
		{"undeclared facet", policy, model.FacetPermissions},
		{"perimeter id", model.Resource{ID: policy.ID + "/servicePerimeters/x", Service: model.ServiceAccessPolicy}, model.FacetPerimeters},
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
}

func TestPerimeterMapping(t *testing.T) {
	t.Parallel()
	pols := sample().perimeters["100"]
	prod := perimeterResource("organizations/9", "100", "default policy (100)", pols[0])
	if prod.Name != "prod" || prod.Description != "prod data" || prod.Metadata["title"] != "Production" ||
		prod.Metadata["type"] != "regular" || prod.Metadata["resources"] != "2 projects" ||
		prod.Metadata["restrictedServices"] != "2 services" || prod.Metadata["accessLevels"] != "corp" ||
		prod.Metadata["ingressPolicies"] != "1" || prod.Metadata["vpcAccessibleServices"] != "RESTRICTED-SERVICES" {
		t.Fatalf("prod metadata %v", prod.Metadata)
	}
	// The dry-run spec differs: badge, warning, and only the changed
	// dry-run fields.
	if prod.Badge != "DRY-RUN" || prod.Status != model.StatusWarning {
		t.Fatalf("badge %q status %v", prod.Badge, prod.Status)
	}
	if want := "resources +1 −0, restricted services +1 −0, access levels +1 −0, ingress rules +1 −0, egress rules +0 −1"; prod.Metadata["dryRun"] != want {
		t.Fatalf("dryRun %q, want %q", prod.Metadata["dryRun"], want)
	}
	if prod.Metadata["dryRunResources"] != "3 projects" || prod.Metadata["dryRunAccessLevels"] != "corp, devices" ||
		prod.Metadata["dryRunIngressPolicies"] != "2" || prod.Metadata["dryRunEgressPolicies"] != "none" {
		t.Fatalf("dry-run fields %v", prod.Metadata)
	}
	if _, ok := prod.Metadata["dryRunVpcAccessibleServices"]; ok {
		t.Fatal("unchanged VPC accessible services repeated as a dry-run field")
	}
	var lists []string
	for _, l := range prod.Lists {
		lists = append(lists, l.ID)
	}
	if want := []string{"resources", "restrictedServices", "dryRunResources", "dryRunRestrictedServices"}; !slices.Equal(lists, want) {
		t.Fatalf("lists %v, want %v", lists, want)
	}
	var facets []string
	for _, f := range prod.Facets {
		facets = append(facets, f.ID)
	}
	if want := []string{model.FacetAccessLevels, model.FacetIngressRules, model.FacetEgressRules}; !slices.Equal(facets, want) || !prod.Facets[0].Inline {
		t.Fatalf("facets %+v, want %v", prod.Facets, want)
	}
	if prod.Fields[0].Key != "title" || prod.Fields[1].Key != "type" {
		t.Fatalf("field order %+v", prod.Fields[:2])
	}

	bridge := perimeterResource("organizations/9", "100", "100", pols[1])
	if bridge.Metadata["type"] != "bridge" || bridge.Metadata["resources"] != "1 project, 1 VPC network" ||
		bridge.Badge != "" || bridge.Status != "" || bridge.Facets != nil {
		t.Fatalf("bridge %+v", bridge)
	}
	if _, ok := bridge.Metadata["title"]; ok {
		t.Fatal("title equal to name repeated")
	}

	same := perimeterResource("organizations/9", "100", "100", Perimeter{Name: "s", Status: &Config{Resources: []string{"projects/1"}}, Spec: &Config{Resources: []string{"projects/1"}}})
	if same.Metadata["dryRun"] != "same as enforced" || same.Badge != "" {
		t.Fatalf("same spec %+v", same)
	}
	// A spec that drops what the status has shows it as none.
	dropped := perimeterResource("organizations/9", "100", "100", Perimeter{Name: "d", Status: &Config{AccessLevels: []string{"corp"}}, Spec: &Config{}})
	if dropped.Metadata["dryRunAccessLevels"] != "none" || dropped.Facets == nil {
		t.Fatalf("dropped %v facets %v", dropped.Metadata, dropped.Facets)
	}
	// A perimeter with only a dry-run spec has an empty status.
	newOnly := perimeterResource("organizations/9", "100", "100", Perimeter{Name: "n", Spec: &Config{Resources: []string{"projects/1"}}})
	if newOnly.Badge != "DRY-RUN" || newOnly.Metadata["dryRunResources"] != "1 project" {
		t.Fatalf("dry-run only %+v", newOnly)
	}
}

func TestPerimeterType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"PERIMETER_TYPE_REGULAR", "regular"},
		{"", "regular"},
		{"PERIMETER_TYPE_BRIDGE", "bridge"},
		{"PERIMETER_TYPE_NEW", "PERIMETER_TYPE_NEW"},
	} {
		if got := perimeterType(tc.in); got != tc.want {
			t.Errorf("perimeterType(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMembership(t *testing.T) {
	t.Parallel()
	net := "//compute.googleapis.com/projects/p/global/networks/"
	cases := []struct {
		name      string
		pm        Perimeter
		wantState string
		wantVia   string
	}{
		{"status only", Perimeter{Status: &Config{Resources: []string{"projects/42"}}}, "enforced", "project"},
		{"network", Perimeter{Status: &Config{Resources: []string{net + "a", net + "b"}}}, "enforced", "VPC network a, b"},
		{"other project's network", Perimeter{Status: &Config{Resources: []string{"//compute.googleapis.com/projects/q/global/networks/a"}}}, "", ""},
		{"both", Perimeter{Status: &Config{Resources: []string{"projects/42"}}, Spec: &Config{Resources: []string{"projects/42"}}}, "enforced, dry-run", "project"},
		{"removed in dry run", Perimeter{Status: &Config{Resources: []string{"projects/42"}}, Spec: &Config{}}, "enforced, removed in dry-run", "project"},
		{"dry run only", Perimeter{Status: &Config{}, Spec: &Config{Resources: []string{"projects/42"}}}, "dry-run only", "project"},
		{"no status", Perimeter{Spec: &Config{Resources: []string{"projects/42"}}}, "dry-run only", "project"},
		{"not a member", Perimeter{Status: &Config{Resources: []string{"projects/420"}}}, "", ""},
	}
	for _, tc := range cases {
		m := membershipOf(tc.pm, "42", "p")
		if m.state != tc.wantState || m.via != tc.wantVia {
			t.Errorf("%s: %+v, want %q via %q", tc.name, m, tc.wantState, tc.wantVia)
		}
	}
}

func TestPerimetersListScopes(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"organizations/9", "folders/7", ""} {
		api, calls := withCalls(sample())
		p := NewPerimeters(api)
		got, err := p.List(context.Background(), scope)
		if got != nil || err != nil || len(*calls) != 0 {
			t.Errorf("%q: got %v err=%v calls=%v", scope, got, err, *calls)
		}
		if perms := p.ListPermissions(scope); perms != nil {
			t.Errorf("%q: perms %v", scope, perms)
		}
	}
	if perms := NewPerimeters(sample()).ListPermissions("projects/p"); !slices.Equal(perms, []string{"resourcemanager.projects.get"}) {
		t.Fatalf("project perms %v", perms)
	}
}

func TestPerimetersProtectingProject(t *testing.T) {
	t.Parallel()
	got, err := NewPerimeters(sample()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"prod", "bridge", "sandbox"}; !slices.Equal(names(got), want) {
		t.Fatalf("names %v, want %v", names(got), want)
	}
	prod, bridge, sandbox := got[0], got[1], got[2]
	if prod.ID != "projects/p/accessPolicies/100/servicePerimeters/prod" || prod.ScopeID != "projects/p" ||
		prod.ParentID != "" || prod.Metadata["membership"] != "enforced, dry-run" || prod.Metadata["via"] != "project" ||
		prod.Metadata["policy"] != "default policy (100)" {
		t.Fatalf("prod %+v", prod)
	}
	if p, ok := model.ParseName(prod.ID); !ok || p.Scope != "projects/p" {
		t.Fatalf("ParseName(%s) = %+v", prod.ID, p)
	}
	if bridge.Metadata["via"] != "VPC network default" || bridge.Metadata["membership"] != "enforced" {
		t.Fatalf("bridge %v", bridge.Metadata)
	}
	if sandbox.Metadata["membership"] != "dry-run only" || sandbox.Metadata["policy"] != "team (200)" {
		t.Fatalf("sandbox %v", sandbox.Metadata)
	}
	keys := make([]string, 0, 4)
	for _, f := range prod.Fields[:5] {
		keys = append(keys, f.Key)
	}
	if want := []string{"title", "type", "policy", "membership", "via"}; !slices.Equal(keys, want) {
		t.Fatalf("field order %v, want %v", keys, want)
	}
}

func TestPerimetersIndexCachedPerOrg(t *testing.T) {
	t.Parallel()
	api, calls := withCalls(sample())
	p := NewPerimeters(api)
	for range 2 {
		if _, err := p.List(context.Background(), "projects/p"); err != nil {
			t.Fatal(err)
		}
	}
	var policyCalls int
	for _, c := range *calls {
		if c == "policies organizations/9" {
			policyCalls++
		}
	}
	if policyCalls != 1 {
		t.Fatalf("listed the org's policies %d times: %v", policyCalls, *calls)
	}
}

func TestPerimetersNoOrg(t *testing.T) {
	t.Parallel()
	api, calls := withCalls(sample())
	api.project.Org = ""
	got, err := NewPerimeters(api).List(context.Background(), "projects/p")
	if got != nil || err != nil || !slices.Equal(*calls, []string{"project p"}) {
		t.Fatalf("got %v err=%v calls=%v", got, err, *calls)
	}
}

func TestPerimetersDeniedIsSilent(t *testing.T) {
	t.Parallel()
	google := errors.New("403 caller lacks accesscontextmanager.policies.list")
	cases := []struct {
		name string
		mod  func(*fakeAPI)
	}{
		{"policy list denied", func(f *fakeAPI) { f.policyErr = fmt.Errorf("%w: %w", ErrDenied, google) }},
		{"every perimeter list denied", func(f *fakeAPI) {
			f.perimErr = map[string]error{"100": fmt.Errorf("%w: %w", ErrDenied, google), "200": fmt.Errorf("%w: %w", ErrDenied, google)}
		}},
	}
	for _, tc := range cases {
		api := sample()
		tc.mod(&api)
		got, err := NewPerimeters(api).List(context.Background(), "projects/p")
		if got != nil || err != nil {
			t.Errorf("%s: got %v err=%v", tc.name, got, err)
		}
	}
}

func TestPerimetersPartialPolicyFailure(t *testing.T) {
	t.Parallel()
	api := sample()
	api.perimErr = map[string]error{"200": fmt.Errorf("%w: scoped", ErrDenied)}
	got, err := NewPerimeters(api).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"prod", "bridge"}; !slices.Equal(names(got), want) {
		t.Fatalf("names %v, want %v", names(got), want)
	}
}

func TestPerimetersErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	cases := []struct {
		name string
		mod  func(*fakeAPI)
	}{
		{"project", func(f *fakeAPI) { f.projectErr = boom }},
		{"policies", func(f *fakeAPI) { f.policyErr = boom }},
		{"every perimeter list", func(f *fakeAPI) { f.perimErr = map[string]error{"100": boom, "200": boom} }},
	}
	for _, tc := range cases {
		api := sample()
		tc.mod(&api)
		_, err := NewPerimeters(api).List(context.Background(), "projects/p")
		if !errors.Is(err, boom) || errors.Is(err, ErrDenied) {
			t.Errorf("%s: err=%v, want boom", tc.name, err)
		}
	}
	// A failed index is not cached.
	api, calls := withCalls(sample())
	api.policyErr = boom
	p := NewPerimeters(api)
	_, _ = p.List(context.Background(), "projects/p")
	_, _ = p.List(context.Background(), "projects/p")
	if n := slices.Index(*calls, "policies organizations/9"); n < 0 || slices.Index((*calls)[n+1:], "policies organizations/9") < 0 {
		t.Fatalf("failed index was cached: %v", *calls)
	}
}

func TestPerimeterAccessLevels(t *testing.T) {
	t.Parallel()
	p := NewPerimeters(sample())
	for _, scope := range []string{"organizations/9", "projects/p"} {
		parent := perimeterResource(scope, "100", "100", sample().perimeters["100"][0])
		got, truncated, err := p.ListRelated(context.Background(), parent, model.FacetAccessLevels)
		if err != nil || truncated {
			t.Fatalf("%s: err=%v truncated=%v", scope, err, truncated)
		}
		if want := []string{"corp", "devices"}; !slices.Equal(names(got), want) {
			t.Fatalf("%s: levels %v, want %v", scope, names(got), want)
		}
		corp, devices := got[0], got[1]
		if corp.ID != scope+"/accessPolicies/100/accessLevels/corp" {
			t.Fatalf("corp id %s", corp.ID)
		}
		if corp.ParentID != parent.ID || corp.FacetID != model.FacetAccessLevels || corp.ScopeID != scope ||
			corp.Metadata["usedBy"] != "enforced, dry-run" || devices.Metadata["usedBy"] != "dry-run" {
			t.Fatalf("%s: corp %+v devices %v", scope, corp, devices.Metadata)
		}
	}
}

func TestPerimeterAccessLevelsIgnoreOthers(t *testing.T) {
	t.Parallel()
	api, calls := withCalls(sample())
	p := NewPerimeters(api)
	prod := perimeterResource("organizations/9", "100", "100", sample().perimeters["100"][0])
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"policy", policyResource("organizations/9", Policy{Name: "100"}), model.FacetAccessLevels},
		{"undeclared facet", prod, model.FacetPerimeters},
		{"level id", model.Resource{ID: "organizations/9/accessPolicies/100/accessLevels/corp", Service: model.ServicePerimeter}, model.FacetAccessLevels},
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
	// A perimeter that uses no levels lists none without reading them.
	api, calls = withCalls(sample())
	other := perimeterResource("organizations/9", "100", "100", sample().perimeters["100"][2])
	got, _, err := NewPerimeters(api).ListRelated(context.Background(), other, model.FacetAccessLevels)
	if got != nil || err != nil || slices.Contains(*calls, "levels 100") {
		t.Fatalf("got %v err=%v calls=%v", got, err, *calls)
	}
}

func TestRelatedErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	api := sample()
	api.err = boom
	prod := perimeterResource("organizations/9", "100", "100", sample().perimeters["100"][0])
	if _, _, err := NewPerimeters(api).ListRelated(context.Background(), prod, model.FacetAccessLevels); !errors.Is(err, boom) {
		t.Errorf("perimeter levels err=%v", err)
	}
	policy := policyResource("organizations/9", Policy{Name: "100"})
	if _, _, err := NewPolicies(api).ListRelated(context.Background(), policy, model.FacetAccessLevels); !errors.Is(err, boom) {
		t.Errorf("policy levels err=%v", err)
	}
	api = sample()
	api.perimErr = map[string]error{"100": boom}
	if _, _, err := NewPolicies(api).ListRelated(context.Background(), policy, model.FacetPerimeters); !errors.Is(err, boom) {
		t.Errorf("policy perimeters err=%v", err)
	}
	api = sample()
	api.policyErr = boom
	if _, err := NewPolicies(api).List(context.Background(), "organizations/9"); !errors.Is(err, boom) {
		t.Errorf("policies err=%v", err)
	}
}

func TestLevelMapping(t *testing.T) {
	t.Parallel()
	levels := sample().levels["100"]
	devices := LevelResource("organizations/9", "100", levels[0], "")
	if devices.Metadata["type"] != "basic" || devices.Metadata["combining"] != "OR" || devices.Metadata["conditions"] != "2" ||
		devices.Metadata["title"] != "Devices" {
		t.Fatalf("devices %v", devices.Metadata)
	}
	if want := []string{"members group:eng@example.com; device encrypted, screen lock", "NOT requires corp"}; len(devices.Lists) != 1 || !slices.Equal(devices.Lists[0].Items, want) {
		t.Fatalf("conditions %+v, want %q", devices.Lists, want)
	}
	corp := LevelResource("organizations/9", "100", levels[1], "")
	if _, ok := corp.Metadata["combining"]; ok || corp.Description != "office" || corp.Lists[0].Items[0] != "ip 203.0.113.0/24; regions US" {
		t.Fatalf("corp %+v", corp)
	}
	if _, ok := corp.Metadata["title"]; ok {
		t.Fatal("title equal to name repeated")
	}
	admins := LevelResource("organizations/9", "100", levels[2], "")
	if admins.Metadata["type"] != "custom" || len(admins.Lists) != 1 || admins.Lists[0].ID != "expression" ||
		!slices.Equal(admins.Lists[0].Items, []string{"request.auth.claims.groups.exists(g, g == 'sec')"}) {
		t.Fatalf("admins %+v", admins)
	}
	if got := conditionText(Condition{}); got != "always" {
		t.Fatalf("empty condition %q", got)
	}
}

func TestParseID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id   string
		want vpcscID
		ok   bool
	}{
		{"organizations/9/accessPolicies/100", vpcscID{scope: "organizations/9", policy: "100"}, true},
		{"projects/p/accessPolicies/100/servicePerimeters/prod", vpcscID{"projects/p", "100", "servicePerimeters", "prod"}, true},
		{"organizations/9/accessPolicies/100/accessLevels/corp", vpcscID{"organizations/9", "100", "accessLevels", "corp"}, true},
		{"folders/7/accessPolicies/100", vpcscID{}, false},
		{"organizations/9/accessPolicies/", vpcscID{}, false},
		{"organizations/9/accessPolicies/100/iam/bindings/x", vpcscID{}, false},
		{"organizations/9/accessPolicies/100/servicePerimeters/", vpcscID{}, false},
		{"organizations/9/policies/100", vpcscID{}, false},
	}
	for _, tc := range cases {
		got, ok := parseID(tc.id)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseID(%q) = %+v %v, want %+v %v", tc.id, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNoAPI(t *testing.T) {
	t.Parallel()
	if _, err := NewPolicies(nil).List(context.Background(), "organizations/9"); err == nil {
		t.Error("policies: no error without an API")
	}
	if _, err := NewPerimeters(nil).List(context.Background(), "projects/p"); err == nil {
		t.Error("perimeters: no error without an API")
	}
}

func TestPerimeterRules(t *testing.T) {
	t.Parallel()
	p := NewPerimeters(sample())
	for _, scope := range []string{"organizations/9", "projects/p"} {
		parent := perimeterResource(scope, "100", "100", sample().perimeters["100"][0])

		in, truncated, err := p.ListRelated(context.Background(), parent, model.FacetIngressRules)
		if err != nil || truncated {
			t.Fatalf("%s: err=%v truncated=%v", scope, err, truncated)
		}
		if want := []string{"CI deploys", "any identity from corp → storage +2"}; !slices.Equal(names(in), want) {
			t.Fatalf("%s: ingress %q, want %q", scope, names(in), want)
		}
		ci, added := in[0], in[1]
		if ci.ID != parent.ID+"/ingressRules/1" || ci.Service != model.ServiceIngressRule || ci.ParentID != parent.ID ||
			ci.FacetID != model.FacetIngressRules || ci.ScopeID != scope || ci.Badge != "" || ci.Status != "" {
			t.Fatalf("%s: ci %+v", scope, ci)
		}
		if ci.Metadata["mode"] != "enforced, dry-run" || ci.Metadata["identities"] != "serviceAccount:ci@build.iam.gserviceaccount.com" ||
			ci.Metadata["sources"] != "corp, projects/50" || ci.Metadata["resources"] != "*" {
			t.Fatalf("%s: ci metadata %v", scope, ci.Metadata)
		}
		if want := []string{"storage.googleapis.com: google.storage.objects.create, permission storage.objects.get", "run.googleapis.com: all methods"}; len(ci.Lists) != 1 || !slices.Equal(ci.Lists[0].Items, want) {
			t.Fatalf("%s: operations %+v, want %q", scope, ci.Lists, want)
		}
		if added.ID != parent.ID+"/dryRunIngressRules/2" || added.Badge != "DRY-RUN" || added.Status != model.StatusWarning ||
			added.Metadata["mode"] != "dry-run only" || added.Metadata["identities"] != "any identity" {
			t.Fatalf("%s: added %+v", scope, added)
		}

		eg, _, err := p.ListRelated(context.Background(), parent, model.FacetEgressRules)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"group:data@example.com → bigquery in projects/99, s3://exports"}; !slices.Equal(names(eg), want) {
			t.Fatalf("%s: egress %q, want %q", scope, names(eg), want)
		}
		export := eg[0]
		if export.Service != model.ServiceEgressRule || export.Badge != "NOT IN DRY-RUN" || export.Status != model.StatusWarning ||
			export.Metadata["mode"] != "enforced, removed in dry-run" || export.Metadata["sourceRestriction"] != "enabled" ||
			export.Metadata["externalResources"] != "s3://exports" || export.Metadata["roles"] != "roles/bigquery.dataViewer" {
			t.Fatalf("%s: export %+v", scope, export)
		}
	}
	// Without a dry-run spec, every rule is simply enforced.
	plain := perimeterResource("organizations/9", "100", "100", Perimeter{Name: "x", Status: &Config{Ingress: []Rule{ciRule}}})
	api := sample()
	api.perimeters = map[string][]Perimeter{"100": {{Name: "x", Status: &Config{Ingress: []Rule{ciRule}}}}}
	in, _, err := NewPerimeters(api).ListRelated(context.Background(), plain, model.FacetIngressRules)
	if err != nil || len(in) != 1 || in[0].Metadata["mode"] != "enforced" || in[0].Badge != "" {
		t.Fatalf("plain %+v err=%v", in, err)
	}
	if len(plain.Facets) != 1 || plain.Facets[0].ID != model.FacetIngressRules {
		t.Fatalf("plain facets %+v", plain.Facets)
	}
}

func TestPerimeterRulesErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	api := sample()
	api.err = boom
	prod := perimeterResource("organizations/9", "100", "100", sample().perimeters["100"][0])
	if _, _, err := NewPerimeters(api).ListRelated(context.Background(), prod, model.FacetEgressRules); !errors.Is(err, boom) {
		t.Fatalf("err=%v, want boom", err)
	}
	policy := policyResource("organizations/9", Policy{Name: "100"})
	if got, _, err := NewPerimeters(api).ListRelated(context.Background(), policy, model.FacetIngressRules); got != nil || err != nil {
		t.Fatalf("policy parent: %v %v", got, err)
	}
}

func TestRuleSummary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		svc  model.ServiceType
		r    Rule
		want string
	}{
		{"ingress no source", model.ServiceIngressRule, Rule{IdentityType: "ANY_USER_ACCOUNT", Operations: []Operation{{Service: "*"}}}, "any user account → all services"},
		{"many identities", model.ServiceIngressRule, Rule{Identities: []string{"user:a", "user:b", "user:c"}, Sources: []string{"*"}}, "user:a +2 from * → no services"},
		{"nobody", model.ServiceIngressRule, Rule{}, "no identity → no services"},
		{"egress no target", model.ServiceEgressRule, Rule{IdentityType: "ANY_IDENTITY", Operations: []Operation{{Service: "storage.googleapis.com"}}}, "any identity → storage"},
	}
	for _, tc := range cases {
		if got := ruleSummary(tc.svc, tc.r, identityText(tc.r)); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	many := ruleResource(model.Resource{ID: "x", ScopeID: "projects/p"}, model.FacetIngressRules, "ingressRules/1",
		Rule{Identities: []string{"user:a", "user:b", "user:c", "user:d"}}, "enforced")
	if many.Metadata["identities"] != "4 identities" || len(many.Lists) != 1 || many.Lists[0].ID != "identities" {
		t.Fatalf("many identities %+v", many)
	}
}

func TestDeviceText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d    Device
		want string
	}{
		{Device{}, "device policy"},
		{Device{Encryption: []string{"ENCRYPTED", "ENCRYPTION_UNSUPPORTED"}}, "device encrypted or encryption unsupported"},
		{Device{
			Management:    []string{"BASIC", "COMPLETE"},
			OS:            []OS{{Type: "DESKTOP_MAC", MinVersion: "10.15"}, {Type: "DESKTOP_CHROME_OS", Verified: true}},
			Screenlock:    true,
			CorpOwned:     true,
			AdminApproval: true,
		}, "device managed BASIC or COMPLETE, DESKTOP_MAC ≥ 10.15 or DESKTOP_CHROME_OS verified, screen lock, corp-owned, admin approval"},
	}
	for _, tc := range cases {
		if got := deviceText(tc.d); got != tc.want {
			t.Errorf("deviceText(%+v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestFormatCEL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "levels.corp", []string{"levels.corp"}},
		{"flat and", "device.is_corp_owned_device &&  origin.region_code == 'US'", []string{
			"device.is_corp_owned_device",
			"&& origin.region_code == 'US'",
		}},
		{"nested", "levels.corp && (origin.region_code in ['US', 'CA'] || device.encryption_status == DeviceEncryptionStatus.ENCRYPTED)", []string{
			"levels.corp",
			"&& (origin.region_code in ['US', 'CA']",
			"  || device.encryption_status == DeviceEncryptionStatus.ENCRYPTED)",
		}},
		{"operators in strings stay", "request.auth.claims.note == 'a && b' || x", []string{
			"request.auth.claims.note == 'a && b'",
			"|| x",
		}},
		{"escaped quote", `a == "say \"hi\" || bye" && b`, []string{
			`a == "say \"hi\" || bye"`,
			"&& b",
		}},
		{"newlines collapse", "a\n  &&\n  b", []string{"a", "&& b"}},
		{"empty", "  ", nil},
	}
	for _, tc := range cases {
		if got := FormatCEL(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("%s: FormatCEL(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}
