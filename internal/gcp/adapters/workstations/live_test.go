package workstations

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"google.golang.org/api/googleapi"
	ws "google.golang.org/api/workstations/v1"
)

func TestDenied(t *testing.T) {
	t.Parallel()
	forbidden := &googleapi.Error{Code: http.StatusForbidden, Message: "missing workstations.workstationClusters.list"}
	cases := []struct {
		name   string
		err    error
		denied bool
	}{
		{"403", forbidden, true},
		{"wrapped 403", fmt.Errorf("list: %w", forbidden), true},
		{"404", &googleapi.Error{Code: http.StatusNotFound}, false},
		{"other", errors.New("boom"), false},
	}
	for _, tc := range cases {
		got := denied(tc.err)
		if errors.Is(got, ErrDenied) != tc.denied {
			t.Errorf("%s: denied = %v, want %v", tc.name, errors.Is(got, ErrDenied), tc.denied)
		}
		// Classify still reads the Google error.
		var gerr *googleapi.Error
		if errors.As(tc.err, &gerr) && !errors.As(got, &gerr) {
			t.Errorf("%s: lost the googleapi.Error", tc.name)
		}
	}
}

func TestFromWorkstationName(t *testing.T) {
	t.Parallel()
	w := fromWorkstation(&ws.Workstation{
		Name:                  "projects/p/locations/us-central1/workstationClusters/dev/workstationConfigs/code/workstations/alice",
		State:                 "STATE_RUNNING",
		StartTime:             "2025-04-01T09:00:00Z",
		PersistentDirectories: []*ws.WorkstationPersistentDirectory{{MountPath: "/home", SizeGb: 200}},
	})
	if w.Name != "alice" || w.Config != "code" || w.Cluster != "dev" || w.Location != "us-central1" || w.State != "STATE_RUNNING" ||
		w.Started.IsZero() || len(w.Directories) != 1 || w.Directories[0].SizeGB != 200 {
		t.Fatalf("workstation %+v", w)
	}
}

func TestFromConfig(t *testing.T) {
	t.Parallel()
	c := fromConfig(&ws.WorkstationConfig{
		Name: "projects/p/locations/us-central1/workstationClusters/dev/workstationConfigs/code",
		Host: &ws.Host{GceInstance: &ws.GceInstance{
			MachineType: "e2-standard-8", PoolSize: 2, DisablePublicIpAddresses: true,
			Accelerators: []*ws.Accelerator{{Type: "nvidia-tesla-t4", Count: 1}},
		}},
		Container: &ws.Container{Image: "code-oss", Env: map[string]string{"A": "1"}},
		PersistentDirectories: []*ws.PersistentDirectory{
			{MountPath: "/home", GcePd: &ws.GceRegionalPersistentDisk{SizeGb: 200, DiskType: "pd-ssd", ReclaimPolicy: "DELETE"}},
			{MountPath: "/data", GceHd: &ws.GceHyperdiskBalancedHighAvailability{SizeGb: 100}},
		},
		Conditions: []*ws.Status{{Message: "quota exceeded"}, {}},
		Degraded:   true,
	})
	if c.Name != "code" || c.Cluster != "dev" || c.Location != "us-central1" || c.MachineType != "e2-standard-8" ||
		c.PoolSize != 2 || !c.NoPublicIP || c.Image != "code-oss" || c.Env["A"] != "1" || !c.Degraded {
		t.Fatalf("config %+v", c)
	}
	if !slices.Equal(c.Accelerators, []string{"nvidia-tesla-t4 ×1"}) || !slices.Equal(c.Conditions, []string{"quota exceeded"}) {
		t.Fatalf("accelerators %q conditions %q", c.Accelerators, c.Conditions)
	}
	want := []Directory{
		{MountPath: "/home", SizeGB: 200, DiskType: "pd-ssd", ReclaimPolicy: "DELETE"},
		{MountPath: "/data", SizeGB: 100, DiskType: "hyperdisk-balanced-ha"},
	}
	if !slices.Equal(c.Directories, want) {
		t.Fatalf("directories %+v", c.Directories)
	}
}

func TestFromConfigSecurity(t *testing.T) {
	t.Parallel()
	got := fromConfig(&ws.WorkstationConfig{
		Name: "projects/p/locations/us-central1/workstationClusters/c/workstationConfigs/k",
		Host: &ws.Host{GceInstance: &ws.GceInstance{
			ShieldedInstanceConfig:     &ws.GceShieldedInstanceConfig{EnableSecureBoot: true, EnableIntegrityMonitoring: true},
			ConfidentialInstanceConfig: &ws.GceConfidentialInstanceConfig{EnableConfidentialCompute: true},
			DisableSsh:                 true,
			BoostConfigs:               []*ws.BoostConfig{{Id: "gpu", MachineType: "n1-standard-8", Accelerators: []*ws.Accelerator{{Type: "nvidia-tesla-t4", Count: 1}}}},
		}},
		AllowedPorts:          []*ws.PortRange{{First: 22, Last: 22}, {First: 1024, Last: 65535}},
		DisableTcpConnections: true,
		EnableAuditAgent:      true,
	})
	if !slices.Equal(got.ShieldedVM, []string{"secure boot", "integrity monitoring"}) || !got.Confidential || !got.NoSSH || !got.NoTCP || !got.AuditAgent ||
		!slices.Equal(got.AllowedPorts, []string{"22", "1024-65535"}) || !slices.Equal(got.Boosts, []string{"gpu  n1-standard-8, nvidia-tesla-t4 ×1"}) {
		t.Fatalf("fromConfig = %+v", got)
	}
	r := configResource("p", got)
	if r.Metadata["ssh"] != "off" || r.Metadata["allowedPorts"] != "22, 1024-65535" || r.Metadata["boostConfigs"] != "1" || len(r.List("boostConfigs")) != 1 {
		t.Fatalf("config resource = %+v", r.Metadata)
	}
}

func TestFromWorkstationVM(t *testing.T) {
	t.Parallel()
	got := fromWorkstation(&ws.Workstation{
		Name:        "projects/p/locations/us-central1/workstationClusters/c/workstationConfigs/k/workstations/w",
		State:       "STATE_RUNNING",
		RuntimeHost: &ws.RuntimeHost{GceInstanceHost: &ws.GceInstanceHost{Name: "workstations-abc123", Zone: "us-central1-a", Id: "123"}},
	})
	if got.VM != "us-central1-a/workstations-abc123" {
		t.Fatalf("VM = %q", got.VM)
	}
	if r := workstationResource("p", got); r.Metadata["vm"] != "us-central1-a/workstations-abc123" {
		t.Fatalf("workstation resource = %+v", r.Metadata)
	}
}
