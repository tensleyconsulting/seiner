package swp

import (
	"context"
	"time"
)

// Gateway is the Google-free slice of a Secure Web Proxy gateway the adapter
// needs. Only gateways of type SECURE_WEB_GATEWAY reach the provider.
type Gateway struct {
	Name         string // short name, not the full resource name
	Location     string
	Description  string
	Policy       string // full gateway security policy name
	Addresses    []string
	Ports        []int64
	RoutingMode  string // EXPLICIT_ROUTING_MODE, NEXT_HOP_ROUTING_MODE
	Network      string // full network name
	Subnetwork   string // full subnetwork name
	Certificates []string
	Scope        string
	Created      time.Time
	Labels       map[string]string
}

// Policy is a gateway security policy: the rule set a proxy enforces.
type Policy struct {
	Name          string
	Location      string
	Description   string
	TLSInspection string // full TLS inspection policy name, or ""
	Created       time.Time
}

// Rule is one gateway security policy rule.
type Rule struct {
	Name               string
	Description        string
	Priority           int64
	Action             string // ALLOW, DENY
	Enabled            bool
	SessionMatcher     string // CEL
	ApplicationMatcher string // CEL
	TLSInspection      bool
	Created            time.Time
}

// URLList is a named list of URL patterns that rules match by name.
type URLList struct {
	Name        string
	Location    string
	Description string
	Values      []string
	Created     time.Time
}

// TLSInspectionPolicy names the CA pool a proxy signs inspected traffic with.
type TLSInspectionPolicy struct {
	Name          string
	Location      string
	Description   string
	CAPool        string
	TrustConfig   string
	MinTLSVersion string
	Profile       string // PROFILE_COMPATIBLE, PROFILE_MODERN, ...
	ExcludePublic bool
	Created       time.Time
}

// API is the Secure Web Proxy ↔ Resource boundary. Tests inject a fake; live
// wraps the Network Services and Network Security REST clients. One API
// serves every provider in this package.
type API interface {
	// GatewayLocations reports the Network Services locations of a project.
	// Gateways are listed per location.
	GatewayLocations(ctx context.Context, project string) ([]string, error)
	// SecurityLocations reports the Network Security locations of a project.
	// Policies, URL lists, and TLS inspection policies are listed per
	// location.
	SecurityLocations(ctx context.Context, project string) ([]string, error)
	// ListGateways lists the Secure Web Proxy gateways in one location,
	// leaving out other gateway types.
	ListGateways(ctx context.Context, project, location string) ([]Gateway, error)
	ListPolicies(ctx context.Context, project, location string) ([]Policy, error)
	// ListRules lists a policy's rules, stopping after limit rules, and
	// reports whether more were left.
	ListRules(ctx context.Context, project, location, policy string, limit int) ([]Rule, bool, error)
	ListURLLists(ctx context.Context, project, location string) ([]URLList, error)
	GetURLList(ctx context.Context, project, location, name string) (URLList, error)
	ListTLSInspectionPolicies(ctx context.Context, project, location string) ([]TLSInspectionPolicy, error)
}
