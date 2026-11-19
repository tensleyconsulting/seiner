package certmanager

import (
	"testing"

	cm "google.golang.org/api/certificatemanager/v1"
)

func TestFromCertificate(t *testing.T) {
	t.Parallel()
	got := fromCertificate("global", &cm.Certificate{
		Name:        "projects/p/locations/global/certificates/web-cert",
		Scope:       "DEFAULT",
		SanDnsnames: []string{"example.com"},
		ExpireTime:  "2026-12-20T09:00:00Z",
		CreateTime:  "2025-03-10T09:00:00.123456789Z",
		UsedBy:      []*cm.UsedBy{{Name: "//certificatemanager.googleapis.com/projects/p/locations/global/certificateMaps/m/certificateMapEntries/e"}},
		Managed: &cm.ManagedCertificate{
			State:             "PROVISIONING",
			Domains:           []string{"example.com"},
			DnsAuthorizations: []string{"projects/p/locations/global/dnsAuthorizations/example-com"},
			ProvisioningIssue: &cm.ProvisioningIssue{Reason: "AUTHORIZATION_ISSUE", Details: "DNS record not found"},
			AuthorizationAttemptInfo: []*cm.AuthorizationAttemptInfo{
				{Domain: "example.com", State: "FAILED", FailureReason: "CONFIG", Details: "no CNAME"},
			},
		},
		PemCertificate: "-----BEGIN CERTIFICATE-----",
	})
	if got.Name != "web-cert" || got.Type != "MANAGED" || got.State != "PROVISIONING" || got.Scope != "" ||
		got.Issue != "AUTHORIZATION_ISSUE: DNS record not found" || got.Expires.IsZero() || got.Created.IsZero() ||
		len(got.UsedBy) != 1 || len(got.Authorizations) != 1 || got.Authorizations[0].Reason != "CONFIG" {
		t.Fatalf("fromCertificate = %+v", got)
	}
	self := fromCertificate("global", &cm.Certificate{Name: "x/self", Scope: "EDGE_CACHE", SelfManaged: &cm.SelfManagedCertificate{PemPrivateKey: "secret"}})
	if self.Type != "SELF_MANAGED" || self.Scope != "EDGE_CACHE" || self.State != "" {
		t.Fatalf("self-managed = %+v", self)
	}
}

func TestFromMap(t *testing.T) {
	t.Parallel()
	got := fromMap("global", &cm.CertificateMap{
		Name: "projects/p/locations/global/certificateMaps/web-map",
		GclbTargets: []*cm.GclbTarget{{
			TargetHttpsProxy: "projects/p/global/targetHttpsProxies/web-proxy",
			IpConfigs:        []*cm.IpConfig{{IpAddress: "203.0.113.20", Ports: []int64{443, 8443}}},
		}},
	})
	if got.Name != "web-map" || len(got.Targets) != 1 || len(got.Addresses) != 1 || got.Addresses[0] != "203.0.113.20:443,8443" {
		t.Fatalf("fromMap = %+v", got)
	}
}

func TestFromEntryAndTrust(t *testing.T) {
	t.Parallel()
	e := fromEntry(&cm.CertificateMapEntry{Name: "x/certificateMapEntries/www", Hostname: "www.example.com", Matcher: "MATCHER_UNSPECIFIED", State: "SERVING_STATE_UNSPECIFIED"})
	if e.Name != "www" || e.Matcher != "" || e.State != "" {
		t.Fatalf("fromEntry = %+v", e)
	}
	tc := fromTrust("global", &cm.TrustConfig{
		Name:                    "x/trustConfigs/mtls",
		TrustStores:             []*cm.TrustStore{{TrustAnchors: []*cm.TrustAnchor{{}, {}}, IntermediateCas: []*cm.IntermediateCA{{}}}},
		SpiffeTrustStores:       map[string]cm.TrustStore{"b.org": {TrustAnchors: []*cm.TrustAnchor{{}}}, "a.org": {}},
		AllowlistedCertificates: []*cm.AllowlistedCertificate{{}},
	})
	if tc.TrustAnchors != 3 || tc.Intermediates != 1 || tc.Allowlisted != 1 || len(tc.SPIFFEDomains) != 2 || tc.SPIFFEDomains[0] != "a.org" {
		t.Fatalf("fromTrust = %+v", tc)
	}
}
