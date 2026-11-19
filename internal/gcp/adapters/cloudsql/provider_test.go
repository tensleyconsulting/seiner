package cloudsql

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	instances []Instance
	databases map[string][]Database // keyed by instance name
	users     map[string][]User
	backups   map[string][]BackupRun
	err       error // returned from every method when set
	calls     *int
}

func (f fakeAPI) called() {
	if f.calls != nil {
		*f.calls++
	}
}

func (f fakeAPI) ListInstances(context.Context, string) ([]Instance, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	return f.instances, nil
}

func (f fakeAPI) ListDatabases(_ context.Context, _, instance string) ([]Database, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	return f.databases[instance], nil
}

func (f fakeAPI) ListUsers(_ context.Context, _, instance string) ([]User, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	return f.users[instance], nil
}

func (f fakeAPI) ListBackupRuns(_ context.Context, _, instance string, limit int) ([]BackupRun, bool, error) {
	f.called()
	if f.err != nil {
		return nil, false, f.err
	}
	bs := f.backups[instance]
	if len(bs) > limit {
		return bs[:limit], true, nil
	}
	return bs, false, nil
}

var created = time.Date(2024, 5, 2, 8, 0, 0, 0, time.UTC)

func sample() fakeAPI {
	return fakeAPI{
		instances: []Instance{
			{
				Name: "pg", Region: "us-central1", Zone: "us-central1-a", SecondaryZone: "us-central1-c",
				State: "RUNNABLE", ActivationPolicy: "ALWAYS", DatabaseVersion: "POSTGRES_16", Edition: "ENTERPRISE_PLUS",
				Tier: "db-perf-optimized-N-4", Availability: "REGIONAL", InstanceType: "CLOUD_SQL_INSTANCE",
				Replicas: []string{"pg-replica"}, ConnectionName: "p:us-central1:pg", PrivateIP: "10.20.0.3",
				PrivateNetwork: "default", DiskSizeGB: 250, DiskType: "PD_SSD", AutoResize: true, Backups: true,
				PITR: true, DeletionProtect: true, Created: created, Labels: map[string]string{"env": "prod"},
				SSLMode: "ENCRYPTED_ONLY", KMSKey: "projects/p/locations/us-central1/keyRings/r/cryptoKeys/sql",
				MaintenanceSet: true, MaintenanceDay: 7, MaintenanceHour: 3, UpdateTrack: "stable",
				NextMaintenance: time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC), DenyMaintenance: []string{"2026-12-15 – 2027-01-05"},
				BackupStart: "02:00", RetainedBackups: 14, RetentionUnit: "COUNT", LogRetentionDays: 7, QueryInsights: true,
				Flags: []string{"max_connections=500", "log_min_duration_statement=250"},
			},
			{
				Name: "pg-replica", Region: "us-central1", State: "RUNNABLE", InstanceType: "READ_REPLICA_INSTANCE",
				Primary: "pg", DatabaseVersion: "POSTGRES_16", PublicIP: "198.51.100.90", PublicIPEnabled: true,
				AuthorizedNetworks: []string{"office 203.0.113.0/24", "198.51.100.7/32 (expires 2026-12-01)"}, RequireSSL: true,
			},
		},
		databases: map[string][]Database{
			"pg": {
				{Name: "postgres", Charset: "UTF8", Collation: "en_US.UTF8"},
				{Name: "app", Charset: "UTF8", Collation: "en_US.UTF8"},
			},
		},
		users: map[string][]User{
			"pg": {
				{Name: "postgres", Type: "BUILT_IN", Roles: []string{"cloudsqlsuperuser"}},
				{Name: "app@p.iam", Type: "CLOUD_IAM_SERVICE_ACCOUNT", IAMEmail: "app@p.iam.gserviceaccount.com"},
				{Name: "report", Host: "%", Type: "BUILT_IN", Locked: true, PasswordExpires: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
			},
		},
		backups: map[string][]BackupRun{
			"pg": {
				{ID: "1730257200000", Status: "SUCCESSFUL", Type: "AUTOMATED", Kind: "SNAPSHOT", Location: "us",
					Enqueued: time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC), Started: time.Date(2026, 10, 30, 2, 0, 5, 0, time.UTC),
					Ended: time.Date(2026, 10, 30, 2, 4, 35, 0, time.UTC)},
				{ID: "1730170800000", Status: "FAILED", Type: "ON_DEMAND", Error: "disk full", Description: "before migration",
					Enqueued: time.Date(2026, 10, 29, 2, 0, 0, 0, time.UTC)},
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

func TestInstanceMapping(t *testing.T) {
	t.Parallel()
	got, err := New(sample()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.ID != "projects/p/instances/pg" || r.Name != "pg" || r.Service != model.ServiceCloudSQL ||
		r.ScopeID != "projects/p" || r.Location != "us-central1" {
		t.Fatalf("identity: %+v", r)
	}
	if r.State != "RUNNABLE" || r.Status != model.StatusHealthy || !r.Created.Equal(created) || r.Labels["env"] != "prod" {
		t.Fatalf("state/created/labels: %+v", r)
	}
	var keys []string
	for _, f := range r.Fields {
		keys = append(keys, f.Key)
	}
	want := []string{"engine", "edition", "tier", "role", "availability", "zone", "connectionName", "publicAccess", "privateIP",
		"network", "ssl", "kmsKey", "storage", "backups", "backupWindow", "backupRetention", "logRetention", "maintenance",
		"nextMaintenance", "denyMaintenance", "queryInsights", "flags", "deletionProtection", "replicas"}
	if !slices.Equal(keys, want) {
		t.Fatalf("fields = %v, want %v", keys, want)
	}
	md := r.Metadata
	if md["zone"] != "us-central1-a (standby us-central1-c)" || md["storage"] != "250 GB PD_SSD, auto-resize" ||
		md["backups"] != "on, point-in-time recovery" || md["role"] != "primary" || md["publicAccess"] != "off" ||
		md["ssl"] != "ENCRYPTED_ONLY" || md["maintenance"] != "Sun 03:00 UTC, stable" || md["nextMaintenance"] != "2026-11-08 03:00 UTC" ||
		md["backupWindow"] != "02:00 UTC" || md["backupRetention"] != "14 backups" || md["logRetention"] != "7 days" ||
		md["flags"] != "2" || md["queryInsights"] != "on" {
		t.Fatalf("metadata: %+v", md)
	}
	if !slices.Equal(r.List("flags"), []string{"max_connections=500", "log_min_duration_statement=250"}) {
		t.Fatalf("flags list: %+v", r.Lists)
	}
	var facets []string
	for _, f := range r.Facets {
		facets = append(facets, f.ID)
	}
	if want := []string{model.FacetDatabases, model.FacetUsers, model.FacetBackups}; !slices.Equal(facets, want) {
		t.Fatalf("facets = %v, want %v", facets, want)
	}
	rep := got[1]
	if len(rep.Facets) != 2 || rep.Facets[1].ID != model.FacetUsers {
		t.Fatalf("a replica has no Backups facet: %+v", rep.Facets)
	}
	if rep.Metadata["role"] != "read replica of pg" || rep.Metadata["publicAccess"] != "on" || rep.Metadata["authorizedNetworks"] != "2" ||
		rep.Metadata["ssl"] != "SSL required" || len(rep.List("authorizedNetworks")) != 2 {
		t.Fatalf("replica: %+v", rep.Metadata)
	}
}

func TestInstanceStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		state, activation string
		want              model.ResourceStatus
	}{
		{"RUNNABLE", "ALWAYS", model.StatusHealthy},
		{"RUNNABLE", "NEVER", model.StatusWarning},
		{"PENDING_CREATE", "", model.StatusWarning},
		{"MAINTENANCE", "", model.StatusWarning},
		{"SUSPENDED", "", model.StatusError},
		{"FAILED", "", model.StatusError},
		{"SQL_INSTANCE_STATE_UNSPECIFIED", "", model.StatusUnknown},
	}
	for _, tc := range cases {
		if got := instanceStatus(tc.state, tc.activation); got != tc.want {
			t.Errorf("instanceStatus(%s, %s) = %v, want %v", tc.state, tc.activation, got, tc.want)
		}
	}
	stopped := instanceResource("p", Instance{Name: "x", State: "RUNNABLE", ActivationPolicy: "NEVER"})
	if stopped.Metadata["serving"] != "stopped" {
		t.Fatalf("stopped instance metadata: %+v", stopped.Metadata)
	}
}

func TestDatabases(t *testing.T) {
	t.Parallel()
	p := New(sample())
	insts, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	parent := insts[0]
	got, truncated, err := p.ListRelated(context.Background(), parent, model.FacetDatabases)
	if err != nil || truncated || len(got) != 2 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	db := got[1]
	if db.ID != "projects/p/instances/pg/databases/app" || db.Name != "app" || db.Service != model.ServiceCloudSQLDatabase ||
		db.ParentID != parent.ID || db.FacetID != model.FacetDatabases || db.ScopeID != "projects/p" ||
		db.Location != "us-central1" || db.Metadata["charset"] != "UTF8" {
		t.Fatalf("database: %+v", db)
	}
}

func TestMaintenanceWindow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   Instance
		want string
	}{
		{Instance{}, ""},
		{Instance{MaintenanceSet: true}, ""},
		{Instance{UpdateTrack: "canary"}, "no window, canary"},
		{Instance{UpdateTrack: "SQL_UPDATE_TRACK_UNSPECIFIED"}, ""},
		{Instance{MaintenanceSet: true, MaintenanceDay: 1, MaintenanceHour: 22, UpdateTrack: "week5"}, "Mon 22:00 UTC, week5"},
	}
	for _, tc := range cases {
		if got := maintenanceWindow(tc.in); got != tc.want {
			t.Errorf("maintenanceWindow(%+v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUsers(t *testing.T) {
	t.Parallel()
	inst := instanceResource("p", sample().instances[0])
	got, truncated, err := New(sample()).ListRelated(context.Background(), inst, model.FacetUsers)
	if err != nil || truncated || len(got) != 3 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	pg := got[0]
	if pg.ID != inst.ID+"/users/postgres" || pg.Service != model.ServiceCloudSQLUser || pg.ParentID != inst.ID ||
		pg.FacetID != model.FacetUsers || pg.ScopeID != "projects/p" || pg.Location != "us-central1" ||
		pg.Status != model.StatusUnknown || pg.Badge != "" || !slices.Equal(pg.List("roles"), []string{"cloudsqlsuperuser"}) {
		t.Fatalf("built-in user: %+v", pg)
	}
	if sa := got[1]; sa.Badge != "IAM SA" || sa.Metadata["iamEmail"] != "app@p.iam.gserviceaccount.com" {
		t.Fatalf("IAM user: %+v", sa)
	}
	r := got[2]
	if r.Name != "report@%" || r.ID != inst.ID+"/users/report@%25" || r.Status != model.StatusWarning || r.State != "LOCKED" ||
		r.Metadata["passwordExpires"] != "2026-12-01 00:00 UTC" {
		t.Fatalf("locked MySQL-style user: %+v", r)
	}
	if d := userResource("", inst.ID, User{Name: "sa", Disabled: true, Locked: true}); d.State != "DISABLED" {
		t.Fatalf("disabled wins over locked: %+v", d)
	}
}

func TestBackups(t *testing.T) {
	t.Parallel()
	inst := instanceResource("p", sample().instances[0])
	got, truncated, err := New(sample()).ListRelated(context.Background(), inst, model.FacetBackups)
	if err != nil || truncated || len(got) != 2 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	b := got[0]
	if b.ID != inst.ID+"/backupRuns/1730257200000" || b.Name != "2026-10-30 02:00 UTC" || b.Service != model.ServiceCloudSQLBackup ||
		b.ParentID != inst.ID || b.FacetID != model.FacetBackups || b.ScopeID != "projects/p" ||
		b.Status != model.StatusHealthy || b.Badge != "SUCCESSFUL" || b.Metadata["duration"] != "4m30s" || b.Metadata["type"] != "AUTOMATED" {
		t.Fatalf("backup: %+v", b)
	}
	// The backup kind must not land on model.MetaKind, which hides it.
	shown := false
	for _, fl := range model.Normalize(b).Fields {
		shown = shown || (fl.Key == "backupKind" && fl.Value == "SNAPSHOT")
	}
	if !shown || b.Metadata[model.MetaKind] != "" {
		t.Fatalf("backup kind not shown: %+v", b.Fields)
	}
	f := got[1]
	if f.Name != "2026-10-29 02:00 UTC" || f.Status != model.StatusError || f.Metadata["error"] != "disk full" ||
		f.Description != "before migration" || f.Metadata["duration"] != "" {
		t.Fatalf("failed backup: %+v", f)
	}
	api := sample()
	api.backups["pg"] = make([]BackupRun, maxBackups+1)
	if got, truncated, _ := New(api).ListRelated(context.Background(), inst, model.FacetBackups); !truncated || len(got) != maxBackups {
		t.Fatalf("len=%d truncated=%v", len(got), truncated)
	}
}

func TestListRelatedIgnoresOthers(t *testing.T) {
	t.Parallel()
	inst := instanceResource("p", sample().instances[0])
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: inst.ID, Service: model.ServiceFirestore}, model.FacetDatabases},
		{"other facet", inst, model.FacetRecords},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			api := sample()
			api.calls = &calls
			got, truncated, err := New(api).ListRelated(context.Background(), tc.parent, tc.facet)
			if got != nil || truncated || err != nil || calls != 0 {
				t.Fatalf("got=%v truncated=%v err=%v calls=%d", got, truncated, err, calls)
			}
		})
	}
}

func TestErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	p := New(fakeAPI{err: boom})
	if _, err := p.List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Fatalf("List err = %v", err)
	}
	inst := instanceResource("p", Instance{Name: "pg"})
	for _, f := range []string{model.FacetDatabases, model.FacetUsers, model.FacetBackups} {
		if _, _, err := p.ListRelated(context.Background(), inst, f); !errors.Is(err, boom) {
			t.Fatalf("ListRelated(%s) err = %v", f, err)
		}
	}
}
