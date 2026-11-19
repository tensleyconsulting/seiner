package orgpolicy

import (
	"context"
	"time"
)

// Policy is the Google-free slice of an organization policy the adapter
// needs: one constraint's policy set directly on an organization, folder, or
// project.
type Policy struct {
	Constraint        string // compute.vmExternalIpAccess
	Rules             []Rule
	InheritFromParent bool
	Reset             bool // restores the constraint's default
	DryRun            bool // a dry-run spec is set
	Updated           time.Time
	// The dry-run spec, when DryRun: what the policy would enforce.
	DryRunRules   []Rule
	DryRunInherit bool
	DryRunReset   bool
}

// Rule is one rule of a policy spec.
type Rule struct {
	AllowAll  bool
	DenyAll   bool
	Enforce   bool // boolean constraints; false is not distinguishable from unset
	Allowed   []string
	Denied    []string
	Condition string // the condition's title, or its expression without one
	// Parameters are a managed constraint's parameters, as compact JSON.
	Parameters string
}

// API is the Org Policy ↔ Resource boundary. Tests inject a fake; live wraps
// the Org Policy REST client.
type API interface {
	// ListPolicies lists the policies set directly on parent (organizations/o,
	// folders/f, or projects/p). Inherited policies are not included.
	ListPolicies(ctx context.Context, parent string) ([]Policy, error)
}
