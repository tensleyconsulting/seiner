package workstations

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// fakeAPI serves one project. denied makes the named lists ("clusters",
// "configs", "workstations") fail with ErrDenied, as for a workstation
// user; usable is what ListUsable returns, filtered by its parent.
type fakeAPI struct {
	locs      []string
	clusters  map[string][]Cluster // by location
	configs   map[string][]Config  // by cluster
	ws        map[string][]Workstation
	usable    []Workstation
	policies  map[string][]Binding // keyed by cluster/config or cluster/config/ws
	denied    map[string]bool
	locErr    map[string]error // ListClusters error by location
	err       error            // returned from every method when set
	mu        *sync.Mutex
	usableFor *[]string // ListUsable parents, "loc/cluster/config"
}

func (f fakeAPI) Locations(context.Context, string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.locs, nil
}

func (f fakeAPI) ListClusters(_ context.Context, _, location string) ([]Cluster, error) {
	switch {
	case f.err != nil:
		return nil, f.err
	case f.locErr[location] != nil:
		return nil, f.locErr[location]
	case f.denied["clusters"]:
		return nil, ErrDenied
	}
	return f.clusters[location], nil
}

func (f fakeAPI) ListConfigs(_ context.Context, _, _, cluster string) ([]Config, error) {
	switch {
	case f.err != nil:
		return nil, f.err
	case f.denied["configs"]:
		return nil, ErrDenied
	}
	return f.configs[cluster], nil
}

func (f fakeAPI) ListWorkstations(_ context.Context, _, _, cluster, config string) ([]Workstation, error) {
	switch {
	case f.err != nil:
		return nil, f.err
	case f.denied["workstations"]:
		return nil, ErrDenied
	}
	return f.ws[cluster+"/"+config], nil
}

func (f fakeAPI) GetConfigIAMPolicy(_ context.Context, _, _, cluster, config string) ([]Binding, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.policies[cluster+"/"+config], nil
}

func (f fakeAPI) GetWorkstationIAMPolicy(_ context.Context, _, _, cluster, config, ws string) ([]Binding, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.policies[cluster+"/"+config+"/"+ws], nil
}

func (f fakeAPI) ListUsable(_ context.Context, _, location, cluster, config string) ([]Workstation, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.usableFor != nil {
		f.mu.Lock()
		*f.usableFor = append(*f.usableFor, location+"/"+cluster+"/"+config)
		f.mu.Unlock()
	}
	var out []Workstation
	for _, w := range f.usable {
		if w.Location == location && (cluster == Any || w.Cluster == cluster) && (config == Any || w.Config == config) {
			out = append(out, w)
		}
	}
	return out, nil
}

var created = time.Date(2025, 4, 1, 9, 0, 0, 0, time.UTC)

func sample() fakeAPI {
	return fakeAPI{
		locs: []string{"europe-west1", "us-central1"},
		clusters: map[string][]Cluster{
			"us-central1": {{Name: "dev", Location: "us-central1", DisplayName: "Dev", Network: "projects/p/global/networks/default",
				Subnetwork: "projects/p/regions/us-central1/subnetworks/default", PrivateEndpoint: true, Hostname: "dev.internal",
				Created: created, Labels: map[string]string{"team": "platform"}}},
			"europe-west1": {{Name: "eu", Location: "europe-west1", Degraded: true, Conditions: []string{"subnet exhausted"}}},
		},
		configs: map[string][]Config{
			"dev": {{Name: "code", Cluster: "dev", Location: "us-central1", MachineType: "e2-standard-8", PoolSize: 2, PooledInstances: 1,
				BootDiskGB: 50, NoPublicIP: true, IdleTimeout: "1200s", RunningTimeout: "43200s", Image: "code-oss:latest",
				Accelerators: []string{"nvidia-tesla-t4 ×1"}, Env: map[string]string{"TEAM": "platform", "GOPROXY": "direct"},
				Directories: []Directory{{MountPath: "/home", SizeGB: 200, DiskType: "pd-balanced", ReclaimPolicy: "RETAIN"}}}},
		},
		ws: map[string][]Workstation{
			"dev/code": {
				{Name: "alice", Config: "code", Cluster: "dev", Location: "us-central1", State: "STATE_RUNNING", Host: "alice.ws.example.com",
					Env: map[string]string{"EDITOR": "vim"}, Started: created},
				{Name: "bob", Config: "code", Cluster: "dev", Location: "us-central1", State: "STATE_STOPPED"},
			},
		},
		usable: []Workstation{
			{Name: "alice", Config: "code", Cluster: "dev", Location: "us-central1", State: "STATE_RUNNING"},
			{Name: "alice-gpu", Config: "gpu", Cluster: "dev", Location: "us-central1", State: "STATE_SUSPENDED"},
		},
	}
}

func names(rs []model.Resource) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func TestListScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope string
		want  []string
	}{
		{"projects/p", []string{"eu", "dev"}},
		{"folders/1", nil},
		{"organizations/1", nil},
		{"", nil},
	}
	for _, tc := range cases {
		got, err := New(sample()).List(context.Background(), tc.scope)
		if err != nil {
			t.Fatalf("%q: %v", tc.scope, err)
		}
		if !slices.Equal(names(got), tc.want) {
			t.Errorf("%q: clusters %q, want %q", tc.scope, names(got), tc.want)
		}
	}
}

func TestMapping(t *testing.T) {
	t.Parallel()
	p := New(sample())
	clusters, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	eu, dev := clusters[0], clusters[1]
	if dev.ID != "projects/p/locations/us-central1/workstationClusters/dev" || dev.Service != model.ServiceWorkstationCluster ||
		dev.ScopeID != "projects/p" || dev.Location != "us-central1" || dev.Status != "" || !dev.Created.Equal(created) ||
		dev.Labels["team"] != "platform" || dev.Metadata["network"] != "default" || dev.Metadata["privateEndpoint"] != "on" {
		t.Fatalf("cluster %+v", dev)
	}
	if f, ok := dev.InlineFacet(); !ok || f.ID != model.FacetConfigs {
		t.Fatalf("cluster facets %+v", dev.Facets)
	}
	if eu.Status != model.StatusError || eu.Metadata["condition"] != "subnet exhausted" {
		t.Fatalf("degraded cluster %+v", eu)
	}

	configs, _, err := p.ListRelated(context.Background(), dev, model.FacetConfigs)
	if err != nil || len(configs) != 1 {
		t.Fatalf("configs %v err %v", configs, err)
	}
	c := configs[0]
	if c.ID != dev.ID+"/workstationConfigs/code" || c.Service != model.ServiceWorkstationConfig || c.ParentID != dev.ID ||
		c.FacetID != model.FacetConfigs || c.ScopeID != "projects/p" {
		t.Fatalf("config identity %+v", c)
	}
	var keys []string
	for _, f := range c.Fields {
		keys = append(keys, f.Key+"="+f.Value)
	}
	want := []string{"machineType=e2-standard-8", "pool=2 (1 ready)", "bootDisk=50 GB", "accelerators=nvidia-tesla-t4 ×1",
		"image=code-oss:latest", "publicIp=off", "idleTimeout=20m0s", "runningTimeout=12h0m0s",
		"persistentDirectories=/home 200 GB pd-balanced, RETAIN"}
	if !slices.Equal(keys, want) {
		t.Fatalf("config fields %q\nwant %q", keys, want)
	}
	if env := c.List("env"); !slices.Equal(env, []string{"GOPROXY=direct", "TEAM=platform"}) {
		t.Fatalf("config env %q", env)
	}

	ws, _, err := p.ListRelated(context.Background(), c, model.FacetWorkstations)
	if err != nil || len(ws) != 2 {
		t.Fatalf("workstations %v err %v", ws, err)
	}
	a := ws[0]
	if a.ID != c.ID+"/workstations/alice" || a.Service != model.ServiceWorkstation || a.ParentID != c.ID ||
		a.FacetID != model.FacetWorkstations || a.State != "RUNNING" || a.Status != model.StatusHealthy ||
		a.Metadata["host"] != "alice.ws.example.com" || a.Metadata["started"] != "2025-04-01 09:00 UTC" {
		t.Fatalf("workstation %+v", a)
	}
	if env := a.List("env"); !slices.Equal(env, []string{"EDITOR=vim"}) {
		t.Fatalf("workstation env %q", env)
	}
	if b := ws[1]; b.State != "STOPPED" || b.Status != "" {
		t.Fatalf("stopped workstation %+v", b)
	}
}

func TestStatuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		state       string
		reconciling bool
		want        model.ResourceStatus
	}{
		{"RUNNING", false, model.StatusHealthy},
		{"RUNNING", true, model.StatusWarning},
		{"STARTING", false, model.StatusWarning},
		{"STOPPING", false, model.StatusWarning},
		{"SUSPENDING", false, model.StatusWarning},
		{"STOPPED", false, ""},
		{"SUSPENDED", false, ""},
		{"", false, model.StatusUnknown},
		{"NEW_STATE", false, model.StatusUnknown},
	}
	for _, tc := range cases {
		if got := workstationStatus(tc.state, tc.reconciling); got != tc.want {
			t.Errorf("workstationStatus(%q, %v) = %q, want %q", tc.state, tc.reconciling, got, tc.want)
		}
	}
	for _, tc := range []struct {
		degraded, reconciling bool
		want                  model.ResourceStatus
	}{
		{false, false, ""},
		{false, true, model.StatusWarning},
		{true, true, model.StatusError},
	} {
		if got := reconcileStatus(tc.degraded, tc.reconciling); got != tc.want {
			t.Errorf("reconcileStatus(%v, %v) = %q, want %q", tc.degraded, tc.reconciling, got, tc.want)
		}
	}
}

// A workstation user may list nothing. Each level then falls back to the
// workstations the caller can use, under the parent it was asked for.
func TestDeniedFallsBackToUsable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		denied     []string
		configs    []string
		workstns   []string
		usableFor  []string
		configNote bool
	}{
		{"workstation user", []string{"clusters", "configs", "workstations"},
			[]string{"code", "gpu"}, []string{"alice"},
			[]string{"europe-west1/-/-", "us-central1/-/-", "us-central1/dev/-", "us-central1/dev/code"}, true},
		{"workstation creator", []string{"configs", "workstations"},
			[]string{"code", "gpu"}, []string{"alice"},
			[]string{"us-central1/dev/-", "us-central1/dev/code"}, true},
		{"admin", nil, []string{"code"}, []string{"alice", "bob"}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var usableFor []string
			api := sample()
			api.denied = map[string]bool{}
			for _, d := range tc.denied {
				api.denied[d] = true
			}
			api.mu, api.usableFor = &sync.Mutex{}, &usableFor
			p := New(api)
			clusters, err := p.List(context.Background(), "projects/p")
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(clusters, func(r model.Resource) bool { return r.Name == "dev" })
			if i < 0 {
				t.Fatalf("clusters %q, want dev", names(clusters))
			}
			dev := clusters[i]
			if f, ok := dev.InlineFacet(); !ok || f.ID != model.FacetConfigs || dev.ID != "projects/p/locations/us-central1/workstationClusters/dev" {
				t.Fatalf("cluster %+v", dev)
			}
			configs, _, err := p.ListRelated(context.Background(), dev, model.FacetConfigs)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(names(configs), tc.configs) {
				t.Fatalf("configs %q, want %q", names(configs), tc.configs)
			}
			code := configs[0]
			if code.ParentID != dev.ID || code.FacetID != model.FacetConfigs || (code.Metadata["listing"] != "") != tc.configNote {
				t.Fatalf("config %+v", code)
			}
			ws, _, err := p.ListRelated(context.Background(), code, model.FacetWorkstations)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(names(ws), tc.workstns) {
				t.Fatalf("workstations %q, want %q", names(ws), tc.workstns)
			}
			slices.Sort(usableFor)
			if !slices.Equal(usableFor, tc.usableFor) {
				t.Fatalf("listUsable parents %q, want %q", usableFor, tc.usableFor)
			}
		})
	}
}

func TestLocationFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	api := sample()
	api.locErr = map[string]error{"europe-west1": boom}
	got, err := New(api).List(context.Background(), "projects/p")
	if err != nil || !slices.Equal(names(got), []string{"dev"}) {
		t.Fatalf("one location failed: %q err %v", names(got), err)
	}
	api.locErr["us-central1"] = boom
	if _, err := New(api).List(context.Background(), "projects/p"); !errors.Is(err, boom) || !strings.Contains(err.Error(), "every location") {
		t.Fatalf("every location failed: err %v", err)
	}
}

func TestListRelatedIgnoresOthers(t *testing.T) {
	t.Parallel()
	p := New(sample())
	cluster := model.Resource{ID: "projects/p/locations/us-central1/workstationClusters/dev", Service: model.ServiceWorkstationCluster, ScopeID: "projects/p"}
	config := model.Resource{ID: cluster.ID + "/workstationConfigs/code", Service: model.ServiceWorkstationConfig, ScopeID: "projects/p"}
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: "projects/p/buckets/b", Service: model.ServiceCloudStorage}, model.FacetConfigs},
		{"cluster, workstations facet", cluster, model.FacetWorkstations},
		{"config, configs facet", config, model.FacetConfigs},
		{"undeclared facet", cluster, model.FacetKeys},
		{"malformed id", model.Resource{ID: "projects/p/workstationClusters/dev", Service: model.ServiceWorkstationCluster}, model.FacetConfigs},
	}
	for _, tc := range cases {
		got, _, err := p.ListRelated(context.Background(), tc.parent, tc.facet)
		if err != nil || got != nil {
			t.Errorf("%s: %v err %v, want nothing", tc.name, got, err)
		}
	}
}

func TestErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	p := New(fakeAPI{err: boom})
	if _, err := p.List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Fatalf("List err = %v", err)
	}
	cluster := model.Resource{ID: "projects/p/locations/us-central1/workstationClusters/dev", Service: model.ServiceWorkstationCluster}
	if _, _, err := p.ListRelated(context.Background(), cluster, model.FacetConfigs); !errors.Is(err, boom) {
		t.Fatalf("ListRelated configs err = %v", err)
	}
	config := model.Resource{ID: cluster.ID + "/workstationConfigs/code", Service: model.ServiceWorkstationConfig}
	if _, _, err := p.ListRelated(context.Background(), config, model.FacetWorkstations); !errors.Is(err, boom) {
		t.Fatalf("ListRelated workstations err = %v", err)
	}
}
