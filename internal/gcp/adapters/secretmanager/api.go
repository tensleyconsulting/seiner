package secretmanager

import (
	"context"
	"time"
)

// Secret is the Google-free slice of a Secret Manager secret the adapter
// needs. It never carries secret payloads.
type Secret struct {
	Name           string   // short name, not the full resource name
	Replication    string   // "automatic" or "user-managed"
	Locations      []string // replica locations of a user-managed secret
	KMSKey         string   // customer-managed encryption key, if any
	RotationPeriod string   // e.g. "2592000s"
	NextRotation   time.Time
	Expires        time.Time
	Topics         []string // Pub/Sub topic names notified on changes
	Aliases        map[string]string
	DestroyTTL     string // delay before a destroyed version is purged
	Created        time.Time
	Labels         map[string]string
	Type           string            // secretType, when set
	Annotations    map[string]string // free-form annotations
}

// Version is one version of a secret, without its payload.
type Version struct {
	Name             string // the version number, "1"
	State            string // ENABLED, DISABLED, DESTROYED
	Created          time.Time
	Destroyed        time.Time
	ScheduledDestroy time.Time
	// KMSKeyVersions are the CMEK key versions that encrypt this version,
	// one per replica that reports one.
	KMSKeyVersions []string
}

// API is the Secret Manager ↔ Resource boundary. Tests inject a fake; live
// wraps the Secret Manager REST client.
type API interface {
	ListSecrets(ctx context.Context, project string) ([]Secret, error)
	// ListVersions stops after limit versions and reports whether more were
	// left.
	ListVersions(ctx context.Context, project, secret string, limit int) ([]Version, bool, error)
}
