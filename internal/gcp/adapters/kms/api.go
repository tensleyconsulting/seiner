package kms

import (
	"context"
	"time"
)

// KeyRing is the Google-free slice of a Cloud KMS key ring the adapter needs.
type KeyRing struct {
	Name     string // short name, not the full resource name
	Location string // us-central1, global, us, europe, ...
	Created  time.Time
}

// Key is one crypto key in a key ring. Key material is never read.
type Key struct {
	Name            string
	Purpose         string // ENCRYPT_DECRYPT, ASYMMETRIC_SIGN, MAC, ...
	Algorithm       string // GOOGLE_SYMMETRIC_ENCRYPTION, EC_SIGN_P256_SHA256, ...
	ProtectionLevel string // SOFTWARE, HSM, EXTERNAL, EXTERNAL_VPC
	Primary         string // primary version number; "" for asymmetric keys
	PrimaryState    string // ENABLED, DISABLED, DESTROY_SCHEDULED, ...
	RotationPeriod  string // e.g. "7776000s"
	NextRotation    time.Time
	DestroyDelay    string // e.g. "2592000s"
	ImportOnly      bool
	Backend         string // EKM connection or HSM single-tenant backend
	Created         time.Time
	Labels          map[string]string
	// TemplateAlgorithm and TemplateProtection are the version template's:
	// what the next version gets. They differ from Algorithm and
	// ProtectionLevel when the template changed after the primary was made.
	TemplateAlgorithm  string
	TemplateProtection string
	AccessReasons      []string // Key Access Justifications allowed reasons
}

// KeyVersion is one crypto key version. Key material is never read; an HSM
// version's attestation is read only on request (GetAttestation).
type KeyVersion struct {
	Name             string // version number, "4"
	State            string // ENABLED, DISABLED, DESTROY_SCHEDULED, ...
	Algorithm        string
	ProtectionLevel  string
	Created          time.Time
	Generated        time.Time
	Imported         time.Time
	DestroyScheduled time.Time // when a DESTROY_SCHEDULED version is destroyed
	Destroyed        time.Time
	ImportJob        string // short name of the import job, if imported
	FailureReason    string // generation, import, or external destruction failure
	ExternalKeyURI   string
	ReimportEligible bool
}

// Attestation is an HSM key version's attestation: the statement the HSM
// signed when it made the key, and the certificate chains that check it. It
// holds no key material. Only HSM versions have one.
type Attestation struct {
	Format  string // CAVIUM_V1_COMPRESSED, CAVIUM_V2_COMPRESSED, ...
	Content []byte // as the HSM wrote it; gzip for the _COMPRESSED formats
	// The chains are PEM certificates, leaf first.
	Cavium          []string
	GoogleCard      []string
	GooglePartition []string
}

// ImportJob wraps key material for import into a key ring. The wrapping
// public key is never read.
type ImportJob struct {
	Name            string
	State           string // PENDING_GENERATION, ACTIVE, EXPIRED
	Method          string // RSA_OAEP_3072_SHA1_AES_256, ...
	ProtectionLevel string
	Backend         string
	Created         time.Time
	Generated       time.Time
	Expires         time.Time
	Expired         time.Time
}

// Binding is one IAM role binding on a key ring or key.
type Binding struct {
	Role      string
	Members   []string
	Condition string // condition title, "" when unconditional
}

// API is the Cloud KMS ↔ Resource boundary. Tests inject a fake; live wraps
// the Cloud KMS REST client.
type API interface {
	// ListLocations reports the KMS locations of a project. Key rings are
	// listed per location.
	ListLocations(ctx context.Context, project string) ([]string, error)
	ListKeyRings(ctx context.Context, project, location string) ([]KeyRing, error)
	// ListKeys stops after limit keys and reports whether more were left.
	ListKeys(ctx context.Context, project, location, ring string, limit int) ([]Key, bool, error)
	// ListVersions lists a key's versions, stopping after limit.
	ListVersions(ctx context.Context, project, location, ring, key string, limit int) ([]KeyVersion, bool, error)
	// GetAttestation reads one version's attestation. ok is false when the
	// version has none (not HSM, or not generated yet).
	GetAttestation(ctx context.Context, project, location, ring, key, version string) (a Attestation, ok bool, err error)
	// ListImportJobs lists a key ring's import jobs, stopping after limit.
	ListImportJobs(ctx context.Context, project, location, ring string, limit int) ([]ImportJob, bool, error)
	// GetIAMPolicy reads the bindings set on a key ring, or on one of its
	// keys when key is not empty. Inherited bindings are not included.
	GetIAMPolicy(ctx context.Context, project, location, ring, key string) ([]Binding, error)
	// GetImportJobIAMPolicy reads the bindings set directly on an import job.
	GetImportJobIAMPolicy(ctx context.Context, project, location, ring, job string) ([]Binding, error)
}
