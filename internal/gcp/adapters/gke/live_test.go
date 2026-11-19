package gke

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	container "google.golang.org/api/container/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/pagetest"
	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestListClustersMissingZones(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		missing string
	}{
		{name: "complete", missing: `[]`},
		{name: "zones unreachable", missing: `["us-east1-b","asia-east2-a"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"clusters":[{"name":"prod","location":"us-central1","status":"RUNNING"}],"missingZones":` + tc.missing + `}`))
			}))
			defer srv.Close()

			p := NewLazy(option.WithEndpoint(srv.URL), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
			got, err := p.List(context.Background(), "projects/p")
			if gotPath != "/v1/projects/p/locations/-/clusters" {
				t.Fatalf("request path = %q", gotPath)
			}
			if len(got) != 1 || got[0].Name != "prod" {
				t.Fatalf("clusters = %+v", got)
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

func TestFromClusterSecurityAndMaintenance(t *testing.T) {
	t.Parallel()
	got := fromCluster(&container.Cluster{
		Name: "prod", CurrentMasterVersion: "1.31.5", CurrentNodeVersion: "1.30.9",
		DatabaseEncryption: &container.DatabaseEncryption{State: "ENCRYPTED", KeyName: "projects/p/locations/l/keyRings/r/cryptoKeys/etcd"},
		MaintenancePolicy: &container.MaintenancePolicy{Window: &container.MaintenanceWindow{
			DailyMaintenanceWindow: &container.DailyMaintenanceWindow{StartTime: "03:00", Duration: "PT4H30M0S"},
			MaintenanceExclusions: map[string]container.TimeWindow{
				"freeze": {StartTime: "2026-12-20T00:00:00Z", EndTime: "2027-01-02T00:00:00Z"},
			},
		}},
		PrivateClusterConfig: &container.PrivateClusterConfig{EnablePrivateNodes: true, EnablePrivateEndpoint: true},
		MasterAuthorizedNetworksConfig: &container.MasterAuthorizedNetworksConfig{Enabled: true, CidrBlocks: []*container.CidrBlock{
			{DisplayName: "office", CidrBlock: "203.0.113.0/24"}, {CidrBlock: "198.51.100.0/24"},
		}},
		ControlPlaneEndpointsConfig: &container.ControlPlaneEndpointsConfig{
			DnsEndpointConfig: &container.DNSEndpointConfig{Endpoint: "gke-abc.goog", AllowExternalTraffic: true},
		},
		NetworkConfig:       &container.NetworkConfig{DatapathProvider: "ADVANCED_DATAPATH"},
		ShieldedNodes:       &container.ShieldedNodes{Enabled: true},
		BinaryAuthorization: &container.BinaryAuthorization{EvaluationMode: "PROJECT_SINGLETON_POLICY_ENFORCE"},
	})
	if got.NodeVersion != "1.30.9" || got.EtcdKey == "" || got.Maintenance != "daily 03:00 UTC (4h30m)" ||
		!slices.Equal(got.Exclusions, []string{"freeze  2026-12-20 – 2027-01-02"}) || got.PublicEndpoint != "off" ||
		!got.AuthorizedNetworks || !slices.Equal(got.AuthorizedCIDRs, []string{"office 203.0.113.0/24", "198.51.100.0/24"}) ||
		got.DNSEndpoint != "gke-abc.goog" || !got.DNSExternal || got.Dataplane != "ADVANCED_DATAPATH" || !got.ShieldedNodes ||
		got.BinAuthz != "PROJECT_SINGLETON_POLICY_ENFORCE" {
		t.Fatalf("fromCluster = %+v", got)
	}
	// The newer endpoints config wins over the private cluster config.
	newer := fromCluster(&container.Cluster{
		PrivateClusterConfig: &container.PrivateClusterConfig{EnablePrivateNodes: true, EnablePrivateEndpoint: true},
		ControlPlaneEndpointsConfig: &container.ControlPlaneEndpointsConfig{IpEndpointsConfig: &container.IPEndpointsConfig{
			Enabled: true, EnablePublicEndpoint: true,
			AuthorizedNetworksConfig: &container.MasterAuthorizedNetworksConfig{Enabled: true},
		}},
		MaintenancePolicy: &container.MaintenancePolicy{Window: &container.MaintenanceWindow{RecurringWindow: &container.RecurringTimeWindow{
			Recurrence: "FREQ=WEEKLY;BYDAY=SA,SU",
			Window:     &container.TimeWindow{StartTime: "2026-01-03T02:00:00Z", EndTime: "2026-01-03T06:00:00Z"},
		}}},
	})
	if newer.PublicEndpoint != "on" || !newer.AuthorizedNetworks || newer.Maintenance != "FREQ=WEEKLY;BYDAY=SA,SU, 02:00–06:00 UTC" {
		t.Fatalf("fromCluster (endpoints config) = %+v", newer)
	}
	if bare := fromCluster(&container.Cluster{Name: "x"}); bare.PublicEndpoint != "" || bare.Maintenance != "" || bare.AuthorizedNetworks {
		t.Fatalf("bare cluster = %+v", bare)
	}
}

func TestFromNodePoolDetails(t *testing.T) {
	t.Parallel()
	got := fromNodePool(&container.NodePool{
		Name: "default-pool",
		Config: &container.NodeConfig{
			Taints:                 []*container.NodeTaint{{Key: "dedicated", Value: "web", Effect: "NO_SCHEDULE"}, {Key: "gpu", Effect: "NO_EXECUTE"}},
			Labels:                 map[string]string{"team": "web", "env": "prod"},
			BootDiskKmsKey:         "projects/p/locations/l/keyRings/r/cryptoKeys/boot",
			WorkloadMetadataConfig: &container.WorkloadMetadataConfig{Mode: "GKE_METADATA"},
			SandboxConfig:          &container.SandboxConfig{Type: "GVISOR"},
			ShieldedInstanceConfig: &container.ShieldedInstanceConfig{EnableSecureBoot: true, EnableIntegrityMonitoring: true},
		},
		UpgradeSettings:   &container.UpgradeSettings{Strategy: "SURGE", MaxSurge: 2, MaxUnavailable: 1},
		InstanceGroupUrls: []string{"https://www.googleapis.com/compute/v1/projects/p/zones/us-central1-a/instanceGroupManagers/gke-prod-default-pool-1-grp"},
	})
	if !slices.Equal(got.Taints, []string{"dedicated=web:NO_SCHEDULE", "gpu:NO_EXECUTE"}) || !slices.Equal(got.NodeLabels, []string{"env=prod", "team=web"}) ||
		got.KMSKey == "" || got.MetadataServer != "GKE_METADATA" || got.Sandbox != "GVISOR" || !got.SecureBoot || !got.Integrity ||
		got.UpgradeType != "SURGE" || got.MaxSurge != 2 || got.MaxUnavailable != 1 ||
		!slices.Equal(got.InstanceGroups, []string{"projects/p/zones/us-central1-a/instanceGroupManagers/gke-prod-default-pool-1-grp"}) {
		t.Fatalf("fromNodePool = %+v", got)
	}
}

func TestISODuration(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"PT4H0M0S": "4h", "PT4H30M0S": "4h30m", "PT45M": "45m", "": "", "bogus": ""} {
		if got := isoDuration(in); got != want {
			t.Errorf("isoDuration(%q) = %q, want %q", in, got, want)
		}
	}
}

func liveFor(t *testing.T, url string) liveAPI {
	t.Helper()
	svc, err := container.NewService(context.Background(), option.WithEndpoint(url), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	return liveAPI{svc: svc}
}

func TestListNodePoolsOverHTTP(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.JSON("/v1/projects/p/locations/us-central1/clusters/prod/nodePools",
		`{"nodePools":[{"name":"default","status":"RUNNING","initialNodeCount":3,"config":{"machineType":"e2-standard-4"}},
		               {"name":"gpu","status":"RECONCILING"}]}`)
	got, err := liveFor(t, srv.URL).ListNodePools(context.Background(), "p", "us-central1", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "default" || got[1].Name != "gpu" || got[1].Status != "RECONCILING" {
		t.Fatalf("node pools %+v", got)
	}
}

func TestListClustersError(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.Error("/v1/projects/p/locations/-/clusters", http.StatusForbidden, "forbidden")
	_, err := liveFor(t, srv.URL).ListClusters(context.Background(), "p")
	var ge *googleapi.Error
	if !errors.As(err, &ge) || ge.Code != http.StatusForbidden || model.IsPartial(err) {
		t.Fatalf("err = %v, want a plain 403 googleapi.Error", err)
	}
}
