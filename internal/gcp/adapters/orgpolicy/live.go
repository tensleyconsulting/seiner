package orgpolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"google.golang.org/api/option"
	op "google.golang.org/api/orgpolicy/v2"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/model"
)

type liveAPI struct{ svc *op.Service }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as this service's
// ProviderError instead.
func NewLazy(opts ...option.ClientOption) Provider {
	l := lazy.New(func(ctx context.Context) (*op.Service, error) {
		return op.NewService(ctx, opts...)
	})
	return New(lazyAPI{l: l})
}

type lazyAPI struct{ l *lazy.Value[*op.Service] }

func (a lazyAPI) ListPolicies(ctx context.Context, parent string) ([]Policy, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListPolicies(ctx, parent)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

// ListPolicies calls the organizations, folders, or projects collection's
// list, which returns only the policies set on parent itself.
func (a liveAPI) ListPolicies(ctx context.Context, parent string) ([]Policy, error) {
	var out []Policy
	collect := func(resp *op.GoogleCloudOrgpolicyV2ListPoliciesResponse) error {
		for _, p := range resp.Policies {
			out = append(out, fromPolicy(p))
		}
		return nil
	}
	var err error
	switch model.ScopeKind(parent) {
	case model.KindOrg:
		err = a.svc.Organizations.Policies.List(parent).Pages(ctx, collect)
	case model.KindFolder:
		err = a.svc.Folders.Policies.List(parent).Pages(ctx, collect)
	case model.KindProject:
		err = a.svc.Projects.Policies.List(parent).Pages(ctx, collect)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func fromPolicy(p *op.GoogleCloudOrgpolicyV2Policy) Policy {
	out := Policy{Constraint: lastSeg(p.Name), DryRun: p.DryRunSpec != nil}
	if s := p.Spec; s != nil {
		out.InheritFromParent = s.InheritFromParent
		out.Reset = s.Reset
		out.Updated = parseTime(s.UpdateTime)
		for _, r := range s.Rules {
			out.Rules = append(out.Rules, fromRule(r))
		}
	}
	if s := p.DryRunSpec; s != nil {
		if out.Updated.IsZero() {
			out.Updated = parseTime(s.UpdateTime)
		}
		out.DryRunInherit = s.InheritFromParent
		out.DryRunReset = s.Reset
		for _, r := range s.Rules {
			out.DryRunRules = append(out.DryRunRules, fromRule(r))
		}
	}
	return out
}

func fromRule(r *op.GoogleCloudOrgpolicyV2PolicySpecPolicyRule) Rule {
	out := Rule{AllowAll: r.AllowAll, DenyAll: r.DenyAll, Enforce: r.Enforce}
	if v := r.Values; v != nil {
		out.Allowed = v.AllowedValues
		out.Denied = v.DeniedValues
	}
	if c := r.Condition; c != nil {
		out.Condition = c.Title
		if out.Condition == "" {
			out.Condition = c.Expression
		}
	}
	if len(r.Parameters) > 0 {
		var buf bytes.Buffer
		if json.Compact(&buf, r.Parameters) == nil && buf.String() != "{}" && buf.String() != "null" {
			out.Parameters = buf.String()
		}
	}
	return out
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}
