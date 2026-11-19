package iap

import (
	"context"
	"time"
)

// Backend is the Google-free slice of a backend service with IAP turned on.
// OAuth client secrets are never read.
type Backend struct {
	Name        string
	Region      string // "" for a global backend service
	Description string
	Scheme      string // EXTERNAL_MANAGED, INTERNAL_MANAGED, ...
	Protocol    string
	ClientID    string // custom OAuth client; "" for the Google-managed one
	Created     time.Time
}

// Settings is the IAP settings set directly on one resource. Settings
// inherited from the project, folder, or organization are not included.
// OAuth client secrets are never read.
type Settings struct {
	ReauthMethod        string // LOGIN, SECURE_KEY, ...; "" when off
	ReauthMaxAge        string // e.g. "3600s"
	ReauthPolicy        string // MINIMUM, DEFAULT
	AllowedDomains      []string
	CORSPreflight       bool // HTTP OPTIONS pass through unauthenticated
	IdentitySources     []string
	GCIPTenants         []string
	LoginHint           string
	ProgrammaticClients []string
	CookieDomain        string
	AccessDeniedPage    string
}

// Binding is one role binding in an IAP resource's IAM policy.
type Binding struct {
	Role      string
	Members   []string
	Condition string // condition title, "" when unconditional
}

// API is the IAP ↔ Resource boundary. Tests inject a fake; live wraps the
// Compute, IAP, and Resource Manager REST clients. resource names below are
// IAP's own, which start with the project number: projects/123/iap_web/...
type API interface {
	// ProjectNumber resolves a project id to the number IAP names use.
	ProjectNumber(ctx context.Context, project string) (string, error)
	// ListBackends lists the project's backend services that have IAP on.
	ListBackends(ctx context.Context, project string) ([]Backend, error)
	GetSettings(ctx context.Context, resource string) (Settings, error)
	GetPolicy(ctx context.Context, resource string) ([]Binding, error)
}
