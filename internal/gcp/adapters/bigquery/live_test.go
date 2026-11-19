package bigquery

import (
	"slices"
	"testing"
	"time"

	bq "google.golang.org/api/bigquery/v2"
	connection "google.golang.org/api/bigqueryconnection/v1"
	transfer "google.golang.org/api/bigquerydatatransfer/v1"
	reservation "google.golang.org/api/bigqueryreservation/v1"
)

func TestFromAccess(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   bq.DatasetAccess
		want Access
	}{
		{"user", bq.DatasetAccess{Role: "READER", UserByEmail: "a@x.com"}, Access{Role: "READER", Member: "user:a@x.com"}},
		{"service account", bq.DatasetAccess{Role: "WRITER", UserByEmail: "etl@p.iam.gserviceaccount.com"}, Access{Role: "WRITER", Member: "serviceAccount:etl@p.iam.gserviceaccount.com"}},
		{"group", bq.DatasetAccess{Role: "READER", GroupByEmail: "g@x.com"}, Access{Role: "READER", Member: "group:g@x.com"}},
		{"domain", bq.DatasetAccess{Role: "READER", Domain: "x.com"}, Access{Role: "READER", Member: "domain:x.com"}},
		{"iam member", bq.DatasetAccess{Role: "roles/bigquery.dataViewer", IamMember: "principalSet://iam.googleapis.com/x"}, Access{Role: "roles/bigquery.dataViewer", Member: "principalSet://iam.googleapis.com/x"}},
		{"project owners", bq.DatasetAccess{Role: "OWNER", SpecialGroup: "projectOwners"}, Access{Role: "OWNER", Member: "projectOwner:p"}},
		{"all authenticated", bq.DatasetAccess{Role: "READER", SpecialGroup: "allAuthenticatedUsers"}, Access{Role: "READER", Member: "allAuthenticatedUsers"}},
		{"condition", bq.DatasetAccess{Role: "READER", UserByEmail: "a@x.com", Condition: &bq.Expr{Title: "Until December"}}, Access{Role: "READER", Member: "user:a@x.com", Condition: "Until December"}},
		{"untitled condition", bq.DatasetAccess{Role: "READER", UserByEmail: "a@x.com", Condition: &bq.Expr{Expression: "request.time < x"}}, Access{Role: "READER", Member: "user:a@x.com", Condition: "request.time < x"}},
		{"view", bq.DatasetAccess{View: &bq.TableReference{ProjectId: "p", DatasetId: "d", TableId: "v"}}, Access{Authorized: "view p.d.v"}},
		{"routine", bq.DatasetAccess{Routine: &bq.RoutineReference{ProjectId: "p", DatasetId: "d", RoutineId: "r"}}, Access{Authorized: "routine p.d.r"}},
		{"dataset", bq.DatasetAccess{Dataset: &bq.DatasetAccessEntry{Dataset: &bq.DatasetReference{ProjectId: "p", DatasetId: "d"}}}, Access{Authorized: "dataset p.d"}},
	}
	for _, tc := range cases {
		if got := fromAccess(&tc.in, "p"); got != tc.want {
			t.Errorf("%s: %+v want %+v", tc.name, got, tc.want)
		}
	}
}

func TestFromDatasetNamesProjectGroups(t *testing.T) {
	t.Parallel()
	d := fromDataset(&bq.Dataset{
		DatasetReference:               &bq.DatasetReference{ProjectId: "p", DatasetId: "shared"},
		Type:                           "LINKED",
		LinkedDatasetSource:            &bq.LinkedDatasetSource{SourceDataset: &bq.DatasetReference{ProjectId: "src", DatasetId: "market"}},
		CreationTime:                   1714554000000,
		DefaultTableExpirationMs:       86400000,
		DefaultPartitionExpirationMs:   3600000,
		MaxTimeTravelHours:             48,
		DefaultEncryptionConfiguration: &bq.EncryptionConfiguration{KmsKeyName: "k"},
		Access:                         []*bq.DatasetAccess{{Role: "READER", SpecialGroup: "projectReaders"}, nil},
	})
	if d.ID != "shared" || !d.Detailed || d.LinkedSource != "src.market" || d.KMSKey != "k" ||
		d.TableExpiration != 24*time.Hour || d.PartitionExpiration != time.Hour || d.TimeTravel != 48*time.Hour ||
		!d.Created.Equal(time.Date(2024, 5, 1, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("dataset %+v", d)
	}
	if len(d.Access) != 1 || d.Access[0].Member != "projectViewer:p" {
		t.Errorf("access %+v", d.Access)
	}
}

func TestFromTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   bq.TableListTables
		want Table
	}{
		{"time partitioned", bq.TableListTables{TableReference: &bq.TableReference{TableId: "events"}, Type: "TABLE",
			TimePartitioning: &bq.TimePartitioning{Type: "DAY", Field: "event_date", RequirePartitionFilter: true}, Clustering: &bq.Clustering{Fields: []string{"user_id"}}},
			Table{ID: "events", Type: "TABLE", Partitioning: "DAY on event_date", PartitionFilter: true, Clustering: []string{"user_id"}}},
		{"ingestion time", bq.TableListTables{TableReference: &bq.TableReference{TableId: "raw"}, TimePartitioning: &bq.TimePartitioning{Type: "HOUR"}},
			Table{ID: "raw", Partitioning: "HOUR"}},
		{"range", bq.TableListTables{TableReference: &bq.TableReference{TableId: "orders"}, RangePartitioning: &bq.RangePartitioning{Field: "id"}},
			Table{ID: "orders", Partitioning: "RANGE on id"}},
		{"legacy view", bq.TableListTables{TableReference: &bq.TableReference{TableId: "v"}, Type: "VIEW", View: &bq.TableListTablesView{UseLegacySql: true}, ExpirationTime: 1714554000000},
			Table{ID: "v", Type: "VIEW", LegacySQL: true, Expires: time.Date(2024, 5, 1, 9, 0, 0, 0, time.UTC)}},
	}
	for _, tc := range cases {
		got := fromTable(&tc.in)
		if got.ID != tc.want.ID || got.Type != tc.want.Type || got.Partitioning != tc.want.Partitioning || got.PartitionFilter != tc.want.PartitionFilter ||
			got.LegacySQL != tc.want.LegacySQL || !slices.Equal(got.Clustering, tc.want.Clustering) || !got.Expires.Equal(tc.want.Expires) || !got.Created.IsZero() {
			t.Errorf("%s: %+v want %+v", tc.name, got, tc.want)
		}
	}
}

func TestFromReservationAndCommitment(t *testing.T) {
	t.Parallel()
	r := fromReservation(&reservation.Reservation{Name: "projects/p/locations/US/reservations/prod", SlotCapacity: 100,
		Autoscale: &reservation.Autoscale{MaxSlots: 200, CurrentSlots: 50}, CreationTime: "2024-05-01T09:00:00Z"})
	if r.Name != "prod" || r.Location != "US" || r.Slots != 100 || r.MaxSlots != 200 || r.CurrentSlots != 50 || r.Created.IsZero() {
		t.Errorf("reservation %+v", r)
	}
	c := fromCommitment(&reservation.CapacityCommitment{Name: "projects/p/locations/EU/capacityCommitments/123", SlotCount: 100, State: "FAILED",
		FailureStatus: &reservation.Status{Message: "no quota"}})
	if c.Name != "123" || c.Location != "EU" || c.Slots != 100 || c.Failure != "no quota" {
		t.Errorf("commitment %+v", c)
	}
}

func TestFromTransfer(t *testing.T) {
	t.Parallel()
	tr := fromTransfer(&transfer.TransferConfig{Name: "projects/p/locations/us/transferConfigs/abc", DisplayName: "Rollup", DataSourceId: "scheduled_query",
		Schedule: "every 24 hours", ScheduleOptions: &transfer.ScheduleOptions{DisableAutoScheduling: true}, OwnerInfo: &transfer.UserInfo{Email: "a@x.com"},
		Error: &transfer.Status{Message: "boom"}, EncryptionConfiguration: &transfer.EncryptionConfiguration{KmsKeyName: "k"}})
	if tr.ID != "abc" || tr.Location != "us" || tr.Schedule != "on demand" || tr.Owner != "a@x.com" || tr.Error != "boom" || tr.KMSKey != "k" {
		t.Errorf("transfer %+v", tr)
	}
}

func TestFromConnection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                   string
		in                     connection.Connection
		kind, identity, target string
	}{
		{"cloud sql", connection.Connection{CloudSql: &connection.CloudSqlProperties{Type: "POSTGRES", ServiceAccountId: "sa@x", InstanceId: "p:r:i", Database: "app"}},
			"Cloud SQL POSTGRES", "sa@x", "p:r:i / app"},
		{"spanner", connection.Connection{CloudSpanner: &connection.CloudSpannerProperties{Database: "projects/p/instances/i/databases/d", DatabaseRole: "reader"}},
			"Cloud Spanner", "database role reader", "projects/p/instances/i/databases/d"},
		{"cloud resource", connection.Connection{CloudResource: &connection.CloudResourceProperties{ServiceAccountId: "bqcx@x"}}, "Cloud resource", "bqcx@x", ""},
		{"spark", connection.Connection{Spark: &connection.SparkProperties{ServiceAccountId: "spark@x"}}, "Spark", "spark@x", ""},
		{"aws", connection.Connection{Aws: &connection.AwsProperties{AccessRole: &connection.AwsAccessRole{IamRoleId: "arn:aws:iam::1:role/r", Identity: "1234"}}},
			"AWS", "1234", "arn:aws:iam::1:role/r"},
		{"azure", connection.Connection{Azure: &connection.AzureProperties{Identity: "az-id", CustomerTenantId: "t1"}}, "Azure", "az-id", "tenant t1"},
		{"unknown", connection.Connection{}, "", "", ""},
	}
	for _, tc := range cases {
		tc.in.Name = "projects/p/locations/us/connections/c"
		got := fromConnection(&tc.in)
		if got.ID != "c" || got.Location != "us" || got.Kind != tc.kind || got.Identity != tc.identity || got.Target != tc.target {
			t.Errorf("%s: %+v", tc.name, got)
		}
	}
}

func TestNameHelpers(t *testing.T) {
	t.Parallel()
	if got := nameLocation("projects/p/locations/us-central1/reservations/r"); got != "us-central1" {
		t.Errorf("nameLocation=%q", got)
	}
	if got := nameLocation("projects/p/transferConfigs/x"); got != "" {
		t.Errorf("nameLocation without a location=%q", got)
	}
	if !millis(0).IsZero() || !millis(-1).IsZero() {
		t.Error("millis(0) is not zero")
	}
	if got := millis(1714554000000); !got.Equal(time.Date(2024, 5, 1, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("millis=%v", got)
	}
}
