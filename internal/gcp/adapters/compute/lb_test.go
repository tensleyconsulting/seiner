package compute

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func (f fakeAPI) ListURLMaps(context.Context, string) ([]URLMap, error) { return f.urlMaps, f.err }

func (f fakeAPI) ListBackendServices(context.Context, string) ([]BackendService, error) {
	if f.backErr != nil {
		return nil, f.backErr
	}
	return f.backends, f.err
}

func (f fakeAPI) ListTargetProxies(context.Context, string) ([]TargetProxy, error) {
	if f.proxyErr != nil && !model.IsPartial(f.proxyErr) {
		return nil, f.proxyErr
	}
	if f.proxyErr != nil {
		return f.proxies, f.proxyErr
	}
	return f.proxies, f.err
}

func (f fakeAPI) ListSSLPolicies(context.Context, string) ([]SSLPolicy, error) {
	return f.ssl, f.err
}

func (f fakeAPI) ListHealthChecks(context.Context, string) ([]HealthCheck, error) {
	return f.checks, f.err
}

func (f fakeAPI) ListSecurityPolicies(context.Context, string) ([]SecurityPolicy, error) {
	return f.policies, f.err
}

// lbSample is an external application LB (web-map, with an HTTP redirect
// map beside it), an internal passthrough LB (api), a TCP proxy LB (db), a
// URL map with no frontend (draft), and a backend service no rule reaches
// (orphan).
func lbSample() fakeAPI {
	return fakeAPI{
		urlMaps: []URLMap{
			{Name: "web-map", ServiceRefs: []string{"global/backendServices/web", "global/backendBuckets/static"}, Hosts: []string{"example.com"}},
			{Name: "web-redirect"},
			{Name: "draft", ServiceRefs: []string{"global/backendServices/web"}},
		},
		backends: []BackendService{
			{Name: "web", Scheme: "EXTERNAL_MANAGED", HealthCheckRefs: []string{"global/healthChecks/web-hc"}, SecurityPolicy: "global/securityPolicies/armor", CDN: true},
			{Name: "api", Region: "us-central1", Scheme: "INTERNAL", HealthCheckRefs: []string{"us-central1/healthChecks/api-hc"}},
			{Name: "db", Scheme: "EXTERNAL", HealthCheckRefs: []string{"global/healthChecks/web-hc"}},
			{Name: "orphan", Scheme: "EXTERNAL_MANAGED"},
		},
		proxies: []TargetProxy{
			{Kind: "targetHttpsProxies", Name: "web-https", URLMapRef: "global/urlMaps/web-map", Certificates: []string{"cert-a"}, SSLPolicy: "modern"},
			{Kind: "targetHttpProxies", Name: "web-http", URLMapRef: "global/urlMaps/web-redirect"},
			{Kind: "targetTcpProxies", Name: "db-proxy", ServiceRef: "global/backendServices/db"},
		},
		rules: []ForwardingRule{
			{Name: "web-443", IPAddress: "203.0.113.10", Ports: "443-443", Scheme: "EXTERNAL_MANAGED", TargetRef: "global/targetHttpsProxies/web-https"},
			{Name: "web-80", IPAddress: "203.0.113.10", Ports: "80-80", Scheme: "EXTERNAL_MANAGED", TargetRef: "global/targetHttpProxies/web-http"},
			{Name: "api-ilb", Region: "us-central1", IPAddress: "10.0.0.5", Ports: "8080", Scheme: "INTERNAL", BackendServiceRef: "us-central1/backendServices/api"},
			{Name: "db-tcp", IPAddress: "203.0.113.30", Ports: "5432-5432", Scheme: "EXTERNAL", TargetRef: "global/targetTcpProxies/db-proxy"},
			{Name: "psc", Region: "us-central1", TargetRef: "us-central1/serviceAttachments/x"},
		},
		checks: []HealthCheck{
			{Name: "web-hc", Type: "HTTP", Port: "80"},
			{Name: "api-hc", Region: "us-central1", Type: "TCP", Port: "8080"},
			{Name: "unused-hc", Type: "TCP"},
		},
	}
}

func TestBuildLoadBalancers(t *testing.T) {
	t.Parallel()
	lbs := buildLoadBalancers(lbInputs{maps: lbSample().urlMaps, services: lbSample().backends, proxies: lbSample().proxies, rules: lbSample().rules})
	type want struct {
		kind      string
		frontends []string
		backends  []string
	}
	got := map[string]want{}
	var names []string
	for _, lb := range lbs {
		w := want{kind: lb.kindLabel()}
		for _, f := range lb.Frontends {
			w.frontends = append(w.frontends, f.Name)
		}
		for _, b := range lb.Backends {
			w.backends = append(w.backends, b.Name)
		}
		got[lb.Name] = w
		names = append(names, lb.Name)
	}
	if !slices.Equal(names, []string{"api", "db", "draft", "web-map", "web-redirect"}) {
		t.Fatalf("load balancers %v", names)
	}
	cases := map[string]want{
		"web-map":      {"Application (external)", []string{"web-443"}, []string{"web"}},
		"web-redirect": {"Application (external)", []string{"web-80"}, nil},
		"draft":        {"Application (external)", nil, []string{"web"}},
		"api":          {"Network passthrough (internal)", []string{"api-ilb"}, []string{"api"}},
		"db":           {"Network proxy (external)", []string{"db-tcp"}, []string{"db"}},
	}
	for name, w := range cases {
		g := got[name]
		if g.kind != w.kind || !slices.Equal(g.frontends, w.frontends) || !slices.Equal(g.backends, w.backends) {
			t.Errorf("%s = %+v, want %+v", name, g, w)
		}
	}
}

func TestKindLabel(t *testing.T) {
	t.Parallel()
	um := &URLMap{Name: "m"}
	cases := []struct {
		lb   loadBalancer
		want string
	}{
		{loadBalancer{URLMap: um, Frontends: []ForwardingRule{{Scheme: "EXTERNAL"}}}, "Application (classic)"},
		{loadBalancer{URLMap: um, Frontends: []ForwardingRule{{Scheme: "INTERNAL_MANAGED"}}}, "Application (internal)"},
		{loadBalancer{URLMap: um, Backends: []BackendService{{Scheme: "EXTERNAL_MANAGED"}}}, "Application (external)"},
		{loadBalancer{Frontends: []ForwardingRule{{Scheme: "EXTERNAL"}}}, "Network passthrough (external)"},
		{loadBalancer{Proxies: []TargetProxy{{}}, Frontends: []ForwardingRule{{Scheme: "INTERNAL_MANAGED"}}}, "Network proxy (internal)"},
		{loadBalancer{}, "Network passthrough"},
	}
	for _, tc := range cases {
		if got := tc.lb.kindLabel(); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.lb, got, tc.want)
		}
	}
}

func TestLoadBalancersList(t *testing.T) {
	t.Parallel()
	got, err := NewLoadBalancers(lbSample()).List(context.Background(), "projects/p")
	if err != nil || len(got) != 5 {
		t.Fatalf("List = %d, err=%v", len(got), err)
	}
	byName := map[string]model.Resource{}
	for _, r := range got {
		if err := r.Validate(); err != nil {
			t.Errorf("%s: %v", r.ID, err)
		}
		byName[r.Name] = r
	}
	web := byName["web-map"]
	if web.ID != "projects/p/global/urlMaps/web-map" || web.Service != model.ServiceLoadBalancer || web.ScopeID != "projects/p" || len(web.Facets) != 3 {
		t.Fatalf("web %+v", web)
	}
	keys, vals := fieldMap(web)
	if want := []string{"type", "frontends", "backends", "hosts", "certificates", "sslPolicy", "cloudArmor", "cdn"}; !slices.Equal(keys, want) {
		t.Errorf("keys %v, want %v", keys, want)
	}
	for k, v := range map[string]string{"frontends": "203.0.113.10:443", "backends": "web, backendBuckets/static", "certificates": "cert-a", "cloudArmor": "armor", "cdn": "on"} {
		if vals[k] != v {
			t.Errorf("%s = %q, want %q", k, vals[k], v)
		}
	}
	if api := byName["api"]; api.ID != "projects/p/regions/us-central1/backendServices/api" || api.Location != "us-central1" {
		t.Errorf("api %s %s", api.ID, api.Location)
	}
	if d := byName["draft"]; d.Status != model.StatusWarning || d.Metadata["frontends"] != "none" {
		t.Errorf("draft status=%s frontends=%q", d.Status, d.Metadata["frontends"])
	}
}

// A proxy kind that could not be listed keeps the load balancers and
// reports a partial error, for List and its facets alike.
func TestLoadBalancersPartialProxies(t *testing.T) {
	t.Parallel()
	api := lbSample()
	api.proxyErr = model.Partial(errors.New("ssl proxies denied"))
	p := NewLoadBalancers(api)
	got, err := p.List(context.Background(), "projects/p")
	if !model.IsPartial(err) || len(got) != 5 {
		t.Fatalf("List = %d, err=%v; want 5 and a partial error", len(got), err)
	}
	kids, _, err := p.ListRelated(context.Background(), got[0], model.FacetFrontends)
	if err != nil || kids == nil {
		t.Fatalf("ListRelated = %v, err=%v", kids, err)
	}
}

func TestLoadBalancersRelated(t *testing.T) {
	t.Parallel()
	p := NewLoadBalancers(lbSample())
	lbs, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(lbs, func(r model.Resource) bool { return r.Name == "web-map" })
	web := lbs[i]
	cases := []struct {
		facet string
		svc   model.ServiceType
		want  []string
	}{
		{model.FacetFrontends, model.ServiceForwardingRule, []string{"web-443"}},
		{model.FacetBackends, model.ServiceBackendService, []string{"web"}},
		{model.FacetHealthChecks, model.ServiceHealthCheck, []string{"web-hc"}},
	}
	for _, tc := range cases {
		got, trunc, err := p.ListRelated(context.Background(), web, tc.facet)
		if err != nil || trunc {
			t.Fatalf("%s: err=%v trunc=%v", tc.facet, err, trunc)
		}
		var names []string
		for _, r := range got {
			names = append(names, r.Name)
			if r.ParentID != web.ID || r.FacetID != tc.facet || r.Service != tc.svc || r.ScopeID != "projects/p" {
				t.Errorf("%s child %+v", tc.facet, r)
			}
		}
		if !slices.Equal(names, tc.want) {
			t.Errorf("%s = %v, want %v", tc.facet, names, tc.want)
		}
	}
	for _, tc := range []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: "projects/p/global/networks/default", Service: model.ServiceVPC, ScopeID: "projects/p"}, model.FacetFrontends},
		{"undeclared facet", web, model.FacetSubnets},
	} {
		if got, trunc, err := p.ListRelated(context.Background(), tc.parent, tc.facet); got != nil || trunc || err != nil {
			t.Errorf("%s: %v %v %v", tc.name, got, trunc, err)
		}
	}
}

func TestHealthChecksAndArmor(t *testing.T) {
	t.Parallel()
	api := lbSample()
	api.policies = []SecurityPolicy{
		{Name: "armor", Type: "CLOUD_ARMOR", AdaptiveProtection: true, Rules: []SecurityRule{
			{Priority: defaultRulePriority, Action: "allow", Match: "src *"},
			{Priority: 1000, Action: "deny(403)", Match: "origin.region_code == 'XX'", Preview: true},
		}},
		{Name: "spare", Region: "us-central1", Type: "CLOUD_ARMOR"},
	}
	hcs, err := NewHealthChecks(api).List(context.Background(), "projects/p")
	if err != nil || len(hcs) != 3 || hcs[1].ID != "projects/p/regions/us-central1/healthChecks/api-hc" {
		t.Fatalf("health checks %v err=%v", hcs, err)
	}
	pols, err := NewSecurityPolicies(api).List(context.Background(), "projects/p")
	if err != nil || len(pols) != 2 {
		t.Fatalf("policies %v err=%v", pols, err)
	}
	armor := pols[0]
	if armor.Service != model.ServiceCloudArmor || armor.Metadata["usedBy"] != "backendServices/web" || armor.Metadata["adaptiveProtection"] != "on" {
		t.Errorf("armor %+v", armor)
	}
	if want := []string{"1000  deny(403)  origin.region_code == 'XX'  (preview)", "default  allow  src *"}; !slices.Equal(armor.List("rules"), want) {
		t.Errorf("rules %q", armor.List("rules"))
	}
	if _, ok := pols[1].Metadata["usedBy"]; ok || pols[1].Location != "us-central1" {
		t.Errorf("spare %+v", pols[1])
	}

	// Attachments are detail: a backend service failure keeps the policies.
	api.backErr = errors.New("denied")
	pols, err = NewSecurityPolicies(api).List(context.Background(), "projects/p")
	if err != nil || len(pols) != 2 {
		t.Errorf("policies without attachments %d err=%v", len(pols), err)
	}
}

func TestSSLPolicies(t *testing.T) {
	t.Parallel()
	api := fakeAPI{
		ssl: []SSLPolicy{
			{Name: "modern", Profile: "MODERN", MinTLSVersion: "TLS_1_2"},
			{Name: "legacy", Region: "us-central1", Profile: "CUSTOM", MinTLSVersion: "TLS_1_0",
				CustomFeatures: []string{"TLS_RSA_WITH_AES_128_CBC_SHA"}, Warnings: []string{"weak cipher"}},
		},
		proxies: []TargetProxy{
			{Kind: "targetHttpsProxies", Name: "web", SSLPolicy: "modern"},
			{Kind: "targetSslProxies", Name: "db", SSLPolicy: "modern"},
			{Kind: "targetHttpsProxies", Name: "int", Region: "us-central1", SSLPolicy: "legacy"},
			// Same name in another region is a different policy.
			{Kind: "targetHttpsProxies", Name: "eu", Region: "europe-west1", SSLPolicy: "legacy"},
		},
	}
	got, err := NewSSLPolicies(api).List(context.Background(), "projects/p")
	if err != nil || len(got) != 2 {
		t.Fatalf("List = %d err=%v", len(got), err)
	}
	modern, legacy := got[0], got[1]
	if modern.ID != "projects/p/global/sslPolicies/modern" || modern.Service != model.ServiceSSLPolicy || modern.Status != "" {
		t.Errorf("modern %+v", modern)
	}
	if modern.Metadata["targets"] != "2" || !slices.Equal(modern.Lists[0].Items, []string{"targetHttpsProxies/web", "targetSslProxies/db"}) {
		t.Errorf("modern used by %+v", modern.Lists)
	}
	if legacy.ID != "projects/p/regions/us-central1/sslPolicies/legacy" || legacy.Location != "us-central1" || legacy.Status != model.StatusWarning {
		t.Errorf("legacy %+v", legacy)
	}
	var lists []string
	for _, l := range legacy.Lists {
		lists = append(lists, l.Label)
	}
	if want := []string{"Used by", "Custom features", "Warnings"}; !slices.Equal(lists, want) || legacy.Lists[0].Items[0] != "targetHttpsProxies/int" || len(legacy.Lists[0].Items) != 1 {
		t.Errorf("legacy lists %v %+v", lists, legacy.Lists)
	}

	// Proxies only add detail.
	api.proxyErr = errors.New("denied")
	got, err = NewSSLPolicies(api).List(context.Background(), "projects/p")
	if err != nil || len(got) != 2 || got[0].Metadata["targets"] != "0" {
		t.Fatalf("without proxies: %d err=%v", len(got), err)
	}
}

func TestLoadBalancerProvidersScopesAndErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	for _, p := range []model.ResourceProvider{NewLoadBalancers(nil), NewHealthChecks(nil), NewSecurityPolicies(nil), NewSSLPolicies(nil)} {
		for _, scope := range []string{"folders/1", "organizations/1", ""} {
			if got, err := p.List(context.Background(), scope); got != nil || err != nil {
				t.Errorf("%s List(%q) = %v, %v", p.Service(), scope, got, err)
			}
		}
	}
	api := fakeAPI{err: boom}
	for _, p := range []model.ResourceProvider{NewLoadBalancers(api), NewHealthChecks(api), NewSecurityPolicies(api), NewSSLPolicies(api)} {
		if _, err := p.List(context.Background(), "projects/p"); !errors.Is(err, boom) {
			t.Errorf("%s err=%v", p.Service(), err)
		}
	}
	lb := model.Resource{ID: "projects/p/global/urlMaps/m", Service: model.ServiceLoadBalancer, ScopeID: "projects/p"}
	if _, _, err := NewLoadBalancers(api).ListRelated(context.Background(), lb, model.FacetFrontends); !errors.Is(err, boom) {
		t.Errorf("ListRelated err=%v", err)
	}
}

func TestDetailMappers(t *testing.T) {
	t.Parallel()
	b := backendServiceResource("p", BackendService{
		Name: "web", Protocol: "HTTPS", Scheme: "EXTERNAL_MANAGED", TimeoutSec: 30,
		HealthCheckRefs: []string{"global/healthChecks/a", "global/healthChecks/b"},
		Backends:        []Backend{{Group: "instanceGroups/ig", Location: "us-east1-b", BalancingMode: "RATE", Capacity: 0.5}},
	}, "")
	if b.ParentID != "" || b.FacetID != "" {
		t.Errorf("top-level backend has parent %q/%q", b.ParentID, b.FacetID)
	}
	_, vals := fieldMap(b)
	if vals["healthChecks"] != "a, b" || vals["timeout"] != "30s" || vals["backends"] != "1" {
		t.Errorf("backend %v", vals)
	}
	if got := b.List("backends"); !slices.Equal(got, []string{"instanceGroups/ig (us-east1-b) RATE 50%"}) {
		t.Errorf("backends %q", got)
	}
	h := healthCheckResource("p", HealthCheck{Name: "hc", Type: "HTTPS", Port: "443", RequestPath: "/", IntervalSec: 5, TimeoutSec: 4, HealthyThreshold: 2, UnhealthyThreshold: 3}, "")
	keys, vals := fieldMap(h)
	if !slices.Equal(keys, []string{"type", "port", "requestPath", "interval", "timeout", "thresholds", "logging"}) || vals["thresholds"] != "healthy 2, unhealthy 3" {
		t.Errorf("health check %v %v", keys, vals)
	}
}

func TestRefs(t *testing.T) {
	t.Parallel()
	const base = "https://www.googleapis.com/compute/v1/projects/p/"
	cases := []struct{ in, want string }{
		{base + "global/backendServices/web", "global/backendServices/web"},
		{base + "regions/us-central1/backendServices/api", "us-central1/backendServices/api"},
		{"projects/p/global/urlMaps/m", "global/urlMaps/m"},
		{base + "zones/us-central1-a/instanceGroups/ig", ""},
		{"all-apis", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := refOf(tc.in); got != tc.want {
			t.Errorf("refOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if refFor("", "urlMaps", "m") != "global/urlMaps/m" || refFor("r", "backendServices", "b") != "r/backendServices/b" {
		t.Error("refFor")
	}
	if frontendSummary(ForwardingRule{IPAddress: "192.0.2.4", Ports: "80-80"}) != "192.0.2.4:80" ||
		frontendSummary(ForwardingRule{IPAddress: "192.0.2.4", Ports: "8000-8080"}) != "192.0.2.4:8000-8080" ||
		frontendSummary(ForwardingRule{IPAddress: "192.0.2.4"}) != "192.0.2.4" {
		t.Error("frontendSummary")
	}
}

func TestLoadBalancerCertificateMap(t *testing.T) {
	t.Parallel()
	lb := loadBalancer{Name: "web-map", Proxies: []TargetProxy{
		{Kind: "targetHttpsProxies", Name: "a", CertificateMap: "web-certs"},
		{Kind: "targetHttpsProxies", Name: "b", CertificateMap: "web-certs"},
	}}
	if got := loadBalancerResource("p", lb).Metadata["certificateMap"]; got != "web-certs" {
		t.Fatalf("certificateMap = %q", got)
	}
}
