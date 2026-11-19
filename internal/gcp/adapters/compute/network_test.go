package compute

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func (f fakeAPI) ListAddresses(context.Context, string) ([]Address, error) {
	return f.addresses, f.err
}

func (f fakeAPI) ListForwardingRules(context.Context, string) ([]ForwardingRule, error) {
	return f.rules, f.err
}

func (f fakeAPI) ListServiceAttachments(context.Context, string) ([]ServiceAttachment, error) {
	return f.attached, f.err
}

func networkSample() fakeAPI {
	return fakeAPI{
		addresses: []Address{
			{Name: "web-ip", Address: "203.0.113.1", AddressType: "EXTERNAL", Status: "IN_USE", Users: []string{"forwardingRules/web-https"}},
			{Name: "db-ip", Region: "us-central1", Address: "10.0.0.5", AddressType: "INTERNAL", Status: "RESERVED", Subnetwork: "default"},
		},
		rules: []ForwardingRule{
			{Name: "web-https", IPAddress: "203.0.113.1", IPProtocol: "TCP", Ports: "443-443", Scheme: "EXTERNAL_MANAGED", Target: "targetHttpsProxies/web"},
			{Name: "api-ilb", Region: "us-central1", IPAddress: "10.0.0.9", Scheme: "INTERNAL", BackendService: "api"},
			{Name: "to-api", Region: "us-central1", IPAddress: "10.1.0.4", Target: "serviceAttachments/api", PSCConnectionStatus: "ACCEPTED", PSCConnectionID: "731"},
			{Name: "googleapis", IPAddress: "10.255.0.2", Target: "all-apis", Network: "default"},
		},
		attached: []ServiceAttachment{
			{Name: "api", Region: "us-central1", TargetService: "forwardingRules/api-ilb", ConnectionPreference: "ACCEPT_MANUAL"},
		},
	}
}

func networkProviders(api API) []model.ResourceProvider {
	return []model.ResourceProvider{NewAddresses(api), NewForwardingRules(api), NewPSCEndpoints(api), NewServiceAttachments(api)}
}

func TestNetworkProvidersListScopes(t *testing.T) {
	t.Parallel()
	want := map[model.ServiceType]int{
		model.ServiceAddress:           2,
		model.ServiceForwardingRule:    2,
		model.ServicePSCEndpoint:       2,
		model.ServiceServiceAttachment: 1,
	}
	for _, p := range networkProviders(networkSample()) {
		t.Run(string(p.Service()), func(t *testing.T) {
			t.Parallel()
			got, err := p.List(context.Background(), "projects/p")
			if err != nil || len(got) != want[p.Service()] {
				t.Fatalf("List = %d err=%v, want %d", len(got), err, want[p.Service()])
			}
			for _, r := range got {
				if r.Service != p.Service() || r.ScopeID != "projects/p" {
					t.Errorf("%+v", r)
				}
				if err := r.Validate(); err != nil {
					t.Errorf("%s: %v", r.ID, err)
				}
			}
		})
	}
}

func TestNetworkProvidersSkipNonProjectScopes(t *testing.T) {
	t.Parallel()
	// With no API, reaching for it is an error, so nil, nil shows it was skipped.
	for _, p := range networkProviders(nil) {
		for _, scope := range []string{"folders/1", "organizations/1", ""} {
			if got, err := p.List(context.Background(), scope); got != nil || err != nil {
				t.Errorf("%s List(%q) = %v, %v", p.Service(), scope, got, err)
			}
		}
	}
}

func TestNetworkProvidersIDs(t *testing.T) {
	t.Parallel()
	api := networkSample()
	cases := []struct {
		p        model.ResourceProvider
		want     []string
		location []string
	}{
		{NewAddresses(api), []string{"projects/p/global/addresses/web-ip", "projects/p/regions/us-central1/addresses/db-ip"}, []string{"global", "us-central1"}},
		{NewForwardingRules(api), []string{"projects/p/global/forwardingRules/web-https", "projects/p/regions/us-central1/forwardingRules/api-ilb"}, []string{"global", "us-central1"}},
		{NewPSCEndpoints(api), []string{"projects/p/regions/us-central1/forwardingRules/to-api", "projects/p/global/forwardingRules/googleapis"}, []string{"us-central1", "global"}},
		{NewServiceAttachments(api), []string{"projects/p/regions/us-central1/serviceAttachments/api"}, []string{"us-central1"}},
	}
	for _, tc := range cases {
		got, err := tc.p.List(context.Background(), "projects/p")
		if err != nil {
			t.Fatal(err)
		}
		var ids, locs []string
		for _, r := range got {
			ids = append(ids, r.ID)
			locs = append(locs, r.Location)
		}
		if !slices.Equal(ids, tc.want) || !slices.Equal(locs, tc.location) {
			t.Errorf("%s ids=%v locs=%v", tc.p.Service(), ids, locs)
		}
	}
}

func TestNetworkProvidersPropagateErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	for _, p := range networkProviders(fakeAPI{err: boom}) {
		if _, err := p.List(context.Background(), "projects/p"); !errors.Is(err, boom) {
			t.Errorf("%s List err=%v", p.Service(), err)
		}
	}
}

func TestNetworkDetailFields(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	cases := []struct {
		name     string
		r        model.Resource
		wantKeys []string
		want     map[string]string
		lists    []string
	}{
		{
			name: "address",
			r: addressResource("p", Address{
				Name: "range", Region: "us-east1", Address: "10.1.0.0", PrefixLength: 16, AddressType: "INTERNAL",
				Purpose: "VPC_PEERING", Network: "vpc", Status: "RESERVED", Users: []string{"forwardingRules/a", "forwardingRules/b"},
				Created: created, Labels: map[string]string{"env": "prod"},
			}),
			wantKeys: []string{"address", "addressType", "purpose", "network", "usedBy"},
			want:     map[string]string{"address": "10.1.0.0/16", "usedBy": "forwardingRules/a, forwardingRules/b"},
			lists:    []string{"users"},
		},
		{
			name: "forwarding rule",
			r: forwardingRuleResource("p", ForwardingRule{
				Name: "psc", Region: "us-east1", IPAddress: "10.2.0.3", Target: "serviceAttachments/api",
				Network: "vpc", Subnetwork: "s", GlobalAccess: true, PSCConnectionStatus: "ACCEPTED",
				Created: created,
			}),
			wantKeys: []string{"ipAddress", "target", "network", "subnetwork", "globalAccess"},
			want:     map[string]string{"globalAccess": "on"},
		},
		{
			name: "service attachment",
			r: serviceAttachmentResource("p", ServiceAttachment{
				Name: "api", Region: "us-east1", TargetService: "forwardingRules/api-ilb", ConnectionPreference: "ACCEPT_MANUAL",
				NATSubnets: []string{"psc-nat-a", "psc-nat-b"}, AcceptList: []string{"consumer (limit 10)"},
				Endpoints: []ConnectedEndpoint{{Endpoint: "consumer/ep", ConsumerNetwork: "consumer/vpc", Status: "ACCEPTED"}},
				Created:   created,
			}),
			wantKeys: []string{"targetService", "connectionPreference", "endpoints", "natSubnets", "proxyProtocol"},
			want:     map[string]string{"endpoints": "1", "natSubnets": "psc-nat-a, psc-nat-b", "proxyProtocol": "off"},
			lists:    []string{"endpoints", "accept"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			keys, vals := fieldMap(tc.r)
			if !slices.Equal(keys, tc.wantKeys) {
				t.Errorf("keys %v, want %v", keys, tc.wantKeys)
			}
			for k, v := range tc.want {
				if vals[k] != v {
					t.Errorf("%s = %q, want %q", k, vals[k], v)
				}
			}
			var lists []string
			for _, l := range tc.r.Lists {
				lists = append(lists, l.ID)
			}
			if !slices.Equal(lists, tc.lists) {
				t.Errorf("lists %v, want %v", lists, tc.lists)
			}
			if !tc.r.Created.Equal(created) {
				t.Errorf("created %v", tc.r.Created)
			}
		})
	}
	if l := cases[0].r.Labels["env"]; l != "prod" {
		t.Errorf("address labels %v", cases[0].r.Labels)
	}
	if got := cases[2].r.List("endpoints"); !slices.Equal(got, []string{"consumer/ep (consumer/vpc) ACCEPTED"}) {
		t.Errorf("endpoints %v", got)
	}
}

func TestNetworkStatuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		r    model.Resource
		want model.ResourceStatus
	}{
		{"address in use", addressResource("p", Address{Name: "a", Status: "IN_USE"}), model.StatusHealthy},
		{"address reserved", addressResource("p", Address{Name: "a", Status: "RESERVED"}), model.StatusHealthy},
		{"address reserving", addressResource("p", Address{Name: "a", Status: "RESERVING"}), model.StatusWarning},
		{"address unknown", addressResource("p", Address{Name: "a", Status: "WHAT"}), model.StatusUnknown},
		{"lb rule", forwardingRuleResource("p", ForwardingRule{Name: "f"}), model.StatusUnknown},
		{"psc accepted", forwardingRuleResource("p", ForwardingRule{Name: "f", PSCConnectionStatus: "ACCEPTED"}), model.StatusHealthy},
		{"psc pending", forwardingRuleResource("p", ForwardingRule{Name: "f", PSCConnectionStatus: "PENDING"}), model.StatusWarning},
		{"psc needs attention", forwardingRuleResource("p", ForwardingRule{Name: "f", PSCConnectionStatus: "NEEDS_ATTENTION"}), model.StatusWarning},
		{"psc rejected", forwardingRuleResource("p", ForwardingRule{Name: "f", PSCConnectionStatus: "REJECTED"}), model.StatusError},
		{"psc closed", forwardingRuleResource("p", ForwardingRule{Name: "f", PSCConnectionStatus: "CLOSED"}), model.StatusError},
		{"psc unknown", forwardingRuleResource("p", ForwardingRule{Name: "f", PSCConnectionStatus: "STATUS_UNSPECIFIED"}), model.StatusUnknown},
		{"attachment idle", serviceAttachmentResource("p", ServiceAttachment{Name: "s", Region: "r"}), ""},
		{"attachment pending endpoint", serviceAttachmentResource("p", ServiceAttachment{Name: "s", Region: "r", Endpoints: []ConnectedEndpoint{{Endpoint: "c/e", Status: "ACCEPTED"}, {Endpoint: "c/f", Status: "PENDING"}}}), model.StatusWarning},
	}
	for _, tc := range cases {
		if tc.r.Status != tc.want {
			t.Errorf("%s: status %s, want %s", tc.name, tc.r.Status, tc.want)
		}
	}
}

func TestLinkHelpers(t *testing.T) {
	t.Parallel()
	const base = "https://www.googleapis.com/compute/v1/projects/consumer/"
	cases := []struct {
		fn   func(string) string
		in   string
		want string
	}{
		{kindName, base + "global/targetHttpsProxies/web", "targetHttpsProxies/web"},
		{kindName, "all-apis", "all-apis"},
		{projectAndName, base + "regions/us-east1/forwardingRules/ep", "consumer/ep"},
		{projectAndName, base + "global/networks/vpc", "consumer/vpc"},
		{projectAndName, "vpc", "vpc"},
	}
	for _, tc := range cases {
		if got := tc.fn(tc.in); got != tc.want {
			t.Errorf("(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPSCEndpointsSplitFromForwardingRules(t *testing.T) {
	t.Parallel()
	api := networkSample()
	ctx := context.Background()
	endpoint := "projects/p/regions/us-central1/forwardingRules/to-api"
	frontend := "projects/p/global/forwardingRules/web-https"
	byID := func(rs []model.Resource) map[string]model.Resource {
		m := map[string]model.Resource{}
		for _, r := range rs {
			m[r.ID] = r
		}
		return m
	}
	rules, err := NewForwardingRules(api).List(ctx, "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := byID(rules)[endpoint]; ok {
		t.Errorf("ForwardingRules lists the PSC endpoint %s", endpoint)
	}
	if _, ok := byID(rules)[frontend]; !ok {
		t.Errorf("ForwardingRules does not list %s", frontend)
	}
	eps, err := NewPSCEndpoints(api).List(ctx, "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	got := byID(eps)
	if _, ok := got[frontend]; ok {
		t.Errorf("PSCEndpoints lists the frontend %s", frontend)
	}
	r, ok := got[endpoint]
	if !ok || r.State != "ACCEPTED" || r.Status != model.StatusHealthy || r.Metadata["targetType"] != "Published service" ||
		r.Metadata["target"] != "serviceAttachments/api" || r.Metadata["pscConnectionId"] != "731" {
		t.Errorf("endpoint %+v", r)
	}
	if g := got["projects/p/global/forwardingRules/googleapis"]; g.Metadata["targetType"] != "Google APIs" {
		t.Errorf("google apis endpoint %+v", g)
	}
}
