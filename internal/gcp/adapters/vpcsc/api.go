package vpcsc

import (
	"context"
	"errors"
)

// ErrDenied marks a permission-denied answer from Access Context Manager.
// The live API wraps the Google error with it, so a project's perimeter
// lookup can stay silent for a caller who cannot read the organization's
// policies while Classify still reads the Google error underneath.
var ErrDenied = errors.New("vpcsc: permission denied")

// Policy is the Google-free slice of an access policy.
type Policy struct {
	Name   string // the policy number, without accessPolicies/
	Title  string
	Scopes []string // folders/N or projects/N for a scoped policy; empty at org level
}

// Config is one side of a perimeter: the enforced status or the dry-run
// spec.
type Config struct {
	Resources          []string // projects/NUMBER, //compute.googleapis.com/projects/ID/global/networks/N
	RestrictedServices []string
	AccessLevels       []string // short names, without accessPolicies/N/accessLevels/
	Ingress            []Rule
	Egress             []Rule
	VPCRestricted      bool     // VPC accessible services are limited
	VPCAllowed         []string // the services VPC networks may reach when restricted
}

// Rule is one ingress or egress policy of a perimeter. Rules have no id;
// two rules are the same rule when their contents are equal.
type Rule struct {
	Title        string
	Identities   []string // user:, group:, serviceAccount:, principal://...
	IdentityType string   // ANY_IDENTITY, ANY_USER_ACCOUNT, ANY_SERVICE_ACCOUNT; "" when Identities is set
	// Sources are where a request may come from: an access level's short
	// name, a project or VPC network resource, "*", or "psc FORWARDING_RULE".
	Sources []string
	// SourceRestriction is an egress rule's SOURCE_RESTRICTION_ENABLED or
	// _DISABLED; "" for ingress.
	SourceRestriction string
	Operations        []Operation
	Resources         []string // what the rule reaches; "*" for every resource
	ExternalResources []string // egress only: s3://bucket, ...
	Roles             []string
}

// Operation is the methods a rule allows on one service.
type Operation struct {
	Service string // storage.googleapis.com, or "*"
	// Methods are method names (google.storage.objects.get) and permissions
	// written as "permission bigquery.tables.getData"; "*" for all.
	Methods []string
}

// Perimeter is the Google-free slice of a service perimeter.
type Perimeter struct {
	Name        string // short name, without accessPolicies/N/servicePerimeters/
	Title       string
	Description string
	Type        string // PERIMETER_TYPE_REGULAR or PERIMETER_TYPE_BRIDGE
	Status      *Config
	// Spec is the dry-run config. It is nil unless the perimeter uses an
	// explicit dry-run spec; without one, dry-run mode tests the status.
	Spec *Config
}

// Condition is one condition of a basic access level.
type Condition struct {
	IPSubnetworks  []string
	Regions        []string
	Members        []string
	RequiredLevels []string // short names
	VPCNetworks    []string
	Device         *Device // nil when the condition has no device policy
	Negate         bool
}

// Device is a condition's device policy.
type Device struct {
	Encryption    []string // ENCRYPTED, UNENCRYPTED, ...
	Management    []string // BASIC, COMPLETE, NONE
	OS            []OS
	Screenlock    bool
	CorpOwned     bool
	AdminApproval bool
}

// OS is one allowed operating system.
type OS struct {
	Type       string // DESKTOP_MAC, DESKTOP_WINDOWS, ..., IOS, ANDROID
	MinVersion string // "" for any version
	Verified   bool   // Chrome OS only: verified boot required
}

// Level is the Google-free slice of an access level. Basic levels have
// Conditions; custom levels have Expression.
type Level struct {
	Name        string // short name, without accessPolicies/N/accessLevels/
	Title       string
	Description string
	Combining   string // AND or OR; basic levels only
	Conditions  []Condition
	Expression  string // CEL; custom levels only
}

// Project is what the perimeter lookup needs to know about a project.
type Project struct {
	Number string
	Org    string // organizations/N; "" when the project has no organization
}

// API is the Access Context Manager ↔ Resource boundary. Tests inject a
// fake; live wraps the Access Context Manager and Resource Manager REST
// clients.
type API interface {
	// ListPolicies lists the access policies of org (organizations/N).
	ListPolicies(ctx context.Context, org string) ([]Policy, error)
	ListPerimeters(ctx context.Context, policy string) ([]Perimeter, error)
	GetPerimeter(ctx context.Context, policy, name string) (Perimeter, error)
	ListLevels(ctx context.Context, policy string) ([]Level, error)
	// Project resolves a project id to its number and organization.
	Project(ctx context.Context, project string) (Project, error)
}
