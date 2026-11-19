package memorystore

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// The fakes return their instances beside a model.Partial err, as the live
// APIs do.
type fakeRedis struct {
	instances []RedisInstance
	err       error
	calls     *int
}

func (f fakeRedis) ListRedisInstances(context.Context, string) ([]RedisInstance, error) {
	if f.calls != nil {
		*f.calls++
	}
	if f.err != nil && !model.IsPartial(f.err) {
		return nil, f.err
	}
	return f.instances, f.err
}

type fakeValkey struct {
	instances []ValkeyInstance
	err       error
	calls     *int
}

func (f fakeValkey) ListValkeyInstances(context.Context, string) ([]ValkeyInstance, error) {
	if f.calls != nil {
		*f.calls++
	}
	if f.err != nil && !model.IsPartial(f.err) {
		return nil, f.err
	}
	return f.instances, f.err
}

var created = time.Date(2024, 8, 20, 13, 0, 0, 0, time.UTC)

func sampleRedis() fakeRedis {
	return fakeRedis{instances: []RedisInstance{{
		Name: "session-cache", Region: "us-central1", Zone: "us-central1-a", AlternativeZone: "us-central1-b",
		DisplayName: "Sessions", State: "READY", Tier: "STANDARD_HA", MemoryGB: 5, Version: "REDIS_7_2",
		Host: "10.30.0.4", Port: 6379, ReadEndpoint: "10.30.0.5", ReadEndpointPort: 6379, ReplicaCount: 1,
		Network: "default", ConnectMode: "PRIVATE_SERVICE_ACCESS", AuthEnabled: true,
		TransitEncryption: "SERVER_AUTHENTICATION", Persistence: "RDB", Created: created,
		Labels: map[string]string{"env": "prod"},
	}}}
}

func sampleValkey() fakeValkey {
	return fakeValkey{instances: []ValkeyInstance{{
		Name: "valkey-cache", Region: "us-central1", State: "ACTIVE", EngineVersion: "VALKEY_8_0", Mode: "CLUSTER",
		NodeType: "HIGHMEM_MEDIUM", NodeSizeGB: 13, ShardCount: 3, ReplicaCount: 1, ZoneMode: "MULTI_ZONE",
		Authorization: "IAM_AUTH", TransitEncryption: "SERVER_AUTHENTICATION", Persistence: "AOF",
		DeletionProtection: true, Created: created, Labels: map[string]string{"team": "checkout"},
		Endpoints: []Endpoint{
			{Type: "DISCOVERY", Address: "10.40.0.2", Port: 6379, Network: "default"},
			{Type: "DISCOVERY", Address: "10.40.0.3", Port: 6379, Network: "default"},
		},
	}}}
}

func TestListScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope     string
		wantCalls int
		wantN     int
	}{
		{"projects/p", 1, 1},
		{"folders/1", 0, 0},
		{"organizations/1", 0, 0},
		{"", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			calls := 0
			r, v := sampleRedis(), sampleValkey()
			r.calls, v.calls = &calls, &calls
			providers := []model.ResourceProvider{NewRedis(r), NewValkey(v)}
			for _, p := range providers {
				got, err := p.List(context.Background(), tc.scope)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != tc.wantN {
					t.Fatalf("%s: len=%d, want %d", p.Service(), len(got), tc.wantN)
				}
			}
			if calls != 2*tc.wantCalls {
				t.Fatalf("calls=%d, want %d", calls, 2*tc.wantCalls)
			}
		})
	}
}

func TestListPartial(t *testing.T) {
	t.Parallel()
	missing := model.Partial(errors.New("locations unreachable: asia-east2"))
	hard := errors.New("boom")
	cases := []struct {
		name    string
		err     error
		wantN   int
		wantErr error
		partial bool
	}{
		{name: "complete", wantN: 1},
		{name: "locations unreachable", err: missing, wantN: 1, wantErr: missing, partial: true},
		{name: "failed", err: hard, wantErr: hard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, v := sampleRedis(), sampleValkey()
			r.err, v.err = tc.err, tc.err
			for _, p := range []model.ResourceProvider{NewRedis(r), NewValkey(v)} {
				got, err := p.List(context.Background(), "projects/p")
				if len(got) != tc.wantN || !errors.Is(err, tc.wantErr) || model.IsPartial(err) != tc.partial {
					t.Fatalf("%s: len=%d err=%v, want %d and %v (partial %v)", p.Service(), len(got), err, tc.wantN, tc.wantErr, tc.partial)
				}
			}
		})
	}
}

func fieldKeys(r model.Resource) []string {
	var keys []string
	for _, f := range r.Fields {
		keys = append(keys, f.Key)
	}
	return keys
}

func TestRedisMapping(t *testing.T) {
	t.Parallel()
	got, err := NewRedis(sampleRedis()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.ID != "projects/p/locations/us-central1/instances/session-cache" || r.Name != "session-cache" ||
		r.Service != model.ServiceRedis || r.ScopeID != "projects/p" || r.Location != "us-central1" {
		t.Fatalf("identity: %+v", r)
	}
	if r.State != "READY" || r.Status != model.StatusHealthy || !r.Created.Equal(created) || r.Labels["env"] != "prod" {
		t.Fatalf("state/created/labels: %+v", r)
	}
	want := []string{"displayName", "version", "tier", "capacity", "zone", "primaryEndpoint", "readEndpoint",
		"replicas", "network", "connectMode", "auth", "transitEncryption", "persistence"}
	if got := fieldKeys(r); !slices.Equal(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	md := r.Metadata
	if md["capacity"] != "5 GB" || md["primaryEndpoint"] != "10.30.0.4:6379" ||
		md["zone"] != "us-central1-a (standby us-central1-b)" || md["auth"] != "on" {
		t.Fatalf("metadata: %+v", md)
	}
	if len(r.Facets) != 0 {
		t.Fatalf("facets: %+v", r.Facets)
	}
}

func TestValkeyMapping(t *testing.T) {
	t.Parallel()
	got, err := NewValkey(sampleValkey()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.ID != "projects/p/locations/us-central1/instances/valkey-cache" || r.Name != "valkey-cache" ||
		r.Service != model.ServiceValkey || r.ScopeID != "projects/p" || r.Location != "us-central1" {
		t.Fatalf("identity: %+v", r)
	}
	if r.State != "ACTIVE" || r.Status != model.StatusHealthy || !r.Created.Equal(created) || r.Labels["team"] != "checkout" {
		t.Fatalf("state/created/labels: %+v", r)
	}
	want := []string{"version", "mode", "nodeType", "nodeSize", "shards", "replicas", "zones", "endpoint",
		"network", "authorization", "transitEncryption", "persistence", "deletionProtection"}
	if got := fieldKeys(r); !slices.Equal(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	md := r.Metadata
	if md["nodeSize"] != "13 GB" || md["endpoint"] != "10.40.0.2:6379" || md["network"] != "default" {
		t.Fatalf("metadata: %+v", md)
	}
	if eps := r.List("endpoints"); !slices.Equal(eps, []string{"DISCOVERY  10.40.0.2:6379", "DISCOVERY  10.40.0.3:6379"}) {
		t.Fatalf("endpoints = %v", eps)
	}
}

func TestStatus(t *testing.T) {
	t.Parallel()
	redis := map[string]model.ResourceStatus{
		"READY": model.StatusHealthy, "CREATING": model.StatusWarning, "MAINTENANCE": model.StatusWarning,
		"FAILING_OVER": model.StatusWarning, "STATE_UNSPECIFIED": model.StatusUnknown,
	}
	for s, want := range redis {
		if got := redisStatus(s); got != want {
			t.Errorf("redisStatus(%s) = %v, want %v", s, got, want)
		}
	}
	valkey := map[string]model.ResourceStatus{
		"ACTIVE": model.StatusHealthy, "UPDATING": model.StatusWarning, "MIGRATING": model.StatusWarning,
		"STATE_UNSPECIFIED": model.StatusUnknown,
	}
	for s, want := range valkey {
		if got := valkeyStatus(s); got != want {
			t.Errorf("valkeyStatus(%s) = %v, want %v", s, got, want)
		}
	}
}

func TestErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	if _, err := NewRedis(fakeRedis{err: boom}).List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Fatalf("Redis List err = %v", err)
	}
	if _, err := NewValkey(fakeValkey{err: boom}).List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Fatalf("Valkey List err = %v", err)
	}
}
