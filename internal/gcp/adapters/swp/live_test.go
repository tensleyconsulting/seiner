package swp

import (
	"testing"

	nsec "google.golang.org/api/networksecurity/v1"
	nsvc "google.golang.org/api/networkservices/v1"
)

func TestFromGateway(t *testing.T) {
	t.Parallel()
	got := fromGateway("us-central1", &nsvc.Gateway{
		Name:                  "projects/p/locations/us-central1/gateways/egress-proxy",
		Type:                  secureWebGateway,
		GatewaySecurityPolicy: "projects/p/locations/us-central1/gatewaySecurityPolicies/egress-policy",
		Addresses:             []string{"10.128.0.100"},
		Ports:                 []int64{443},
		RoutingMode:           "EXPLICIT_ROUTING_MODE",
		CertificateUrls:       []string{"projects/p/locations/us-central1/certificates/c"},
		CreateTime:            "2025-06-01T09:00:00.123456789Z",
		Labels:                map[string]string{"env": "prod"},
	})
	if got.Name != "egress-proxy" || got.Location != "us-central1" || got.Policy == "" || len(got.Ports) != 1 ||
		got.RoutingMode != "EXPLICIT_ROUTING_MODE" || got.Created.IsZero() || got.Labels["env"] != "prod" {
		t.Fatalf("fromGateway = %+v", got)
	}
}

func TestFromRule(t *testing.T) {
	t.Parallel()
	got := fromRule(&nsec.GatewaySecurityPolicyRule{
		Name:                 "projects/p/locations/l/gatewaySecurityPolicies/x/rules/allow-github",
		BasicProfile:         "ALLOW",
		Priority:             100,
		Enabled:              true,
		SessionMatcher:       "host() == 'github.com'",
		TlsInspectionEnabled: true,
		CreateTime:           "2025-06-01T09:00:00Z",
	})
	if got.Name != "allow-github" || got.Action != "ALLOW" || got.Priority != 100 || !got.Enabled ||
		got.SessionMatcher == "" || !got.TLSInspection || got.Created.IsZero() {
		t.Fatalf("fromRule = %+v", got)
	}
}

func TestFromTLS(t *testing.T) {
	t.Parallel()
	got := fromTLS("us-central1", &nsec.TlsInspectionPolicy{
		Name:               "projects/p/locations/us-central1/tlsInspectionPolicies/egress-tls",
		CaPool:             "projects/p/locations/us-central1/caPools/swp-pool",
		MinTlsVersion:      "TLS_1_2",
		TlsFeatureProfile:  "PROFILE_MODERN",
		ExcludePublicCaSet: true,
	})
	if got.Name != "egress-tls" || got.Location != "us-central1" || got.CAPool == "" || got.MinTLSVersion != "TLS_1_2" ||
		got.Profile != "PROFILE_MODERN" || !got.ExcludePublic {
		t.Fatalf("fromTLS = %+v", got)
	}
}

func TestIsZone(t *testing.T) {
	t.Parallel()
	for loc, want := range map[string]bool{
		"global":                false,
		"us-central1":           false,
		"europe-west12":         false,
		"africa-south1":         false,
		"us-central1-a":         true,
		"africa-south1-c":       true,
		"europe-west12-b":       true,
		"northamerica-south1-a": true,
	} {
		if got := isZone(loc); got != want {
			t.Errorf("isZone(%q) = %v, want %v", loc, got, want)
		}
	}
}
