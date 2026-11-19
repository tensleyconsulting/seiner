package clouddns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	dns "google.golang.org/api/dns/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/pagetest"
)

func liveFor(t *testing.T, url string) liveAPI {
	t.Helper()
	svc, err := dns.NewService(context.Background(), option.WithEndpoint(url), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	return liveAPI{svc: svc}
}

func rrset(name string) string {
	return fmt.Sprintf(`{"name":%q,"type":"A","ttl":300,"rrdatas":["10.0.0.1"]}`, name)
}

// ListRecords stops paging at the limit, asks for no page past the one
// that reached it, and reports truncation only when a record was left.
func TestListRecordsStopsAtLimit(t *testing.T) {
	t.Parallel()
	pages := [][]string{
		{rrset("a."), rrset("b.")},
		{rrset("c."), rrset("d.")},
		{rrset("e.")},
	}
	tests := []struct {
		name       string
		limit      int
		wantNames  []string
		truncated  bool
		wantTokens []string
	}{
		{"within the first page", 1, []string{"a."}, true, []string{""}},
		{"at a page boundary", 2, []string{"a.", "b."}, true, []string{"", "p1"}},
		{"across pages", 3, []string{"a.", "b.", "c."}, true, []string{"", "p1"}},
		{"exactly everything", 5, []string{"a.", "b.", "c.", "d.", "e."}, false, []string{"", "p1", "p2"}},
		{"more than everything", 10, []string{"a.", "b.", "c.", "d.", "e."}, false, []string{"", "p1", "p2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := pagetest.New(t)
			srv.Pages("/projects/p/managedZones/z/rrsets", "rrsets", pages...)
			got, truncated, err := liveFor(t, srv.URL).ListRecords(context.Background(), "p", "z", tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, r := range got {
				names = append(names, r.Name)
			}
			if !slices.Equal(names, tt.wantNames) || truncated != tt.truncated {
				t.Errorf("got %v truncated=%v, want %v truncated=%v", names, truncated, tt.wantNames, tt.truncated)
			}
			if got := srv.Tokens("/projects/p/managedZones/z/rrsets"); !slices.Equal(got, tt.wantTokens) {
				t.Errorf("page tokens %q, want %q", got, tt.wantTokens)
			}
		})
	}
}

func TestListZonesPagesAndMaps(t *testing.T) {
	t.Parallel()
	pages := [][]string{
		{`{"name":"public","dnsName":"example.com.","visibility":"public","creationTime":"2025-01-02T03:04:05Z",
		   "nameServers":["ns1.example."],"dnssecConfig":{"state":"on"},"cloudLoggingConfig":{"enableLogging":true}}`},
		{`{"name":"private","dnsName":"corp.internal.","visibility":"private",
		   "privateVisibilityConfig":{"networks":[{"networkUrl":"https://www.googleapis.com/compute/v1/projects/p/global/networks/vpc-a"}]},
		   "forwardingConfig":{"targetNameServers":[{"ipv4Address":"10.1.1.1"},{"domainName":"dns.corp."}]}}`},
	}
	srv := pagetest.New(t)
	srv.Pages("/projects/p/managedZones", "managedZones", pages...)
	got, err := liveFor(t, srv.URL).ListZones(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("zones = %+v", got)
	}
	pub, priv := got[0], got[1]
	if pub.Name != "public" || pub.DNSSEC != "on" || !pub.Logging || !pub.Created.Equal(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("public = %+v", pub)
	}
	if !slices.Equal(priv.Networks, []string{"vpc-a"}) || !slices.Equal(priv.Forwarding, []string{"10.1.1.1", "dns.corp."}) {
		t.Errorf("private = %+v", priv)
	}
	if got := srv.Tokens("/projects/p/managedZones"); !slices.Equal(got, []string{"", "p1"}) {
		t.Errorf("page tokens %q", got)
	}
}

// A server error comes back as a googleapi.Error, which gcp.Classify reads.
func TestListZonesError(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.Error("/projects/p/managedZones", http.StatusForbidden, "forbidden")
	_, err := liveFor(t, srv.URL).ListZones(context.Background(), "p")
	var ge *googleapi.Error
	if !errors.As(err, &ge) || ge.Code != http.StatusForbidden {
		t.Fatalf("err = %v, want a 403 googleapi.Error", err)
	}
}
