package vpcsc

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/tensleyconsulting/seiner/internal/gcp/cache"
	"github.com/tensleyconsulting/seiner/internal/model"
)

const (
	// perimeterConcurrency bounds the per-policy perimeter lists when a
	// project's organization is indexed. An org has one policy plus a few
	// scoped ones.
	perimeterConcurrency = 4
	// indexTTL keeps an organization's perimeters as long as the catalog
	// keeps a list result, so expanding many projects in turn lists the
	// organization once.
	indexTTL = 60 * time.Second
)

// Policies implements model.ResourceProvider for access policies. An
// organization lists its policies, org-level and scoped; each expands to
// its service perimeters and access levels.
type Policies struct{ api API }

func NewPolicies(api API) Policies { return Policies{api: api} }

func (p Policies) Service() model.ServiceType { return model.ServiceAccessPolicy }

// ListPermissions is what List needs: the organization's policy list.
func (p Policies) ListPermissions(scopeID string) []string {
	if model.ScopeKind(scopeID) != model.KindOrg {
		return nil
	}
	return []string{"accesscontextmanager.policies.list"}
}

func (p Policies) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if model.ScopeKind(scopeID) != model.KindOrg {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("vpcsc: no API")
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

// ListRelated lists a policy's perimeters, access levels, or IAM bindings.
func (p Policies) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceAccessPolicy ||
		(facetID != model.FacetPerimeters && facetID != model.FacetAccessLevels && facetID != model.FacetPermissions) {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	id, ok := parseID(parent.ID)
	if !ok || id.kind != "" {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("vpcsc: no API")
	}
	if facetID == model.FacetPermissions {
		bindings, err := p.api.GetPolicyIAMPolicy(ctx, id.policy)
		if err != nil {
			return nil, false, err
		}
		out := make([]model.Resource, 0, len(bindings))
		for _, b := range bindings {
			r := model.BindingResource(parent.ID, parent.ScopeID, b.Role, b.Members, b.Condition)
			r.ParentID = parent.ID
			r.FacetID = model.FacetPermissions
			out = append(out, r)
		}
		return out, false, nil
	}
	var out []model.Resource
	if facetID == model.FacetPerimeters {
		perims, err := p.api.ListPerimeters(ctx, id.policy)
		if err != nil {
			return nil, false, err
		}
		slices.SortFunc(perims, func(a, b Perimeter) int { return cmp.Compare(a.Name, b.Name) })
		for _, pm := range perims {
			out = append(out, perimeterResource(id.scope, id.policy, policyLabel(Policy{Name: id.policy, Title: parent.Name}), pm))
		}
	} else {
		levels, err := p.api.ListLevels(ctx, id.policy)
		if err != nil {
			return nil, false, err
		}
		slices.SortFunc(levels, func(a, b Level) int { return cmp.Compare(a.Name, b.Name) })
		for _, l := range levels {
			out = append(out, LevelResource(id.scope, id.policy, l, ""))
		}
	}
	for i := range out {
		out[i].ParentID = parent.ID
		out[i].FacetID = facetID
	}
	return out, false, nil
}

// Perimeters implements model.ResourceProvider for the service perimeters
// that protect a project. The API has no lookup from a project to its
// perimeters, so List reads every perimeter in the project's organization
// and keeps those whose resources name the project or one of its VPC
// networks. A perimeter, wherever it lists, expands to the access levels it
// uses.
type Perimeters struct {
	api API
	idx *cache.Cache[[]orgPerimeter]
}

// orgPerimeter is one perimeter of an organization with its policy.
type orgPerimeter struct {
	policy    Policy
	perimeter Perimeter
}

func NewPerimeters(api API) Perimeters {
	return Perimeters{api: api, idx: cache.New[[]orgPerimeter](indexTTL)}
}

func (p Perimeters) Service() model.ServiceType { return model.ServicePerimeter }

// ListPermissions is what List needs on the project: the lookup of its
// number and organization. Access Context Manager permissions are granted
// on the organization or on a scoped policy, never on a project, so they
// cannot be tested here. A caller without them sees no perimeters.
func (p Perimeters) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "resourcemanager.projects.get")
}

// List lists the perimeters that protect a project, enforced or in dry
// run. If the caller cannot read the organization's policies, it lists
// nothing rather than failing.
func (p Perimeters) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("vpcsc: no API")
	}
	proj, err := p.api.Project(ctx, project)
	if err != nil {
		return nil, err
	}
	if proj.Org == "" {
		return nil, nil
	}
	all, err := p.orgPerimeters(ctx, proj.Org)
	if errors.Is(err, ErrDenied) {
		slog.Info("vpcsc: organization policies not readable; no perimeters for project", "project", project, "org", proj.Org, "err", err)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []model.Resource
	for _, op := range all {
		m := membershipOf(op.perimeter, proj.Number, project)
		if m.state == "" {
			continue
		}
		r := perimeterResource(scopeID, op.policy.Name, policyLabel(op.policy), op.perimeter)
		r.Metadata["membership"] = m.state
		if m.via != "" {
			r.Metadata["via"] = m.via
		}
		r.Fields = model.FieldsOf(r.Metadata, perimeterFields...)
		out = append(out, r)
	}
	return out, nil
}

// orgPerimeters lists every perimeter in org, from the index when it is
// fresh. A policy whose perimeters cannot be read (a scoped policy the
// caller has no role on) is logged and skipped; only when every policy
// fails does it return the first error.
func (p Perimeters) orgPerimeters(ctx context.Context, org string) ([]orgPerimeter, error) {
	if all, ok := p.idx.Get(org); ok {
		return all, nil
	}
	pols, err := p.api.ListPolicies(ctx, org)
	if err != nil {
		return nil, err
	}
	perims := make([][]Perimeter, len(pols))
	errs := make([]error, len(pols))
	var g errgroup.Group
	g.SetLimit(perimeterConcurrency)
	for i, pol := range pols {
		g.Go(func() error {
			perims[i], errs[i] = p.api.ListPerimeters(ctx, pol.Name)
			return nil
		})
	}
	_ = g.Wait()
	var all []orgPerimeter
	var first error
	failed := 0
	for i, pol := range pols {
		if errs[i] != nil {
			slog.Warn("vpcsc: perimeters failed", "org", org, "policy", pol.Name, "err", errs[i])
			failed++
			if first == nil {
				first = errs[i]
			}
			continue
		}
		for _, pm := range perims[i] {
			all = append(all, orgPerimeter{policy: pol, perimeter: pm})
		}
	}
	if failed > 0 && failed == len(pols) {
		return nil, fmt.Errorf("vpcsc: perimeters of %s: %w", org, first)
	}
	p.idx.Set(org, all)
	return all, nil
}

// ListRelated lists the access levels a perimeter uses, enforced or in its
// dry-run spec, or its ingress or egress rules.
func (p Perimeters) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServicePerimeter {
		return nil, false, nil
	}
	if facetID == model.FacetIngressRules || facetID == model.FacetEgressRules {
		return p.listRules(ctx, parent, facetID)
	}
	if facetID != model.FacetAccessLevels {
		return nil, false, nil
	}
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
	used := map[string][]string{}
	if pm.Status != nil {
		for _, l := range pm.Status.AccessLevels {
			used[l] = append(used[l], "enforced")
		}
	}
	if pm.Spec != nil {
		for _, l := range pm.Spec.AccessLevels {
			used[l] = append(used[l], "dry-run")
		}
	}
	if len(used) == 0 {
		return nil, false, nil
	}
	levels, err := p.api.ListLevels(ctx, id.policy)
	if err != nil {
		return nil, false, err
	}
	slices.SortFunc(levels, func(a, b Level) int { return cmp.Compare(a.Name, b.Name) })
	var out []model.Resource
	for _, l := range levels {
		modes, ok := used[l.Name]
		if !ok {
			continue
		}
		r := LevelResource(id.scope, id.policy, l, strings.Join(modes, ", "))
		r.ParentID = parent.ID
		r.FacetID = facetID
		out = append(out, r)
	}
	return out, false, nil
}

// vpcscID is a resource id this package builds:
// SCOPE/accessPolicies/N[/KIND/NAME], where SCOPE is organizations/O for
// what lists under the organization and projects/P for what lists under
// a project it protects.
type vpcscID struct {
	scope, policy, kind, name string
}

func policyID(scope, policy string) string {
	return scope + "/accessPolicies/" + url.PathEscape(policy)
}

func childID(scope, policy, kind, name string) string {
	return policyID(scope, policy) + "/" + kind + "/" + url.PathEscape(name)
}

// parseID reads an id built by policyID or childID. Ids of other shapes,
// such as a binding under a policy, are not ours.
func parseID(id string) (vpcscID, bool) {
	segs := strings.Split(id, "/")
	if len(segs) != 4 && len(segs) != 6 {
		return vpcscID{}, false
	}
	if segs[0] != "organizations" && segs[0] != "projects" || segs[1] == "" || segs[2] != "accessPolicies" || segs[3] == "" {
		return vpcscID{}, false
	}
	out := vpcscID{scope: segs[0] + "/" + segs[1], policy: segs[3]}
	if len(segs) == 6 {
		if segs[4] != "servicePerimeters" && segs[4] != "accessLevels" || segs[5] == "" {
			return vpcscID{}, false
		}
		name, err := url.PathUnescape(segs[5])
		if err != nil {
			return vpcscID{}, false
		}
		out.kind, out.name = segs[4], name
	}
	return out, true
}

func policyResource(org string, pol Policy) model.Resource {
	md := map[string]string{"policy": pol.Name}
	r := model.Resource{
		ID:       policyID(org, pol.Name),
		Name:     cmp.Or(pol.Title, pol.Name),
		Service:  model.ServiceAccessPolicy,
		ScopeID:  org,
		Location: "global",
		Metadata: md,
		Facets: []model.Facet{
			{ID: model.FacetPerimeters, Label: "Perimeters"},
			{ID: model.FacetAccessLevels, Label: "Access Levels"},
			{ID: model.FacetPermissions, Label: "Permissions", Optional: true},
		},
	}
	if len(pol.Scopes) > 0 {
		md["scope"] = strings.Join(pol.Scopes, ", ")
		r.Badge = "SCOPED"
	} else {
		md["scope"] = "organization"
	}
	r.Fields = model.FieldsOf(md, "policy", "scope")
	return r
}

// policyLabel names a policy on a perimeter row: its title and number.
func policyLabel(pol Policy) string {
	if pol.Title == "" || pol.Title == pol.Name {
		return pol.Name
	}
	return pol.Title + " (" + pol.Name + ")"
}

var perimeterFields = []string{"title", "type", "policy", "membership", "via", "dryRun", "resources", "restrictedServices",
	"accessLevels", "ingressPolicies", "egressPolicies", "vpcAccessibleServices",
	"dryRunResources", "dryRunRestrictedServices", "dryRunAccessLevels", "dryRunIngressPolicies", "dryRunEgressPolicies", "dryRunVpcAccessibleServices"}

// perimeterResource maps a perimeter listed under scope. Its fields come
// from the enforced status. A dry-run spec that differs adds a DRY-RUN
// badge, a warning status, and dryRun* fields for what it changes.
func perimeterResource(scope, policy, policyName string, pm Perimeter) model.Resource {
	md := map[string]string{"type": perimeterType(pm.Type), "policy": policyName}
	if pm.Title != "" && pm.Title != pm.Name {
		md["title"] = pm.Title
	}
	status := pm.Status
	if status == nil {
		status = &Config{}
	}
	configFields(md, "", *status)
	lists := configLists("", "", *status)
	r := model.Resource{
		ID:          childID(scope, policy, "servicePerimeters", pm.Name),
		Name:        pm.Name,
		Service:     model.ServicePerimeter,
		ScopeID:     scope,
		Location:    "global",
		Description: pm.Description,
		Metadata:    md,
	}
	if pm.Spec != nil {
		if d := configDiff(*status, *pm.Spec); d != "" {
			md["dryRun"] = d
			r.Badge = "DRY-RUN"
			r.Status = model.StatusWarning
			changed := map[string]string{}
			configFields(changed, "dryRun", *pm.Spec)
			mine := map[string]string{}
			configFields(mine, "dryRun", *status)
			for k, v := range changed {
				if mine[k] != v {
					md[k] = v
				}
			}
			for k := range mine {
				if _, ok := changed[k]; !ok {
					md[k] = "none"
				}
			}
			lists = append(lists, changedLists(*status, *pm.Spec)...)
		} else {
			md["dryRun"] = "same as enforced"
		}
	}
	r.Fields = model.FieldsOf(md, perimeterFields...)
	r.Lists = lists
	spec := pm.Spec
	if spec == nil {
		spec = &Config{}
	}
	// Access Levels lists inline when it is the only facet.
	if len(status.AccessLevels) > 0 || len(spec.AccessLevels) > 0 {
		r.Facets = append(r.Facets, model.Facet{ID: model.FacetAccessLevels, Label: "Access Levels", Inline: true})
	}
	if len(status.Ingress) > 0 || len(spec.Ingress) > 0 {
		r.Facets = append(r.Facets, model.Facet{ID: model.FacetIngressRules, Label: "Ingress Rules"})
	}
	if len(status.Egress) > 0 || len(spec.Egress) > 0 {
		r.Facets = append(r.Facets, model.Facet{ID: model.FacetEgressRules, Label: "Egress Rules"})
	}
	return r
}

func perimeterType(t string) string {
	switch t {
	case "PERIMETER_TYPE_BRIDGE":
		return "bridge"
	case "", "PERIMETER_TYPE_REGULAR":
		return "regular"
	}
	return t
}

// configFields writes c's summary fields into md, each key prefixed (and
// recased) when prefix is set: resources becomes dryRunResources.
func configFields(md map[string]string, prefix string, c Config) {
	key := func(k string) string {
		if prefix == "" {
			return k
		}
		return prefix + strings.ToUpper(k[:1]) + k[1:]
	}
	if s := resourceSummary(c.Resources); s != "" {
		md[key("resources")] = s
	}
	if n := len(c.RestrictedServices); n > 0 {
		md[key("restrictedServices")] = count(n, "service")
	}
	if len(c.AccessLevels) > 0 {
		md[key("accessLevels")] = strings.Join(c.AccessLevels, ", ")
	}
	if n := len(c.Ingress); n > 0 {
		md[key("ingressPolicies")] = strconv.Itoa(n)
	}
	if n := len(c.Egress); n > 0 {
		md[key("egressPolicies")] = strconv.Itoa(n)
	}
	if c.VPCRestricted {
		v := "none"
		if len(c.VPCAllowed) > 0 {
			v = strings.Join(c.VPCAllowed, ", ")
		}
		md[key("vpcAccessibleServices")] = v
	}
}

// configLists is c's resources and restricted services in full.
func configLists(idPrefix, labelPrefix string, c Config) []model.List {
	var out []model.List
	if len(c.Resources) > 0 {
		out = append(out, model.List{ID: idPrefix + "resources", Label: labelPrefix + "Resources", Items: slices.Clone(c.Resources)})
	}
	if len(c.RestrictedServices) > 0 {
		out = append(out, model.List{ID: idPrefix + "restrictedServices", Label: labelPrefix + "Restricted services", Items: slices.Clone(c.RestrictedServices)})
	}
	return out
}

// changedLists is the dry-run spec's resources and restricted services in
// full, for whichever of the two differ from the status.
func changedLists(status, spec Config) []model.List {
	var out []model.List
	if !sameSet(status.Resources, spec.Resources) {
		out = append(out, model.List{ID: "dryRunResources", Label: "Dry-run resources", Items: slices.Clone(spec.Resources)})
	}
	if !sameSet(status.RestrictedServices, spec.RestrictedServices) {
		out = append(out, model.List{ID: "dryRunRestrictedServices", Label: "Dry-run restricted services", Items: slices.Clone(spec.RestrictedServices)})
	}
	return out
}

// configDiff summarizes how spec differs from status, or "" when they
// agree: "resources +1 −0, ingress policies 1→2".
func configDiff(status, spec Config) string {
	var parts []string
	set := func(label string, a, b []string) {
		add, del := setDelta(a, b)
		if add > 0 || del > 0 {
			parts = append(parts, fmt.Sprintf("%s +%d −%d", label, add, del))
		}
	}
	set("resources", status.Resources, spec.Resources)
	set("restricted services", status.RestrictedServices, spec.RestrictedServices)
	set("access levels", status.AccessLevels, spec.AccessLevels)
	rules := func(label string, a, b []Rule) {
		add, del := ruleDelta(a, b)
		if add > 0 || del > 0 {
			parts = append(parts, fmt.Sprintf("%s +%d −%d", label, add, del))
		}
	}
	rules("ingress rules", status.Ingress, spec.Ingress)
	rules("egress rules", status.Egress, spec.Egress)
	if status.VPCRestricted != spec.VPCRestricted || !sameSet(status.VPCAllowed, spec.VPCAllowed) {
		parts = append(parts, "VPC accessible services")
	}
	return strings.Join(parts, ", ")
}

// setDelta counts the items of b not in a, and of a not in b.
func setDelta(a, b []string) (add, del int) {
	for _, x := range b {
		if !slices.Contains(a, x) {
			add++
		}
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			del++
		}
	}
	return add, del
}

func sameSet(a, b []string) bool {
	add, del := setDelta(a, b)
	return add == 0 && del == 0
}

// resourceSummary reads "3 projects, 1 VPC network".
func resourceSummary(rs []string) string {
	projects, networks := 0, 0
	for _, r := range rs {
		if strings.HasPrefix(r, "projects/") {
			projects++
		} else {
			networks++
		}
	}
	var parts []string
	if projects > 0 {
		parts = append(parts, count(projects, "project"))
	}
	if networks > 0 {
		parts = append(parts, count(networks, "VPC network"))
	}
	return strings.Join(parts, ", ")
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// membership is how a perimeter protects one project.
type membership struct {
	state string // "enforced", "enforced, dry-run", "dry-run only", "enforced, removed in dry-run"; "" when not a member
	via   string // "project", or the VPC networks that put it there
}

// membershipOf reports whether pm protects the project, by its number or
// through one of its VPC networks, in the status, the dry-run spec, or
// both.
func membershipOf(pm Perimeter, number, project string) membership {
	inStatus, viaStatus := memberOf(pm.Status, number, project)
	if pm.Spec == nil {
		if !inStatus {
			return membership{}
		}
		return membership{state: "enforced", via: viaStatus}
	}
	inSpec, viaSpec := memberOf(pm.Spec, number, project)
	switch {
	case inStatus && inSpec:
		return membership{state: "enforced, dry-run", via: viaStatus}
	case inStatus:
		return membership{state: "enforced, removed in dry-run", via: viaStatus}
	case inSpec:
		return membership{state: "dry-run only", via: viaSpec}
	}
	return membership{}
}

// memberOf reports whether c names projects/NUMBER or a VPC network of the
// project, and how.
func memberOf(c *Config, number, project string) (bool, string) {
	if c == nil {
		return false, ""
	}
	netPrefix := "//compute.googleapis.com/projects/" + project + "/global/networks/"
	var nets []string
	for _, r := range c.Resources {
		if number != "" && r == "projects/"+number {
			return true, "project"
		}
		if n, ok := strings.CutPrefix(r, netPrefix); ok {
			nets = append(nets, n)
		}
	}
	if len(nets) > 0 {
		return true, "VPC network " + strings.Join(nets, ", ")
	}
	return false, ""
}

var (
	_ model.ResourceProvider   = Policies{}
	_ model.PermissionDeclarer = Policies{}
	_ model.RelatedLister      = Policies{}
	_ model.ResourceProvider   = Perimeters{}
	_ model.PermissionDeclarer = Perimeters{}
	_ model.RelatedLister      = Perimeters{}
)

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}
