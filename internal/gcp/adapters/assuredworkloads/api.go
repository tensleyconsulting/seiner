package assuredworkloads

import (
	"context"
	"time"
)

// Resource types from Workload.resources[].resourceType.
const (
	TypeConsumerFolder        = "CONSUMER_FOLDER"
	TypeEncryptionKeysProject = "ENCRYPTION_KEYS_PROJECT"
)

// Workload is the Google-free slice of an Assured Workload the annotator
// needs.
type Workload struct {
	Name              string // organizations/1/locations/us/workloads/w
	DisplayName       string
	ComplianceRegime  string // the API's enum word, e.g. "EU_DATA_BOUNDARY_AND_SUPPORT"
	Partner           string // "PARTNER_UNSPECIFIED" or ""
	SovereignControls bool
	KAJEnrollment     string // "KAJ_ENROLLMENT_STATE_COMPLETE", ...
	// HasStatus reports whether the API returned a compliance status; the
	// counts are zero when it did not.
	HasStatus              bool
	ActiveViolations       int64
	AcknowledgedViolations int64
	Resources              []Resource
	DisallowedServices     []string
	Created                time.Time
}

// Resource is one entry of Workload.resources[].
type Resource struct {
	ID   string // folder id or project number
	Type string // TypeConsumerFolder, TypeEncryptionKeysProject, ...
}

// API is the Assured Workloads boundary. Tests inject a fake; live wraps the
// REST client.
type API interface {
	// ListWorkloads lists the workloads of org (a bare id) in location.
	ListWorkloads(ctx context.Context, org, location string) ([]Workload, error)
}
