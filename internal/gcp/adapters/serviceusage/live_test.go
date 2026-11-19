package serviceusage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	gapi "google.golang.org/api/serviceusage/v1"

	"github.com/tensleyconsulting/seiner/internal/gcp/pagetest"
)

func TestListEnabledReadsDocumentation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"services":[{"name":"projects/1/services/storage.googleapis.com","config":{"name":"storage.googleapis.com","title":"Cloud Storage API","documentation":{"summary":"Stores objects.","documentationRootUrl":"https://cloud.google.com/storage/docs"}}}]}`))
	}))
	defer srv.Close()
	svc, err := gapi.NewService(context.Background(), option.WithEndpoint(srv.URL), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := liveAPI{svc: svc}.ListEnabled(context.Background(), "p")
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if want := (APIService{Name: "storage.googleapis.com", Title: "Cloud Storage API", Summary: "Stores objects.", DocsURL: "https://cloud.google.com/storage/docs"}); got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
}

// ListEnabled filters to enabled services and reads every page.
func TestListEnabledFiltersAndPages(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.Pages("/projects/p/services", "services",
		[]string{`{"name":"projects/1/services/compute.googleapis.com","config":{"name":"compute.googleapis.com","title":"Compute Engine API"}}`},
		[]string{`{"name":"projects/1/services/storage.googleapis.com","config":{"name":"storage.googleapis.com"}}`})
	svc, err := gapi.NewService(context.Background(), option.WithEndpoint(srv.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	got, err := liveAPI{svc: svc}.ListEnabled(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"compute.googleapis.com", "storage.googleapis.com"}) {
		t.Fatalf("names %v", names)
	}
	asked := srv.Asked("/projects/p/services")
	if len(asked) != 2 {
		t.Fatalf("requests %d, want 2 pages", len(asked))
	}
	for _, q := range asked {
		if q.Get("filter") != "state:ENABLED" {
			t.Errorf("filter %q, want state:ENABLED", q.Get("filter"))
		}
	}
}

// A disabled Service Usage API surfaces as the googleapi error Classify
// reads as api-disabled.
func TestListEnabledError(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.Error("/projects/p/services", http.StatusForbidden, "accessNotConfigured")
	svc, err := gapi.NewService(context.Background(), option.WithEndpoint(srv.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	_, err = liveAPI{svc: svc}.ListEnabled(context.Background(), "p")
	var ge *googleapi.Error
	if !errors.As(err, &ge) || ge.Code != http.StatusForbidden || len(ge.Errors) != 1 || ge.Errors[0].Reason != "accessNotConfigured" {
		t.Fatalf("err = %#v", err)
	}
}
