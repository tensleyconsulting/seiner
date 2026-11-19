package iap

import (
	"testing"

	gce "google.golang.org/api/compute/v1"
	iapapi "google.golang.org/api/iap/v1"
)

func TestFromBackend(t *testing.T) {
	t.Parallel()
	got := fromBackend(&gce.BackendService{
		Name:                "grafana",
		Region:              "https://www.googleapis.com/compute/v1/projects/p/regions/us-central1",
		LoadBalancingScheme: "INTERNAL_MANAGED",
		Protocol:            "HTTP",
		CreationTimestamp:   "2025-02-03T02:00:00.000-08:00",
		Iap: &gce.BackendServiceIAP{
			Enabled:                  true,
			Oauth2ClientId:           "123-abc.apps.googleusercontent.com",
			Oauth2ClientSecretSha256: "deadbeef",
		},
	})
	if got.Name != "grafana" || got.Region != "us-central1" || got.Scheme != "INTERNAL_MANAGED" ||
		got.ClientID != "123-abc.apps.googleusercontent.com" || got.Created.IsZero() {
		t.Fatalf("fromBackend = %+v", got)
	}
	if global := fromBackend(&gce.BackendService{Name: "admin-backend"}); global.Region != "" || global.ClientID != "" {
		t.Fatalf("global = %+v", global)
	}
}

func TestFromSettings(t *testing.T) {
	t.Parallel()
	got := fromSettings(&iapapi.IapSettings{
		AccessSettings: &iapapi.AccessSettings{
			ReauthSettings:         &iapapi.ReauthSettings{Method: "LOGIN", MaxAge: "3600s", PolicyType: "MINIMUM"},
			AllowedDomainsSettings: &iapapi.AllowedDomainsSettings{Enable: true, Domains: []string{"example.com"}},
			CorsSettings:           &iapapi.CorsSettings{AllowHttpOptions: true},
			GcipSettings:           &iapapi.GcipSettings{TenantIds: []string{"tenant-1"}},
			OauthSettings: &iapapi.OAuthSettings{
				LoginHint: "example.com", ProgrammaticClients: []string{"cli"},
				ClientId: "id", ClientSecret: "must-not-copy",
			},
		},
		ApplicationSettings: &iapapi.ApplicationSettings{
			CookieDomain:             "example.com",
			AccessDeniedPageSettings: &iapapi.AccessDeniedPageSettings{AccessDeniedPageUri: "https://example.com/denied"},
		},
	})
	if got.ReauthMethod != "LOGIN" || got.ReauthMaxAge != "3600s" || got.ReauthPolicy != "MINIMUM" ||
		len(got.AllowedDomains) != 1 || !got.CORSPreflight || len(got.GCIPTenants) != 1 || got.LoginHint != "example.com" ||
		len(got.ProgrammaticClients) != 1 || got.CookieDomain != "example.com" || got.AccessDeniedPage == "" {
		t.Fatalf("fromSettings = %+v", got)
	}
	// Allowed domains that are switched off are not in force.
	off := fromSettings(&iapapi.IapSettings{AccessSettings: &iapapi.AccessSettings{
		AllowedDomainsSettings: &iapapi.AllowedDomainsSettings{Domains: []string{"example.com"}},
	}})
	if len(off.AllowedDomains) != 0 {
		t.Fatalf("disabled domains = %v", off.AllowedDomains)
	}
	if empty := fromSettings(&iapapi.IapSettings{}); empty.ReauthMethod != "" || empty.CookieDomain != "" {
		t.Fatalf("empty = %+v", empty)
	}
}
