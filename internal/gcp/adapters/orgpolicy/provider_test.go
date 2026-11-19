package orgpolicy

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	policies map[string][]Policy // keyed by parent
	err      error
	parents  *[]string
}

func (f fakeAPI) ListPolicies(_ context.Context, parent string) ([]Policy, error) {
	if f.parents != nil {
		*f.parents = append(*f.parents, parent)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.policies[parent], nil
}

var updated = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

func sample() fakeAPI {
	return fakeAPI{policies: map[string][]Policy{
		"organizations/1": {
			{Constraint: "compute.vmExternalIpAccess", Rules: []Rule{{DenyAll: true}}, Updated: updated},
			{Constraint: "compute.requireOsLogin", Rules: []Rule{{Enforce: true}}, DryRun: true, DryRunRules: []Rule{{Enforce: false}}},
		},
		"folders/2": {
			{Constraint: "gcp.resourceLocations", InheritFromParent: true, Rules: []Rule{
				{Allowed: []string{"in:us-locations"}},
				{Allowed: []string{"in:europe-locations"}, Condition: "EU team"},
			}},
		},
		"projects/p": {
			{Constraint: "iam.allowedPolicyMemberDomains", Reset: true},
		},
	}}
}

func TestListScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope string
		wantN int
		calls int
	}{
		{"organizations/1", 2, 1},
		{"folders/2", 1, 1},
		{"projects/p", 1, 1},
		{"projects/empty", 0, 1},
		{"", 0, 0},
		{"projects/p/buckets/b", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			var parents []string
			api := sample()
			api.parents = &parents
			got, err := New(api).List(context.Background(), tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.wantN || len(parents) != tc.calls {
				t.Fatalf("len=%d calls=%v, want %d and %d", len(got), parents, tc.wantN, tc.calls)
			}
			if tc.calls == 1 && parents[0] != tc.scope {
				t.Fatalf("parent = %q, want %q", parents[0], tc.scope)
			}
		})
	}
}

func TestPolicyMapping(t *testing.T) {
	t.Parallel()
	got, err := New(sample()).List(context.Background(), "organizations/1")
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.ID != "organizations/1/policies/compute.vmExternalIpAccess" || r.Name != "compute.vmExternalIpAccess" ||
		r.Service != model.ServiceOrgPolicy || r.ScopeID != "organizations/1" || r.Status != "" || len(r.Facets) != 0 {
		t.Fatalf("identity: %+v", r)
	}
	var keys []string
	for _, f := range r.Fields {
		keys = append(keys, f.Key)
	}
	if want := []string{"enforcement", "inheritFromParent", "updated"}; !slices.Equal(keys, want) {
		t.Fatalf("fields = %v, want %v", keys, want)
	}
	if r.Metadata["enforcement"] != "deny all" || r.Metadata["updated"] != "2024-01-15 10:00 UTC" || len(r.Lists) != 0 {
		t.Fatalf("deny-all policy: %+v", r)
	}
	if b := got[1]; b.Metadata["enforcement"] != "enforced" || b.Metadata["dryRun"] != "not enforced" || b.Metadata["inheritFromParent"] != "" ||
		b.Badge != "DRY-RUN" {
		t.Fatalf("boolean policy metadata: %+v", b.Metadata)
	}
}

func TestDryRunRulesAndParameters(t *testing.T) {
	t.Parallel()
	r := policyResource("projects/p", Policy{
		Constraint: "compute.vmExternalIpAccess",
		Rules:      []Rule{{DenyAll: true}},
		DryRun:     true,
		DryRunRules: []Rule{
			{Allowed: []string{"projects/p/zones/z/instances/bastion"}},
			{DenyAll: true, Condition: "env is prod"},
		},
		DryRunInherit: true,
	})
	if r.Metadata["dryRun"] != "2 rules" || r.Metadata["dryRunInheritFromParent"] != "on" ||
		!slices.Equal(r.List("dryRunRules"), []string{"allow projects/p/zones/z/instances/bastion", "deny all  when env is prod"}) {
		t.Fatalf("dry run = %+v %+v", r.Metadata, r.Lists)
	}
	managed := policyResource("projects/p", Policy{Constraint: "compute.managed.restrictProtocolForwardingCreationForTypes",
		Rules: []Rule{{Enforce: true, Parameters: `{"allowedSchemes":["INTERNAL"]}`}}})
	if managed.Metadata["enforcement"] != "1 rule" || !slices.Equal(managed.List("rules"), []string{`enforced  parameters {"allowedSchemes":["INTERNAL"]}`}) {
		t.Fatalf("managed constraint = %+v %+v", managed.Metadata, managed.Lists)
	}
}

func TestEnforcement(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		pol  Policy
		want string
	}{
		{"reset", Policy{Reset: true, Rules: []Rule{{Enforce: true}}}, "reset to default"},
		{"no rules", Policy{}, "inherited"},
		{"dry run only", Policy{DryRun: true}, "dry run only"},
		{"enforced", Policy{Rules: []Rule{{Enforce: true}}}, "enforced"},
		{"not enforced", Policy{Rules: []Rule{{}}}, "not enforced"},
		{"allow all", Policy{Rules: []Rule{{AllowAll: true}}}, "allow all"},
		{"values", Policy{Rules: []Rule{{Allowed: []string{"a", "b"}, Denied: []string{"c"}}}}, "allow a, b; deny c"},
		{"conditional", Policy{Rules: []Rule{{Enforce: true, Condition: "prod"}}}, "1 rule"},
		{"several", Policy{Rules: []Rule{{Enforce: true}, {}}}, "2 rules"},
	}
	for _, tc := range cases {
		if got := enforcement(tc.pol); got != tc.want {
			t.Errorf("%s: enforcement = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRulesList(t *testing.T) {
	t.Parallel()
	got, err := New(sample()).List(context.Background(), "folders/2")
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	want := []string{"allow in:us-locations", "allow in:europe-locations  when EU team"}
	if rules := r.List("rules"); !slices.Equal(rules, want) {
		t.Fatalf("rules = %v, want %v", rules, want)
	}
	if r.Metadata["enforcement"] != "2 rules" || r.Metadata["inheritFromParent"] != "on" {
		t.Fatalf("metadata: %+v", r.Metadata)
	}
}

func TestValueRows(t *testing.T) {
	t.Parallel()
	api := sample()
	api.policies["projects/q"] = []Policy{
		{Constraint: "gcp.restrictServiceUsage", Rules: []Rule{{Allowed: []string{"compute.googleapis.com", "storage.googleapis.com"}, Denied: []string{"under:folders/9"}}}},
	}
	p := New(api)
	cases := []struct {
		name  string
		scope string
		want  []string // name action rule condition badge
	}{
		{"one rule", "projects/q", []string{
			"compute.googleapis.com allow   ALLOW",
			"storage.googleapis.com allow   ALLOW",
			"under:folders/9 deny   DENY",
		}},
		{"conditional rules", "folders/2", []string{
			"in:us-locations allow 1  ALLOW",
			"in:europe-locations allow 2 EU team ALLOW",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pols, err := p.List(context.Background(), tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			parent := pols[0]
			if f, ok := parent.InlineFacet(); !ok || f.ID != model.FacetPolicyValues {
				t.Fatalf("facets = %+v, want inline values", parent.Facets)
			}
			kids, _, err := p.ListRelated(context.Background(), parent, model.FacetPolicyValues)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, k := range kids {
				if k.Service != model.ServiceOrgPolicyValue || k.ParentID != parent.ID || k.FacetID != model.FacetPolicyValues ||
					k.ScopeID != tc.scope || !strings.HasPrefix(k.ID, parent.ID+"/rules/") {
					t.Fatalf("identity: %+v", k)
				}
				got = append(got, strings.Join([]string{k.Name, k.Metadata["action"], k.Metadata["rule"], k.Metadata["condition"], k.Badge}, " "))
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("rows = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValueRowsSkip(t *testing.T) {
	t.Parallel()
	p := New(sample())
	pols, err := p.List(context.Background(), "organizations/1")
	if err != nil {
		t.Fatal(err)
	}
	for _, pol := range pols {
		if len(pol.Facets) != 0 {
			t.Errorf("%s: facets = %+v, want none without values", pol.Name, pol.Facets)
		}
	}
	listed := pols[0]
	gone := listed
	gone.Name = "compute.gone"
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other facet", listed, model.FacetRules},
		{"other service", model.Resource{ID: "projects/p/buckets/b", Service: model.ServiceCloudStorage, ScopeID: "projects/p"}, model.FacetPolicyValues},
		{"policy gone", gone, model.FacetPolicyValues},
	}
	for _, tc := range cases {
		kids, _, err := p.ListRelated(context.Background(), tc.parent, tc.facet)
		if err != nil || len(kids) != 0 {
			t.Errorf("%s: kids=%v err=%v, want none", tc.name, kids, err)
		}
	}
}

func TestErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	if _, err := New(fakeAPI{err: boom}).List(context.Background(), "folders/2"); !errors.Is(err, boom) {
		t.Fatalf("List err = %v", err)
	}
	parent := model.Resource{ID: "folders/2/policies/gcp.resourceLocations", Name: "gcp.resourceLocations", Service: model.ServiceOrgPolicy, ScopeID: "folders/2"}
	if _, _, err := New(fakeAPI{err: boom}).ListRelated(context.Background(), parent, model.FacetPolicyValues); !errors.Is(err, boom) {
		t.Fatalf("ListRelated err = %v", err)
	}
}
