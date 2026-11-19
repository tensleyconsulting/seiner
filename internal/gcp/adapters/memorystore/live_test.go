package memorystore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloud.google.com/go/memorystore/apiv1/memorystorepb"
	"google.golang.org/api/option"
	redis "google.golang.org/api/redis/v1"
	"google.golang.org/genproto/googleapis/type/dayofweek"
	"google.golang.org/genproto/googleapis/type/timeofday"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestFromRedis(t *testing.T) {
	t.Parallel()
	got := fromRedis(&redis.Instance{
		Name: "projects/p/locations/us-central1/instances/cache", LocationId: "us-central1-a",
		CurrentLocationId: "us-central1-b", AlternativeLocationId: "us-central1-a", Tier: "STANDARD_HA",
		AuthorizedNetwork: "projects/p/global/networks/default", CreateTime: "2024-08-20T13:00:00Z",
		PersistenceConfig: &redis.PersistenceConfig{PersistenceMode: "RDB"},
	})
	if got.Name != "cache" || got.Region != "us-central1" || got.Zone != "us-central1-b" ||
		got.AlternativeZone != "us-central1-a" || got.Network != "default" || got.Persistence != "RDB" ||
		!got.Created.Equal(time.Date(2024, 8, 20, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("fromRedis = %+v", got)
	}
	if basic := fromRedis(&redis.Instance{Tier: "BASIC", LocationId: "us-east1-b", AlternativeLocationId: "us-east1-c"}); basic.Zone != "us-east1-b" || basic.AlternativeZone != "" {
		t.Fatalf("basic tier = %+v", basic)
	}
}

func TestFromValkey(t *testing.T) {
	t.Parallel()
	created := time.Date(2025, 6, 11, 15, 20, 0, 0, time.UTC)
	replicas := int32(1)
	got := fromValkey(&memorystorepb.Instance{
		Name:       "projects/p/locations/us-central1/instances/vk",
		CreateTime: timestamppb.New(created),
		State:      memorystorepb.Instance_ACTIVE,
		Mode:       memorystorepb.Instance_CLUSTER,
		NodeType:   memorystorepb.Instance_NODE_TYPE_UNSPECIFIED,
		NodeConfig: &memorystorepb.NodeConfig{SizeGb: 13},
		ShardCount: 3, ReplicaCount: &replicas,
		Endpoints: []*memorystorepb.Instance_InstanceEndpoint{{
			Connections: []*memorystorepb.Instance_ConnectionDetail{{
				Connection: &memorystorepb.Instance_ConnectionDetail_PscAutoConnection{PscAutoConnection: &memorystorepb.PscAutoConnection{
					IpAddress: "10.40.0.2", Ports: &memorystorepb.PscAutoConnection_Port{Port: 6379},
					Network: "projects/p/global/networks/default", ConnectionType: memorystorepb.ConnectionType_CONNECTION_TYPE_PRIMARY,
				}},
			}},
		}},
	})
	if got.Name != "vk" || got.Region != "us-central1" || got.State != "ACTIVE" || got.Mode != "CLUSTER" ||
		got.NodeType != "" || got.NodeSizeGB != 13 || got.ShardCount != 3 || got.ReplicaCount != 1 || !got.Created.Equal(created) {
		t.Fatalf("fromValkey = %+v", got)
	}
	want := Endpoint{Type: "PRIMARY", Address: "10.40.0.2", Port: 6379, Network: "default"}
	if len(got.Endpoints) != 1 || got.Endpoints[0] != want {
		t.Fatalf("endpoints = %+v, want %+v", got.Endpoints, want)
	}

	legacy := fromValkey(&memorystorepb.Instance{DiscoveryEndpoints: []*memorystorepb.DiscoveryEndpoint{{Address: "10.0.0.9", Port: 6379}}})
	if len(legacy.Endpoints) != 1 || legacy.Endpoints[0].Type != "DISCOVERY" {
		t.Fatalf("discovery fallback = %+v", legacy.Endpoints)
	}
}

func TestConnType(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"CONNECTION_TYPE_DISCOVERY":   "DISCOVERY",
		"CONNECTION_TYPE_READER":      "READER",
		"CONNECTION_TYPE_UNSPECIFIED": "",
	} {
		if got := connType(in); got != want {
			t.Errorf("connType(%s) = %q, want %q", in, got, want)
		}
	}
}

// TestValkeyListsAllLocationsOverREST guards the transport: the gRPC
// frontend answers the "-" location wildcard with a 404 (surfacing as
// Unimplemented), so the live client must send it as a REST call.
func TestValkeyListsAllLocationsOverREST(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		unreachable string
	}{
		{name: "complete", unreachable: `[]`},
		{name: "locations unreachable", unreachable: `["asia-east2"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"instances":[{"name":"projects/p/locations/us-central1/instances/vk","state":"ACTIVE","mode":"CLUSTER"}],"unreachable":` + tc.unreachable + `}`))
			}))
			defer srv.Close()

			valkey := NewLazyValkey(nil, option.WithEndpoint(srv.URL), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
			got, err := valkey.List(context.Background(), "projects/p")
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if gotPath != "/v1/projects/p/locations/-/instances" {
				t.Fatalf("request path = %q, want the REST list with the - wildcard", gotPath)
			}
			if len(got) != 1 || got[0].Name != "vk" || got[0].Location != "us-central1" || got[0].State != "ACTIVE" {
				t.Fatalf("instances = %+v", got)
			}
		})
	}
}

// TestRedisUnreachableAcrossPages ignores unreachable locations across pages
// while returning every page's instances.
func TestRedisUnreachableAcrossPages(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		pages []string
	}{
		{
			name: "complete",
			pages: []string{
				`{"instances":[{"name":"projects/p/locations/us-central1/instances/a"}],"nextPageToken":"2"}`,
				`{"instances":[{"name":"projects/p/locations/us-east1/instances/b"}]}`,
			},
		},
		{
			name: "locations unreachable",
			pages: []string{
				`{"instances":[{"name":"projects/p/locations/us-central1/instances/a"}],"unreachable":["asia-east2"],"nextPageToken":"2"}`,
				`{"instances":[{"name":"projects/p/locations/us-east1/instances/b"}],"unreachable":["asia-east2","europe-west9"]}`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				page := tc.pages[0]
				if r.URL.Query().Get("pageToken") == "2" {
					page = tc.pages[1]
				}
				_, _ = w.Write([]byte(page))
			}))
			defer srv.Close()

			rd := NewLazyRedis(option.WithEndpoint(srv.URL), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
			got, err := rd.List(context.Background(), "projects/p")
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if len(got) != 2 {
				t.Fatalf("instances = %+v, want both pages", got)
			}
		})
	}
}

func TestFromRedisMaintenanceAndSnapshots(t *testing.T) {
	t.Parallel()
	got := fromRedis(&redis.Instance{
		Name:              "projects/p/locations/us-central1/instances/cache",
		PersistenceConfig: &redis.PersistenceConfig{PersistenceMode: "RDB", RdbSnapshotPeriod: "TWELVE_HOURS", RdbNextSnapshotTime: "2026-11-01T00:00:00Z"},
		MaintenancePolicy: &redis.MaintenancePolicy{WeeklyMaintenanceWindow: []*redis.WeeklyMaintenanceWindow{
			{Day: "SUNDAY", StartTime: &redis.TimeOfDay{Hours: 3}, Duration: "5400s"},
		}},
		MaintenanceSchedule: &redis.MaintenanceSchedule{StartTime: "2026-11-08T03:00:00Z"},
	})
	if got.Maintenance != "SUNDAY 03:00 UTC (1h30m)" || got.NextMaintenance.IsZero() || got.RDBPeriod != "TWELVE_HOURS" || got.NextSnapshot.IsZero() {
		t.Fatalf("fromRedis = %+v", got)
	}
	r := redisResource("p", got)
	if r.Metadata["persistence"] != "RDB, every twelve hours" || r.Metadata["nextMaintenance"] != "2026-11-08 03:00 UTC" {
		t.Fatalf("redis resource = %+v", r.Metadata)
	}
}

func TestFromValkeyBackupsAndReplication(t *testing.T) {
	t.Parallel()
	key := "projects/p/locations/us-central1/keyRings/r/cryptoKeys/vk"
	got := fromValkey(&memorystorepb.Instance{
		Name:   "projects/p/locations/us-east1/instances/vk-dr",
		KmsKey: &key,
		AutomatedBackupConfig: &memorystorepb.AutomatedBackupConfig{
			AutomatedBackupMode: memorystorepb.AutomatedBackupConfig_ENABLED,
			Schedule: &memorystorepb.AutomatedBackupConfig_FixedFrequencySchedule_{FixedFrequencySchedule: &memorystorepb.AutomatedBackupConfig_FixedFrequencySchedule{
				StartTime: &timeofday.TimeOfDay{Hours: 2},
			}},
			Retention: durationpb.New(7 * 24 * time.Hour),
		},
		MaintenancePolicy: &memorystorepb.MaintenancePolicy{WeeklyMaintenanceWindow: []*memorystorepb.WeeklyMaintenanceWindow{
			{Day: dayofweek.DayOfWeek_SATURDAY, StartTime: &timeofday.TimeOfDay{Hours: 4, Minutes: 30}},
		}},
		CrossInstanceReplicationConfig: &memorystorepb.CrossInstanceReplicationConfig{
			InstanceRole:    memorystorepb.CrossInstanceReplicationConfig_SECONDARY,
			PrimaryInstance: &memorystorepb.CrossInstanceReplicationConfig_RemoteInstance{Instance: "projects/p/locations/us-central1/instances/vk"},
		},
	})
	if got.KMSKey != key || got.Backups != "on, daily 02:00 UTC, keep 7 days" || got.Maintenance != "SATURDAY 04:30 UTC" ||
		got.ReplicationRole != "SECONDARY" || got.ReplicationPeer != "us-central1/vk" {
		t.Fatalf("fromValkey = %+v", got)
	}
	if r := valkeyResource("p", got); r.Metadata["replication"] != "secondary of us-central1/vk" || r.Metadata["cmek"] != key {
		t.Fatalf("valkey resource = %+v", r.Metadata)
	}
}
