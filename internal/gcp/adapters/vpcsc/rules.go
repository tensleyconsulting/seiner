package vpcsc

import (
	"cmp"
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// ruleDelta counts the rules of b not in a, and of a not in b, matching
// rules by content.
func ruleDelta(a, b []Rule) (add, del int) {
	for _, x := range b {
		if !containsRule(a, x) {
			add++
		}
	}
	for _, x := range a {
		if !containsRule(b, x) {
			del++
		}
	}
	return add, del
}

func containsRule(rules []Rule, r Rule) bool {
	return slices.ContainsFunc(rules, func(x Rule) bool { return reflect.DeepEqual(x, r) })
}

// listRules lists a perimeter's ingress or egress rules: the enforced
// rules in order, each marked when the dry-run spec drops it, then the
// rules only the spec has. Rules have no id, so they are matched by
// content and named by position.
func (p Perimeters) listRules(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	id, ok := parseID(parent.ID)
	if !ok || id.kind != "servicePerimeters" {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("vpcsc: no API")
	}
	pm, err := p.api.GetPerimeter(ctx, id.policy, id.name)
	if err != nil {
		return nil, false, err
	}
	pick := func(c *Config) []Rule {
		if c == nil {
			return nil
		}
		if facetID == model.FacetIngressRules {
			return c.Ingress
		}
		return c.Egress
	}
	enforced, dryRun := pick(pm.Status), pick(pm.Spec)
	kind := "ingressRules"
	if facetID == model.FacetEgressRules {
		kind = "egressRules"
	}
	var out []model.Resource
	for i, r := range enforced {
		mode := "enforced"
		if pm.Spec != nil {
			mode = "enforced, dry-run"
			if !containsRule(dryRun, r) {
				mode = "enforced, removed in dry-run"
			}
		}
		out = append(out, ruleResource(parent, facetID, kind+"/"+strconv.Itoa(i+1), r, mode))
	}
	for i, r := range dryRun {
		if !containsRule(enforced, r) {
			out = append(out, ruleResource(parent, facetID, "dryRun"+strings.ToUpper(kind[:1])+kind[1:]+"/"+strconv.Itoa(i+1), r, "dry-run only"))
		}
	}
	return out, false, nil
}

var ruleFields = []string{"mode", "identities", "sources", "sourceRestriction", "resources", "externalResources", "roles"}

// ruleResource maps one rule listed under parent's facet; tail is its id
// under the perimeter (ingressRules/1, dryRunEgressRules/2). A rule only in
// the dry-run spec carries a DRY-RUN badge, and an enforced rule the spec
// drops a NOT IN DRY-RUN badge, both with a warning status.
func ruleResource(parent model.Resource, facetID, tail string, r Rule, mode string) model.Resource {
	svc := model.ServiceIngressRule
	if facetID == model.FacetEgressRules {
		svc = model.ServiceEgressRule
	}
	md := map[string]string{"mode": mode}
	who := identityText(r)
	if len(r.Identities) > 3 {
		md["identities"] = strconv.Itoa(len(r.Identities)) + " identities"
	} else {
		set(md, "identities", who)
	}
	set(md, "sources", strings.Join(r.Sources, ", "))
	switch r.SourceRestriction {
	case "SOURCE_RESTRICTION_ENABLED":
		md["sourceRestriction"] = "enabled"
	case "SOURCE_RESTRICTION_DISABLED":
		md["sourceRestriction"] = "disabled"
	}
	set(md, "resources", strings.Join(r.Resources, ", "))
	set(md, "externalResources", strings.Join(r.ExternalResources, ", "))
	set(md, "roles", strings.Join(r.Roles, ", "))
	var lists []model.List
	if len(r.Identities) > 3 {
		lists = append(lists, model.List{ID: "identities", Label: "Identities", Items: slices.Clone(r.Identities)})
	}
	if len(r.Operations) > 0 {
		items := make([]string, 0, len(r.Operations))
		for _, op := range r.Operations {
			items = append(items, operationText(op))
		}
		lists = append(lists, model.List{ID: "operations", Label: "Operations", Items: items})
	}
	name := r.Title
	if name == "" {
		name = ruleSummary(svc, r, who)
	}
	out := model.Resource{
		ID:       parent.ID + "/" + tail,
		Name:     name,
		Service:  svc,
		ScopeID:  parent.ScopeID,
		ParentID: parent.ID,
		FacetID:  facetID,
		Location: "global",
		Metadata: md,
		Fields:   model.FieldsOf(md, ruleFields...),
		Lists:    lists,
	}
	switch mode {
	case "dry-run only":
		out.Badge = "DRY-RUN"
		out.Status = model.StatusWarning
	case "enforced, removed in dry-run":
		out.Badge = "NOT IN DRY-RUN"
		out.Status = model.StatusWarning
	}
	return out
}

// identityText reads who a rule admits: "any identity", or its identities.
func identityText(r Rule) string {
	if r.IdentityType != "" {
		return strings.ToLower(strings.ReplaceAll(r.IdentityType, "_", " "))
	}
	return strings.Join(r.Identities, ", ")
}

// ruleSummary names an untitled rule: "any identity from corp_network →
// storage, bigquery" for ingress, "serviceAccount:ci@… → storage in
// projects/42" for egress.
func ruleSummary(svc model.ServiceType, r Rule, who string) string {
	if len(r.Identities) > 1 && r.IdentityType == "" {
		who = r.Identities[0] + " +" + strconv.Itoa(len(r.Identities)-1)
	}
	if who == "" {
		who = "no identity"
	}
	services := make([]string, 0, len(r.Operations))
	for _, op := range r.Operations {
		services = append(services, shortService(op.Service))
	}
	to := cmp.Or(firstPlus(services), "no services")
	if svc == model.ServiceIngressRule {
		if src := firstPlus(r.Sources); src != "" {
			return who + " from " + src + " → " + to
		}
		return who + " → " + to
	}
	target := firstPlus(append(slices.Clone(r.Resources), r.ExternalResources...))
	if target == "" {
		return who + " → " + to
	}
	return who + " → " + to + " in " + target
}

// firstPlus reads "a, b" for up to two items and "a +2" for more.
func firstPlus(items []string) string {
	switch {
	case len(items) == 0:
		return ""
	case len(items) <= 2:
		return strings.Join(items, ", ")
	}
	return items[0] + " +" + strconv.Itoa(len(items)-1)
}

func shortService(s string) string {
	if s == "*" {
		return "all services"
	}
	return strings.TrimSuffix(s, ".googleapis.com")
}

// operationText reads one operation: "storage.googleapis.com:
// google.storage.objects.get, permission storage.objects.list", or "all
// methods" for "*".
func operationText(op Operation) string {
	service := op.Service
	if service == "*" {
		service = "all services"
	}
	if len(op.Methods) == 0 {
		return service
	}
	methods := slices.Clone(op.Methods)
	for i, m := range methods {
		if m == "*" {
			methods[i] = "all methods"
		}
	}
	return service + ": " + strings.Join(methods, ", ")
}
