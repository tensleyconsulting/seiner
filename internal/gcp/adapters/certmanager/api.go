package certmanager

import (
	"context"
	"time"
)

// Certificate is the Google-free slice of a Certificate Manager
// certificate. The PEM body and private key are not copied.
type Certificate struct {
	Name        string // short name, not the full resource name
	Location    string
	Description string
	Type        string // MANAGED, SELF_MANAGED, or MANAGED_IDENTITY
	Scope       string // DEFAULT, EDGE_CACHE, ALL_REGIONS, CLIENT_AUTH; "" for DEFAULT
	SANs        []string
	// State is a managed certificate's state word (ACTIVE, PROVISIONING,
	// FAILED); "" for self-managed ones.
	State string
	// Issue is a managed certificate's provisioning issue, "REASON: details";
	// "" when there is none.
	Issue string
	// Domains, DNSAuthorizations, IssuanceConfig, and Authorizations are
	// set on Google-managed certificates only.
	Domains           []string
	DNSAuthorizations []string // full names
	IssuanceConfig    string   // full name
	Authorizations    []Authorization
	Identity          string // workload identity of a managed-identity certificate
	Expires           time.Time
	UsedBy            []string // full names of the resources using it
	Created           time.Time
	Updated           time.Time
	Labels            map[string]string
}

// Authorization is the latest authorization attempt for one domain of a
// managed certificate.
type Authorization struct {
	Domain  string
	State   string // AUTHORIZING, AUTHORIZED, FAILED
	Reason  string // failure reason: CONFIG, CAA, RATE_LIMITED; "" if none
	Details string
}

// Map is a certificate map: the hostname-to-certificate table a load
// balancer target proxy serves.
type Map struct {
	Name        string
	Location    string
	Description string
	Targets     []string // full target proxy names
	Addresses   []string // ip:port1,port2 per target IP config
	Created     time.Time
	Updated     time.Time
	Labels      map[string]string
}

// MapEntry is one hostname (or the primary entry) in a certificate map.
type MapEntry struct {
	Name         string
	Description  string
	Hostname     string   // "" for a matcher entry
	Matcher      string   // PRIMARY; "" for a hostname entry
	State        string   // ACTIVE, PENDING
	Certificates []string // full names
	Created      time.Time
	Updated      time.Time
	Labels       map[string]string
}

// DNSAuthorization proves control of a domain for managed certificates.
type DNSAuthorization struct {
	Name        string
	Location    string
	Description string
	Domain      string
	Type        string // FIXED_RECORD, PER_PROJECT_RECORD
	RecordName  string // the CNAME to create
	RecordType  string
	RecordData  string
	Created     time.Time
	Updated     time.Time
	Labels      map[string]string
}

// IssuanceConfig tells Certificate Manager to issue managed certificates
// from a CA Service pool.
type IssuanceConfig struct {
	Name           string
	Location       string
	Description    string
	CAPool         string // full name
	KeyAlgorithm   string // RSA_2048, ECDSA_P256
	Lifetime       string // e.g. "2592000s"
	RotationWindow int64  // percent of lifetime
	Created        time.Time
	Updated        time.Time
	Labels         map[string]string
}

// TrustConfig is the CA set an mTLS policy validates client certificates
// against. Only counts are copied, not the PEM bodies.
type TrustConfig struct {
	Name          string
	Location      string
	Description   string
	TrustAnchors  int
	Intermediates int
	Allowlisted   int
	SPIFFEDomains []string
	Created       time.Time
	Updated       time.Time
	Labels        map[string]string
}

// API is the Certificate Manager ↔ Resource boundary. Tests inject a fake;
// live wraps the REST client. One API serves every provider in this
// package.
type API interface {
	// Locations reports the Certificate Manager locations of a project.
	// Every list is per location.
	Locations(ctx context.Context, project string) ([]string, error)
	ListCertificates(ctx context.Context, project, location string) ([]Certificate, error)
	ListMaps(ctx context.Context, project, location string) ([]Map, error)
	// ListMapEntries lists a map's entries, stopping after limit entries,
	// and reports whether more were left.
	ListMapEntries(ctx context.Context, project, location, certMap string, limit int) ([]MapEntry, bool, error)
	ListDNSAuthorizations(ctx context.Context, project, location string) ([]DNSAuthorization, error)
	ListIssuanceConfigs(ctx context.Context, project, location string) ([]IssuanceConfig, error)
	ListTrustConfigs(ctx context.Context, project, location string) ([]TrustConfig, error)
}
