package iam

import (
	"context"
	"time"
)

// Binding is one IAM role binding on a CRM resource.
type Binding struct {
	Role      string
	Members   []string
	Condition string // condition title; "" when unconditional
}

// Policy is the Google-free GetIamPolicy result.
type Policy struct {
	Bindings []Binding
}

// Node is the Google-free slice of a CRM project, folder, or organization
// that the ancestry walk needs.
type Node struct {
	DisplayName string
	Parent      string // folders/… or organizations/…; "" for an organization
}

// API is the IAM policy ↔ Resource boundary, served by Resource Manager.
type API interface {
	GetPolicy(ctx context.Context, resource string) (Policy, error)
	// GetNode reads the CRM node name (projects/p, folders/2, or
	// organizations/1).
	GetNode(ctx context.Context, name string) (Node, error)
}

// ServiceAccount is the Google-free slice of an IAM service account.
type ServiceAccount struct {
	Email          string
	DisplayName    string
	Description    string
	UniqueID       string
	OAuth2ClientID string
	Disabled       bool
}

// ServiceAccountKey is a service account key's metadata. Key material,
// public or private, is never read.
type ServiceAccountKey struct {
	ID          string // the key id, last segment of its name
	Type        string // USER_MANAGED or SYSTEM_MANAGED
	Origin      string // GOOGLE_PROVIDED or USER_PROVIDED
	Algorithm   string // KEY_ALG_RSA_2048, ...
	ValidAfter  time.Time
	ValidBefore time.Time
	Disabled    bool
}

// Role is the Google-free slice of an IAM role, predefined or custom.
type Role struct {
	Name        string // full name: projects/p/roles/r or organizations/1/roles/r
	Title       string
	Description string
	Stage       string // launch stage word: GA, BETA, ALPHA, EAP, DEPRECATED, DISABLED
	Permissions []string
}

// AdminAPI is the IAM Admin ↔ Resource boundary: service accounts and
// custom roles, served by iam.googleapis.com.
type AdminAPI interface {
	ListServiceAccounts(ctx context.Context, project string) ([]ServiceAccount, error)
	// ListServiceAccountKeys lists an account's keys, user- and
	// system-managed.
	ListServiceAccountKeys(ctx context.Context, project, email string) ([]ServiceAccountKey, error)
	// GetServiceAccountPolicy reads the IAM policy set directly on a service
	// account.
	GetServiceAccountPolicy(ctx context.Context, project, email string) (Policy, error)
	// ListRoles lists the custom roles defined on parent (projects/p or
	// organizations/1), with their permissions.
	ListRoles(ctx context.Context, parent string) ([]Role, error)
	// GetRole reads one role, predefined (roles/viewer) or custom
	// (projects/p/roles/r), with its permissions.
	GetRole(ctx context.Context, name string) (Role, error)
}
