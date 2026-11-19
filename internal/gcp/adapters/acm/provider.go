package acm

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/vpcsc"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// groupConcurrency bounds the Cloud Identity group lookups. Lookups are
// cached per session, so this matters only on the first list.
const groupConcurrency = 8

// Bindings implements model.ResourceProvider for access bindings. An
// organization lists them; other scopes list nothing.
type Bindings struct{ api API }

func NewBindings(api API) Bindings { return Bindings{api: api} }

func (p Bindings) Service() model.ServiceType { return model.ServiceAccessBinding }

// ListPermissions is what List needs: the organization's binding list.
// Group email lookups are Cloud Identity reads, not IAM permissions, and
// fall back to the group id when they fail.
func (p Bindings) ListPermissions(scopeID string) []string {
	if model.ScopeKind(scopeID) != model.KindOrg {
		return nil
	}
	return []string{"accesscontextmanager.gcpUserAccessBindings.list"}
}

// List lists the organization's group and service account bindings. A
// failed service account list is logged and the group bindings still
// list; a group whose email cannot be read lists by its id.
func (p Bindings) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if model.ScopeKind(scopeID) != model.KindOrg {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("acm: no API")
	}
	bindings, err := p.api.ListBindings(ctx, scopeID)
	if err != nil {
		return nil, err
	}
	sa, err := p.api.ListServiceAccountBindings(ctx, scopeID)
	if err != nil {
		slog.Warn("acm: service account bindings failed; listing group bindings only", "org", scopeID, "err", err)
	}
	seen := make(map[string]bool, len(bindings))
	for _, b := range bindings {
		seen[b.Name] = true
	}
	for _, b := range sa {
		if !seen[b.Name] {
			seen[b.Name] = true
			bindings = append(bindings, b)
		}
	}
	groups := p.groups(ctx, bindings)
	out := make([]model.Resource, 0, len(bindings))
	for _, b := range bindings {
		var g *Group
		if gr, ok := groups[b.GroupKey]; ok {
			g = &gr
		}
		out = append(out, bindingResource(scopeID, b, g))
	}
	return out, nil
}

// groups looks up every distinct group the bindings name. A lookup that
// fails is logged and left out.
func (p Bindings) groups(ctx context.Context, bindings []Binding) map[string]Group {
	var keys []string
	seen := map[string]bool{}
	for _, b := range bindings {
		if b.GroupKey != "" && !seen[b.GroupKey] {
			seen[b.GroupKey] = true
			keys = append(keys, b.GroupKey)
		}
	}
	found := make([]*Group, len(keys))
	var g errgroup.Group
	g.SetLimit(groupConcurrency)
	for i, key := range keys {
		g.Go(func() error {
			gr, err := p.api.Group(ctx, key)
			if err != nil {
				slog.Warn("acm: group lookup failed; listing by id", "group", key, "err", err)
				return nil
			}
			found[i] = &gr
			return nil
		})
	}
	_ = g.Wait()
	out := make(map[string]Group, len(keys))
	for i, key := range keys {
		if found[i] != nil {
			out[key] = *found[i]
		}
	}
	return out
}

var bindingFields = []string{"principalType", "group", "groupId", "displayName", "principal",
	"accessLevels", "dryRunAccessLevels", "session", "scopedAccess", "binding"}

// bindingResource maps a binding; g is its group as Cloud Identity reads
// it, or nil. A binding with dry-run access levels, at the top or in a
// scoped setting, carries a DRY-RUN badge and a warning status, as a
// perimeter whose dry-run spec differs does.
func bindingResource(org string, b Binding, g *Group) model.Resource {
	md := map[string]string{"binding": b.Name}
	var name string
	switch {
	case b.GroupKey != "":
		md["principalType"] = "group"
		md["groupId"] = b.GroupKey
		name = b.GroupKey
		if g != nil && g.Email != "" {
			md["group"] = g.Email
			name = g.Email
			if g.DisplayName != "" && g.DisplayName != g.Email {
				md["displayName"] = g.DisplayName
			}
		}
	case b.ServiceAccount != "":
		md["principalType"] = "service account"
		md["principal"] = b.ServiceAccount
		name = b.ServiceAccount
	case b.SAProject != "":
		md["principalType"] = "service accounts of project"
		md["principal"] = "projects/" + b.SAProject
		name = "service accounts of projects/" + b.SAProject
	case b.Federated != "":
		md["principalType"] = "federated"
		md["principal"] = b.Federated
		name = b.Federated
	default:
		name = b.Name
	}
	set(md, "accessLevels", strings.Join(b.Levels, ", "))
	set(md, "dryRunAccessLevels", strings.Join(b.DryRunLevels, ", "))
	set(md, "session", sessionText(b.Session))
	dryRun := len(b.DryRunLevels) > 0
	var lists []model.List
	if len(b.Scoped) > 0 {
		md["scopedAccess"] = strconv.Itoa(len(b.Scoped))
		items := make([]string, 0, len(b.Scoped))
		for _, s := range b.Scoped {
			items = append(items, scopedText(s))
			dryRun = dryRun || len(s.DryRunLevels) > 0 || s.DryRunSession != nil
		}
		lists = []model.List{{ID: "scopedAccess", Label: "Scoped access", Items: items}}
	}
	r := model.Resource{
		ID:       org + "/gcpUserAccessBindings/" + url.PathEscape(b.Name),
		Name:     name,
		Service:  model.ServiceAccessBinding,
		ScopeID:  org,
		Location: "global",
		Metadata: md,
		Fields:   model.FieldsOf(md, bindingFields...),
		Lists:    lists,
	}
	if dryRun {
		r.Badge = "DRY-RUN"
		r.Status = model.StatusWarning
	}
	if len(levelUses(b)) > 0 {
		r.Facets = []model.Facet{{ID: model.FacetAccessLevels, Label: "Access Levels", Inline: true}}
	}
	return r
}

// ListRelated lists the access levels a binding uses, each with how it is
// used: by the binding, in dry run, or in a scoped setting.
func (p Bindings) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceAccessBinding || facetID != model.FacetAccessLevels {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	org, name, ok := parseBindingID(parent.ID)
	if !ok {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("acm: no API")
	}
	b, err := p.api.GetBinding(ctx, org, name)
	if err != nil {
		return nil, false, err
	}
	uses := levelUses(b)
	if len(uses) == 0 || b.Policy == "" {
		return nil, false, nil
	}
	levels, err := p.api.ListLevels(ctx, b.Policy)
	if err != nil {
		return nil, false, err
	}
	slices.SortFunc(levels, func(a, b vpcsc.Level) int { return cmp.Compare(a.Name, b.Name) })
	var out []model.Resource
	for _, l := range levels {
		u, ok := uses[l.Name]
		if !ok {
			continue
		}
		r := vpcsc.LevelResource(org, b.Policy, l, strings.Join(u, "; "))
		r.ParentID = parent.ID
		r.FacetID = facetID
		out = append(out, r)
	}
	return out, false, nil
}

// levelUses maps each access level a binding names to how it uses it:
// "binding", "dry-run", "app Google Cloud SDK", "project projects/42
// dry-run".
func levelUses(b Binding) map[string][]string {
	out := map[string][]string{}
	add := func(levels []string, use string) {
		for _, l := range levels {
			if !slices.Contains(out[l], use) {
				out[l] = append(out[l], use)
			}
		}
	}
	add(b.Levels, "binding")
	add(b.DryRunLevels, "dry-run")
	for _, s := range b.Scoped {
		target := "unscoped"
		switch {
		case s.App != "":
			target = "app " + s.App
		case s.Project != "":
			target = "project " + s.Project
		}
		add(s.Levels, target)
		add(s.DryRunLevels, target+" dry-run")
	}
	return out
}

// parseBindingID splits organizations/O/gcpUserAccessBindings/ID.
func parseBindingID(id string) (org, name string, ok bool) {
	segs := strings.Split(id, "/")
	if len(segs) != 4 || segs[0] != "organizations" || segs[1] == "" || segs[2] != "gcpUserAccessBindings" || segs[3] == "" {
		return "", "", false
	}
	name, err := url.PathUnescape(segs[3])
	if err != nil {
		return "", "", false
	}
	return segs[0] + "/" + segs[1], name, true
}

// scopedText reads one scoped setting:
// "app Google Cloud SDK: levels corp; dry-run devices; reauth LOGIN every 1h".
func scopedText(s Scoped) string {
	var target string
	switch {
	case s.App != "":
		target = "app " + s.App
	case s.Project != "":
		target = "project " + s.Project
	default:
		target = "unscoped"
	}
	var parts []string
	if len(s.Levels) > 0 {
		parts = append(parts, "levels "+strings.Join(s.Levels, ", "))
	}
	if t := sessionText(s.Session); t != "" {
		parts = append(parts, t)
	}
	if len(s.DryRunLevels) > 0 {
		parts = append(parts, "dry-run levels "+strings.Join(s.DryRunLevels, ", "))
	}
	if t := sessionText(s.DryRunSession); t != "" {
		parts = append(parts, "dry-run "+t)
	}
	if len(parts) == 0 {
		return target
	}
	return target + ": " + strings.Join(parts, "; ")
}

// sessionText reads session controls: "reauth LOGIN every 8h0m0s, idle
// 30m0s", or "" when none are in force.
func sessionText(s *Session) string {
	if s == nil {
		return ""
	}
	var parts []string
	if s.LengthEnabled && s.Length != "" {
		p := "every " + duration(s.Length)
		if s.ReauthMethod != "" && s.ReauthMethod != "SESSION_REAUTH_METHOD_UNSPECIFIED" {
			p = "reauth " + s.ReauthMethod + " " + p
		} else {
			p = "reauth " + p
		}
		parts = append(parts, p)
	}
	if s.MaxInactivity != "" {
		parts = append(parts, "idle "+duration(s.MaxInactivity))
	}
	if s.UseOIDCMaxAge {
		parts = append(parts, "OIDC max_age")
	}
	return strings.Join(parts, ", ")
}

// duration reads a protobuf duration ("3600s") as Go does ("1h0m0s"), or
// returns it unchanged.
func duration(s string) string {
	if d, err := time.ParseDuration(s); err == nil {
		return d.String()
	}
	return s
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

var (
	_ model.ResourceProvider   = Bindings{}
	_ model.PermissionDeclarer = Bindings{}
	_ model.RelatedLister      = Bindings{}
)
