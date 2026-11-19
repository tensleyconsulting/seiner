package logging

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// TestLiveRequests checks sinks list on the scope itself and buckets list
// every location with the "-" wildcard.
func TestLiveRequests(t *testing.T) {
	t.Parallel()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v2/projects/p/metrics":
			_, _ = w.Write([]byte(`{"metrics":[{"name":"errors","filter":"severity>=ERROR","labelExtractors":{"b":"EXTRACT(b)","a":"EXTRACT(a)"},"metricDescriptor":{"metricKind":"DELTA","valueType":"INT64"},"bucketName":"projects/p/locations/global/buckets/_Default"}]}`))
		case r.URL.Path == "/v2/projects/p/locations/us-central1/buckets/app/views":
			_, _ = w.Write([]byte(`{"views":[{"name":"projects/p/locations/us-central1/buckets/app/views/_AllLogs","filter":""}]}`))
		case r.URL.Path == "/v2/projects/p/locations/us-central1/buckets/app/links":
			_, _ = w.Write([]byte(`{"links":[{"name":"projects/p/locations/us-central1/buckets/app/links/bq","bigqueryDataset":{"datasetId":"bq"},"lifecycleState":"ACTIVE"}]}`))
		case r.URL.Path == "/v2/projects/p/locations/-/buckets":
			_, _ = w.Write([]byte(`{"buckets":[{"name":"projects/p/locations/us-central1/buckets/app","retentionDays":90,"lifecycleState":"ACTIVE","analyticsEnabled":true,"cmekSettings":{"kmsKeyName":"k","kmsKeyVersionName":"k/cryptoKeyVersions/1"},"indexConfigs":[{"fieldPath":"jsonPayload.user","type":"INDEX_TYPE_STRING"}]}]}`))
		default:
			_, _ = w.Write([]byte(`{"sinks":[{"name":"s","destination":"storage.googleapis.com/b","exclusions":[{"name":"e","filter":"f"}],"createTime":"2024-09-12T15:00:00Z"}]}`))
		}
	}))
	defer srv.Close()
	api := NewLazy(option.WithEndpoint(srv.URL), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
	sinks, err := NewSinks(api).List(context.Background(), "folders/2")
	if err != nil {
		t.Fatal(err)
	}
	if len(sinks) != 1 || sinks[0].ID != "folders/2/sinks/s" || len(sinks[0].List("exclusions")) != 1 || sinks[0].Created.IsZero() {
		t.Fatalf("sinks = %+v", sinks)
	}
	buckets, err := NewBuckets(api).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 1 || buckets[0].ID != "projects/p/locations/us-central1/buckets/app" ||
		buckets[0].Location != "us-central1" || buckets[0].Metadata["kmsKey"] != "k" {
		t.Fatalf("buckets = %+v", buckets)
	}
	if b := buckets[0]; b.Metadata["kmsKeyVersion"] != "k/cryptoKeyVersions/1" || !slices.Equal(b.List("indexedFields"), []string{"jsonPayload.user INDEX_TYPE_STRING"}) {
		t.Fatalf("bucket details = %+v", b)
	}
	views, _, err := NewBuckets(api).ListRelated(context.Background(), buckets[0], model.FacetViews)
	if err != nil || len(views) != 1 || views[0].Name != "_AllLogs" {
		t.Fatalf("views = %+v, %v", views, err)
	}
	links, _, err := NewBuckets(api).ListRelated(context.Background(), buckets[0], model.FacetLinks)
	if err != nil || len(links) != 1 || links[0].Metadata["dataset"] != "p.bq" {
		t.Fatalf("links = %+v, %v", links, err)
	}
	metrics, err := NewMetrics(api).List(context.Background(), "projects/p")
	if err != nil || len(metrics) != 1 || !slices.Equal(metrics[0].List("labels"), []string{"a  EXTRACT(a)", "b  EXTRACT(b)"}) ||
		metrics[0].Metadata["type"] != "DELTA INT64" {
		t.Fatalf("metrics = %+v, %v", metrics, err)
	}
	if want := []string{"/v2/folders/2/sinks", "/v2/projects/p/locations/-/buckets", "/v2/projects/p/locations/us-central1/buckets/app/views",
		"/v2/projects/p/locations/us-central1/buckets/app/links", "/v2/projects/p/metrics"}; !slices.Equal(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}
