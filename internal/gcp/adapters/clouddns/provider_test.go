package clouddns

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	zones   []Zone
	records map[string][]Record // keyed by zone name
	err     error               // returned from every method when set
	calls   *int
}

func (f fakeAPI) called() {
	if f.calls != nil {
		*f.calls++
	}
}

func (f fakeAPI) ListZones(context.Context, string) ([]Zone, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	return f.zones, nil
}

func (f fakeAPI) ListRecords(_ context.Context, _, zone string, limit int) ([]Record, bool, error) {
	f.called()
	if f.err != nil {
		return nil, false, f.err
	}
	rs := f.records[zone]
	if len(rs) > limit {
		return rs[:limit], true, nil
	}
	return rs, false, nil
}

var created = time.Date(2023, 2, 10, 9, 0, 0, 0, time.UTC)

func sample() fakeAPI {
	return fakeAPI{
		zones: []Zone{
			{
				Name: "example-com", DNSName: "example.com.", Description: "Public zone", Visibility: "public",
				NameServers: []string{"ns-cloud-a1.googledomains.com.", "ns-cloud-a2.googledomains.com."},
				DNSSEC:      "on", Created: created, Labels: map[string]string{"env": "prod"},
			},
			{
				Name: "corp-internal", DNSName: "corp.internal.", Visibility: "private",
				Networks: []string{"default", "shared"}, GKEClusters: []string{"prod"}, Logging: true,
			},
		},
		records: map[string][]Record{
			"example-com": {
				{Name: "example.com.", Type: "A", TTL: 300, Data: []string{"203.0.113.110"}},
				{Name: "example.com.", Type: "NS", TTL: 21600, Data: []string{"ns-cloud-a1.googledomains.com.", "ns-cloud-a2.googledomains.com."}},
				{Name: "geo.example.com.", Type: "A", TTL: 60, Routing: "geo"},
			},
		},
	}
}

func TestListScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope     string
		wantCalls int
		wantN     int
	}{
		{"projects/p", 1, 2},
		{"folders/1", 0, 0},
		{"organizations/1", 0, 0},
		{"", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			api := sample()
			calls := 0
			api.calls = &calls
			got, err := New(api).List(context.Background(), tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.wantN || calls != tc.wantCalls {
				t.Fatalf("len=%d calls=%d, want %d and %d", len(got), calls, tc.wantN, tc.wantCalls)
			}
		})
	}
}

func TestZoneMapping(t *testing.T) {
	t.Parallel()
	got, err := New(sample()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.ID != "projects/p/managedZones/example-com" || r.Name != "example-com" || r.Service != model.ServiceCloudDNS ||
		r.ScopeID != "projects/p" || r.Location != "global" || r.Description != "Public zone" {
		t.Fatalf("identity: %+v", r)
	}
	if r.Status != "" || !r.Created.Equal(created) || r.Labels["env"] != "prod" {
		t.Fatalf("status/created/labels: %+v", r)
	}
	var keys []string
	for _, f := range r.Fields {
		keys = append(keys, f.Key)
	}
	if want := []string{"dnsName", "visibility", "dnssec", "nameServers", "logging"}; !slices.Equal(keys, want) {
		t.Fatalf("fields = %v, want %v", keys, want)
	}
	if len(r.Facets) != 1 || r.Facets[0].ID != model.FacetRecords || !r.Facets[0].Inline {
		t.Fatalf("facets: %+v", r.Facets)
	}

	private := got[1]
	if private.Metadata["networks"] != "default, shared" || private.Metadata["gkeClusters"] != "prod" ||
		private.Metadata["logging"] != "on" {
		t.Fatalf("private zone metadata: %+v", private.Metadata)
	}
}

func TestRecords(t *testing.T) {
	t.Parallel()
	p := New(sample())
	zones, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	zone := zones[0]
	got, truncated, err := p.ListRelated(context.Background(), zone, model.FacetRecords)
	if err != nil || truncated {
		t.Fatalf("err=%v truncated=%v", err, truncated)
	}
	if len(got) != 3 {
		t.Fatalf("records: %+v", got)
	}
	a := got[0]
	if a.ID != "projects/p/managedZones/example-com/rrsets/example.com./A" || a.Name != "example.com." ||
		a.Service != model.ServiceDNSRecord || a.ParentID != zone.ID || a.FacetID != model.FacetRecords ||
		a.ScopeID != "projects/p" || a.Badge != "A" {
		t.Fatalf("A record identity: %+v", a)
	}
	if a.Metadata["data"] != "203.0.113.110" || a.Metadata["ttl"] != "300" || len(a.Lists) != 0 {
		t.Fatalf("A record data: %+v", a)
	}
	ns := got[1]
	if ns.Metadata["data"] != "2 values" || len(ns.List("data")) != 2 {
		t.Fatalf("NS record data: %+v", ns)
	}
	if geo := got[2]; geo.Metadata["routing"] != "geo" {
		t.Fatalf("geo record: %+v", geo)
	}
}

func TestRecordsTruncate(t *testing.T) {
	t.Parallel()
	api := sample()
	many := make([]Record, maxRecords+1)
	for i := range many {
		many[i] = Record{Name: "r.example.com.", Type: "TXT"}
	}
	api.records["example-com"] = many
	zone := zoneResource("p", api.zones[0])
	got, truncated, err := New(api).ListRelated(context.Background(), zone, model.FacetRecords)
	if err != nil || !truncated || len(got) != maxRecords {
		t.Fatalf("len=%d truncated=%v err=%v", len(got), truncated, err)
	}
}

func TestListRelatedIgnoresOthers(t *testing.T) {
	t.Parallel()
	zone := zoneResource("p", sample().zones[0])
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: zone.ID, Service: model.ServiceGKE}, model.FacetRecords},
		{"other facet", zone, model.FacetNodePools},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			api := sample()
			api.calls = &calls
			got, truncated, err := New(api).ListRelated(context.Background(), tc.parent, tc.facet)
			if got != nil || truncated || err != nil || calls != 0 {
				t.Fatalf("got=%v truncated=%v err=%v calls=%d", got, truncated, err, calls)
			}
		})
	}
}

func TestErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	p := New(fakeAPI{err: boom})
	if _, err := p.List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Fatalf("List err = %v", err)
	}
	zone := zoneResource("p", Zone{Name: "z"})
	if _, _, err := p.ListRelated(context.Background(), zone, model.FacetRecords); !errors.Is(err, boom) {
		t.Fatalf("ListRelated err = %v", err)
	}
}
