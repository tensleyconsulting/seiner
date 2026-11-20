package logging

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	sinks   map[string][]Sink // keyed by parent
	buckets []Bucket
	metrics []Metric
	views   map[string][]View // keyed by location/bucket
	links   map[string][]Link
	viewIAM map[string][]Binding // keyed by location/bucket/view
	err     error
	calls   *[]string
}

func (f fakeAPI) ListSinks(_ context.Context, parent string) ([]Sink, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "sinks "+parent)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.sinks[parent], nil
}

func (f fakeAPI) ListBuckets(_ context.Context, project string) ([]Bucket, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "buckets "+project)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.buckets, nil
}

func (f fakeAPI) ListMetrics(_ context.Context, project string) ([]Metric, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "metrics "+project)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.metrics, nil
}

func (f fakeAPI) ListViews(_ context.Context, _, location, bucket string) ([]View, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.views[location+"/"+bucket], nil
}

func (f fakeAPI) ListLinks(_ context.Context, _, location, bucket string) ([]Link, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.links[location+"/"+bucket], nil
}

func (f fakeAPI) GetViewIAMPolicy(_ context.Context, _, location, bucket, view string) ([]Binding, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.viewIAM[location+"/"+bucket+"/"+view], nil
}

var created = time.Date(2024, 9, 12, 15, 0, 0, 0, time.UTC)

func sample() fakeAPI {
	return fakeAPI{
		sinks: map[string][]Sink{
			"organizations/1": {{
				Name: "org-audit", Destination: "storage.googleapis.com/example-audit", Filter: `logName:"cloudaudit"`,
				IncludeChildren: true, WriterIdentity: "serviceAccount:o1@gcp-sa-logging.iam.gserviceaccount.com",
			}},
			"projects/p": {
				{Name: "_Default", Destination: "logging.googleapis.com/projects/p/locations/global/buckets/_Default",
					Exclusions: []Exclusion{{Name: "drop-health", Filter: `httpRequest.requestUrl="/healthz"`}, {Name: "old", Filter: "x", Disabled: true}}},
				{Name: "debug", Destination: "pubsub.googleapis.com/projects/p/topics/debug", Disabled: true, Created: created,
					Updated: time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC), Description: "Old debug stream"},
			},
		},
		buckets: []Bucket{
			{Name: "_Default", Location: "global", RetentionDays: 30, State: "ACTIVE"},
			{Name: "app-logs", Location: "us-central1", Description: "App logs", RetentionDays: 90, Locked: true,
				State: "ACTIVE", Analytics: true, KMSKey: "projects/p/locations/us-central1/keyRings/r/cryptoKeys/k", Created: created,
				KMSKeyVersion: "projects/p/locations/us-central1/keyRings/r/cryptoKeys/k/cryptoKeyVersions/3",
				Indexes:       []string{"jsonPayload.user INDEX_TYPE_STRING"}, Updated: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)},
		},
		metrics: []Metric{
			{Name: "checkout_errors", Description: "Checkout 5xx", Filter: `resource.type="k8s_container" severity>=ERROR`,
				Kind: "DELTA", ValueType: "INT64", Unit: "1", Labels: []string{"route  EXTRACT(httpRequest.requestUrl)"}, Created: created},
			{Name: "latency", Filter: "x", Kind: "DELTA", ValueType: "DISTRIBUTION", ValueExtractor: "EXTRACT(jsonPayload.ms)", Disabled: true, Bucket: "projects/p/locations/global/buckets/_Default"},
		},
		views: map[string][]View{
			"us-central1/app-logs": {{Name: "_AllLogs", Description: "All logs"}, {Name: "web-only", Filter: `resource.type="gce_instance"`}},
		},
		links: map[string][]Link{
			"us-central1/app-logs": {{Name: "app_logs_bq", Dataset: "app_logs_bq", State: "ACTIVE", Created: created}},
		},
	}
}

func TestScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope            string
		sinks, buckets   int
		wantCallsOnScope []string
	}{
		{"organizations/1", 1, 0, []string{"sinks organizations/1"}},
		{"folders/2", 0, 0, []string{"sinks folders/2"}},
		{"projects/p", 2, 2, []string{"sinks projects/p", "buckets p"}},
		{"", 0, 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			var calls []string
			api := sample()
			api.calls = &calls
			sinks, err := NewSinks(api).List(context.Background(), tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			buckets, err := NewBuckets(api).List(context.Background(), tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(sinks) != tc.sinks || len(buckets) != tc.buckets || !slices.Equal(calls, tc.wantCallsOnScope) {
				t.Fatalf("sinks=%d buckets=%d calls=%v, want %d, %d, %v", len(sinks), len(buckets), calls, tc.sinks, tc.buckets, tc.wantCallsOnScope)
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

func TestSinkMapping(t *testing.T) {
	t.Parallel()
	org, err := NewSinks(sample()).List(context.Background(), "organizations/1")
	if err != nil {
		t.Fatal(err)
	}
	r := org[0]
	if r.ID != "organizations/1/sinks/org-audit" || r.Name != "org-audit" || r.Service != model.ServiceLogSink ||
		r.ScopeID != "organizations/1" || r.Status != "" {
		t.Fatalf("identity: %+v", r)
	}
	if want := []string{"destinationType", "destination", "filter", "includeChildren", "writerIdentity"}; !slices.Equal(fieldKeys(r), want) {
		t.Fatalf("org sink fields = %v, want %v", fieldKeys(r), want)
	}
	if r.Metadata["destinationType"] != "Cloud Storage" || r.Metadata["includeChildren"] != "on" {
		t.Fatalf("org sink metadata: %+v", r.Metadata)
	}

	proj, err := NewSinks(sample()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	def := proj[0]
	if def.Metadata["destinationType"] != "Log bucket" || def.Metadata["filter"] != "(all logs)" ||
		def.Metadata["exclusions"] != "2" || def.Metadata["includeChildren"] != "" {
		t.Fatalf("_Default metadata: %+v", def.Metadata)
	}
	want := []string{`drop-health  httpRequest.requestUrl="/healthz"`, "old  x  (disabled)"}
	if ex := def.List("exclusions"); !slices.Equal(ex, want) {
		t.Fatalf("exclusions = %v, want %v", ex, want)
	}
	dbg := proj[1]
	if dbg.Status != model.StatusWarning || dbg.State != "DISABLED" || dbg.Badge != "off" ||
		dbg.Metadata["updated"] != "2025-01-02 03:04 UTC" || !dbg.Created.Equal(created) || dbg.Description != "Old debug stream" {
		t.Fatalf("disabled sink: %+v", dbg)
	}
}

func TestDestinationType(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"storage.googleapis.com/b":                                     "Cloud Storage",
		"bigquery.googleapis.com/projects/p/datasets/d":                "BigQuery",
		"pubsub.googleapis.com/projects/p/topics/t":                    "Pub/Sub",
		"logging.googleapis.com/projects/p/locations/global/buckets/b": "Log bucket",
		"logging.googleapis.com/projects/other":                        "Logging project",
		"":                                                             "",
	}
	for dest, want := range cases {
		if got := destinationType(dest); got != want {
			t.Errorf("destinationType(%q) = %q, want %q", dest, got, want)
		}
	}
}

func TestBucketMapping(t *testing.T) {
	t.Parallel()
	got, err := NewBuckets(sample()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	r := got[1]
	if r.ID != "projects/p/locations/us-central1/buckets/app-logs" || r.Name != "app-logs" || r.Service != model.ServiceLogBucket ||
		r.ScopeID != "projects/p" || r.Location != "us-central1" || r.State != "ACTIVE" || r.Status != model.StatusHealthy ||
		r.Description != "App logs" || !r.Created.Equal(created) {
		t.Fatalf("identity: %+v", r)
	}
	if want := []string{"retention", "locked", "analytics", "kmsKey", "kmsKeyVersion", "indexedFields", "updated"}; !slices.Equal(fieldKeys(r), want) {
		t.Fatalf("fields = %v, want %v", fieldKeys(r), want)
	}
	if r.Metadata["retention"] != "90 days" || r.Metadata["locked"] != "on" || r.Metadata["analytics"] != "on" {
		t.Fatalf("metadata: %+v", r.Metadata)
	}
	if len(r.Facets) != 2 || r.Facets[0].ID != model.FacetViews || r.Facets[1].ID != model.FacetLinks ||
		len(r.List("indexedFields")) != 1 {
		t.Fatalf("facets/lists: %+v %+v", r.Facets, r.Lists)
	}
	if def := got[0]; len(def.Facets) != 1 || def.Facets[0].ID != model.FacetViews {
		t.Fatalf("a bucket without Log Analytics links no datasets: %+v", def.Facets)
	}
	for s, want := range map[string]model.ResourceStatus{
		"ACTIVE": model.StatusHealthy, "DELETE_REQUESTED": model.StatusWarning, "FAILED": model.StatusError, "": model.StatusUnknown,
	} {
		if got := bucketStatus(s); got != want {
			t.Errorf("bucketStatus(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestBucketViewsAndLinks(t *testing.T) {
	t.Parallel()
	p := NewBuckets(sample())
	buckets, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	app := buckets[1]
	views, truncated, err := p.ListRelated(context.Background(), app, model.FacetViews)
	if err != nil || truncated || len(views) != 2 {
		t.Fatalf("views: %+v %v %v", views, truncated, err)
	}
	if v := views[0]; v.ID != app.ID+"/views/_AllLogs" || v.Service != model.ServiceLogView || v.ParentID != app.ID ||
		v.FacetID != model.FacetViews || v.ScopeID != "projects/p" || v.Metadata["filter"] != "(all logs)" || v.Description != "All logs" {
		t.Fatalf("view: %+v", v)
	}
	links, _, err := p.ListRelated(context.Background(), app, model.FacetLinks)
	if err != nil || len(links) != 1 {
		t.Fatalf("links: %+v %v", links, err)
	}
	if l := links[0]; l.ID != app.ID+"/links/app_logs_bq" || l.Service != model.ServiceLogLink || l.Status != model.StatusHealthy ||
		l.Metadata["dataset"] != "p.app_logs_bq" || l.FacetID != model.FacetLinks {
		t.Fatalf("link: %+v", l)
	}
	for _, tc := range []struct {
		parent model.Resource
		facet  string
	}{
		{app, model.FacetKeys},
		{model.Resource{ID: app.ID, Service: model.ServiceLogSink}, model.FacetViews},
		{model.Resource{ID: "projects/p/sinks/s", Service: model.ServiceLogBucket}, model.FacetViews},
	} {
		if got, _, err := p.ListRelated(context.Background(), tc.parent, tc.facet); got != nil || err != nil {
			t.Errorf("%s %s: %v %v", tc.parent.ID, tc.facet, got, err)
		}
	}
}

func TestMetrics(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"organizations/1", "folders/2", ""} {
		if got, err := NewMetrics(sample()).List(context.Background(), scope); got != nil || err != nil {
			t.Errorf("%q: %v %v", scope, got, err)
		}
	}
	got, err := NewMetrics(sample()).List(context.Background(), "projects/p")
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	m := got[0]
	if m.ID != "projects/p/metrics/checkout_errors" || m.Service != model.ServiceLogMetric || m.ScopeID != "projects/p" ||
		m.Description != "Checkout 5xx" || m.Metadata["type"] != "DELTA INT64" || m.Status != "" || !m.Created.Equal(created) ||
		!slices.Equal(m.List("labels"), []string{"route  EXTRACT(httpRequest.requestUrl)"}) {
		t.Fatalf("metric: %+v", m)
	}
	if want := []string{"filter", "type", "unit", "labels"}; !slices.Equal(fieldKeys(m), want) {
		t.Fatalf("fields = %v, want %v", fieldKeys(m), want)
	}
	if d := got[1]; d.Status != model.StatusWarning || d.State != "DISABLED" || d.Badge != "off" || d.Metadata["bucket"] == "" ||
		d.Metadata["valueExtractor"] != "EXTRACT(jsonPayload.ms)" {
		t.Fatalf("disabled metric: %+v", d)
	}
}

func TestErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	api := fakeAPI{err: boom}
	if _, err := NewSinks(api).List(context.Background(), "folders/2"); !errors.Is(err, boom) {
		t.Fatalf("sinks err = %v", err)
	}
	if _, err := NewBuckets(api).List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Fatalf("buckets err = %v", err)
	}
	if _, err := NewMetrics(api).List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Fatalf("metrics err = %v", err)
	}
	bucket := bucketResource("p", Bucket{Name: "b", Location: "global", Analytics: true})
	for _, f := range []string{model.FacetViews, model.FacetLinks} {
		if _, _, err := NewBuckets(api).ListRelated(context.Background(), bucket, f); !errors.Is(err, boom) {
			t.Fatalf("%s err = %v", f, err)
		}
	}
}
