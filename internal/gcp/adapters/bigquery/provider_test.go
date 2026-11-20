package bigquery

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/regions"
	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeDatasets struct {
	listed   []Dataset
	full     map[string]Dataset // GetDataset results by id
	getErr   map[string]error   // GetDataset errors by id
	tables   []Table
	routines []Routine
	models   []Model
	tableIAM map[string][]Binding // keyed by dataset/table
	routIAM  map[string][]Binding // keyed by dataset/routine
	err      error                // returned from every method when set
	calls    *atomic.Int32
}

func (f fakeDatasets) call() error {
	if f.calls != nil {
		f.calls.Add(1)
	}
	return f.err
}

func (f fakeDatasets) ListDatasets(context.Context, string) ([]Dataset, error) {
	if err := f.call(); err != nil {
		return nil, err
	}
	return f.listed, nil
}

func (f fakeDatasets) GetDataset(_ context.Context, _, id string) (Dataset, error) {
	if err := f.call(); err != nil {
		return Dataset{}, err
	}
	if err := f.getErr[id]; err != nil {
		return Dataset{}, err
	}
	return f.full[id], nil
}

func limited[T any](items []T, limit int) ([]T, bool) {
	if len(items) > limit {
		return items[:limit], true
	}
	return items, false
}

func (f fakeDatasets) ListTables(_ context.Context, _, _ string, limit int) ([]Table, bool, error) {
	if err := f.call(); err != nil {
		return nil, false, err
	}
	out, more := limited(f.tables, limit)
	return out, more, nil
}

func (f fakeDatasets) ListRoutines(_ context.Context, _, _ string, limit int) ([]Routine, bool, error) {
	if err := f.call(); err != nil {
		return nil, false, err
	}
	out, more := limited(f.routines, limit)
	return out, more, nil
}

func (f fakeDatasets) ListModels(_ context.Context, _, _ string, limit int) ([]Model, bool, error) {
	if err := f.call(); err != nil {
		return nil, false, err
	}
	out, more := limited(f.models, limit)
	return out, more, nil
}

func (f fakeDatasets) GetTableIAMPolicy(_ context.Context, _, dataset, table string) ([]Binding, error) {
	if err := f.call(); err != nil {
		return nil, err
	}
	return f.tableIAM[dataset+"/"+table], nil
}

func (f fakeDatasets) GetRoutineIAMPolicy(_ context.Context, _, dataset, routine string) ([]Binding, error) {
	if err := f.call(); err != nil {
		return nil, err
	}
	return f.routIAM[dataset+"/"+routine], nil
}

type fakeRegional struct {
	locations    []string
	reservations map[string][]Reservation // keyed by location
	assignments  map[string][]Assignment  // keyed by reservation name
	commitments  map[string][]Commitment
	transfers    map[string][]Transfer
	connections  map[string][]Connection
	resIAM       map[string][]Binding // keyed by location/reservation
	assignIAM    map[string][]Binding // keyed by location/reservation/assignment
	connIAM      map[string][]Binding // keyed by location/connection
	failLocs     map[string]error     // per-location list error
	err          error                // returned from every method when set
	calls        *atomic.Int32
}

func (f fakeRegional) call(location string) error {
	if f.calls != nil {
		f.calls.Add(1)
	}
	if f.err != nil {
		return f.err
	}
	return f.failLocs[location]
}

func (f fakeRegional) Locations(context.Context, string) ([]string, error) {
	if err := f.call(""); err != nil {
		return nil, err
	}
	return f.locations, nil
}

func (f fakeRegional) ListReservations(_ context.Context, _, loc string) ([]Reservation, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.reservations[loc], nil
}

func (f fakeRegional) ListAssignments(_ context.Context, _, _, name string) ([]Assignment, error) {
	if err := f.call(""); err != nil {
		return nil, err
	}
	return f.assignments[name], nil
}

func (f fakeRegional) GetReservationIAMPolicy(_ context.Context, _, loc, name string) ([]Binding, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.resIAM[loc+"/"+name], nil
}

func (f fakeRegional) GetAssignmentIAMPolicy(_ context.Context, _, loc, res, assignment string) ([]Binding, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.assignIAM[loc+"/"+res+"/"+assignment], nil
}

func (f fakeRegional) GetConnectionIAMPolicy(_ context.Context, _, loc, id string) ([]Binding, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.connIAM[loc+"/"+id], nil
}

func (f fakeRegional) ListCommitments(_ context.Context, _, loc string) ([]Commitment, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.commitments[loc], nil
}

func (f fakeRegional) ListTransfers(_ context.Context, _, loc string) ([]Transfer, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.transfers[loc], nil
}

func (f fakeRegional) ListConnections(_ context.Context, _, loc string) ([]Connection, error) {
	if err := f.call(loc); err != nil {
		return nil, err
	}
	return f.connections[loc], nil
}

var (
	_ DatasetAPI     = fakeDatasets{}
	_ ReservationAPI = fakeRegional{}
	_ TransferAPI    = fakeRegional{}
	_ ConnectionAPI  = fakeRegional{}
)

var created = time.Date(2024, 5, 1, 9, 0, 0, 0, time.UTC)

func TestListScopes(t *testing.T) {
	t.Parallel()
	ds := fakeDatasets{listed: []Dataset{{ID: "a", Location: "US"}}}
	reg := fakeRegional{locations: []string{"US"}, reservations: map[string][]Reservation{"US": {{Name: "r", Location: "US"}}},
		commitments: map[string][]Commitment{"US": {{Name: "c", Location: "US"}}}, transfers: map[string][]Transfer{"US": {{ID: "t", Location: "US"}}},
		connections: map[string][]Connection{"US": {{ID: "x", Location: "US"}}}}
	providers := func(calls *atomic.Int32) []model.ResourceProvider {
		d, r := ds, reg
		d.calls, r.calls = calls, calls
		return []model.ResourceProvider{NewDatasets(d), NewReservations(r), NewCommitments(r), NewTransfers(r), NewConnections(r)}
	}
	for _, scope := range []string{"folders/1", "organizations/1", ""} {
		var calls atomic.Int32
		for _, p := range providers(&calls) {
			got, err := p.List(context.Background(), scope)
			if err != nil || got != nil {
				t.Errorf("%s List(%q)=%v,%v want nil,nil", p.Service(), scope, got, err)
			}
		}
		if calls.Load() != 0 {
			t.Errorf("scope %q called the API %d times", scope, calls.Load())
		}
	}
	for _, p := range providers(nil) {
		got, err := p.List(context.Background(), "projects/p")
		if err != nil || len(got) != 1 {
			t.Errorf("%s List=%v,%v want one item", p.Service(), got, err)
			continue
		}
		if got[0].Service != p.Service() || got[0].ScopeID != "projects/p" {
			t.Errorf("%s item %+v", p.Service(), got[0])
		}
		if err := got[0].Validate(); err != nil {
			t.Errorf("%s: %v", p.Service(), err)
		}
	}
}

func TestDatasetsListReadsDetails(t *testing.T) {
	t.Parallel()
	api := fakeDatasets{
		listed: []Dataset{
			{ID: "analytics", Location: "US", Type: "DEFAULT"},
			{ID: "shared", Location: "EU", Type: "LINKED", FriendlyName: "Shared"},
		},
		full: map[string]Dataset{"analytics": {
			ID: "analytics", Location: "US", Type: "DEFAULT", FriendlyName: "Analytics", Detailed: true,
			Description: "Events", Created: created, TableExpiration: 90 * 24 * time.Hour, TimeTravel: 168 * time.Hour,
			KMSKey: "projects/p/locations/us/keyRings/r/cryptoKeys/k", StorageBilling: "PHYSICAL", Labels: map[string]string{"team": "data"},
			Access: []Access{{Role: "READER", Member: "group:g@x"}, {Authorized: "view p.reporting.v"}},
		}},
		getErr: map[string]error{"shared": errors.New("boom")},
	}
	got, err := NewDatasets(api).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d datasets", len(got))
	}
	a, s := got[0], got[1]
	if a.ID != "projects/p/datasets/analytics" || a.Name != "analytics" || a.Location != "US" || a.Description != "Events" ||
		!a.Created.Equal(created) || a.Labels["team"] != "data" || model.Normalize(a).Status != model.StatusUnknown {
		t.Errorf("analytics %+v", a)
	}
	wantMeta := map[string]string{"friendlyName": "Analytics", "tableExpiration": "90 days", "timeTravel": "7 days", "storageBilling": "PHYSICAL", "authorized": "1"}
	for k, v := range wantMeta {
		if a.Metadata[k] != v {
			t.Errorf("analytics %s=%q want %q", k, a.Metadata[k], v)
		}
	}
	if _, ok := a.Metadata["type"]; ok {
		t.Error("a DEFAULT dataset shows its type")
	}
	if keys := fieldKeys(a); !slices.Equal(keys[:3], []string{"friendlyName", "tableExpiration", "kmsKey"}) {
		t.Errorf("field order %v", keys)
	}
	if len(a.Lists) != 1 || !slices.Equal(a.Lists[0].Items, []string{"view p.reporting.v"}) {
		t.Errorf("authorized list %+v", a.Lists)
	}
	var facets []string
	for _, f := range a.Facets {
		facets = append(facets, f.ID)
	}
	if !slices.Equal(facets, []string{model.FacetTables, model.FacetRoutines, model.FacetModels, model.FacetPermissions}) {
		t.Errorf("facets %v", facets)
	}
	// A dataset whose details failed keeps what the list said.
	if s.Name != "shared" || s.Badge != "LINK" || s.Metadata["type"] != "LINKED" || s.Metadata["friendlyName"] != "Shared" || len(s.Facets) != 4 {
		t.Errorf("shared %+v", s)
	}
}

func fieldKeys(r model.Resource) []string {
	var out []string
	for _, f := range r.Fields {
		out = append(out, f.Key)
	}
	return out
}

func TestDatasetsListErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	if _, err := NewDatasets(fakeDatasets{err: boom}).List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Errorf("List err=%v", err)
	}
	if _, err := NewDatasets(nil).List(context.Background(), "projects/p"); err == nil {
		t.Error("nil API listed")
	}
}

var analyticsDataset = model.Resource{ID: "projects/p/datasets/analytics", Name: "analytics", Service: model.ServiceBigQueryDataset, ScopeID: "projects/p", Location: "US"}

func TestDatasetsListRelated(t *testing.T) {
	t.Parallel()
	api := fakeDatasets{
		tables: []Table{
			{ID: "events", Type: "TABLE", Created: created, Partitioning: "DAY on event_date", PartitionFilter: true, Clustering: []string{"user_id", "name"}},
			{ID: "dau", Type: "VIEW", LegacySQL: true},
		},
		routines: []Routine{{ID: "refresh", Type: "PROCEDURE", Language: "SQL"}},
		models:   []Model{{ID: "churn", Type: "LOGISTIC_REGRESSION", Labels: map[string]string{"a": "b"}}},
		full: map[string]Dataset{"analytics": {ID: "analytics", Access: []Access{
			{Role: "READER", Member: "group:analysts@x"},
			{Role: "READER", Member: "projectViewer:p"},
			{Role: "READER", Member: "group:analysts@x"},
			{Role: "OWNER", Member: "projectOwner:p"},
			{Role: "roles/bigquery.dataEditor", Member: "serviceAccount:etl@p.iam.gserviceaccount.com"},
			{Role: "READER", Member: "user:temp@x", Condition: "Until December"},
			{Authorized: "view p.reporting.v"},
		}}},
	}
	p := NewDatasets(api)
	cases := []struct {
		facet string
		want  []string // ids
		svc   model.ServiceType
	}{
		{model.FacetTables, []string{"projects/p/datasets/analytics/tables/events", "projects/p/datasets/analytics/tables/dau"}, model.ServiceBigQueryTable},
		{model.FacetRoutines, []string{"projects/p/datasets/analytics/routines/refresh"}, model.ServiceBigQueryRoutine},
		{model.FacetModels, []string{"projects/p/datasets/analytics/models/churn"}, model.ServiceBigQueryModel},
	}
	for _, tc := range cases {
		got, more, err := p.ListRelated(context.Background(), analyticsDataset, tc.facet)
		if err != nil || more {
			t.Fatalf("%s: %v %v", tc.facet, more, err)
		}
		var ids []string
		for _, r := range got {
			ids = append(ids, r.ID)
			if r.ParentID != analyticsDataset.ID || r.FacetID != tc.facet || r.ScopeID != "projects/p" || r.Location != "US" || r.Service != tc.svc {
				t.Errorf("%s child %+v", tc.facet, r)
			}
		}
		if !slices.Equal(ids, tc.want) {
			t.Errorf("%s ids %v want %v", tc.facet, ids, tc.want)
		}
	}

	tables, _, _ := p.ListRelated(context.Background(), analyticsDataset, model.FacetTables)
	events, dau := tables[0], tables[1]
	if events.Metadata["partitioning"] != "DAY on event_date" || events.Metadata["partitionFilter"] != "required" ||
		events.Metadata["clustering"] != "user_id, name" || !events.Created.Equal(created) || events.Badge != "" {
		t.Errorf("events %+v", events)
	}
	if dau.Badge != "VIEW" || dau.Metadata["sql"] != "legacy" {
		t.Errorf("dau %+v", dau)
	}
	routines, _, _ := p.ListRelated(context.Background(), analyticsDataset, model.FacetRoutines)
	if routines[0].Badge != "PROC" || routines[0].Metadata["language"] != "SQL" {
		t.Errorf("routine %+v", routines[0])
	}

	perms, _, err := p.ListRelated(context.Background(), analyticsDataset, model.FacetPermissions)
	if err != nil {
		t.Fatal(err)
	}
	type binding struct {
		role, condition string
		members         []string
	}
	var got []binding
	for _, r := range perms {
		if r.Service != model.ServiceIAMBinding || r.ParentID != analyticsDataset.ID || r.FacetID != model.FacetPermissions {
			t.Errorf("binding %+v", r)
		}
		got = append(got, binding{r.Metadata["role"], r.Metadata["condition"], r.Lists[0].Items})
	}
	want := []binding{
		{"roles/bigquery.dataViewer", "", []string{"group:analysts@x", "projectViewer:p"}},
		{"roles/bigquery.dataOwner", "", []string{"projectOwner:p"}},
		{"roles/bigquery.dataEditor", "", []string{"serviceAccount:etl@p.iam.gserviceaccount.com"}},
		{"roles/bigquery.dataViewer", "Until December", []string{"user:temp@x"}},
	}
	if len(got) != len(want) {
		t.Fatalf("bindings %+v", got)
	}
	for i := range want {
		if got[i].role != want[i].role || got[i].condition != want[i].condition || !slices.Equal(got[i].members, want[i].members) {
			t.Errorf("binding %d = %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestDatasetsListRelatedIgnoresOthers(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	p := NewDatasets(fakeDatasets{calls: &calls})
	bucket := model.Resource{ID: "projects/p/buckets/b", Service: model.ServiceCloudStorage}
	table := model.Resource{ID: "projects/p/datasets/d/tables/t", Service: model.ServiceBigQueryDataset}
	for _, tc := range []struct {
		parent model.Resource
		facet  string
	}{
		{bucket, model.FacetPermissions},
		{analyticsDataset, model.FacetObjects},
		{table, model.FacetTables},
	} {
		got, more, err := p.ListRelated(context.Background(), tc.parent, tc.facet)
		if got != nil || more || err != nil {
			t.Errorf("%s %s = %v %v %v", tc.parent.ID, tc.facet, got, more, err)
		}
	}
	if calls.Load() != 0 {
		t.Errorf("called the API %d times", calls.Load())
	}
}

func TestDatasetsListRelatedTruncatesAndFails(t *testing.T) {
	t.Parallel()
	many := make([]Table, maxTables+1)
	for i := range many {
		many[i] = Table{ID: "t" + strings.Repeat("x", i%3) + string(rune('a'+i%26))}
	}
	got, more, err := NewDatasets(fakeDatasets{tables: many}).ListRelated(context.Background(), analyticsDataset, model.FacetTables)
	if err != nil || !more || len(got) != maxTables {
		t.Errorf("truncation: %d %v %v", len(got), more, err)
	}
	boom := errors.New("boom")
	for _, facet := range []string{model.FacetTables, model.FacetRoutines, model.FacetModels, model.FacetPermissions} {
		if _, _, err := NewDatasets(fakeDatasets{err: boom}).ListRelated(context.Background(), analyticsDataset, facet); !errors.Is(err, boom) {
			t.Errorf("%s err=%v", facet, err)
		}
	}
}

func TestReservations(t *testing.T) {
	t.Parallel()
	api := fakeRegional{
		locations: []string{"US", "EU", "us-central1"},
		reservations: map[string][]Reservation{
			"US": {{Name: "prod-etl", Location: "US", Edition: "ENTERPRISE", Slots: 500, MaxSlots: 500, CurrentSlots: 100, Created: created, Labels: map[string]string{"team": "data"}}},
			"EU": {{Name: "adhoc", Location: "EU", IgnoreIdle: true, Concurrency: 10}},
		},
		assignments: map[string][]Assignment{"prod-etl": {
			{Name: "1", Assignee: "projects/p", JobType: "PIPELINE", State: "ACTIVE"},
			{Name: "2", Assignee: "folders/9", JobType: "QUERY", State: "PENDING"},
		}},
		failLocs: map[string]error{"us-central1": errors.New("unavailable")},
	}
	p := NewReservations(api)
	got, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	etl := got[0]
	if etl.ID != "projects/p/locations/US/reservations/prod-etl" || etl.Location != "US" || !etl.Created.Equal(created) || etl.Labels["team"] != "data" {
		t.Errorf("etl %+v", etl)
	}
	if etl.Metadata["slots"] != "500" || etl.Metadata["autoscale"] != "up to 500 more (100 in use)" || etl.Metadata["idleSlots"] != "used" {
		t.Errorf("etl metadata %v", etl.Metadata)
	}
	if len(etl.Facets) != 2 || etl.Facets[0].ID != model.FacetAssignments || etl.Facets[1].ID != model.FacetPermissions {
		t.Errorf("etl facets %+v", etl.Facets)
	}
	if got[1].Metadata["idleSlots"] != "not used" || got[1].Metadata["concurrency"] != "10" {
		t.Errorf("adhoc metadata %v", got[1].Metadata)
	}

	as, more, err := p.ListRelated(context.Background(), etl, model.FacetAssignments)
	if err != nil || more || len(as) != 2 {
		t.Fatalf("assignments %v %v %v", as, more, err)
	}
	a := as[0]
	if a.ID != etl.ID+"/assignments/1" || a.Name != "projects/p" || a.Badge != "PIPELINE" || a.Status != model.StatusHealthy ||
		a.ParentID != etl.ID || a.FacetID != model.FacetAssignments || a.ScopeID != "projects/p" {
		t.Errorf("assignment %+v", a)
	}
	if as[1].Status != model.StatusWarning || as[1].State != "PENDING" {
		t.Errorf("pending assignment %+v", as[1])
	}
	if got, _, _ := p.ListRelated(context.Background(), analyticsDataset, model.FacetAssignments); got != nil {
		t.Errorf("a dataset has assignments %v", got)
	}
}

func TestRegionalListFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	one := fakeRegional{
		locations:    []string{"US", "EU"},
		failLocs:     map[string]error{"EU": boom},
		reservations: map[string][]Reservation{"US": {{Name: "prod", Location: "US", Slots: 100}}},
	}
	got, err := NewReservations(one).List(context.Background(), "projects/p")
	if err != nil || len(got) != 1 || got[0].Name != "prod" {
		t.Fatalf("partial location failure: got %v, err=%v", got, err)
	}
	all := fakeRegional{locations: []string{"US", "EU"}, failLocs: map[string]error{"US": boom, "EU": errors.New("other")}}
	for _, p := range []model.ResourceProvider{NewReservations(all), NewCommitments(all), NewTransfers(all), NewConnections(all)} {
		if _, err := p.List(context.Background(), "projects/p"); !errors.Is(err, boom) {
			t.Errorf("%s: every location failing gave err=%v", p.Service(), err)
		}
	}
	locs := fakeRegional{err: boom}
	if _, err := NewConnections(locs).List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Errorf("location list failing gave err=%v", err)
	}
	if _, _, err := NewReservations(locs).ListRelated(context.Background(), model.Resource{ID: "projects/p/locations/US/reservations/r", Service: model.ServiceBigQueryReservation}, model.FacetAssignments); !errors.Is(err, boom) {
		t.Errorf("assignments err=%v", err)
	}
}

func TestCommitmentsTransfersConnections(t *testing.T) {
	t.Parallel()
	api := fakeRegional{
		locations: []string{"us"},
		commitments: map[string][]Commitment{"us": {
			{Name: "111", Location: "us", Slots: 100, Plan: "ANNUAL", State: "ACTIVE"},
			{Name: "222", Location: "us", Slots: 50, Plan: "FLEX", State: "FAILED", Failure: "no quota"},
		}},
		transfers: map[string][]Transfer{"us": {
			{ID: "abc", Location: "us", DisplayName: "Daily rollup", DataSource: "scheduled_query", Schedule: "every 24 hours", State: "SUCCEEDED"},
			{ID: "def", Location: "us", Schedule: "every 1 hours", State: "FAILED", Error: "not found"},
			{ID: "ghi", Location: "us", Schedule: "every 24 hours", State: "SUCCEEDED", Disabled: true},
		}},
		connections: map[string][]Connection{"us": {
			{ID: "orders", Location: "us", Kind: "Cloud SQL POSTGRES", Identity: "sa@x", Target: "p:r:i / app", Created: created},
		}},
	}
	cs, err := NewCommitments(api).List(context.Background(), "projects/p")
	if err != nil || len(cs) != 2 {
		t.Fatalf("commitments %v %v", cs, err)
	}
	if cs[0].ID != "projects/p/locations/us/capacityCommitments/111" || cs[0].Status != model.StatusHealthy || cs[0].Badge != "ANNUAL" {
		t.Errorf("commitment %+v", cs[0])
	}
	if cs[1].Status != model.StatusError || cs[1].Fields[0].Key != "failure" {
		t.Errorf("failed commitment %+v", cs[1])
	}

	ts, err := NewTransfers(api).List(context.Background(), "projects/p")
	if err != nil || len(ts) != 3 {
		t.Fatalf("transfers %v %v", ts, err)
	}
	if ts[0].Name != "Daily rollup" || ts[0].ID != "projects/p/locations/us/transferConfigs/abc" || ts[0].Status != model.StatusHealthy {
		t.Errorf("transfer %+v", ts[0])
	}
	if ts[1].Name != "def" || ts[1].Status != model.StatusError || ts[1].Fields[0].Key != "error" {
		t.Errorf("failed transfer %+v", ts[1])
	}
	if ts[2].Badge != "OFF" || ts[2].Status != model.StatusWarning || ts[2].Metadata["schedule"] != "every 24 hours (disabled)" {
		t.Errorf("disabled transfer %+v", ts[2])
	}

	conns, err := NewConnections(api).List(context.Background(), "projects/p")
	if err != nil || len(conns) != 1 {
		t.Fatalf("connections %v %v", conns, err)
	}
	c := conns[0]
	if c.ID != "projects/p/locations/us/connections/orders" || c.Metadata["identity"] != "sa@x" || !c.Created.Equal(created) ||
		!slices.Equal(fieldKeys(c), []string{"type", "identity", "target"}) {
		t.Errorf("connection %+v", c)
	}
}

func TestStatuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		fn   func(string) model.ResourceStatus
		in   string
		want model.ResourceStatus
	}{
		{"assignment", assignmentStatus, "ACTIVE", model.StatusHealthy},
		{"assignment", assignmentStatus, "PENDING", model.StatusWarning},
		{"assignment", assignmentStatus, "", model.StatusUnknown},
		{"commitment", commitmentStatus, "ACTIVE", model.StatusHealthy},
		{"commitment", commitmentStatus, "PENDING", model.StatusWarning},
		{"commitment", commitmentStatus, "FAILED", model.StatusError},
		{"commitment", commitmentStatus, "WHATEVER", model.StatusUnknown},
		{"transfer", transferStatus, "SUCCEEDED", model.StatusHealthy},
		{"transfer", transferStatus, "CANCELLED", model.StatusWarning},
		{"transfer", transferStatus, "FAILED", model.StatusError},
		{"transfer", transferStatus, "RUNNING", model.StatusUnknown},
		{"transfer", transferStatus, "PENDING", model.StatusUnknown},
	}
	for _, tc := range cases {
		if got := tc.fn(tc.in); got != tc.want {
			t.Errorf("%s %q = %v want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestLocations(t *testing.T) {
	t.Parallel()
	got, err := Locations(regions.Fake{Names: []string{"us-central1"}}).Regions(context.Background(), "p")
	if err != nil || !slices.Equal(got, []string{"US", "EU", "us-central1"}) {
		t.Errorf("Regions=%v,%v", got, err)
	}
	got, err = Locations(regions.Fake{Err: errors.New("compute off")}).Regions(context.Background(), "p")
	if err != nil || !slices.Equal(got, append([]string{"US", "EU"}, regions.DefaultRegions...)) {
		t.Errorf("fallback Regions=%v,%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Locations(regions.Fake{}).Regions(ctx, "p"); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled err=%v", err)
	}
}

func TestSpan(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, ""},
		{24 * time.Hour, "1 day"},
		{168 * time.Hour, "7 days"},
		{time.Hour, "1 hour"},
		{36 * time.Hour, "36 hours"},
		{90 * time.Minute, "1h30m0s"},
	}
	for _, tc := range cases {
		if got := span(tc.in); got != tc.want {
			t.Errorf("span(%v)=%q want %q", tc.in, got, tc.want)
		}
	}
}
