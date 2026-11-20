package compute

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	networks  []Network
	subnets   []Subnet
	firewalls []Firewall
	instances []Instance
	addresses []Address
	rules     []ForwardingRule
	attached  []ServiceAttachment
	urlMaps   []URLMap
	backends  []BackendService
	proxies   []TargetProxy
	checks    []HealthCheck
	policies  []SecurityPolicy
	ssl       []SSLPolicy
	disks     []Disk
	templates []InstanceTemplate
	groups    []InstanceGroup
	managed   map[string][]ManagedInstance // by project/location/group
	routes    []Route
	routers   []Router
	iam       map[string][]Binding // keyed by kind/loc/name (loc="" for global)
	err       error                // returned by every method added after instances
	backErr   error                // returned by ListBackendServices alone
	proxyErr  error                // returned by ListTargetProxies alone
}

func (f fakeAPI) ListNetworks(context.Context, string) ([]Network, error) {
	return f.networks, nil
}
func (f fakeAPI) ListSubnets(context.Context, string) ([]Subnet, error) {
	return f.subnets, nil
}
func (f fakeAPI) ListFirewalls(context.Context, string) ([]Firewall, error) {
	return f.firewalls, nil
}
func (f fakeAPI) ListInstances(context.Context, string) ([]Instance, error) {
	return f.instances, nil
}
func (f fakeAPI) GetIAMPolicy(_ context.Context, _, kind, zone, region, name string) ([]Binding, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.iam[kind+"/"+zone+region+"/"+name], nil
}
func sample() fakeAPI {
	return fakeAPI{
		networks: []Network{{Name: "default", AutoCreate: true, RoutingMode: "REGIONAL"}},
		subnets: []Subnet{
			{Name: "default", Region: "us-central1", Network: "default", CIDR: "10.128.0.0/20"},
			{Name: "other", Region: "us-east1", Network: "custom", CIDR: "10.0.0.0/24"},
		},
		firewalls: []Firewall{
			{Name: "default-allow-internal", Network: "default", Direction: "INGRESS", Allowed: "tcp:1-65535"},
			{Name: "custom-allow", Network: "custom", Direction: "INGRESS"},
		},
		instances: []Instance{
			{Name: "web-1", Zone: "us-central1-a", Status: "RUNNING", MachineType: "e2-micro", Network: "default", Subnetwork: "default", InternalIP: "10.128.0.2"},
		},
	}
}

func TestNetworksListAndRelated(t *testing.T) {
	t.Parallel()
	api := sample()
	p := NewNetworks(api)
	got, err := p.List(context.Background(), "projects/demo-project")
	if err != nil || len(got) != 1 {
		t.Fatalf("list %v err=%v", got, err)
	}
	if got[0].Service != model.ServiceVPC || len(got[0].Facets) != 3 {
		t.Fatalf("%+v", got[0])
	}
	subs, _, err := p.ListRelated(context.Background(), got[0], model.FacetSubnets)
	if err != nil || len(subs) != 1 || subs[0].Name != "default" {
		t.Fatalf("subnets %v err=%v", subs, err)
	}
	if subs[0].ParentID != got[0].ID {
		t.Fatalf("parent %s", subs[0].ParentID)
	}
	fws, _, err := p.ListRelated(context.Background(), got[0], model.FacetFirewalls)
	if err != nil || len(fws) != 1 || fws[0].Name != "default-allow-internal" {
		t.Fatalf("firewalls %v err=%v", fws, err)
	}
}

func TestInstances(t *testing.T) {
	t.Parallel()
	api := sample()
	vms, err := NewInstances(api).List(context.Background(), "projects/demo-project")
	if err != nil || len(vms) != 1 {
		t.Fatalf("vms %v err=%v", vms, err)
	}
	if vms[0].Status != model.StatusHealthy || vms[0].Metadata["machineType"] != "e2-micro" {
		t.Fatalf("%+v", vms[0])
	}
	if vms[0].Location != "us-central1-a" {
		t.Fatalf("zone %s", vms[0].Location)
	}
}

func TestParseIDs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id, kind           string
		project, loc, name string
		ok                 bool
	}{
		{"projects/p/global/networks/default", "networks", "p", "", "default", true},
		{"projects/p/regions/us-central1/subnetworks/default", "subnetworks", "p", "us-central1", "default", true},
		{"projects/p/global/firewalls/allow-ssh", "firewalls", "p", "", "allow-ssh", true},
		{"projects/p/zones/us-central1-a/instances/web-1", "instances", "p", "us-central1-a", "web-1", true},
		{"projects/p/zones/us-central1-a/instances/web-1", "networks", "", "", "", false},
		{"organizations/1/global/networks/default", "networks", "", "", "", false},
		{"default", "networks", "", "", "", false},
	}
	for _, tc := range cases {
		p, l, n, ok := parseID(tc.id, tc.kind)
		if p != tc.project || l != tc.loc || n != tc.name || ok != tc.ok {
			t.Errorf("parseID(%q, %q) = %q %q %q %v", tc.id, tc.kind, p, l, n, ok)
		}
	}
}

func TestOrgScopeEmpty(t *testing.T) {
	t.Parallel()
	got, err := NewNetworks(sample()).List(context.Background(), "organizations/9")
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestInstanceRicherFields(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	api := fakeAPI{instances: []Instance{{
		Name: "db-1", Zone: "us-east1-b", Status: "TERMINATED", MachineType: "n2-standard-4",
		Network: "default", Subnetwork: "default", InternalIP: "10.0.0.9", ExternalIP: "203.0.113.20",
		Created: created, Labels: map[string]string{"env": "prod"},
	}}}
	vms, err := NewInstances(api).List(context.Background(), "projects/demo-project")
	if err != nil || len(vms) != 1 {
		t.Fatalf("vms %v err=%v", vms, err)
	}
	r := vms[0]
	if r.State != "TERMINATED" || r.Status != model.StatusWarning {
		t.Errorf("state=%q status=%s", r.State, r.Status)
	}
	if _, ok := r.Metadata["status"]; ok {
		t.Error("status belongs in State, not Metadata")
	}
	if !r.Created.Equal(created) || r.Labels["env"] != "prod" {
		t.Errorf("created=%v labels=%v", r.Created, r.Labels)
	}
	var keys []string
	for _, f := range r.Fields {
		keys = append(keys, f.Key)
	}
	want := []string{"machineType", "internalIP", "externalIP", "network", "subnetwork"}
	if !slices.Equal(keys, want) {
		t.Errorf("field order %v, want %v", keys, want)
	}
}

func TestParseCreated(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want time.Time
	}{
		{"2024-05-06T07:08:09.123-07:00", time.Date(2024, 5, 6, 14, 8, 9, 123000000, time.UTC)},
		{"", time.Time{}},
		{"yesterday", time.Time{}},
	}
	for _, tc := range cases {
		if got := parseCreated(tc.in); !got.Equal(tc.want) {
			t.Errorf("parseCreated(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func fieldMap(r model.Resource) (keys []string, vals map[string]string) {
	vals = map[string]string{}
	for _, f := range r.Fields {
		keys = append(keys, f.Key)
		vals[f.Key] = f.Value
	}
	return keys, vals
}

func TestDetailFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		r        model.Resource
		wantKeys []string
		want     map[string]string
	}{
		{
			name: "network",
			r: networkResource("p", Network{
				Name: "vpc", ID: 42, AutoCreate: false, RoutingMode: "GLOBAL", MTU: 1500, Subnets: 3,
				Peerings: []Peering{{Name: "peer-a", State: "ACTIVE"}, {Name: "peer-b"}},
			}),
			wantKeys: []string{"subnetMode", "subnets", "routingMode", "mtu", "peerings", "networkId"},
			want:     map[string]string{"subnetMode": "custom", "subnets": "3", "mtu": "1500", "peerings": "peer-a (ACTIVE), peer-b", "networkId": "42"},
		},
		{
			name: "subnet",
			r: subnetResource("p", Subnet{
				Name: "s", Region: "us-east1", Network: "vpc", CIDR: "10.0.0.0/24", Gateway: "10.0.0.1",
				SecondaryRanges: []SecondaryRange{{Name: "pods", CIDR: "10.4.0.0/14"}},
				Purpose:         "PRIVATE", StackType: "IPV4_ONLY", PrivateGoogleAccess: true, State: "READY",
			}, ""),
			wantKeys: []string{"cidr", "gateway", "network", "secondaryRanges", "purpose", "stackType", "privateGoogleAccess", "flowLogs"},
			want:     map[string]string{"secondaryRanges": "pods=10.4.0.0/14", "privateGoogleAccess": "on", "flowLogs": "off"},
		},
		{
			name: "firewall",
			r: firewallResource("p", Firewall{
				Name: "fw", Network: "vpc", Direction: "INGRESS", Priority: 1000, Allowed: "tcp:22",
				SourceTags: []string{"bastion"}, TargetServiceAccounts: []string{"sa@p.iam.gserviceaccount.com"}, Logging: true,
			}, ""),
			wantKeys: []string{"direction", "priority", "network", "allowed", "sourceTags", "targetServiceAccounts", "logging"},
			want:     map[string]string{"priority": "1000", "logging": "on", "sourceTags": "bastion"},
		},
		{
			name: "instance",
			r: instanceResource("p", Instance{
				Name: "vm", Zone: "us-east1-b", Status: "RUNNING", MachineType: "e2-micro", CPUPlatform: "AMD Rome",
				Interfaces: 2, Disks: 2, BootDiskGB: 20, ServiceAccount: "sa@p.iam.gserviceaccount.com",
				Tags: []string{"web", "ssh"}, ProvisioningModel: "SPOT", DeletionProtection: true,
				LastStarted: time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC),
			}),
			wantKeys: []string{"machineType", "cpuPlatform", "interfaces", "disks", "serviceAccount", "networkTags", "provisioning", "deletionProtection", "lastStarted"},
			want:     map[string]string{"disks": "2 (boot 20 GB)", "networkTags": "web,ssh", "lastStarted": "2026-01-02 03:04 UTC"},
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
		})
	}
}

func TestSubnetStateNotReadyWarns(t *testing.T) {
	t.Parallel()
	r := subnetResource("p", Subnet{Name: "s", Region: "r", State: "DRAINING"}, "")
	if r.Status != model.StatusWarning || r.State != "DRAINING" {
		t.Errorf("status=%s state=%q", r.Status, r.State)
	}
}
