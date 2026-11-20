package privateca

import (
	"context"
	"time"
)

// Pool is the Google-free slice of a Certificate Authority Service CA pool.
type Pool struct {
	Name                  string // short name, not the full resource name
	Location              string
	Tier                  string // ENTERPRISE, DEVOPS
	PublishCACert         bool
	PublishCRL            bool
	EncodingFormat        string // PEM, DER
	AllowedKeyTypes       []string
	MaxLifetime           string // e.g. "31536000s"
	BackdateDuration      string
	RequesterNotBefore    bool
	IssuanceModes         string // "CSR, config", "CSR only", "config only", "none", or ""
	IdentityPassthrough   string // "subject, SANs", "SANs", "subject", "none", or ""
	IdentityCEL           string
	PassthroughExtensions []string
	KMSKey                string
	Labels                map[string]string
}

// Authority is one certificate authority in a CA pool. Private key material
// lives in Cloud KMS and is never read; PEM CA certificates are exposed only
// as copy targets.
type Authority struct {
	Name          string
	Type          string // SELF_SIGNED, SUBORDINATE
	State         string // ENABLED, STAGED, DISABLED, AWAITING_USER_ACTIVATION, DELETED
	Tier          string // ENTERPRISE, DEVOPS
	Subject       string
	SANs          []string
	IssuerCA      string // full resource name when subordinate to another CAS CA
	NotBefore     time.Time
	NotAfter      time.Time
	Lifetime      string
	KeyAlgorithm  string
	KMSKeyVersion string
	Serial        string
	SHA256        string
	GCSBucket     string
	CACertURL     string
	CRLURLs       []string
	PEMCerts      []string // leaf CA first, root last
	Created       time.Time
	Updated       time.Time
	Deleted       time.Time
	PurgeTime     time.Time
	Labels        map[string]string
}

// IssuedCertificate is a signed X.509 certificate in an Enterprise CA pool.
type IssuedCertificate struct {
	Name             string
	Subject          string
	SANs             []string
	IssuerCA         string // full resource name
	Template         string // full resource name
	SubjectMode      string // DEFAULT, RDN_SEQUENCE, REFLECTED_SPIFFE
	NotBefore        time.Time
	NotAfter         time.Time
	Lifetime         string
	Serial           string
	SHA256           string
	Revoked          bool
	RevocationReason string
	RevokedAt        time.Time
	PEMCert          string
	PEMChain         []string // issuer-to-root
	Created          time.Time
	Updated          time.Time
	Labels           map[string]string
}

// RevokedCert is one entry in a certificate revocation list.
type RevokedCert struct {
	Certificate string // full resource name
	Serial      string
	Reason      string
}

// CRL is one revision of a Certificate Authority's X.509 Certificate
// Revocation List.
type CRL struct {
	Name           string
	State          string // ACTIVE, SUPERSEDED
	SequenceNumber int64
	RevisionID     string
	AccessURL      string
	Revoked        []RevokedCert
	PEMCRL         string
	Created        time.Time
	Updated        time.Time
	Labels         map[string]string
}

// Template is a managed certificate issuance template.
type Template struct {
	Name                  string
	Location              string
	Description           string
	MaxLifetime           string
	IsCA                  bool
	MaxIssuerPathLength   int64
	KeyUsages             []string
	ExtendedKeyUsages     []string
	IdentityPassthrough   string
	IdentityCEL           string
	PassthroughExtensions []string
	Created               time.Time
	Updated               time.Time
	Labels                map[string]string
}

// Binding is one IAM role binding on a CA pool or certificate template.
type Binding struct {
	Role      string
	Members   []string
	Condition string
}

// API is the Certificate Authority Service ↔ Resource boundary. Tests inject
// a fake; live wraps the Private CA v1 REST client.
type API interface {
	ListPools(ctx context.Context, project string) ([]Pool, error)
	ListAuthorities(ctx context.Context, project, location, pool string) ([]Authority, error)
	// ListCertificates stops after limit certificates and reports whether more were left.
	ListCertificates(ctx context.Context, project, location, pool string, limit int) ([]IssuedCertificate, bool, error)
	// ListCRLs stops after limit CRL revisions and reports whether more were left.
	ListCRLs(ctx context.Context, project, location, pool, ca string, limit int) ([]CRL, bool, error)
	ListTemplates(ctx context.Context, project string) ([]Template, error)
	GetPoolIAMPolicy(ctx context.Context, project, location, pool string) ([]Binding, error)
	GetTemplateIAMPolicy(ctx context.Context, project, location, template string) ([]Binding, error)
	GetCRLIAMPolicy(ctx context.Context, project, location, pool, ca, crl string) ([]Binding, error)
}
