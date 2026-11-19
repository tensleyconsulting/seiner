package networkconnectivity

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	endpoints []RegionalEndpoint
	policies  []ConnectionPolicy
	byKey     map[string]ConnectionPolicy
	called    bool
	err       error
}

func (f *fakeAPI) ListRegionalEndpoints(context.Context, string) ([]RegionalEndpoint, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	return f.endpoints, nil
}

func (f *fakeAPI) ListConnectionPolicies(context.Context, string) ([]ConnectionPolicy, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	return f.policies, nil
}

func (f *fakeAPI) GetConnectionPolicy(_ context.Context, project, location, name string) (ConnectionPolicy, error) {
	f.called = true
	if f.err != nil {
		return ConnectionPolicy{}, f.err
	}
	return f.byKey[project+"/"+location+"/"+name], nil
}

func TestListScopeHandling(t *testing.T) {
	t.Parallel()
	scopes := []struct {
		scope string
		want  bool
	}{
		{"projects/p", true},
		{"folders/1", false},
		{"organizations/1", false},
		{"", false},
	}
	for _, tc := range scopes {
		t.Run("endpoints/"+tc.scope, func(t *testing.T) {
			t.Parallel()
			api := &fakeAPI{endpoints: []RegionalEndpoint{{Name: "ep1", Location: "us-east4"}}}
			p := NewRegionalEndpoints(api)
			got, err := p.List(context.Background(), tc.scope)
			if err != nil {
				t.Fatalf("List(%q) err=%v", tc.scope, err)
			}
			if api.called != tc.want {
				t.Errorf("called=%v want %v", api.called, tc.want)
			}
			if tc.want && len(got) != 1 {
				t.Errorf("got %d resources, want 1", len(got))
			}
			if !tc.want && got != nil {
				t.Errorf("got %v, want nil", got)
			}
		})
		t.Run("policies/"+tc.scope, func(t *testing.T) {
			t.Parallel()
			api := &fakeAPI{policies: []ConnectionPolicy{{Name: "pol1", Location: "us-east4"}}}
			p := NewConnectionPolicies(api)
			got, err := p.List(context.Background(), tc.scope)
			if err != nil {
				t.Fatalf("List(%q) err=%v", tc.scope, err)
			}
			if api.called != tc.want {
				t.Errorf("called=%v want %v", api.called, tc.want)
			}
			if tc.want && len(got) != 1 {
				t.Errorf("got %d resources, want 1", len(got))
			}
			if !tc.want && got != nil {
				t.Errorf("got %v, want nil", got)
			}
		})
	}
}

func TestListPermissions(t *testing.T) {
	t.Parallel()
	ep := NewRegionalEndpoints(nil)
	cp := NewConnectionPolicies(nil)
	if got := ep.ListPermissions("projects/p"); !slices.Equal(got, []string{"networkconnectivity.regionalEndpoints.list"}) {
		t.Errorf("RegionalEndpoints.ListPermissions(projects/p)=%v", got)
	}
	if got := cp.ListPermissions("projects/p"); !slices.Equal(got, []string{"networkconnectivity.serviceConnectionPolicies.list"}) {
		t.Errorf("ConnectionPolicies.ListPermissions(projects/p)=%v", got)
	}
	for _, scope := range []string{"folders/1", "organizations/1", ""} {
		if got := ep.ListPermissions(scope); got != nil {
			t.Errorf("RegionalEndpoints.ListPermissions(%q)=%v, want nil", scope, got)
		}
		if got := cp.ListPermissions(scope); got != nil {
			t.Errorf("ConnectionPolicies.ListPermissions(%q)=%v, want nil", scope, got)
		}
	}
}

func TestRegionalEndpointsMapping(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 11, 5, 16, 43, 42, 0, time.UTC)
	updated := time.Date(2026, 11, 5, 16, 44, 28, 0, time.UTC)
	api := &fakeAPI{endpoints: []RegionalEndpoint{{
		Name:            "net-host-svc-rep-telemetry-use4",
		Location:        "us-east4",
		TargetGoogleAPI: "telemetry.us-east4.rep.googleapis.com",
		AccessType:      "REGIONAL",
		IPAddress:       "192.168.16.27",
		AddressName:     "custom-addr",
		Network:         "net-host-svc",
		Subnetwork:      "net-host-svc-gke-use4",
		ForwardingRule:  "rep-autogen-fr-net-host-svc-rep-telemetry-use4",
		Description:     "Regional endpoint",
		Created:         created,
		Updated:         updated,
		Labels:          map[string]string{"env": "prod"},
	}}}
	got, err := NewRegionalEndpoints(api).List(context.Background(), "projects/net-host")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	r := got[0]
	if r.ID != "projects/net-host/locations/us-east4/regionalEndpoints/net-host-svc-rep-telemetry-use4" {
		t.Errorf("ID=%q", r.ID)
	}
	if r.Service != model.ServiceRegionalEndpoint || r.ScopeID != "projects/net-host" || r.Location != "us-east4" {
		t.Errorf("identity = %s %s %s", r.Service, r.ScopeID, r.Location)
	}
	if r.Status != model.StatusUnknown || r.Badge != "REGIONAL" || !r.Created.Equal(created) || r.Labels["env"] != "prod" {
		t.Errorf("status/badge/created/labels = %v %q %v %v", r.Status, r.Badge, r.Created, r.Labels)
	}
	wantFields := []string{"targetGoogleApi", "accessType", "ipAddress", "address", "network", "subnetwork", "forwardingRule", "updated"}
	if len(r.Fields) != len(wantFields) {
		t.Fatalf("fields = %+v, want %d", r.Fields, len(wantFields))
	}
	for i, k := range wantFields {
		if r.Fields[i].Key != k {
			t.Errorf("field[%d]=%q want %q", i, r.Fields[i].Key, k)
		}
	}
}

func TestConnectionPolicyMappingAndStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		pol        ConnectionPolicy
		wantStatus model.ResourceStatus
		wantFacet  bool
		wantConn   string
		wantLists  int
	}{
		{
			name: "active within limit",
			pol: ConnectionPolicy{
				Name:             "sql-policy",
				Location:         "us-east4",
				ServiceClass:     "google-cloud-sql",
				Infrastructure:   "PSC",
				Network:          "svc",
				Subnetworks:      []string{"svc-gke"},
				ConnectionLimit:  8,
				ProducerLocation: "CUSTOM_RESOURCE_HIERARCHY_LEVELS",
				AllowedProducers: []string{"folders/123", "folders/456"},
				Connections:      []Connection{{ID: "1", State: "ACTIVE"}},
			},
			wantStatus: model.StatusHealthy,
			wantFacet:  true,
			wantConn:   "1 / 8",
			wantLists:  1,
		},
		{
			name: "zero connections omits facet",
			pol: ConnectionPolicy{
				Name:            "empty-policy",
				Location:        "us-east4",
				ServiceClass:    "gcp-memorystore",
				ConnectionLimit: 4,
			},
			wantStatus: model.StatusUnknown,
			wantFacet:  false,
			wantConn:   "0 / 4",
		},
		{
			name: "at connection limit warns",
			pol: ConnectionPolicy{
				Name:            "full-policy",
				Location:        "us-east4",
				ConnectionLimit: 1,
				Connections:     []Connection{{ID: "1", State: "ACTIVE"}},
			},
			wantStatus: model.StatusWarning,
			wantFacet:  true,
			wantConn:   "1 / 1",
		},
		{
			name: "creating connection warns",
			pol: ConnectionPolicy{
				Name:        "creating-policy",
				Location:    "us-east4",
				Connections: []Connection{{ID: "1", State: "ACTIVE"}, {ID: "2", State: "CREATING"}},
			},
			wantStatus: model.StatusWarning,
			wantFacet:  true,
			wantConn:   "2",
		},
		{
			name: "failed connection errors",
			pol: ConnectionPolicy{
				Name:        "failed-policy",
				Location:    "us-east4",
				Connections: []Connection{{ID: "1", State: "CREATING"}, {ID: "2", State: "FAILED", Error: "exhausted"}},
			},
			wantStatus: model.StatusError,
			wantFacet:  true,
			wantConn:   "2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := &fakeAPI{policies: []ConnectionPolicy{tc.pol}}
			got, err := NewConnectionPolicies(api).List(context.Background(), "projects/p")
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			r := got[0]
			if r.Status != tc.wantStatus {
				t.Errorf("Status=%q want %q", r.Status, tc.wantStatus)
			}
			if (len(r.Facets) > 0) != tc.wantFacet {
				t.Errorf("Facets=%+v wantFacet=%v", r.Facets, tc.wantFacet)
			}
			if tc.wantFacet && (!r.Facets[0].Inline || r.Facets[0].ID != model.FacetConnections) {
				t.Errorf("Facet=%+v", r.Facets[0])
			}
			if r.Metadata["connections"] != tc.wantConn {
				t.Errorf("connections=%q want %q", r.Metadata["connections"], tc.wantConn)
			}
			if len(r.Lists) != tc.wantLists {
				t.Errorf("lists=%d want %d", len(r.Lists), tc.wantLists)
			}
		})
	}
}

func TestListRelatedConnections(t *testing.T) {
	t.Parallel()
	polID := "projects/p/locations/us-east4/serviceConnectionPolicies/sql-pol"
	api := &fakeAPI{
		byKey: map[string]ConnectionPolicy{
			"p/us-east4/sql-pol": {
				Name:     "sql-pol",
				Location: "us-east4",
				Connections: []Connection{
					{
						ID:                 "223045",
						State:              "ACTIVE",
						ConsumerProject:    "svc-proj",
						ForwardingRule:     "sca-auto-fr-1",
						ConsumerAddress:    "sca-auto-addr-1",
						Subnetwork:         "svc-gke",
						ProducerInstanceID: "inst-1",
						ServiceClass:       "google-cloud-sql",
						IPVersion:          "IPV4",
					},
					{
						ID:             "223046",
						State:          "FAILED",
						ForwardingRule: "sca-auto-fr-2",
						ErrorType:      "CONNECTION_ERROR_INTERNAL",
						Error:          "IP_EXHAUSTED: no IPs left",
					},
					{
						ID:    "223047",
						State: "PRODUCER_REPAIRING",
					},
					{
						State: "",
					},
				},
			},
		},
	}
	p := NewConnectionPolicies(api)
	parent := model.Resource{
		ID:       polID,
		Name:     "sql-pol",
		Service:  model.ServiceConnectionPolicy,
		ScopeID:  "projects/p",
		Location: "us-east4",
	}

	got, truncated, err := p.ListRelated(context.Background(), parent, model.FacetConnections)
	if err != nil || truncated {
		t.Fatalf("ListRelated err=%v truncated=%v", err, truncated)
	}
	if len(got) != 4 {
		t.Fatalf("got %d connections, want 4", len(got))
	}
	wantNames := []string{"svc-proj/sca-auto-fr-1", "sca-auto-fr-2", "223047", "connection-4"}
	wantStatuses := []model.ResourceStatus{model.StatusHealthy, model.StatusError, model.StatusWarning, model.StatusUnknown}
	for i, c := range got {
		if c.ParentID != polID || c.FacetID != model.FacetConnections || c.Service != model.ServicePSCConnection {
			t.Errorf("[%d] parent/facet/svc = %q %q %q", i, c.ParentID, c.FacetID, c.Service)
		}
		if c.Name != wantNames[i] {
			t.Errorf("[%d] Name=%q want %q", i, c.Name, wantNames[i])
		}
		if c.Status != wantStatuses[i] {
			t.Errorf("[%d] Status=%q want %q", i, c.Status, wantStatuses[i])
		}
	}

	// Non-matching service or facet returns nil, false, nil without calling API.
	api.called = false
	for _, tc := range []struct {
		svc   model.ServiceType
		facet string
	}{
		{model.ServiceRegionalEndpoint, model.FacetConnections},
		{model.ServiceConnectionPolicy, model.FacetPermissions},
	} {
		items, trunc, err := p.ListRelated(context.Background(), model.Resource{ID: polID, Service: tc.svc}, tc.facet)
		if err != nil || trunc || items != nil || api.called {
			t.Errorf("(%s,%s) = %v,%v,%v called=%v", tc.svc, tc.facet, items, trunc, err, api.called)
		}
	}
}

func TestErrorPropagation(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	api := &fakeAPI{err: sentinel}

	if _, err := NewRegionalEndpoints(api).List(context.Background(), "projects/p"); !errors.Is(err, sentinel) {
		t.Errorf("RegionalEndpoints.List err = %v, want %v", err, sentinel)
	}
	cp := NewConnectionPolicies(api)
	if _, err := cp.List(context.Background(), "projects/p"); !errors.Is(err, sentinel) {
		t.Errorf("ConnectionPolicies.List err = %v, want %v", err, sentinel)
	}
	parent := model.Resource{
		ID:      "projects/p/locations/us-east4/serviceConnectionPolicies/pol",
		Service: model.ServiceConnectionPolicy,
		ScopeID: "projects/p",
	}
	if _, _, err := cp.ListRelated(context.Background(), parent, model.FacetConnections); !errors.Is(err, sentinel) {
		t.Errorf("ConnectionPolicies.ListRelated err = %v, want %v", err, sentinel)
	}
}
