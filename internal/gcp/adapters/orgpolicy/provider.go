package orgpolicy

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// Provider implements model.ResourceProvider for organization policies. It
// lists only the policies set directly on a node; what a node inherits from
// its ancestors is not shown.
type Provider struct{ api API }

func New(api API) Provider { return Provider{api: api} }

func (p Provider) Service() model.ServiceType { return model.ServiceOrgPolicy }

// ListPermissions is what List needs: policies list at every CRM scope.
func (p Provider) ListPermissions(scopeID string) []string {
	if model.ScopeKind(scopeID) == "" {
		return nil
	}
	return []string{"orgpolicy.policies.list"}
}

func (p Provider) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if model.ScopeKind(scopeID) == "" {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("orgpolicy: no API")
	}
	pols, err := p.api.ListPolicies(ctx, scopeID)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(pols))
	for _, pol := range pols {
		out = append(out, policyResource(scopeID, pol))
	}
	return out, nil
}

// ListRelated lists a policy's allowed and denied values, one row each,
// under the policy. Org Policy has no cheaper read than the scope's list,
// so it lists the policy's scope again and picks the policy out.
func (p Provider) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceOrgPolicy || facetID != model.FacetPolicyValues {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if model.ScopeKind(parent.ScopeID) == "" {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("orgpolicy: no API")
	}
	pols, err := p.api.ListPolicies(ctx, parent.ScopeID)
	if err != nil {
		return nil, false, err
	}
	i := slices.IndexFunc(pols, func(pol Policy) bool { return pol.Constraint == parent.Name })
	if i < 0 {
		return nil, false, nil
	}
	return valueResources(parent, pols[i]), false, nil
}

// valueResources is one row per allowed or denied value of pol, in rule
// order, allowed before denied within a rule. A row names its rule only
// when the policy has more than one.
func valueResources(parent model.Resource, pol Policy) []model.Resource {
	var out []model.Resource
	add := func(n int, rule Rule, action, v string) {
		md := map[string]string{"action": action}
		if rule.Condition != "" {
			md["condition"] = rule.Condition
		}
		if len(pol.Rules) > 1 {
			md["rule"] = strconv.Itoa(n)
		}
		out = append(out, model.Resource{
			ID:       parent.ID + "/rules/" + strconv.Itoa(n) + "/" + action + "/" + url.PathEscape(v),
			Name:     v,
			Service:  model.ServiceOrgPolicyValue,
			ScopeID:  parent.ScopeID,
			Location: "global",
			Badge:    strings.ToUpper(action),
			ParentID: parent.ID,
			FacetID:  model.FacetPolicyValues,
			Metadata: md,
			Fields:   model.FieldsOf(md, "action", "condition", "rule"),
		})
	}
	for i, rule := range pol.Rules {
		for _, v := range rule.Allowed {
			add(i+1, rule, "allow", v)
		}
		for _, v := range rule.Denied {
			add(i+1, rule, "deny", v)
		}
	}
	return out
}

func policyResource(scopeID string, pol Policy) model.Resource {
	md := map[string]string{"enforcement": enforcement(pol)}
	if !pol.Reset && hasValues(pol.Rules) {
		md["inheritFromParent"] = onOff(pol.InheritFromParent)
	}
	if pol.DryRun {
		md["dryRun"] = enforcement(Policy{Rules: pol.DryRunRules, Reset: pol.DryRunReset})
		if !pol.DryRunReset && hasValues(pol.DryRunRules) {
			md["dryRunInheritFromParent"] = onOff(pol.DryRunInherit)
		}
	}
	if !pol.Updated.IsZero() {
		md["updated"] = pol.Updated.UTC().Format("2006-01-02 15:04 UTC")
	}
	r := model.Resource{
		ID:       scopeID + "/policies/" + url.PathEscape(pol.Constraint),
		Name:     pol.Constraint,
		Service:  model.ServiceOrgPolicy,
		ScopeID:  scopeID,
		Location: "global",
		Metadata: md,
		Fields:   model.FieldsOf(md, "enforcement", "inheritFromParent", "dryRun", "dryRunInheritFromParent", "updated"),
	}
	if pol.DryRun {
		r.Badge = "DRY-RUN"
	}
	if !pol.Reset && hasListValues(pol.Rules) {
		r.Facets = []model.Facet{{ID: model.FacetPolicyValues, Label: "Values", Inline: true}}
	}
	if needsRuleList(pol.Rules) {
		r.Lists = append(r.Lists, model.List{ID: "rules", Label: "Rules", Items: ruleTexts(pol.Rules)})
	}
	if pol.DryRun && needsRuleList(pol.DryRunRules) {
		r.Lists = append(r.Lists, model.List{ID: "dryRunRules", Label: "Dry-run rules", Items: ruleTexts(pol.DryRunRules)})
	}
	return r
}

// needsRuleList reports whether rules say more than their one-line
// summary: several rules, a condition, or parameters.
func needsRuleList(rules []Rule) bool {
	return len(rules) > 1 || len(rules) == 1 && (rules[0].Condition != "" || rules[0].Parameters != "")
}

func ruleTexts(rules []Rule) []string {
	items := make([]string, 0, len(rules))
	for _, rule := range rules {
		items = append(items, ruleText(rule))
	}
	return items
}

// enforcement summarizes a policy in a few words: its one rule, or how many
// rules it has.
func enforcement(pol Policy) string {
	switch {
	case pol.Reset:
		return "reset to default"
	case len(pol.Rules) == 0:
		if pol.DryRun {
			return "dry run only"
		}
		return "inherited"
	case len(pol.Rules) == 1 && pol.Rules[0].Condition == "" && pol.Rules[0].Parameters == "":
		return ruleSummary(pol.Rules[0])
	case len(pol.Rules) == 1:
		return "1 rule"
	default:
		return strconv.Itoa(len(pol.Rules)) + " rules"
	}
}

func ruleSummary(r Rule) string {
	switch {
	case r.Enforce:
		return "enforced"
	case r.AllowAll:
		return "allow all"
	case r.DenyAll:
		return "deny all"
	case len(r.Allowed) > 0 || len(r.Denied) > 0:
		var parts []string
		if len(r.Allowed) > 0 {
			parts = append(parts, "allow "+strings.Join(r.Allowed, ", "))
		}
		if len(r.Denied) > 0 {
			parts = append(parts, "deny "+strings.Join(r.Denied, ", "))
		}
		return strings.Join(parts, "; ")
	default:
		return "not enforced"
	}
}

// ruleText is one rule as a line in the Rules list, with its condition.
func ruleText(r Rule) string {
	s := ruleSummary(r)
	if r.Parameters != "" {
		s += "  parameters " + r.Parameters
	}
	if r.Condition != "" {
		s += "  when " + r.Condition
	}
	return s
}

// hasValues reports whether rules belong to a list constraint, where
// inheriting from the parent is meaningful.
func hasValues(rules []Rule) bool {
	for _, r := range rules {
		if r.AllowAll || r.DenyAll || len(r.Allowed) > 0 || len(r.Denied) > 0 {
			return true
		}
	}
	return false
}

// hasListValues reports whether any rule names allowed or denied values,
// which the policy then expands to.
func hasListValues(rules []Rule) bool {
	return slices.ContainsFunc(rules, func(r Rule) bool { return len(r.Allowed) > 0 || len(r.Denied) > 0 })
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

var (
	_ model.ResourceProvider = Provider{}
	_ model.RelatedLister    = Provider{}
)
