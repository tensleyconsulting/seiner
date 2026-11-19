package gke

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	clusters []Cluster
	pools    map[string][]NodePool // keyed by cluster name
	err      error                 // returned from every method when set
	calls    *int
}

func (f fakeAPI) called() {
	if f.calls != nil {
		*f.calls++
	}
}

// ListClusters returns the clusters beside a model.Partial err, as the live
// API does.
func (f fakeAPI) ListClusters(context.Context, string) ([]Cluster, error) {
	f.called()
	if f.err != nil && !model.IsPartial(f.err) {
		return nil, f.err
	}
	return f.clusters, f.err
}

func (f fakeAPI) ListNodePools(_ context.Context, _, _, cluster string) ([]NodePool, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	return f.pools[cluster], nil
}

var created = time.Date(2024, 11, 4, 16, 20, 0, 0, time.UTC)

func sample() fakeAPI {
	return fakeAPI{
		clusters: []Cluster{
			{
				Name: "prod", Location: "us-central1", Status: "RUNNING", MasterVersion: "1.31.5-gke.100",
				ReleaseChannel: "REGULAR", NodeCount: 7, NodePools: []string{"default-pool", "gpu-pool"},
				Endpoint: "198.51.100.40", PrivateNodes: true, Network: "default", Subnetwork: "default",
				PodRange: "10.4.0.0/14", ServicesRange: "10.8.0.0/20", WorkloadPool: "p.svc.id.goog",
				Created: created, Labels: map[string]string{"env": "prod"},
				NodeVersion: "1.30.9-gke.200", EtcdKey: "projects/p/locations/us-central1/keyRings/r/cryptoKeys/etcd",
				Maintenance: "daily 03:00 UTC (4h)", Exclusions: []string{"black-friday  2026-11-26 – 2026-12-01"},
				AuthorizedNetworks: true, AuthorizedCIDRs: []string{"office 203.0.113.0/24"}, PublicEndpoint: "off",
				DNSEndpoint: "gke-abc.us-central1.gke.goog", Dataplane: "ADVANCED_DATAPATH", ShieldedNodes: true,
				BinAuthz: "PROJECT_SINGLETON_POLICY_ENFORCE",
			},
			{Name: "edge", Location: "europe-west1-b", Status: "RUNNING", Autopilot: true, ReleaseChannel: "UNSPECIFIED"},
		},
		pools: map[string][]NodePool{
			"prod": {
				{
					Name: "default-pool", Status: "RUNNING", Version: "1.31.5-gke.100", MachineType: "e2-standard-4",
					Autoscaling: true, MinNodes: 1, MaxNodes: 3, Zones: []string{"us-central1-a", "us-central1-b"},
					DiskSizeGB: 100, DiskType: "pd-balanced", ImageType: "COS_CONTAINERD", ServiceAccount: "nodes@p.iam.gserviceaccount.com",
					AutoUpgrade: true, AutoRepair: true, MaxPodsPerNode: 110, InstanceGroups: []string{"projects/p/zones/us-central1-a/instanceGroupManagers/grp-a", "projects/p/zones/us-central1-b/instanceGroupManagers/grp-b"},
					Taints: []string{"dedicated=web:NO_SCHEDULE"}, NodeLabels: []string{"team=web"}, MetadataServer: "GKE_METADATA",
					SecureBoot: true, Integrity: true, UpgradeType: "SURGE", MaxSurge: 1,
				},
				{
					Name: "gpu-pool", Status: "RECONCILING", MachineType: "g2-standard-8", Spot: true, InitialNodes: 1,
					Accelerators: []Accelerator{{Type: "nvidia-l4", Count: 1}},
				},
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

func TestListPartial(t *testing.T) {
	t.Parallel()
	missing := model.Partial(errors.New("zones unreachable: us-east1-b"))
	hard := errors.New("boom")
	cases := []struct {
		name    string
		err     error
		wantN   int
		wantErr error
		partial bool
	}{
		{name: "complete", wantN: 2},
		{name: "zones unreachable", err: missing, wantN: 2, wantErr: missing, partial: true},
		{name: "failed", err: hard, wantErr: hard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := sample()
			api.err = tc.err
			got, err := New(api).List(context.Background(), "projects/p")
			if len(got) != tc.wantN || !errors.Is(err, tc.wantErr) || model.IsPartial(err) != tc.partial {
				t.Fatalf("len=%d err=%v, want %d and %v (partial %v)", len(got), err, tc.wantN, tc.wantErr, tc.partial)
			}
		})
	}
}

func TestClusterMapping(t *testing.T) {
	t.Parallel()
	got, err := New(sample()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.ID != "projects/p/locations/us-central1/clusters/prod" || r.Name != "prod" || r.Service != model.ServiceGKE ||
		r.ScopeID != "projects/p" || r.Location != "us-central1" {
		t.Fatalf("identity: %+v", r)
	}
	if r.State != "RUNNING" || r.Status != model.StatusHealthy || !r.Created.Equal(created) || r.Labels["env"] != "prod" {
		t.Fatalf("state/created/labels: %+v", r)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	wantFields := []string{"mode", "version", "nodeVersion", "releaseChannel", "locationType", "nodes", "nodePools", "endpoint",
		"publicEndpoint", "dnsEndpoint", "authorizedNetworks", "privateNodes", "network", "subnetwork", "podRange",
		"servicesRange", "dataplane", "workloadPool", "etcdEncryption", "shieldedNodes", "binaryAuthorization",
		"maintenance", "maintenanceExclusions"}
	if keys := fieldKeys(r); !slices.Equal(keys, wantFields) {
		t.Errorf("fields = %v\nwant %v", keys, wantFields)
	}
	md := map[string]string{
		"mode": "Standard", "locationType": "regional", "nodes": "7",
		"nodePools": "2 (default-pool, gpu-pool)", "privateNodes": "on", "nodeVersion": "1.30.9-gke.200",
		"authorizedNetworks": "1", "publicEndpoint": "off", "dataplane": "V2", "shieldedNodes": "on",
		"maintenanceExclusions": "1", "dnsEndpoint": "gke-abc.us-central1.gke.goog",
	}
	for k, v := range md {
		if r.Metadata[k] != v {
			t.Errorf("%s = %q, want %q", k, r.Metadata[k], v)
		}
	}
	if len(r.Facets) != 1 || r.Facets[0].ID != model.FacetNodePools {
		t.Errorf("facets = %+v", r.Facets)
	}
	if !slices.Equal(r.List("authorizedNetworks"), []string{"office 203.0.113.0/24"}) || len(r.List("maintenanceExclusions")) != 1 {
		t.Errorf("lists = %+v", r.Lists)
	}
	edge := got[1]
	if edge.Metadata["authorizedNetworks"] != "off" || edge.Metadata["nodeVersion"] != "" || edge.Metadata["binaryAuthorization"] != "" {
		t.Errorf("edge: %+v", edge.Metadata)
	}
	if edge.Metadata["mode"] != "Autopilot" || edge.Metadata["locationType"] != "zonal" || edge.Metadata["nodePools"] != "0" {
		t.Errorf("edge metadata = %v", edge.Metadata)
	}
	if _, ok := edge.Metadata["releaseChannel"]; ok {
		t.Errorf("UNSPECIFIED release channel should be omitted: %v", edge.Metadata)
	}
}

func TestPoolSummaryCaps(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b", "c", "d", "e", "f", "g"}
	if got, want := poolSummary(names), "7 (a, b, c, d, e, …)"; got != want {
		t.Errorf("poolSummary = %q, want %q", got, want)
	}
}

func TestStatusMapping(t *testing.T) {
	t.Parallel()
	clusters := []struct {
		state string
		want  model.ResourceStatus
	}{
		{"RUNNING", model.StatusHealthy},
		{"PROVISIONING", model.StatusWarning},
		{"RECONCILING", model.StatusWarning},
		{"STOPPING", model.StatusWarning},
		{"ERROR", model.StatusError},
		{"DEGRADED", model.StatusError},
		{"STATUS_UNSPECIFIED", model.StatusUnknown},
		{"", model.StatusUnknown},
	}
	for _, tc := range clusters {
		if got := clusterStatus(tc.state); got != tc.want {
			t.Errorf("clusterStatus(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
	pools := []struct {
		state string
		want  model.ResourceStatus
	}{
		{"RUNNING", model.StatusHealthy},
		{"RUNNING_WITH_ERROR", model.StatusWarning},
		{"PROVISIONING", model.StatusWarning},
		{"RECONCILING", model.StatusWarning},
		{"STOPPING", model.StatusWarning},
		{"ERROR", model.StatusError},
		{"STATUS_UNSPECIFIED", model.StatusUnknown},
	}
	for _, tc := range pools {
		if got := nodePoolStatus(tc.state); got != tc.want {
			t.Errorf("nodePoolStatus(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestListRelatedNodePools(t *testing.T) {
	t.Parallel()
	p := New(sample())
	clusters, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	parent := clusters[0]
	got, truncated, err := p.ListRelated(context.Background(), parent, model.FacetNodePools)
	if err != nil || truncated || len(got) != 2 {
		t.Fatalf("got %d truncated=%v err=%v", len(got), truncated, err)
	}
	def := got[0]
	if def.ID != parent.ID+"/nodePools/default-pool" || def.Service != model.ServiceGKENodePool ||
		def.ParentID != parent.ID || def.FacetID != model.FacetNodePools || def.ScopeID != "projects/p" ||
		def.Location != "us-central1" || def.State != "RUNNING" || def.Status != model.StatusHealthy {
		t.Fatalf("default-pool: %+v", def)
	}
	if err := def.Validate(); err != nil {
		t.Fatal(err)
	}
	wantFields := []string{"machineType", "version", "autoscaling", "zones", "disk", "imageType", "provisioning",
		"serviceAccount", "metadataServer", "shieldedVM", "autoUpgrade", "autoRepair", "upgradeStrategy", "maxPodsPerNode",
		"taints", "nodeLabels", "instanceGroups"}
	if keys := fieldKeys(def); !slices.Equal(keys, wantFields) {
		t.Errorf("fields = %v\nwant %v", keys, wantFields)
	}
	for k, v := range map[string]string{
		"autoscaling": "1–3 per zone", "disk": "100 GB pd-balanced", "provisioning": "STANDARD",
		"zones": "us-central1-a, us-central1-b", "instanceGroups": "2", "upgradeStrategy": "SURGE (surge 1, unavailable 0)",
		"shieldedVM": "secure boot, integrity monitoring", "taints": "1",
	} {
		if def.Metadata[k] != v {
			t.Errorf("default-pool %s = %q, want %q", k, def.Metadata[k], v)
		}
	}

	if f, ok := def.InlineFacet(); !ok || f.ID != model.FacetNodes {
		t.Errorf("default-pool facets = %+v", def.Facets)
	}
	if len(def.List(model.ListInstanceGroups)) != 2 || def.List("taints")[0] != "dedicated=web:NO_SCHEDULE" {
		t.Errorf("default-pool lists = %+v", def.Lists)
	}

	gpu := got[1]
	if len(gpu.Facets) != 0 {
		t.Errorf("a pool without instance groups has no Nodes facet: %+v", gpu.Facets)
	}
	for k, v := range map[string]string{
		"accelerators": "1× nvidia-l4", "provisioning": "SPOT", "initialNodes": "1 per zone",
		"autoUpgrade": "off",
	} {
		if gpu.Metadata[k] != v {
			t.Errorf("gpu-pool %s = %q, want %q", k, gpu.Metadata[k], v)
		}
	}
	if gpu.Status != model.StatusWarning {
		t.Errorf("gpu-pool status = %q", gpu.Status)
	}
}

func TestPoolAutoscalingTotal(t *testing.T) {
	t.Parallel()
	r := nodePoolResource("p", "us-central1", "projects/p/locations/us-central1/clusters/c",
		NodePool{Name: "np", Autoscaling: true, MinNodes: 1, MaxNodes: 3, TotalMin: 0, TotalMax: 1000})
	if got := r.Metadata["autoscaling"]; got != "0–1000 total" {
		t.Errorf("autoscaling = %q", got)
	}
}

func TestListRelatedIgnoresOthers(t *testing.T) {
	t.Parallel()
	api := sample()
	calls := 0
	api.calls = &calls
	p := New(api)
	cluster := model.Resource{ID: "projects/p/locations/us-central1/clusters/prod", Service: model.ServiceGKE, ScopeID: "projects/p"}
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: "projects/p/global/networks/default", Service: model.ServiceVPC}, model.FacetNodePools},
		{"undeclared facet", cluster, model.FacetSubnets},
		{"malformed id", model.Resource{ID: "nope", Service: model.ServiceGKE}, model.FacetNodePools},
	}
	for _, tc := range cases {
		got, truncated, err := p.ListRelated(context.Background(), tc.parent, tc.facet)
		if got != nil || truncated || err != nil {
			t.Errorf("%s: got %v %v %v", tc.name, got, truncated, err)
		}
	}
	if calls != 0 {
		t.Errorf("API called %d times", calls)
	}
}

func TestErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	p := New(fakeAPI{err: boom})
	ctx := context.Background()
	if _, err := p.List(ctx, "projects/p"); !errors.Is(err, boom) {
		t.Errorf("List err = %v", err)
	}
	parent := model.Resource{ID: "projects/p/locations/us-central1/clusters/prod", Service: model.ServiceGKE, ScopeID: "projects/p"}
	if _, _, err := p.ListRelated(ctx, parent, model.FacetNodePools); !errors.Is(err, boom) {
		t.Errorf("ListRelated err = %v", err)
	}
}

func TestNilAPI(t *testing.T) {
	t.Parallel()
	if _, err := New(nil).List(context.Background(), "projects/p"); err == nil {
		t.Error("List with nil API should fail")
	}
}

func fieldKeys(r model.Resource) []string {
	out := make([]string, 0, len(r.Fields))
	for _, f := range r.Fields {
		out = append(out, f.Key)
	}
	return out
}
