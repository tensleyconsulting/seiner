package certmanager

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	locations []string
	certs     map[string][]Certificate // keyed by location
	maps      map[string][]Map
	entries   map[string][]MapEntry // keyed by map name
	dnsAuths  map[string][]DNSAuthorization
	issuance  map[string][]IssuanceConfig
	trust     map[string][]TrustConfig
	failLocs  map[string]error // per-location list error
	err       error            // returned from every method when set
	calls     *atomic.Int32
}

func (f fakeAPI) call(location string) error {
	if f.calls != nil {
		f.calls.Add(1)
	}
	if f.err != nil {
		return f.err
	}
	return f.failLocs[location]
}

func (f fakeAPI) Locations(context.Context, string) ([]string, error) {
	if err := f.call(""); err != nil {
		return nil, err
	}
	return f.locations, nil
}

func (f fakeAPI) ListCertificates(_ context.Context, _, loc string) ([]Certificate, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.certs[loc], nil
}

func (f fakeAPI) ListMaps(_ context.Context, _, loc string) ([]Map, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.maps[loc], nil
}

func (f fakeAPI) ListMapEntries(_ context.Context, _, _, certMap string, limit int) ([]MapEntry, bool, error) {
	if err := f.call(""); err != nil {
		return nil, false, err
	}
	es := f.entries[certMap]
	if len(es) > limit {
		return es[:limit], true, nil
	}
	return es, false, nil
}

func (f fakeAPI) ListDNSAuthorizations(_ context.Context, _, loc string) ([]DNSAuthorization, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.dnsAuths[loc], nil
}

func (f fakeAPI) ListIssuanceConfigs(_ context.Context, _, loc string) ([]IssuanceConfig, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.issuance[loc], nil
}

func (f fakeAPI) ListTrustConfigs(_ context.Context, _, loc string) ([]TrustConfig, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.trust[loc], nil
}

var (
	created = time.Date(2025, 3, 10, 9, 0, 0, 0, time.UTC)
	now     = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
)

func sample() fakeAPI {
	return fakeAPI{
		locations: []string{"global", "us-central1"},
		certs: map[string][]Certificate{
			"global": {{
				Name: "web-cert", Location: "global", Description: "Public site", Type: "MANAGED", State: "ACTIVE",
				SANs: []string{"example.com", "www.example.com"}, Domains: []string{"example.com", "www.example.com"},
				DNSAuthorizations: []string{"projects/p/locations/global/dnsAuthorizations/example-com"},
				Authorizations:    []Authorization{{Domain: "example.com", State: "AUTHORIZED"}},
				Expires:           now.Add(60 * 24 * time.Hour), Created: created, Labels: map[string]string{"env": "prod"},
			}},
			"us-central1": {{Name: "internal", Location: "us-central1", Type: "SELF_MANAGED", Expires: now.Add(-time.Hour)}},
		},
		maps: map[string][]Map{
			"global": {{Name: "web-map", Location: "global", Targets: []string{"projects/p/global/targetHttpsProxies/web-proxy"}, Addresses: []string{"203.0.113.20:443"}, Created: created}},
		},
		entries: map[string][]MapEntry{
			"web-map": {
				{Name: "primary", Matcher: "PRIMARY", State: "ACTIVE", Certificates: []string{"projects/p/locations/global/certificates/web-cert"}},
				{Name: "www", Hostname: "www.example.com", State: "PENDING", Certificates: []string{"projects/p/locations/global/certificates/web-cert"}},
			},
		},
		dnsAuths: map[string][]DNSAuthorization{
			"global": {{Name: "example-com", Location: "global", Domain: "example.com", Type: "FIXED_RECORD", RecordName: "_acme-challenge.example.com.", RecordType: "CNAME", RecordData: "x.authorize.certificatemanager.goog."}},
		},
		issuance: map[string][]IssuanceConfig{
			"global": {{Name: "internal-ca", Location: "global", CAPool: "projects/p/locations/us-central1/caPools/pool", KeyAlgorithm: "ECDSA_P256", Lifetime: "2592000s", RotationWindow: 66}},
		},
		trust: map[string][]TrustConfig{
			"global": {{Name: "mtls", Location: "global", TrustAnchors: 2, Intermediates: 1, SPIFFEDomains: []string{"example.org"}}},
		},
	}
}

func certs(api API) Certificates {
	p := NewCertificates(api)
	p.now = func() time.Time { return now }
	return p
}

func TestListScopes(t *testing.T) {
	t.Parallel()
	listers := map[string]model.ResourceProvider{
		"certificates": certs(nil), "maps": NewMaps(nil), "dns": NewDNSAuthorizations(nil),
		"issuance": NewIssuanceConfigs(nil), "trust": NewTrustConfigs(nil),
	}
	for name, l := range listers {
		for _, scope := range []string{"folders/1", "organizations/1", ""} {
			got, err := l.List(context.Background(), scope)
			if got != nil || err != nil {
				t.Errorf("%s List(%q) = %v, %v", name, scope, got, err)
			}
		}
		if _, err := l.List(context.Background(), "projects/p"); err == nil {
			t.Errorf("%s List with nil API should fail", name)
		}
	}
}

func TestListEveryType(t *testing.T) {
	t.Parallel()
	api := sample()
	cases := []struct {
		lister model.ResourceProvider
		svc    model.ServiceType
		ids    []string
	}{
		{certs(api), model.ServiceCertificate, []string{"projects/p/locations/global/certificates/web-cert", "projects/p/locations/us-central1/certificates/internal"}},
		{NewMaps(api), model.ServiceCertificateMap, []string{"projects/p/locations/global/certificateMaps/web-map"}},
		{NewDNSAuthorizations(api), model.ServiceDNSAuthorization, []string{"projects/p/locations/global/dnsAuthorizations/example-com"}},
		{NewIssuanceConfigs(api), model.ServiceIssuanceConfig, []string{"projects/p/locations/global/certificateIssuanceConfigs/internal-ca"}},
		{NewTrustConfigs(api), model.ServiceTrustConfig, []string{"projects/p/locations/global/trustConfigs/mtls"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.svc), func(t *testing.T) {
			t.Parallel()
			if tc.lister.Service() != tc.svc {
				t.Fatalf("Service = %s", tc.lister.Service())
			}
			got, err := tc.lister.List(context.Background(), "projects/p")
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, r := range got {
				if r.Service != tc.svc || r.ScopeID != "projects/p" || r.Validate() != nil {
					t.Errorf("resource %+v", r)
				}
				ids = append(ids, r.ID)
			}
			if !slices.Equal(ids, tc.ids) {
				t.Errorf("ids = %v, want %v", ids, tc.ids)
			}
		})
	}
}

func TestCertificateMapping(t *testing.T) {
	t.Parallel()
	got, err := certs(sample()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	web := got[0]
	if web.Name != "web-cert" || web.Location != "global" || web.State != "ACTIVE" || web.Status != model.StatusHealthy ||
		web.Badge != "" || !web.Created.Equal(created) || web.Labels["env"] != "prod" || web.Description != "Public site" {
		t.Errorf("web-cert %+v", web)
	}
	for k, v := range map[string]string{
		"type": "Google-managed", "domains": "example.com, www.example.com", "dnsAuthorizations": "example-com",
		"expires": "2026-12-26 12:00 UTC",
	} {
		if web.Metadata[k] != v {
			t.Errorf("web-cert %s = %q, want %q", k, web.Metadata[k], v)
		}
	}
	if keys := fieldKeys(web); !slices.Equal(keys[:3], []string{"type", "domains", "expires"}) {
		t.Errorf("fields = %v", keys)
	}
	if sans := web.List("sans"); !slices.Equal(sans, []string{"example.com", "www.example.com"}) {
		t.Errorf("sans = %v", sans)
	}
	if auth := web.List("authorizations"); !slices.Equal(auth, []string{"example.com: AUTHORIZED"}) {
		t.Errorf("authorizations = %v", auth)
	}
	if internal := got[1]; internal.Status != model.StatusError || internal.Badge != "EXPIRED" || internal.Metadata["type"] != "self-managed" {
		t.Errorf("internal %+v", internal)
	}
}

func TestCertStatus(t *testing.T) {
	t.Parallel()
	soon := now.Add(10*24*time.Hour + time.Hour)
	cases := []struct {
		name   string
		c      Certificate
		status model.ResourceStatus
		badge  string
	}{
		{"active", Certificate{State: "ACTIVE"}, model.StatusHealthy, ""},
		{"self-managed, far", Certificate{Expires: now.Add(90 * 24 * time.Hour)}, model.StatusUnknown, ""},
		{"provisioning", Certificate{State: "PROVISIONING"}, model.StatusWarning, ""},
		{"failed", Certificate{State: "FAILED"}, model.StatusError, ""},
		{"unknown state", Certificate{State: "NEW"}, model.StatusUnknown, ""},
		{"expiring", Certificate{State: "ACTIVE", Expires: soon}, model.StatusWarning, "10d"},
		{"failed and expiring", Certificate{State: "FAILED", Expires: soon}, model.StatusError, "10d"},
		{"expired", Certificate{State: "ACTIVE", Expires: now}, model.StatusError, "EXPIRED"},
	}
	for _, tc := range cases {
		status, badge := certStatus(tc.c, now)
		if status != tc.status || badge != tc.badge {
			t.Errorf("%s: %s %q, want %s %q", tc.name, status, badge, tc.status, tc.badge)
		}
	}
}

func TestOtherMappings(t *testing.T) {
	t.Parallel()
	api := sample()
	ctx := context.Background()
	check := func(r model.Resource, want map[string]string, keys ...string) {
		t.Helper()
		for k, v := range want {
			if r.Metadata[k] != v {
				t.Errorf("%s %s = %q, want %q", r.Name, k, r.Metadata[k], v)
			}
		}
		if got := fieldKeys(r); !slices.Equal(got, keys) {
			t.Errorf("%s fields = %v, want %v", r.Name, got, keys)
		}
	}
	maps, _ := NewMaps(api).List(ctx, "projects/p")
	check(maps[0], map[string]string{"targets": "web-proxy", "addresses": "203.0.113.20:443"}, "targets", "addresses")
	if f := maps[0].Facets; len(f) != 1 || f[0].ID != model.FacetMapEntries || !f[0].Inline {
		t.Errorf("map facets = %+v", f)
	}
	dns, _ := NewDNSAuthorizations(api).List(ctx, "projects/p")
	check(dns[0], map[string]string{"domain": "example.com", "type": "fixed record", "recordType": "CNAME"},
		"domain", "type", "recordName", "recordType", "recordData")
	iss, _ := NewIssuanceConfigs(api).List(ctx, "projects/p")
	check(iss[0], map[string]string{"keyAlgorithm": "ECDSA_P256", "lifetime": "720h0m0s", "rotationWindow": "66%"},
		"caPool", "keyAlgorithm", "lifetime", "rotationWindow")
	trust, _ := NewTrustConfigs(api).List(ctx, "projects/p")
	check(trust[0], map[string]string{"trustAnchors": "2", "intermediates": "1", "spiffeTrustDomains": "example.org"},
		"trustAnchors", "intermediates", "spiffeTrustDomains")
}

func TestMapEntries(t *testing.T) {
	t.Parallel()
	p := NewMaps(sample())
	parent := model.Resource{ID: "projects/p/locations/global/certificateMaps/web-map", Service: model.ServiceCertificateMap, ScopeID: "projects/p"}
	got, truncated, err := p.ListRelated(context.Background(), parent, model.FacetMapEntries)
	if err != nil || truncated || len(got) != 2 {
		t.Fatalf("got %d truncated=%v err=%v", len(got), truncated, err)
	}
	primary, www := got[0], got[1]
	if primary.Name != "PRIMARY" || primary.Badge != "PRIMARY" || primary.Status != model.StatusHealthy ||
		primary.ID != parent.ID+"/certificateMapEntries/primary" || primary.ParentID != parent.ID ||
		primary.FacetID != model.FacetMapEntries || primary.ScopeID != "projects/p" || primary.Metadata["certificates"] != "web-cert" {
		t.Errorf("primary %+v", primary)
	}
	if www.Name != "www.example.com" || www.Status != model.StatusWarning || www.State != "PENDING" || www.Badge != "" {
		t.Errorf("www %+v", www)
	}
}

func TestMapEntriesTruncate(t *testing.T) {
	t.Parallel()
	api := sample()
	many := make([]MapEntry, maxEntries+1)
	for i := range many {
		many[i] = MapEntry{Name: "e" + string(rune('a'+i%26)) + time.Duration(i).String()}
	}
	api.entries = map[string][]MapEntry{"web-map": many}
	parent := model.Resource{ID: "projects/p/locations/global/certificateMaps/web-map", Service: model.ServiceCertificateMap}
	got, truncated, err := NewMaps(api).ListRelated(context.Background(), parent, model.FacetMapEntries)
	if err != nil || !truncated || len(got) != maxEntries {
		t.Fatalf("got %d truncated=%v err=%v", len(got), truncated, err)
	}
}

func TestMapEntriesIgnoresOthers(t *testing.T) {
	t.Parallel()
	api := sample()
	api.calls = new(atomic.Int32)
	p := NewMaps(api)
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: "projects/p/locations/global/certificateMaps/m", Service: model.ServiceCertificate}, model.FacetMapEntries},
		{"other facet", model.Resource{ID: "projects/p/locations/global/certificateMaps/m", Service: model.ServiceCertificateMap}, model.FacetRules},
		{"entry id", model.Resource{ID: "projects/p/locations/global/certificateMaps/m/certificateMapEntries/e", Service: model.ServiceCertificateMap}, model.FacetMapEntries},
		{"malformed", model.Resource{ID: "nope", Service: model.ServiceCertificateMap}, model.FacetMapEntries},
	}
	for _, tc := range cases {
		got, truncated, err := p.ListRelated(context.Background(), tc.parent, tc.facet)
		if got != nil || truncated || err != nil {
			t.Errorf("%s: %v %v %v", tc.name, got, truncated, err)
		}
	}
	if api.calls.Load() != 0 {
		t.Errorf("API called %d times", api.calls.Load())
	}
}

func TestLocationFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	api := sample()
	api.failLocs = map[string]error{"us-central1": boom}
	got, err := certs(api).List(context.Background(), "projects/p")
	if err != nil || len(got) != 1 || got[0].Name != "web-cert" {
		t.Fatalf("one failed location: %v %v", got, err)
	}
	api.failLocs["global"] = boom
	if _, err := certs(api).List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Fatalf("every location failed: err = %v", err)
	}
}

func TestErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	api := fakeAPI{err: boom}
	ctx := context.Background()
	for _, l := range []model.ResourceProvider{certs(api), NewMaps(api), NewDNSAuthorizations(api), NewIssuanceConfigs(api), NewTrustConfigs(api)} {
		if _, err := l.List(ctx, "projects/p"); !errors.Is(err, boom) {
			t.Errorf("%s List err = %v", l.Service(), err)
		}
	}
	parent := model.Resource{ID: "projects/p/locations/global/certificateMaps/m", Service: model.ServiceCertificateMap}
	if _, _, err := NewMaps(api).ListRelated(ctx, parent, model.FacetMapEntries); !errors.Is(err, boom) {
		t.Errorf("ListRelated err = %v", err)
	}
}

func TestListPermissions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d    model.PermissionDeclarer
		perm string
	}{
		{certs(nil), "certificatemanager.certs.list"},
		{NewMaps(nil), "certificatemanager.certmaps.list"},
		{NewDNSAuthorizations(nil), "certificatemanager.dnsauthorizations.list"},
		{NewIssuanceConfigs(nil), "certificatemanager.certissuanceconfigs.list"},
		{NewTrustConfigs(nil), "certificatemanager.trustconfigs.list"},
	}
	for _, tc := range cases {
		if got, want := tc.d.ListPermissions("projects/p"), []string{"certificatemanager.locations.list", tc.perm}; !slices.Equal(got, want) {
			t.Errorf("ListPermissions = %v, want %v", got, want)
		}
		if got := tc.d.ListPermissions("folders/1"); got != nil {
			t.Errorf("folder permissions = %v", got)
		}
	}
}

func fieldKeys(r model.Resource) []string {
	out := make([]string, 0, len(r.Fields))
	for _, f := range r.Fields {
		out = append(out, f.Key)
	}
	return out
}
