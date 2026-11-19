package artifactregistry

import (
	"context"
	"time"
)

// Repository is the Google-free slice of an Artifact Registry repository.
type Repository struct {
	Name        string // short name, not the full resource name
	Location    string // region or multi-region (us-central1, us)
	Description string
	Format      string // DOCKER, MAVEN, NPM, PYTHON, APT, YUM, GO, …
	Mode        string // STANDARD_REPOSITORY, REMOTE_REPOSITORY, VIRTUAL_REPOSITORY
	RegistryURI string // us-central1-docker.pkg.dev/p/repo
	SizeBytes   int64
	KMSKey      string
	// ImmutableTags is set on Docker repositories that forbid moving tags.
	ImmutableTags   bool
	CleanupPolicies int
	CleanupDryRun   bool
	// ScanState is the vulnerability scanning enablement state
	// (SCANNING_ACTIVE, SCANNING_DISABLED, SCANNING_UNSUPPORTED); "" if unset.
	ScanState  string
	ScanReason string
	// Upstream describes a remote repository's source; "" otherwise.
	Upstream string
	// VirtualUpstreams counts a virtual repository's upstream policies.
	VirtualUpstreams int
	// CleanupRules spells out each cleanup policy: "id  ACTION  condition".
	CleanupRules []string
	// UpstreamRules are a virtual repository's upstreams, by priority:
	// "priority 10  projects/p/locations/l/repositories/r".
	UpstreamRules []string
	// RemoteSource is a remote repository's source: a public preset
	// (DOCKER_HUB) or a custom URI.
	RemoteSource string
	// UpstreamUser and UpstreamSecret name a remote repository's upstream
	// credentials: the username and the Secret Manager version holding the
	// password. The secret's value is never read.
	UpstreamUser       string
	UpstreamSecret     string
	NoUpstreamValidate bool
	MavenPolicy        string // RELEASE or SNAPSHOT
	MavenOverwrites    bool   // snapshot versions may be overwritten
	Created            time.Time
	Updated            time.Time
	Labels             map[string]string
}

// Package is one package in a repository. For Docker repositories it is an
// image (web/frontend).
type Package struct {
	Name        string // package id, unescaped (web/frontend)
	DisplayName string
	Created     time.Time
	Updated     time.Time
}

// Version is one version of a package. For Docker it is an image digest
// (sha256:…) and its tags.
type Version struct {
	Name        string // version id, unescaped (sha256:…, 1.2.3)
	Description string
	Tags        []string // tag names, not full resource names
	Created     time.Time
	Updated     time.Time
	// Docker image metadata; zero when the format has none.
	SizeBytes     int64
	MediaType     string
	ArtifactType  string
	BuildTime     time.Time
	SubjectDigest string // OCI referrer subject digest (sha256:…), when attached to a parent image
	ParentDigest  string // parent OCI image index digest (sha256:…), when a child manifest
	Platform      string // os/arch for index child manifests
	IsAttestation bool   // true for buildx attestation manifests (unknown/unknown)
}

// Signature is one cosign signature attached to a Docker image digest,
// discovered via the OCI 1.1 Referrers API (Sigstore bundles or simple
// signing referrers) or the legacy sha256-<hex>.sig tag.
type Signature struct {
	ID             string // unique under the signed version (manifest digest, plus layer index when >1)
	Source         string // "referrer" or "tag"
	Tag            string // sha256-<hex>.sig when Source is "tag"
	ManifestDigest string // sha256:…
	ArtifactType   string // application/vnd.dev.sigstore.bundle.v0.3+json, application/vnd.dev.cosign.simplesigning.v1+json, …
	PredicateType  string // dev.sigstore.bundle.predicateType when set
	PayloadDigest  string // layer blob digest (sha256:…)
	Signature      string // base64-encoded signature
	KeyHint        string // publicKey.hint or DSSE keyid when key-signed
	CertificatePEM string // leaf X.509 PEM certificate (Fulcio keyless or X.509 cert)
	ChainPEM       []string
	BundleJSON     string // raw Sigstore bundle or Rekor bundle annotation JSON
	HasRekor       bool   // true when RekorLogIndex is populated (index 0 is valid in private Rekor)
	RekorLogIndex  int64
	RekorLogID     string
	IntegratedTime time.Time
}

// Vulnerability is one vulnerability occurrence from Artifact Analysis.
type Vulnerability struct {
	ID           string // occurrence name, projects/p/occurrences/o
	CVE          string // note id, e.g. CVE-2024-1234
	Severity     string // note provider's severity
	Effective    string // distro severity when known, else Severity
	CVSS         float64
	CVSSVersion  string // CVSS_VERSION_3, …
	FixAvailable bool
	Summary      string
	Details      string
	Packages     []AffectedPackage
	URLs         []string
	Published    time.Time
	Created      time.Time
	Updated      time.Time
}

// AffectedPackage is one package a vulnerability was found in.
type AffectedPackage struct {
	Name      string
	Type      string // OS, MAVEN, GO, …
	Installed string
	Fixed     string // "" when no fix is available
	Path      string // file path for language packages; "" for OS packages
}

// Binding is one role binding in a repository's IAM policy.
type Binding struct {
	Role      string
	Members   []string
	Condition string // condition title, "" when unconditional
}

// API is the Artifact Registry ↔ Resource boundary. Tests inject a fake;
// live wraps the Artifact Registry gRPC client.
type API interface {
	// ListLocations lists the locations Artifact Registry serves for project.
	ListLocations(ctx context.Context, project string) ([]string, error)
	ListRepositories(ctx context.Context, project, location string) ([]Repository, error)
	// GetRepositoryPolicy reads the bindings set on the repository itself.
	GetRepositoryPolicy(ctx context.Context, project, location, name string) ([]Binding, error)
	ListPackages(ctx context.Context, project, location, repo string, limit int) (pkgs []Package, truncated bool, err error)
	ListVersions(ctx context.Context, project, location, repo, pkg string, limit int) (versions []Version, truncated bool, err error)
	ListSignatures(ctx context.Context, project, location, repo, pkg, digest string, limit int) (sigs []Signature, truncated bool, err error)
	ListArtifacts(ctx context.Context, project, location, repo, pkg, digest string, limit int) (artifacts []Version, truncated bool, err error)
}

// ScanAPI is the Artifact Analysis (Container Analysis) ↔ Resource boundary.
type ScanAPI interface {
	// ListVulnerabilities lists vulnerability occurrences for one image,
	// identified by its https:// resource URL.
	ListVulnerabilities(ctx context.Context, project, resourceURL string, limit int) (vulns []Vulnerability, truncated bool, err error)
}
