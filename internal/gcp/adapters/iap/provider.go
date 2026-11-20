package iap

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/iam"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// settingsConcurrency bounds the per-app settings reads. A project has a
// handful of IAP-secured backends, so this rarely takes more than one round.
const settingsConcurrency = 8

// Apps implements model.ResourceProvider for IAP-secured load balancer
// backends. An app's Access facet lists its IAP role bindings, its own and
// inherited.
type Apps struct {
	api API
	crm iam.API // project, folder, and organization policies; nil skips them
}

// NewApps builds the provider. crm reads the CRM policies an app inherits
// IAP roles from; nil lists only IAP's own levels.
func NewApps(api API, crm iam.API) Apps { return Apps{api: api, crm: crm} }

func (p Apps) Service() model.ServiceType { return model.ServiceIAPApp }

// ListPermissions is what List needs: the backend service list, and the
// project lookup that turns the id into the number IAP names use.
func (p Apps) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.backendServices.list", "resourcemanager.projects.get")
}

// List lists backend services with IAP on, each with its own IAP settings.
// A settings read that fails is logged and the app lists without them.
func (p Apps) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("iap: no API")
	}
	backends, err := p.api.ListBackends(ctx, project)
	if err != nil {
		return nil, err
	}
	if len(backends) == 0 {
		return nil, nil
	}
	settings := make([]*Settings, len(backends))
	number, err := p.api.ProjectNumber(ctx, project)
	if err != nil {
		slog.Warn("iap: project number failed; listing without settings", "project", project, "err", err)
	} else {
		var g errgroup.Group
		g.SetLimit(settingsConcurrency)
		for i, b := range backends {
			g.Go(func() error {
				s, err := p.api.GetSettings(ctx, "projects/"+number+"/"+appTail(b))
				if err != nil {
					slog.Warn("iap: settings failed", "project", project, "backend", b.Name, "region", b.Region, "err", err)
					return nil
				}
				settings[i] = &s
				return nil
			})
		}
		_ = g.Wait()
	}
	out := make([]model.Resource, 0, len(backends))
	for i, b := range backends {
		out = append(out, appResource(project, b, settings[i]))
	}
	return out, nil
}

// ListRelated lists an app's IAP role bindings: those set on the app,
// then those it inherits, nearest first. IAP's own levels above the app
// (its backend services, then all web services) list in full. The
// project, folders, and organization list only IAP web roles, since no
// other role grants access through IAP. Only the app's own policy must
// load; an ancestor that fails is logged and skipped.
func (p Apps) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceIAPApp || facetID != model.FacetPermissions {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	project, tail, ok := parseAppID(parent.ID)
	if !ok {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("iap: no API")
	}
	number, err := p.api.ProjectNumber(ctx, project)
	if err != nil {
		return nil, false, err
	}
	levels := iapLevels(number, tail)
	var (
		own        []Binding
		ownErr     error
		levelPols  = make([][]Binding, len(levels))
		ancestors  []model.Ancestor
		ancestPols [][]iam.Binding
		wg         sync.WaitGroup
	)
	wg.Go(func() { own, ownErr = p.api.GetPolicy(ctx, "projects/"+number+"/"+tail) })
	for i, l := range levels {
		wg.Go(func() {
			b, err := p.api.GetPolicy(ctx, l.Name)
			if err != nil {
				slog.Warn("iap: inherited policy unavailable", "app", parent.ID, "level", l.Name, "err", err)
				return
			}
			levelPols[i] = b
		})
	}
	if p.crm != nil {
		wg.Go(func() { ancestors, ancestPols = p.crmPolicies(ctx, project, len(levels)) })
	}
	wg.Wait()
	if ownErr != nil {
		return nil, false, ownErr
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	out := make([]model.Resource, 0, len(own))
	for _, b := range own {
		out = append(out, model.BindingResource(parent.ID, parent.ScopeID, b.Role, b.Members, b.Condition))
	}
	for i, l := range levels {
		for _, b := range levelPols[i] {
			out = append(out, model.InheritedBindingUnder(parent.ID, parent.ScopeID, l, model.ServiceIAPApp, b.Role, b.Members, b.Condition))
		}
	}
	for i, a := range ancestors {
		for _, b := range ancestPols[i] {
			if webRole(b.Role) {
				out = append(out, model.InheritedBindingUnder(parent.ID, parent.ScopeID, a, model.ScopeService(a.Name), b.Role, b.Members, b.Condition))
			}
		}
	}
	for i := range out {
		out[i].ParentID = parent.ID
		out[i].FacetID = model.FacetPermissions
	}
	return out, false, nil
}

// iapLevels are the IAP resources above an app, nearest first: the
// project's backend services (global or in one region), then all its web
// services.
func iapLevels(number, tail string) []model.Ancestor {
	kind := strings.Split(tail, "/")[1] // compute or compute-REGION
	label := "Backend Services"
	if region, ok := strings.CutPrefix(kind, "compute-"); ok {
		label += " (" + region + ")"
	}
	web := "projects/" + number + "/iap_web"
	return []model.Ancestor{
		{Name: web + "/" + kind, DisplayName: label, Level: 1},
		{Name: web, DisplayName: "All Web Services", Level: 2},
	}
}

// crmPolicies reads the policies of project and the folders and
// organization above it, ranked after the IAP levels above an app.
// Ancestors whose policy fails are logged and left with none.
func (p Apps) crmPolicies(ctx context.Context, project string, above int) ([]model.Ancestor, [][]iam.Binding) {
	name := "projects/" + project
	ancestors := []model.Ancestor{{Name: name, DisplayName: project, Level: above + 1}}
	up, err := iam.Ancestors(ctx, p.crm, name)
	if err != nil {
		slog.Warn("iap: ancestry incomplete; inherited bindings may be missing", "project", project, "found", len(up), "err", err)
	}
	for _, a := range up {
		a.Level += above + 1
		ancestors = append(ancestors, a)
	}
	policies := make([][]iam.Binding, len(ancestors))
	var wg sync.WaitGroup
	for i, a := range ancestors {
		wg.Go(func() {
			pol, err := p.crm.GetPolicy(ctx, a.Name)
			if err != nil {
				slog.Warn("iap: ancestor policy unavailable", "project", project, "ancestor", a.Name, "err", err)
				return
			}
			policies[i] = pol.Bindings
		})
	}
	wg.Wait()
	return ancestors, policies
}

// webRole reports whether role is a predefined IAP role for web apps.
// Tunnel roles are IAP's too but grant TCP forwarding, not app access.
func webRole(role string) bool {
	return strings.HasPrefix(role, "roles/iap.") && !strings.HasPrefix(role, "roles/iap.tunnel")
}

// appTail is the part of a backend's IAP resource name after the project:
// iap_web/compute/services/NAME, or iap_web/compute-REGION/services/NAME.
func appTail(b Backend) string {
	kind := "compute"
	if b.Region != "" {
		kind += "-" + b.Region
	}
	return "iap_web/" + kind + "/services/" + url.PathEscape(b.Name)
}

// parseAppID splits an app id, projects/ID/<tail>, into the project id and
// the IAP name tail. Binding ids under an app are not apps.
func parseAppID(id string) (project, tail string, ok bool) {
	rest, found := strings.CutPrefix(id, "projects/")
	if !found {
		return "", "", false
	}
	project, tail, found = strings.Cut(rest, "/")
	if !found || project == "" {
		return "", "", false
	}
	segs := strings.Split(tail, "/")
	if len(segs) != 4 || segs[0] != "iap_web" || segs[2] != "services" || segs[3] == "" ||
		(segs[1] != "compute" && !strings.HasPrefix(segs[1], "compute-")) {
		return "", "", false
	}
	return project, tail, true
}

func appResource(project string, b Backend, s *Settings) model.Resource {
	md := map[string]string{}
	set(md, "loadBalancingScheme", b.Scheme)
	set(md, "protocol", b.Protocol)
	if b.ClientID != "" {
		md["oauthClient"] = b.ClientID
	} else {
		md["oauthClient"] = "Google-managed"
	}
	var lists []model.List
	if s != nil {
		set(md, "reauth", reauth(*s))
		set(md, "allowedDomains", strings.Join(s.AllowedDomains, ", "))
		if s.CORSPreflight {
			md["corsPreflight"] = "allowed"
		}
		set(md, "identitySources", strings.Join(s.IdentitySources, ", "))
		set(md, "identityPlatformTenants", strings.Join(s.GCIPTenants, ", "))
		set(md, "loginHint", s.LoginHint)
		set(md, "cookieDomain", s.CookieDomain)
		set(md, "accessDeniedPage", s.AccessDeniedPage)
		if len(s.ProgrammaticClients) > 0 {
			lists = append(lists, model.List{ID: "programmaticClients", Label: "Programmatic clients", Items: append([]string(nil), s.ProgrammaticClients...)})
		}
	}
	loc := b.Region
	if loc == "" {
		loc = "global"
	}
	return model.Resource{
		ID:          "projects/" + project + "/" + appTail(b),
		Name:        b.Name,
		Service:     model.ServiceIAPApp,
		ScopeID:     "projects/" + project,
		Location:    loc,
		Description: b.Description,
		Created:     b.Created,
		Metadata:    md,
		Fields: model.FieldsOf(md, "loadBalancingScheme", "protocol", "oauthClient", "reauth", "allowedDomains",
			"corsPreflight", "identitySources", "identityPlatformTenants", "loginHint", "cookieDomain", "accessDeniedPage"),
		Lists:  lists,
		Facets: []model.Facet{{ID: model.FacetPermissions, Label: "Access", Optional: true}},
	}
}

// reauth reads "LOGIN every 1h0m0s (minimum)", or "" when re-auth is off.
func reauth(s Settings) string {
	if s.ReauthMethod == "" || s.ReauthMethod == "METHOD_UNSPECIFIED" {
		return ""
	}
	out := s.ReauthMethod
	if s.ReauthMaxAge != "" {
		age := s.ReauthMaxAge
		if d, err := time.ParseDuration(age); err == nil {
			age = d.String()
		}
		out += " every " + age
	}
	switch s.ReauthPolicy {
	case "MINIMUM", "DEFAULT":
		out += " (" + strings.ToLower(s.ReauthPolicy) + ")"
	}
	return out
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

var (
	_ model.ResourceProvider   = Apps{}
	_ model.PermissionDeclarer = Apps{}
	_ model.RelatedLister      = Apps{}
)
