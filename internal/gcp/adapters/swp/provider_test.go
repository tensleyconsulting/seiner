package swp

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	locations []string
	gateways  map[string][]Gateway // keyed by location
	policies  map[string][]Policy
	urlLists  map[string][]URLList
	tls       map[string][]TLSInspectionPolicy
	rules     map[string][]Rule // keyed by "project/location/policy"
	locErr    map[string]error  // per-location lists fail for these
	err       error             // returned from every method when set
	mu        *sync.Mutex
	calls     *int
}

func (f fakeAPI) called() {
	if f.calls != nil {
		f.mu.Lock()
		*f.calls++
		f.mu.Unlock()
	}
}

func (f fakeAPI) GatewayLocations(context.Context, string) ([]string, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	return f.locations, nil
}

func (f fakeAPI) SecurityLocations(ctx context.Context, project string) ([]string, error) {
	return f.GatewayLocations(ctx, project)
}

func perLocation[T any](f fakeAPI, m map[string][]T, location string) ([]T, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	if err := f.locErr[location]; err != nil {
		return nil, err
	}
	return m[location], nil
}

func (f fakeAPI) ListGateways(_ context.Context, _, location string) ([]Gateway, error) {
	return perLocation(f, f.gateways, location)
}

func (f fakeAPI) ListPolicies(_ context.Context, _, location string) ([]Policy, error) {
	return perLocation(f, f.policies, location)
}

func (f fakeAPI) ListURLLists(_ context.Context, _, location string) ([]URLList, error) {
	return perLocation(f, f.urlLists, location)
}

func (f fakeAPI) GetURLList(_ context.Context, _, location, name string) (URLList, error) {
	f.called()
	if f.err != nil {
		return URLList{}, f.err
	}
	for _, u := range f.urlLists[location] {
		if u.Name == name {
			return u, nil
		}
	}
	return URLList{}, errors.New("not found")
}

func (f fakeAPI) ListTLSInspectionPolicies(_ context.Context, _, location string) ([]TLSInspectionPolicy, error) {
	return perLocation(f, f.tls, location)
}

func (f fakeAPI) ListRules(_ context.Context, project, location, policy string, limit int) ([]Rule, bool, error) {
	f.called()
	if f.err != nil {
		return nil, false, f.err
	}
	rs := f.rules[project+"/"+location+"/"+policy]
	if len(rs) > limit {
		return rs[:limit], true, nil
	}
	return rs, false, nil
}

var created = time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)

func sample() fakeAPI {
	return fakeAPI{
		locations: []string{"global", "us-central1", "us-east1"},
		gateways: map[string][]Gateway{
			"us-central1": {{
				Name: "egress-proxy", Location: "us-central1", Description: "Egress",
				Policy:       "projects/p/locations/us-central1/gatewaySecurityPolicies/egress-policy",
				Addresses:    []string{"10.128.0.100"},
				Ports:        []int64{443, 8080},
				RoutingMode:  "EXPLICIT_ROUTING_MODE",
				Network:      "projects/p/global/networks/default",
				Subnetwork:   "projects/p/regions/us-central1/subnetworks/default",
				Certificates: []string{"projects/p/locations/us-central1/certificates/swp-cert"},
				Created:      created,
				Labels:       map[string]string{"env": "prod"},
			}},
			"us-east1": {{
				Name: "shared-proxy", Location: "us-east1",
				Policy: "projects/hub/locations/us-east1/gatewaySecurityPolicies/central",
			}},
		},
		policies: map[string][]Policy{
			"us-central1": {{
				Name: "egress-policy", Location: "us-central1", Created: created,
				TLSInspection: "projects/p/locations/us-central1/tlsInspectionPolicies/egress-tls",
			}},
		},
		urlLists: map[string][]URLList{
			"us-central1": {{Name: "dev-tools", Location: "us-central1", Values: []string{"github.com", "*.github.com"}}},
		},
		tls: map[string][]TLSInspectionPolicy{
			"us-central1": {{
				Name: "egress-tls", Location: "us-central1",
				CAPool:        "projects/p/locations/us-central1/caPools/swp-pool",
				MinTLSVersion: "TLS_1_2", Profile: "PROFILE_MODERN", ExcludePublic: true,
			}},
		},
		rules: map[string][]Rule{
			"p/us-central1/egress-policy": {
				{Name: "allow-github", Priority: 100, Action: "ALLOW", Enabled: true, SessionMatcher: "host() == 'github.com'", Created: created},
				{Name: "block-uploads", Priority: 300, Action: "DENY", ApplicationMatcher: "request.method == 'POST'", TLSInspection: true},
			},
			"hub/us-east1/central": {{Name: "allow-all", Priority: 1000, Action: "ALLOW", Enabled: true}},
		},
		mu: &sync.Mutex{},
	}
}

type lister interface {
	List(context.Context, string) ([]model.Resource, error)
}

func providers(api API) map[model.ServiceType]lister {
	return map[model.ServiceType]lister{
		model.ServiceWebProxy:      NewWebProxies(api),
		model.ServiceSWPPolicy:     NewPolicies(api),
		model.ServiceURLList:       NewURLLists(api),
		model.ServiceTLSInspection: NewTLSInspectionPolicies(api),
	}
}

func TestListScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope     string
		wantCalls int
		wantN     map[model.ServiceType]int
	}{
		// locations + one call per location
		{"projects/p", 4, map[model.ServiceType]int{model.ServiceWebProxy: 2, model.ServiceSWPPolicy: 1, model.ServiceURLList: 1, model.ServiceTLSInspection: 1}},
		{"folders/1", 0, nil},
		{"organizations/1", 0, nil},
		{"", 0, nil},
	}
	for _, tc := range cases {
		for svc := range providers(nil) {
			t.Run(tc.scope+"/"+string(svc), func(t *testing.T) {
				t.Parallel()
				api := sample()
				calls := 0
				api.calls = &calls
				got, err := providers(api)[svc].List(context.Background(), tc.scope)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != tc.wantN[svc] || calls != tc.wantCalls {
					t.Fatalf("len=%d calls=%d, want %d and %d", len(got), calls, tc.wantN[svc], tc.wantCalls)
				}
				for _, r := range got {
					if r.Service != svc {
						t.Errorf("%s listed a %s", svc, r.Service)
					}
				}
			})
		}
	}
}

func TestGatewayMapping(t *testing.T) {
	t.Parallel()
	got, err := NewWebProxies(sample()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.ID != "projects/p/locations/us-central1/gateways/egress-proxy" || r.Name != "egress-proxy" ||
		r.ScopeID != "projects/p" || r.Location != "us-central1" || r.Status != "" ||
		!r.Created.Equal(created) || r.Labels["env"] != "prod" || r.Description != "Egress" {
		t.Fatalf("%+v", r)
	}
	want := map[string]string{
		"policy":       "egress-policy",
		"addresses":    "10.128.0.100",
		"ports":        "443, 8080",
		"routingMode":  "EXPLICIT",
		"network":      "default",
		"subnetwork":   "default",
		"certificates": "swp-cert",
	}
	for k, v := range want {
		if r.Metadata[k] != v {
			t.Errorf("%s=%q want %q", k, r.Metadata[k], v)
		}
	}
	var keys []string
	for _, f := range r.Fields {
		keys = append(keys, f.Key)
	}
	if want := []string{"policy", "addresses", "ports", "routingMode", "network", "subnetwork", "certificates"}; !slices.Equal(keys, want) {
		t.Errorf("fields %v, want %v", keys, want)
	}
	if len(r.Facets) != 1 || r.Facets[0].ID != model.FacetRules || !r.Facets[0].Inline {
		t.Errorf("facets %+v", r.Facets)
	}
	// A policy in another project keeps its full name.
	if p := got[1].Metadata["policy"]; p != "projects/hub/locations/us-east1/gatewaySecurityPolicies/central" {
		t.Errorf("cross-project policy %q", p)
	}
}

func TestGatewayWithoutPolicyHasNoFacet(t *testing.T) {
	t.Parallel()
	r := gatewayResource("p", Gateway{Name: "g", Location: "us-central1"})
	if len(r.Facets) != 0 || r.Metadata["policy"] != "" {
		t.Fatalf("%+v", r)
	}
}

func TestPolicyURLListTLSMapping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pols, err := NewPolicies(sample()).List(ctx, "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	pol := pols[0]
	if pol.ID != "projects/p/locations/us-central1/gatewaySecurityPolicies/egress-policy" ||
		pol.Metadata["tlsInspectionPolicy"] != "egress-tls" || !pol.Created.Equal(created) ||
		len(pol.Facets) != 1 || pol.Facets[0].ID != model.FacetRules {
		t.Fatalf("policy %+v", pol)
	}

	lists, err := NewURLLists(sample()).List(ctx, "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	ul := lists[0]
	if ul.ID != "projects/p/locations/us-central1/urlLists/dev-tools" ||
		len(ul.Lists) != 1 || !slices.Equal(ul.Lists[0].Items, []string{"github.com", "*.github.com"}) {
		t.Fatalf("url list %+v", ul)
	}

	tls, err := NewTLSInspectionPolicies(sample()).List(ctx, "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	tp := tls[0]
	if tp.ID != "projects/p/locations/us-central1/tlsInspectionPolicies/egress-tls" ||
		tp.Metadata["caPool"] != "projects/p/locations/us-central1/caPools/swp-pool" ||
		tp.Metadata["profile"] != "MODERN" || tp.Metadata["minTlsVersion"] != "TLS_1_2" ||
		tp.Metadata["excludePublicCAs"] != "on" {
		t.Fatalf("tls %+v", tp)
	}
}

func TestRuleStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		enabled bool
		want    model.ResourceStatus
	}{
		{true, ""},
		{false, model.StatusWarning},
	}
	for _, tc := range cases {
		r := ruleResource("projects/p/locations/l/gatewaySecurityPolicies/x", "l", Rule{Name: "r", Action: "ALLOW", Enabled: tc.enabled})
		if r.Status != tc.want || (r.Metadata["disabled"] == "on") == tc.enabled {
			t.Errorf("enabled=%v: status %s disabled=%q", tc.enabled, r.Status, r.Metadata["disabled"])
		}
	}
}

func TestListRelated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	api := sample()
	proxies, err := NewWebProxies(api).List(ctx, "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	pols, err := NewPolicies(api).List(ctx, "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		related   model.RelatedLister
		parent    model.Resource
		wantRules []string
		wantID    string
	}{
		{"proxy", NewWebProxies(api), proxies[0], []string{"allow-github", "block-uploads"},
			"projects/p/locations/us-central1/gatewaySecurityPolicies/egress-policy/rules/allow-github"},
		{"proxy with cross-project policy", NewWebProxies(api), proxies[1], []string{"allow-all"},
			"projects/hub/locations/us-east1/gatewaySecurityPolicies/central/rules/allow-all"},
		{"policy", NewPolicies(api), pols[0], []string{"allow-github", "block-uploads"},
			"projects/p/locations/us-central1/gatewaySecurityPolicies/egress-policy/rules/allow-github"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, truncated, err := tc.related.ListRelated(ctx, tc.parent, model.FacetRules)
			if err != nil || truncated {
				t.Fatalf("err=%v truncated=%v", err, truncated)
			}
			var names []string
			for _, r := range got {
				names = append(names, r.Name)
				if r.ParentID != tc.parent.ID || r.FacetID != model.FacetRules || r.ScopeID != "projects/p" || r.Service != model.ServiceSWPRule {
					t.Errorf("rule %+v", r)
				}
			}
			if !slices.Equal(names, tc.wantRules) {
				t.Fatalf("rules %v, want %v", names, tc.wantRules)
			}
			if got[0].ID != tc.wantID || got[0].Badge != "ALLOW" {
				t.Errorf("first rule %+v", got[0])
			}
		})
	}
}

func TestListRelatedIgnoresOthers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	api := fakeAPI{err: errors.New("must not be called")}
	cases := []struct {
		name    string
		related model.RelatedLister
		parent  model.Resource
		facet   string
	}{
		{"other service", NewWebProxies(api), model.Resource{ID: "projects/p/locations/l/keyRings/r", Service: model.ServiceKMS}, model.FacetRules},
		{"other facet", NewWebProxies(api), model.Resource{ID: "projects/p/locations/l/gateways/g", Service: model.ServiceWebProxy, Metadata: map[string]string{"policy": "x"}}, model.FacetKeys},
		{"proxy without policy", NewWebProxies(api), model.Resource{ID: "projects/p/locations/l/gateways/g", Service: model.ServiceWebProxy}, model.FacetRules},
		{"malformed proxy", NewWebProxies(api), model.Resource{ID: "nope", Service: model.ServiceWebProxy, Metadata: map[string]string{"policy": "x"}}, model.FacetRules},
		{"policy as proxy", NewPolicies(api), model.Resource{ID: "projects/p/locations/l/gateways/g", Service: model.ServiceWebProxy}, model.FacetRules},
		{"malformed policy", NewPolicies(api), model.Resource{ID: "projects/p/locations/l/urlLists/u", Service: model.ServiceSWPPolicy}, model.FacetRules},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, truncated, err := tc.related.ListRelated(ctx, tc.parent, tc.facet)
			if got != nil || truncated || err != nil {
				t.Fatalf("got %v %v %v", got, truncated, err)
			}
		})
	}
}

func TestRuleURLListDrill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	api := sample()
	api.rules["p/us-central1/egress-policy"] = []Rule{
		{
			Name:           "allowlist",
			Priority:       100,
			Action:         "ALLOW",
			Enabled:        true,
			SessionMatcher: "inUrlList(host(), 'projects/p/locations/us-central1/urlLists/dev-tools')",
			Created:        created,
		},
		{
			Name:           "plain",
			Priority:       200,
			Action:         "DENY",
			SessionMatcher: "host() == 'bad.example'",
		},
	}
	proxies, err := NewWebProxies(api).List(ctx, "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	rules, _, err := NewWebProxies(api).ListRelated(ctx, proxies[0], model.FacetRules)
	if err != nil || len(rules) != 2 {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
	if rules[0].Metadata["urlLists"] != "dev-tools" || len(rules[0].Facets) != 1 || rules[0].Facets[0].ID != model.FacetURLLists || !rules[0].Facets[0].Inline {
		t.Fatalf("allowlist rule %+v", rules[0])
	}
	if len(rules[1].Facets) != 0 {
		t.Fatalf("plain rule should have no facets: %+v", rules[1].Facets)
	}
	lists, truncated, err := NewURLLists(api).ListRelated(ctx, rules[0], model.FacetURLLists)
	if err != nil || truncated || len(lists) != 1 {
		t.Fatalf("lists=%+v truncated=%v err=%v", lists, truncated, err)
	}
	if lists[0].Name != "dev-tools" || lists[0].ParentID != rules[0].ID || lists[0].FacetID != model.FacetURLLists ||
		!slices.Equal(lists[0].List(model.ListURLs), []string{"github.com", "*.github.com"}) {
		t.Fatalf("url list %+v", lists[0])
	}
}

func TestRulesTruncate(t *testing.T) {
	t.Parallel()
	api := sample()
	many := make([]Rule, maxRules+1)
	for i := range many {
		many[i] = Rule{Name: "r", Priority: int64(i)}
	}
	api.rules = map[string][]Rule{"p/us-central1/egress-policy": many}
	parent := policyResource("p", Policy{Name: "egress-policy", Location: "us-central1"})
	got, truncated, err := NewPolicies(api).ListRelated(context.Background(), parent, model.FacetRules)
	if err != nil || !truncated || len(got) != maxRules {
		t.Fatalf("len=%d truncated=%v err=%v", len(got), truncated, err)
	}
}

func TestPartialLocationFailure(t *testing.T) {
	t.Parallel()
	api := sample()
	api.locErr = map[string]error{"us-east1": errors.New("unavailable")}
	got, err := NewWebProxies(api).List(context.Background(), "projects/p")
	if err != nil || len(got) != 1 {
		t.Fatalf("len=%d err=%v, want 1 and nil error", len(got), err)
	}
	boom := errors.New("down")
	api.locErr = map[string]error{"global": boom, "us-central1": boom, "us-east1": boom}
	if _, err := NewWebProxies(api).List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Fatalf("err=%v, want %v", err, boom)
	}
}

func TestErrorPropagation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	boom := errors.New("boom")
	api := sample()
	api.err = boom
	for svc, p := range providers(api) {
		if _, err := p.List(ctx, "projects/p"); !errors.Is(err, boom) {
			t.Errorf("%s List err=%v", svc, err)
		}
	}
	parent := policyResource("p", Policy{Name: "egress-policy", Location: "us-central1"})
	if _, _, err := NewPolicies(api).ListRelated(ctx, parent, model.FacetRules); !errors.Is(err, boom) {
		t.Errorf("ListRelated err=%v", err)
	}
}

func TestNoAPI(t *testing.T) {
	t.Parallel()
	for svc, p := range providers(nil) {
		if _, err := p.List(context.Background(), "projects/p"); err == nil {
			t.Errorf("%s: want error without an API", svc)
		}
	}
}

func TestListPermissions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		p    model.PermissionDeclarer
		want []string
	}{
		{NewWebProxies(nil), []string{"networkservices.locations.list", "networkservices.gateways.list"}},
		{NewPolicies(nil), []string{"networksecurity.locations.list", "networksecurity.gatewaySecurityPolicies.list"}},
		{NewURLLists(nil), []string{"networksecurity.locations.list", "networksecurity.urlLists.list"}},
		{NewTLSInspectionPolicies(nil), []string{"networksecurity.locations.list", "networksecurity.tlsInspectionPolicies.list"}},
	}
	for _, tc := range cases {
		if got := tc.p.ListPermissions("projects/p"); !slices.Equal(got, tc.want) {
			t.Errorf("got %v, want %v", got, tc.want)
		}
		if got := tc.p.ListPermissions("folders/1"); got != nil {
			t.Errorf("folder permissions %v", got)
		}
	}
}
