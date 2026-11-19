package compute

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	gce "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestFromManagedInstance(t *testing.T) {
	t.Parallel()
	got := fromManagedInstance(&gce.ManagedInstance{
		Instance:       "https://www.googleapis.com/compute/v1/projects/p/zones/us-central1-a/instances/web-1",
		InstanceStatus: "RUNNING",
		TargetStatus:   "RUNNING",
		CurrentAction:  "NONE",
		Version:        &gce.ManagedInstanceVersion{InstanceTemplate: "projects/p/global/instanceTemplates/web-v3", Name: "canary"},
		InstanceHealth: []*gce.ManagedInstanceInstanceHealth{{DetailedHealthState: "HEALTHY", HealthCheck: "projects/p/global/healthChecks/web-hc"}},
		LastAttempt: &gce.ManagedInstanceLastAttempt{Errors: &gce.ManagedInstanceLastAttemptErrors{
			Errors: []*gce.ManagedInstanceLastAttemptErrorsErrors{{Code: "QUOTA_EXCEEDED", Message: "CPUS"}},
		}},
	})
	want := ManagedInstance{Name: "web-1", Zone: "us-central1-a", Status: "RUNNING", TargetStatus: "RUNNING", Action: "NONE",
		Template: "web-v3", Version: "canary", Health: []string{"HEALTHY (web-hc)"}, Errors: []string{"QUOTA_EXCEEDED: CPUS"}}
	if got.Name != want.Name || got.Zone != want.Zone || got.Template != want.Template || got.Version != want.Version ||
		!slices.Equal(got.Health, want.Health) || !slices.Equal(got.Errors, want.Errors) {
		t.Fatalf("fromManagedInstance = %+v", got)
	}
}

func TestFromInstanceGroupPolicies(t *testing.T) {
	t.Parallel()
	got := fromInstanceGroup(&gce.InstanceGroupManager{
		Name:   "api-mig",
		Region: "https://www.googleapis.com/compute/v1/projects/p/regions/us-central1",
		UpdatePolicy: &gce.InstanceGroupManagerUpdatePolicy{Type: "PROACTIVE", MaxSurge: &gce.FixedOrPercent{Fixed: 3},
			MaxUnavailable: &gce.FixedOrPercent{Percent: 20}, MinimalAction: "REPLACE", ReplacementMethod: "SUBSTITUTE"},
		AutoHealingPolicies: []*gce.InstanceGroupManagerAutoHealingPolicy{{HealthCheck: "projects/p/global/healthChecks/hc", InitialDelaySec: 300}},
		DistributionPolicy: &gce.DistributionPolicy{TargetShape: "EVEN", Zones: []*gce.DistributionPolicyZoneConfiguration{
			{Zone: "https://www.googleapis.com/compute/v1/projects/p/zones/us-central1-a"},
			{Zone: "https://www.googleapis.com/compute/v1/projects/p/zones/us-central1-b"},
		}},
	})
	if got.MaxSurge != "3" || got.MaxUnavailable != "20%" || got.MinimalAction != "REPLACE" || got.ReplacementMethod != "SUBSTITUTE" ||
		got.AutoHealDelay != 300 || !slices.Equal(got.Zones, []string{"us-central1-a", "us-central1-b"}) || got.TargetShape != "EVEN" {
		t.Fatalf("fromInstanceGroup = %+v", got)
	}
}

func TestWithAutoscaler(t *testing.T) {
	t.Parallel()
	var g InstanceGroup
	withAutoscaler(&g, &gce.Autoscaler{Status: "ACTIVE", RecommendedSize: 4, AutoscalingPolicy: &gce.AutoscalingPolicy{
		MinNumReplicas: 2, MaxNumReplicas: 10, CpuUtilization: &gce.AutoscalingPolicyCpuUtilization{UtilizationTarget: 0.6},
	}})
	if g.AutoscaleMode != "ON" || g.AutoscaleMin != 2 || g.AutoscaleMax != 10 || g.AutoscaleCPU != 0.6 || g.Recommended != 4 {
		t.Fatalf("withAutoscaler = %+v", g)
	}
}

func TestFromInstanceSecurityAndScheduling(t *testing.T) {
	t.Parallel()
	restart, oslogin, secret := false, "TRUE", "echo hunter2"
	got := fromInstance(&gce.Instance{
		Name: "vm", Zone: "projects/p/zones/us-central1-a", CanIpForward: true, LastStopTimestamp: "2026-10-01T10:00:00Z",
		ShieldedInstanceConfig:     &gce.ShieldedInstanceConfig{EnableSecureBoot: true, EnableVtpm: true, EnableIntegrityMonitoring: true},
		ConfidentialInstanceConfig: &gce.ConfidentialInstanceConfig{ConfidentialInstanceType: "SEV_SNP"},
		GuestAccelerators:          []*gce.AcceleratorConfig{{AcceleratorCount: 2, AcceleratorType: "projects/p/zones/us-central1-a/acceleratorTypes/nvidia-l4"}},
		ServiceAccounts: []*gce.ServiceAccount{{Email: "sa@p.iam.gserviceaccount.com", Scopes: []string{
			"https://www.googleapis.com/auth/cloud-platform", "https://www.googleapis.com/auth/devstorage.read_only",
		}}},
		Scheduling: &gce.Scheduling{OnHostMaintenance: "TERMINATE", AutomaticRestart: &restart, InstanceTerminationAction: "STOP"},
		Metadata: &gce.Metadata{Items: []*gce.MetadataItems{
			{Key: "startup-script", Value: &secret},
			{Key: "enable-oslogin", Value: &oslogin},
		}},
	})
	if !got.SecureBoot || !got.VTPM || !got.Integrity || got.Confidential != "SEV_SNP" || !got.IPForwarding || got.LastStopped.IsZero() ||
		!slices.Equal(got.Accelerators, []string{"2× nvidia-l4"}) || !slices.Equal(got.Scopes, []string{"cloud-platform", "devstorage.read_only"}) ||
		got.OSLogin != "true" || got.OnHostMaintenance != "TERMINATE" || got.AutoRestart == nil || *got.AutoRestart || got.TerminationAction != "STOP" {
		t.Fatalf("fromInstance = %+v", got)
	}
	r := instanceResource("p", got)
	if r.Metadata["maintenance"] != "TERMINATE, no auto restart, STOP on preemption" || r.Metadata["shieldedVM"] != "secure boot, vTPM, integrity monitoring" ||
		r.Metadata["scopes"] != "2" || len(r.List("scopes")) != 2 || r.Metadata["ipForwarding"] != "on" || r.Metadata["osLogin"] != "true" {
		t.Fatalf("instance resource = %+v", r.Metadata)
	}
	for _, f := range r.Fields {
		if f.Value == secret {
			t.Fatal("instance metadata other than enable-oslogin must not be read")
		}
	}
}

func TestFromBackendServiceDetails(t *testing.T) {
	t.Parallel()
	got := fromBackendService(&gce.BackendService{
		Name: "web", EnableCDN: true, CdnPolicy: &gce.BackendServiceCdnPolicy{CacheMode: "CACHE_ALL_STATIC"},
		Iap: &gce.BackendServiceIAP{Enabled: true}, ConnectionDraining: &gce.ConnectionDraining{DrainingTimeoutSec: 300},
		LogConfig: &gce.BackendServiceLogConfig{Enable: true, SampleRate: 0.25},
	})
	r := backendServiceResource("p", got, "")
	if r.Metadata["iap"] != "on" || r.Metadata["cdn"] != "on, CACHE_ALL_STATIC" || r.Metadata["connectionDraining"] != "300s" ||
		r.Metadata["logging"] != "on, 25% sampled" {
		t.Fatalf("backend service = %+v", r.Metadata)
	}
}

func TestListTargetProxiesSkipsAFailedKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		deny    func(path string) bool
		want    []string
		wantErr bool // any error
		partial bool // a model.Partial error beside the items
	}{
		{name: "none denied", deny: func(string) bool { return false }, want: []string{"web-proxy"}},
		{name: "ssl denied", deny: func(p string) bool { return strings.Contains(p, "targetSslProxies") }, want: []string{"web-proxy"}, wantErr: true, partial: true},
		{name: "all denied", deny: func(string) bool { return true }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case tt.deny(r.URL.Path):
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"error":{"code":403,"message":"denied"}}`))
				case strings.HasSuffix(r.URL.Path, "/aggregated/targetHttpProxies"):
					_, _ = w.Write([]byte(`{"items":{"global":{"targetHttpProxies":[{"name":"web-proxy"}]}}}`))
				default:
					_, _ = w.Write([]byte(`{}`))
				}
			}))
			defer srv.Close()
			svc, err := gce.NewService(context.Background(), option.WithEndpoint(srv.URL), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
			if err != nil {
				t.Fatal(err)
			}
			got, err := liveAPI{svc: svc}.ListTargetProxies(context.Background(), "p")
			if (err != nil) != tt.wantErr || model.IsPartial(err) != tt.partial {
				t.Fatalf("err = %v, wantErr %v, partial %v", err, tt.wantErr, tt.partial)
			}
			var names []string
			for _, p := range got {
				names = append(names, p.Name)
			}
			if !slices.Equal(names, tt.want) {
				t.Fatalf("names = %v, want %v", names, tt.want)
			}
		})
	}
}
