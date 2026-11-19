package compute

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func (f fakeAPI) ListDisks(context.Context, string) ([]Disk, error) { return f.disks, f.err }

func (f fakeAPI) ListInstanceTemplates(context.Context, string) ([]InstanceTemplate, error) {
	return f.templates, f.err
}

func (f fakeAPI) ListInstanceGroups(context.Context, string) ([]InstanceGroup, error) {
	return f.groups, f.err
}

func (f fakeAPI) ListManagedInstances(_ context.Context, project, zone, region, group string, limit int) ([]ManagedInstance, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	ms := f.managed[project+"/"+zone+region+"/"+group]
	if len(ms) > limit {
		return ms[:limit], true, nil
	}
	return ms, false, nil
}

func (f fakeAPI) ListRoutes(context.Context, string) ([]Route, error) { return f.routes, f.err }

func (f fakeAPI) ListRouters(context.Context, string) ([]Router, error) { return f.routers, f.err }

func (f fakeAPI) GetRouter(_ context.Context, _, region, name string) (Router, error) {
	if f.err != nil {
		return Router{}, f.err
	}
	for _, r := range f.routers {
		if r.Region == region && r.Name == name {
			return r, nil
		}
	}
	return Router{}, model.ErrNotFound
}

func infraSample() fakeAPI {
	return fakeAPI{
		disks: []Disk{
			{Name: "web-1", Zone: "us-central1-a", Type: "pd-balanced", SizeGB: 20, Status: "READY", Users: []string{"web-1"}},
			{Name: "shared", Region: "us-central1", Type: "pd-ssd", SizeGB: 100, Status: "READY", ReplicaZones: []string{"us-central1-a", "us-central1-b"}},
		},
		templates: []InstanceTemplate{
			{Name: "web-v2", MachineType: "e2-medium", BootImage: "debian-12", BootDiskGB: 20, BootDiskType: "pd-balanced"},
			{Name: "api-v1", Region: "us-central1", MachineType: "n2-standard-2"},
		},
		groups: []InstanceGroup{
			{Name: "web-mig", Zone: "us-central1-a", TargetSize: 3, Stable: true},
			{Name: "api-mig", Region: "us-central1", TargetSize: 2},
		},
		networks: []Network{{Name: "default"}},
		routes: []Route{
			{Name: "default-route-1", Network: "default", DestRange: "0.0.0.0/0", Priority: 1000, NextHop: "default-internet-gateway"},
			{Name: "other-net", Network: "custom", DestRange: "10.9.0.0/16"},
		},
		routers: []Router{
			{Name: "nat-router", Region: "us-central1", Network: "default", ASN: 64512,
				NATs: []NAT{{Name: "egress", IPAllocation: "AUTO_ONLY", SourceRanges: "ALL_SUBNETWORKS_ALL_IP_RANGES"}}},
		},
	}
}

func TestInfraProvidersList(t *testing.T) {
	t.Parallel()
	api := infraSample()
	cases := []struct {
		p    model.ResourceProvider
		ids  []string
		locs []string
	}{
		{NewDisks(api), []string{"projects/p/zones/us-central1-a/disks/web-1", "projects/p/regions/us-central1/disks/shared"}, []string{"us-central1-a", "us-central1"}},
		{NewInstanceTemplates(api), []string{"projects/p/global/instanceTemplates/web-v2", "projects/p/regions/us-central1/instanceTemplates/api-v1"}, []string{"global", "us-central1"}},
		{NewInstanceGroups(api), []string{"projects/p/zones/us-central1-a/instanceGroupManagers/web-mig", "projects/p/regions/us-central1/instanceGroupManagers/api-mig"}, []string{"us-central1-a", "us-central1"}},
		{NewRouters(api), []string{"projects/p/regions/us-central1/routers/nat-router"}, []string{"us-central1"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.p.Service()), func(t *testing.T) {
			t.Parallel()
			got, err := tc.p.List(context.Background(), "projects/p")
			if err != nil {
				t.Fatal(err)
			}
			var ids, locs []string
			for _, r := range got {
				if err := r.Validate(); err != nil {
					t.Errorf("%s: %v", r.ID, err)
				}
				if r.Service != tc.p.Service() || r.ScopeID != "projects/p" {
					t.Errorf("%+v", r)
				}
				ids = append(ids, r.ID)
				locs = append(locs, r.Location)
			}
			if !slices.Equal(ids, tc.ids) || !slices.Equal(locs, tc.locs) {
				t.Errorf("ids=%v locs=%v", ids, locs)
			}
		})
	}
}

func TestInfraProvidersScopesAndErrors(t *testing.T) {
	t.Parallel()
	nilAPI := []model.ResourceProvider{NewDisks(nil), NewInstanceTemplates(nil), NewInstanceGroups(nil), NewRouters(nil)}
	for _, p := range nilAPI {
		for _, scope := range []string{"folders/1", "organizations/1", ""} {
			if got, err := p.List(context.Background(), scope); got != nil || err != nil {
				t.Errorf("%s List(%q) = %v, %v", p.Service(), scope, got, err)
			}
		}
	}
	boom := errors.New("boom")
	api := fakeAPI{err: boom}
	for _, p := range []model.ResourceProvider{NewDisks(api), NewInstanceTemplates(api), NewInstanceGroups(api), NewRouters(api)} {
		if _, err := p.List(context.Background(), "projects/p"); !errors.Is(err, boom) {
			t.Errorf("%s err=%v", p.Service(), err)
		}
	}
	router := model.Resource{ID: "projects/p/regions/r/routers/x", Service: model.ServiceRouter, ScopeID: "projects/p"}
	if _, _, err := NewRouters(api).ListRelated(context.Background(), router, model.FacetNATs); !errors.Is(err, boom) {
		t.Errorf("routers ListRelated err=%v", err)
	}
	vpc := model.Resource{ID: "projects/p/global/networks/default", Name: "default", Service: model.ServiceVPC, ScopeID: "projects/p"}
	if _, _, err := NewNetworks(api).ListRelated(context.Background(), vpc, model.FacetRoutes); !errors.Is(err, boom) {
		t.Errorf("routes err=%v", err)
	}
}

func TestRouterNATs(t *testing.T) {
	t.Parallel()
	p := NewRouters(infraSample())
	routers, err := p.List(context.Background(), "projects/p")
	if err != nil || len(routers) != 1 {
		t.Fatalf("%v %v", routers, err)
	}
	r := routers[0]
	if len(r.Facets) != 1 || r.Facets[0].ID != model.FacetNATs {
		t.Fatalf("facets %+v", r.Facets)
	}
	nats, trunc, err := p.ListRelated(context.Background(), r, model.FacetNATs)
	if err != nil || trunc || len(nats) != 1 {
		t.Fatalf("nats %v %v %v", nats, trunc, err)
	}
	n := nats[0]
	if n.ID != r.ID+"/nats/egress" || n.ParentID != r.ID || n.FacetID != model.FacetNATs || n.Service != model.ServiceCloudNAT || n.Location != "us-central1" {
		t.Errorf("nat %+v", n)
	}
	if err := n.Validate(); err != nil {
		t.Error(err)
	}
	for _, tc := range []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: "projects/p/global/networks/default", Service: model.ServiceVPC}, model.FacetNATs},
		{"undeclared facet", r, model.FacetRoutes},
	} {
		if got, trunc, err := p.ListRelated(context.Background(), tc.parent, tc.facet); got != nil || trunc || err != nil {
			t.Errorf("%s: %v %v %v", tc.name, got, trunc, err)
		}
	}
}

func TestNetworkRoutes(t *testing.T) {
	t.Parallel()
	p := NewNetworks(infraSample())
	nets, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	routes, _, err := p.ListRelated(context.Background(), nets[0], model.FacetRoutes)
	if err != nil || len(routes) != 1 {
		t.Fatalf("routes %v %v", routes, err)
	}
	r := routes[0]
	if r.ID != "projects/p/global/routes/default-route-1" || r.ParentID != nets[0].ID || r.FacetID != model.FacetRoutes || r.Service != model.ServiceRoute {
		t.Errorf("route %+v", r)
	}
}

func TestInfraDetailFields(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 4, 5, 6, 7, 0, 0, time.UTC)
	cases := []struct {
		name     string
		r        model.Resource
		wantKeys []string
		want     map[string]string
	}{
		{
			name: "disk",
			r: diskResource("p", Disk{Name: "d", Zone: "z", Type: "pd-ssd", SizeGB: 50, Status: "READY",
				SourceImage: "debian-12", KMSKey: "projects/p/locations/l/keyRings/k/cryptoKeys/c", Created: created}),
			wantKeys: []string{"type", "size", "attachedTo", "sourceImage", "encryption"},
			want:     map[string]string{"size": "50 GB", "attachedTo": "none", "encryption": "projects/p/locations/l/keyRings/k/cryptoKeys/c"},
		},
		{
			name: "template",
			r: instanceTemplateResource("p", InstanceTemplate{Name: "t", MachineType: "e2-small", BootImage: "cos", BootDiskGB: 10,
				BootDiskType: "pd-standard", Disks: 2, Network: "default", ExternalIP: true, Tags: []string{"web"}}),
			wantKeys: []string{"machineType", "bootImage", "bootDisk", "disks", "network", "externalIP", "networkTags"},
			want:     map[string]string{"bootDisk": "10 GB pd-standard", "externalIP": "on"},
		},
		{
			name: "instance group",
			r: instanceGroupResource("p", InstanceGroup{Name: "g", Zone: "z", TargetSize: 4, Templates: []string{"web-v1", "web-v2"},
				Actions: map[string]int64{"none": 3, "creating": 1}, Stable: false, UpdateType: "PROACTIVE", NamedPorts: []string{"http:80"}}),
			wantKeys: []string{"targetSize", "instances", "template", "updatePolicy", "namedPorts"},
			want:     map[string]string{"instances": "none 3, creating 1", "template": "web-v1, web-v2"},
		},
		{
			name: "instance group with autoscaler and update limits",
			r: instanceGroupResource("p", InstanceGroup{Name: "g", Region: "us-central1", TargetSize: 3, AutoscaleMode: "ONLY_SCALE_OUT",
				AutoscaleMin: 2, AutoscaleMax: 10, AutoscaleCPU: 0.6, Recommended: 4, MaxSurge: "3", MaxUnavailable: "20%",
				MinimalAction: "REPLACE", ReplacementMethod: "SUBSTITUTE", AutoHealing: []string{"web-hc"}, AutoHealDelay: 300,
				Zones: []string{"us-central1-a", "us-central1-b"}, TargetShape: "EVEN"}),
			wantKeys: []string{"targetSize", "autoscaling", "recommendedSize", "updateLimits", "minimalAction", "replacementMethod",
				"autoHealing", "autoHealingDelay", "zones"},
			want: map[string]string{"autoscaling": "2–10, CPU 60%, ONLY_SCALE_OUT", "updateLimits": "surge 3, unavailable 20%",
				"autoHealingDelay": "5m0s", "zones": "us-central1-a, us-central1-b (EVEN)", "recommendedSize": "4"},
		},
		{
			name: "managed instance",
			r: managedInstanceResource("p", "projects/p/zones/z/instanceGroupManagers/g", model.FacetInstances, "g", "z", ManagedInstance{
				Name: "g-abcd", Zone: "z", Status: "RUNNING", TargetStatus: "RUNNING", Action: "VERIFYING", Template: "web-v2",
				Health: []string{"HEALTHY (web-hc)"}}),
			wantKeys: []string{"instanceStatus", "currentAction", "health", "template", "group", "vm"},
			want:     map[string]string{"currentAction": "VERIFYING", "health": "HEALTHY (web-hc)"},
		},
		{
			name:     "route",
			r:        routeResource("p", Route{Name: "r", Network: "n", DestRange: "10.0.0.0/8", Priority: 100, NextHop: "instances/nat-vm", Warnings: []string{"NEXT_HOP_INSTANCE_NOT_ON_NETWORK"}}, ""),
			wantKeys: []string{"destination", "priority", "nextHop", "network", "warnings"},
			want:     map[string]string{"nextHop": "instances/nat-vm"},
		},
		{
			name: "router",
			r: routerResource("p", Router{Name: "r", Region: "us-east1", Network: "n", ASN: 64512, Interfaces: 2,
				Peers: []BGPPeer{{Name: "onprem", PeerIP: "169.254.0.2", PeerASN: 65001, Enabled: true}}}),
			wantKeys: []string{"network", "asn", "interfaces", "bgpPeers", "natGateways"},
			want:     map[string]string{"bgpPeers": "1", "natGateways": "0"},
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
	router := cases[len(cases)-1].r
	if got := router.List("peers"); !slices.Equal(got, []string{"onprem  169.254.0.2  AS65001"}) {
		t.Errorf("peers %q", got)
	}
}

func TestInfraStatuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		r     model.Resource
		want  model.ResourceStatus
		state string
	}{
		{"disk ready", diskResource("p", Disk{Name: "d", Zone: "z", Status: "READY"}), model.StatusHealthy, "READY"},
		{"disk creating", diskResource("p", Disk{Name: "d", Zone: "z", Status: "CREATING"}), model.StatusWarning, "CREATING"},
		{"disk restoring", diskResource("p", Disk{Name: "d", Zone: "z", Status: "RESTORING"}), model.StatusWarning, "RESTORING"},
		{"disk deleting", diskResource("p", Disk{Name: "d", Zone: "z", Status: "DELETING"}), model.StatusWarning, "DELETING"},
		{"disk failed", diskResource("p", Disk{Name: "d", Zone: "z", Status: "FAILED"}), model.StatusError, "FAILED"},
		{"disk unknown", diskResource("p", Disk{Name: "d", Zone: "z", Status: "ODD"}), model.StatusUnknown, "ODD"},
		{"group stable", instanceGroupResource("p", InstanceGroup{Name: "g", Zone: "z", Stable: true}), model.StatusHealthy, "STABLE"},
		{"group updating", instanceGroupResource("p", InstanceGroup{Name: "g", Zone: "z"}), model.StatusWarning, "UPDATING"},
		{"route ok", routeResource("p", Route{Name: "r"}, ""), "", ""},
		{"route warning", routeResource("p", Route{Name: "r", Warnings: []string{"X"}}, ""), model.StatusWarning, ""},
	}
	for _, tc := range cases {
		if tc.r.Status != tc.want || tc.r.State != tc.state {
			t.Errorf("%s: status %s state %q, want %s %q", tc.name, tc.r.Status, tc.r.State, tc.want, tc.state)
		}
	}
}

func TestAutoscaling(t *testing.T) {
	t.Parallel()
	cases := []struct {
		g    InstanceGroup
		want string
	}{
		{InstanceGroup{}, ""},
		{InstanceGroup{AutoscaleMode: "OFF", AutoscaleMax: 5}, "off"},
		{InstanceGroup{AutoscaleMode: "ON", AutoscaleMin: 1, AutoscaleMax: 3}, "1–3"},
		{InstanceGroup{AutoscaleMode: "ON", AutoscaleMin: 1, AutoscaleMax: 3, AutoscaleStatus: "ERROR"}, "1–3, autoscaler error"},
	}
	for _, tc := range cases {
		if got := autoscaling(tc.g); got != tc.want {
			t.Errorf("autoscaling(%+v) = %q, want %q", tc.g, got, tc.want)
		}
	}
}

func TestManagedInstanceStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		m     ManagedInstance
		want  model.ResourceStatus
		badge string
	}{
		{ManagedInstance{Status: "RUNNING", Action: "NONE"}, model.StatusHealthy, ""},
		{ManagedInstance{Status: "RUNNING", Action: "RECREATING"}, model.StatusWarning, "RECREATING"},
		{ManagedInstance{Status: "RUNNING", Action: "NONE", Health: []string{"UNHEALTHY (hc)"}}, model.StatusError, ""},
		{ManagedInstance{Action: "CREATING", Errors: []string{"QUOTA_EXCEEDED: CPUS"}}, model.StatusError, "CREATING"},
	}
	for _, tc := range cases {
		r := managedInstanceResource("p", "projects/p/zones/z/instanceGroupManagers/g", model.FacetInstances, "g", "z", ManagedInstance{Name: "vm", Zone: "z", Status: tc.m.Status, Action: tc.m.Action, Health: tc.m.Health, Errors: tc.m.Errors})
		if r.Status != tc.want || r.Badge != tc.badge {
			t.Errorf("%+v: status %v badge %q, want %v %q", tc.m, r.Status, r.Badge, tc.want, tc.badge)
		}
	}
}

func TestManagedInstances(t *testing.T) {
	t.Parallel()
	api := fakeAPI{managed: map[string][]ManagedInstance{
		"p/us-central1-a/web-mig":    {{Name: "web-1", Zone: "us-central1-a", Status: "RUNNING", Action: "NONE"}},
		"p/us-central1/api-mig":      {{Name: "api-1", Zone: "us-central1-b", Status: "RUNNING"}, {Name: "api-2", Zone: "us-central1-c", Status: "STAGING", Action: "CREATING"}, {Action: "CREATING"}, {Name: "api-3", Action: "CREATING"}},
		"p/us-central1-a/gke-pool-a": {{Name: "gke-pool-a-1", Zone: "us-central1-a", Status: "RUNNING"}},
		"p/us-central1-b/gke-pool-b": {{Name: "gke-pool-b-1", Zone: "us-central1-b", Status: "RUNNING"}},
	}}
	p := NewManagedInstances(api)
	if got, err := p.List(context.Background(), "projects/p"); got != nil || err != nil {
		t.Fatalf("List = %v %v, want nothing", got, err)
	}
	regional := instanceGroupResource("p", InstanceGroup{Name: "api-mig", Region: "us-central1"})
	got, truncated, err := p.ListRelated(context.Background(), regional, model.FacetInstances)
	if err != nil || truncated || len(got) != 3 {
		t.Fatalf("regional group (a nameless instance is skipped): %+v %v %v", got, truncated, err)
	}
	if r := got[1]; r.ID != regional.ID+"/instances/api-2" || r.Service != model.ServiceManagedInstance ||
		r.ParentID != regional.ID || r.FacetID != model.FacetInstances || r.Location != "us-central1-c" || r.Badge != "CREATING" ||
		r.Metadata["vm"] != "projects/p/zones/us-central1-c/instances/api-2" {
		t.Fatalf("managed instance: %+v", r)
	}
	if r := got[2]; r.Location != "us-central1" || r.Metadata["vm"] != "" {
		t.Fatalf("an instance without a zone yet takes the group's location: %+v", r)
	}
	pool := model.Resource{
		ID: "projects/p/locations/us-central1/clusters/prod/nodePools/default-pool", Service: model.ServiceGKENodePool, ScopeID: "projects/p",
		Lists: []model.List{{ID: model.ListInstanceGroups, Items: []string{
			"projects/p/zones/us-central1-a/instanceGroupManagers/gke-pool-a",
			"projects/p/zones/us-central1-b/instanceGroupManagers/gke-pool-b",
		}}},
	}
	nodes, truncated, err := p.ListRelated(context.Background(), pool, model.FacetNodes)
	if err != nil || truncated || len(nodes) != 2 || nodes[0].Name != "gke-pool-a-1" || nodes[1].ParentID != pool.ID ||
		nodes[1].FacetID != model.FacetNodes || nodes[1].Metadata["group"] != "gke-pool-b" {
		t.Fatalf("node pool nodes: %+v %v %v", nodes, truncated, err)
	}
	for _, tc := range []struct {
		parent model.Resource
		facet  string
	}{
		{regional, model.FacetNodes},
		{pool, model.FacetInstances},
		{model.Resource{ID: "projects/p/zones/z/instances/vm", Service: model.ServiceComputeEngine}, model.FacetInstances},
	} {
		if got, _, err := p.ListRelated(context.Background(), tc.parent, tc.facet); got != nil || err != nil {
			t.Errorf("%s %s: %v %v", tc.parent.ID, tc.facet, got, err)
		}
	}
	boom := errors.New("boom")
	if _, _, err := NewManagedInstances(fakeAPI{err: boom}).ListRelated(context.Background(), regional, model.FacetInstances); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestManagedInstancesTruncate(t *testing.T) {
	t.Parallel()
	many := make([]ManagedInstance, maxManagedInstances)
	for i := range many {
		many[i] = ManagedInstance{Name: "vm", Zone: "a"}
	}
	api := fakeAPI{managed: map[string][]ManagedInstance{"p/a/g1": many, "p/b/g2": many}}
	pool := model.Resource{ID: "projects/p/locations/l/clusters/c/nodePools/np", Service: model.ServiceGKENodePool,
		Lists: []model.List{{ID: model.ListInstanceGroups, Items: []string{"projects/p/zones/a/instanceGroupManagers/g1", "projects/p/zones/b/instanceGroupManagers/g2"}}}}
	got, truncated, err := NewManagedInstances(api).ListRelated(context.Background(), pool, model.FacetNodes)
	if err != nil || !truncated || len(got) != maxManagedInstances {
		t.Fatalf("len=%d truncated=%v err=%v", len(got), truncated, err)
	}
}
