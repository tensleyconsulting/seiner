package gcs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	iampb "cloud.google.com/go/iam/apiv1/iampb"
	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/type/expr"

	"github.com/tensleyconsulting/seiner/internal/gcp/pagetest"
)

func TestFromBindings(t *testing.T) {
	t.Parallel()
	got := fromBindings([]*iampb.Binding{
		{Role: "roles/storage.objectViewer", Members: []string{"allUsers"}},
		{Role: "roles/storage.objectViewer", Members: []string{"user:a"}, Condition: &expr.Expr{Title: "prefix"}},
	})
	want := []PolicyBinding{
		{Role: "roles/storage.objectViewer", Members: []string{"allUsers"}},
		{Role: "roles/storage.objectViewer", Members: []string{"user:a"}, Condition: "prefix"},
	}
	if !slices.EqualFunc(got, want, func(a, b PolicyBinding) bool {
		return a.Role == b.Role && a.Condition == b.Condition && slices.Equal(a.Members, b.Members)
	}) {
		t.Errorf("bindings %+v, want %+v", got, want)
	}
}

func TestFromAttrsPlacementAndHolds(t *testing.T) {
	t.Parallel()
	got := fromAttrs(&storage.BucketAttrs{
		Name: "b", RPO: storage.RPOAsyncTurbo,
		CustomPlacementConfig: &storage.CustomPlacementConfig{DataLocations: []string{"US-EAST1", "US-WEST1"}},
		Logging:               &storage.BucketLogging{LogBucket: "access-logs"},
		HierarchicalNamespace: &storage.HierarchicalNamespace{Enabled: true},
		ObjectRetentionMode:   "Enabled",
		DefaultEventBasedHold: true,
	})
	if !got.TurboReplication || len(got.DataLocations) != 2 || got.LogBucket != "access-logs" || !got.HNS ||
		got.ObjectRetention != "Enabled" || !got.EventBasedHold {
		t.Fatalf("fromAttrs = %+v", got)
	}
	if plain := fromAttrs(&storage.BucketAttrs{Name: "p"}); plain.TurboReplication || plain.HNS || plain.LogBucket != "" {
		t.Fatalf("plain bucket = %+v", plain)
	}
}

// ListObjects asks for one page just past the limit, with only the fields it
// reads, and reports truncation from the peeked object.
func TestListObjectsPageSize(t *testing.T) {
	t.Parallel()
	queries := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.RawQuery
		var items []string
		for i := range 3 {
			items = append(items, fmt.Sprintf(`{"name":"o%d","size":"1"}`, i))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"items":[%s],"nextPageToken":"more"}`, strings.Join(items, ","))
	}))
	defer srv.Close()
	client, err := storage.NewClient(context.Background(), option.WithEndpoint(srv.URL+"/storage/v1/"), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	got, truncated, err := liveAPI{client: client}.ListObjects(context.Background(), "b", 2)
	if err != nil {
		t.Fatal(err)
	}
	query := <-queries
	if len(got) != 2 || !truncated {
		t.Fatalf("got %d objects, truncated=%v; want 2, true", len(got), truncated)
	}
	if !strings.Contains(query, "maxResults=3") {
		t.Fatalf("query %q should ask for maxResults=3", query)
	}
	if !strings.Contains(query, "fields=") || !strings.Contains(query, "name") {
		t.Fatalf("query %q should select fields", query)
	}
}

func gcsFor(t *testing.T, url string) liveAPI {
	t.Helper()
	client, err := storage.NewClient(context.Background(), option.WithEndpoint(url+"/storage/v1/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return liveAPI{client: client}
}

// ListBuckets asks for the project's buckets and reads every page.
func TestListBucketsPages(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.Pages("/storage/v1/b", "items",
		[]string{`{"name":"logs","location":"US","storageClass":"STANDARD"}`},
		[]string{`{"name":"assets","location":"EU","billing":{"requesterPays":true}}`})
	got, err := gcsFor(t, srv.URL).ListBuckets(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "logs" || got[0].StorageClass != "STANDARD" || got[1].Name != "assets" || !got[1].RequesterPays {
		t.Fatalf("buckets %+v", got)
	}
	asked := srv.Asked("/storage/v1/b")
	if len(asked) != 2 {
		t.Fatalf("requests %d, want 2 pages", len(asked))
	}
	for _, q := range asked {
		if q.Get("project") != "p" {
			t.Errorf("project %q, want p", q.Get("project"))
		}
	}
}

// Bucket policies are read as version 3: the v1 handle fails on a bucket
// with conditional bindings.
func TestGetBucketPolicyAsksForVersion3(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.JSON("/storage/v1/b/logs/iam", `{"version":3,"bindings":[
		{"role":"roles/storage.objectViewer","members":["allUsers"]},
		{"role":"roles/storage.admin","members":["user:a@x"],"condition":{"title":"temp","expression":"true"}}]}`)
	got, err := gcsFor(t, srv.URL).GetBucketPolicy(context.Background(), "logs")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Condition != "temp" {
		t.Fatalf("bindings %+v", got)
	}
	asked := srv.Asked("/storage/v1/b/logs/iam")
	if len(asked) != 1 || asked[0].Get("optionsRequestedPolicyVersion") != "3" {
		t.Fatalf("asked %v, want optionsRequestedPolicyVersion=3", asked)
	}
}

func TestListBucketsError(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.Error("/storage/v1/b", http.StatusForbidden, "forbidden")
	_, err := gcsFor(t, srv.URL).ListBuckets(context.Background(), "p")
	var ge *googleapi.Error
	if !errors.As(err, &ge) || ge.Code != http.StatusForbidden {
		t.Fatalf("err = %v, want a 403 googleapi.Error", err)
	}
}
