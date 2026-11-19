package acm

import (
	"context"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/vpcsc"
)

// Session is a binding's session controls.
type Session struct {
	Length        string // e.g. "28800s"; "" when not set
	LengthEnabled bool
	MaxInactivity string
	ReauthMethod  string // LOGIN, SECURITY_KEY, PASSWORD
	UseOIDCMaxAge bool
}

// Scoped is one scoped access setting: levels and session controls that
// apply to one client application or one project instead of the whole
// binding.
type Scoped struct {
	App           string // the client application's name, or its client id
	Project       string // projects/NUMBER
	Levels        []string
	DryRunLevels  []string
	Session       *Session
	DryRunSession *Session
}

// Binding is the Google-free slice of a GCP user access binding. Exactly
// one of GroupKey, ServiceAccount, SAProject, and Federated is set.
type Binding struct {
	Name           string // the binding id, without organizations/N/gcpUserAccessBindings/
	GroupKey       string // the group's Directory id, not its email
	ServiceAccount string
	SAProject      string   // every service account of this project number
	Federated      string   // principal:// or principalSet:// identifier
	Policy         string   // the access policy number its levels belong to; "" when it names none
	Levels         []string // short names, without accessPolicies/N/accessLevels/
	DryRunLevels   []string
	Session        *Session
	Scoped         []Scoped
}

// Group is what Cloud Identity says about a group.
type Group struct {
	Email       string
	DisplayName string
}

// API is the Access Context Manager ↔ Resource boundary. Tests inject a
// fake; live wraps the Access Context Manager and Cloud Identity REST
// clients.
type API interface {
	// ListBindings lists org's (organizations/N) group bindings.
	ListBindings(ctx context.Context, org string) ([]Binding, error)
	// ListServiceAccountBindings lists org's bindings for service accounts,
	// which the API returns only when asked for them.
	ListServiceAccountBindings(ctx context.Context, org string) ([]Binding, error)
	// GetBinding reads one binding of org by its id.
	GetBinding(ctx context.Context, org, name string) (Binding, error)
	// Group looks up a group by its Directory id.
	Group(ctx context.Context, key string) (Group, error)
	// ListLevels lists an access policy's levels, as VPC Service Controls
	// reads them.
	ListLevels(ctx context.Context, policy string) ([]vpcsc.Level, error)
}
