package workspace

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// Limits on one list. Past them the rest is left out and logged; users
// and groups are fetched whole because the explorer buckets them.
const (
	maxUsers   = 50000
	maxGroups  = 50000
	maxMembers = 10000
)

// Users implements model.ResourceProvider for Workspace users. An
// organization lists its customer's users; other scopes list nothing.
type Users struct{ api API }

func NewUsers(api API) Users { return Users{api: api} }

func (p Users) Service() model.ServiceType { return model.ServiceWorkspaceUser }

// List lists the users of the customer that owns the organization. A
// caller who may not read them sees none and no error: see ErrDenied.
func (p Users) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	customer, err := customerOf(ctx, p.api, scopeID)
	if customer == "" || err != nil {
		return nil, err
	}
	users, truncated, err := p.api.ListUsers(ctx, customer, maxUsers)
	if err != nil {
		return nil, quiet(err, "users", scopeID)
	}
	if truncated {
		slog.Warn("workspace: user list stopped at its limit", "org", scopeID, "limit", maxUsers)
	}
	out := make([]model.Resource, 0, len(users))
	for _, u := range users {
		out = append(out, userResource(scopeID, u))
	}
	return out, nil
}

var userFields = []string{"fullName", model.MetaOrgUnit, "admin", "twoStepVerification", "lastLogin", "id"}

func userResource(org string, u User) model.Resource {
	md := map[string]string{"id": u.ID}
	set(md, "fullName", u.FullName)
	set(md, model.MetaOrgUnit, orgUnit(u.OrgUnit))
	switch {
	case u.Admin:
		md["admin"] = "super admin"
	case u.Delegated:
		md["admin"] = "delegated admin"
	}
	md["twoStepVerification"] = twoSV(u.Enrolled2SV, u.Enforced2SV)
	md["lastLogin"] = "never"
	if !u.LastLogin.IsZero() {
		md["lastLogin"] = u.LastLogin.UTC().Format("2006-01-02 15:04 UTC")
	}
	r := model.Resource{
		ID:       org + "/users/" + url.PathEscape(u.ID),
		Name:     u.Email,
		Service:  model.ServiceWorkspaceUser,
		ScopeID:  org,
		Location: "global",
		State:    "ACTIVE",
		Status:   model.StatusHealthy,
		Created:  u.Created,
		Metadata: md,
		Fields:   model.FieldsOf(md, userFields...),
	}
	switch {
	case u.Archived:
		r.State, r.Status = "ARCHIVED", model.StatusWarning
	case u.Suspended:
		r.State, r.Status = "SUSPENDED", model.StatusWarning
	}
	if u.Admin {
		r.Badge = "ADMIN"
	}
	return r
}

// orgUnit is p as an org unit path: "/" for the root.
func orgUnit(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

func twoSV(enrolled, enforced bool) string {
	s := "not enrolled"
	if enrolled {
		s = "enrolled"
	}
	if enforced {
		s += ", enforced"
	}
	return s
}

// Groups implements model.ResourceProvider for Workspace groups, read
// through Cloud Identity. Each group lists its direct members inline.
type Groups struct{ api API }

func NewGroups(api API) Groups { return Groups{api: api} }

func (p Groups) Service() model.ServiceType { return model.ServiceWorkspaceGroup }

// List lists the groups of the customer that owns the organization. A
// caller who may not read them sees none and no error: see ErrDenied.
func (p Groups) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	customer, err := customerOf(ctx, p.api, scopeID)
	if customer == "" || err != nil {
		return nil, err
	}
	groups, truncated, err := p.api.ListGroups(ctx, customer, maxGroups)
	if err != nil {
		return nil, quiet(err, "groups", scopeID)
	}
	if truncated {
		slog.Warn("workspace: group list stopped at its limit", "org", scopeID, "limit", maxGroups)
	}
	out := make([]model.Resource, 0, len(groups))
	for _, g := range groups {
		out = append(out, groupResource(scopeID, g))
	}
	return out, nil
}

var groupFields = []string{"displayName", "groupType", "updated", "id"}

func groupResource(org string, g Group) model.Resource {
	md := map[string]string{"id": g.ID}
	if g.DisplayName != g.Email {
		set(md, "displayName", g.DisplayName)
	}
	set(md, "groupType", strings.Join(g.Types, ", "))
	if !g.Updated.IsZero() {
		md["updated"] = g.Updated.UTC().Format("2006-01-02 15:04 UTC")
	}
	name := g.Email
	if name == "" {
		name = g.ID
	}
	return model.Resource{
		ID:          org + "/groups/" + url.PathEscape(g.ID),
		Name:        name,
		Service:     model.ServiceWorkspaceGroup,
		ScopeID:     org,
		Location:    "global",
		Description: g.Description,
		Created:     g.Created,
		Metadata:    md,
		Fields:      model.FieldsOf(md, groupFields...),
		Facets:      []model.Facet{{ID: model.FacetMembers, Label: "Members", Inline: true}},
	}
}

// ListRelated lists a group's direct members. A caller who may not read
// them sees none and no error.
func (p Groups) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceWorkspaceGroup || facetID != model.FacetMembers {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	group, ok := parseGroupID(parent.ID)
	if !ok {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("workspace: no API")
	}
	members, truncated, err := p.api.ListMembers(ctx, group, maxMembers)
	if err != nil {
		return nil, false, quiet(err, "members", parent.ID)
	}
	out := make([]model.Resource, 0, len(members))
	for _, m := range members {
		out = append(out, memberResource(parent, m))
	}
	return out, truncated, nil
}

var memberFields = []string{"type", "roles"}

func memberResource(group model.Resource, m Member) model.Resource {
	md := map[string]string{}
	set(md, "type", m.Type)
	set(md, "roles", strings.Join(m.Roles, ", "))
	name := m.Email
	if name == "" {
		name = m.ID
	}
	r := model.Resource{
		ID:       group.ID + "/memberships/" + url.PathEscape(m.ID),
		Name:     name,
		Service:  model.ServiceWorkspaceMember,
		ScopeID:  group.ScopeID,
		Location: "global",
		ParentID: group.ID,
		FacetID:  model.FacetMembers,
		Created:  m.Created,
		Metadata: md,
		Fields:   model.FieldsOf(md, memberFields...),
	}
	for _, role := range []string{"OWNER", "MANAGER"} {
		if containsFold(m.Roles, role) {
			r.Badge = role
			break
		}
	}
	return r
}

// parseGroupID reads the Cloud Identity group id from
// organizations/O/groups/ID.
func parseGroupID(id string) (string, bool) {
	segs := strings.Split(id, "/")
	if len(segs) != 4 || segs[0] != "organizations" || segs[1] == "" || segs[2] != "groups" || segs[3] == "" {
		return "", false
	}
	g, err := url.PathUnescape(segs[3])
	return g, err == nil
}

// customerOf is the customer that owns the organization scopeID, or ""
// with a nil error when there is nothing to list: another kind of scope,
// an organization with no customer, or one the caller may not read.
func customerOf(ctx context.Context, api API, scopeID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if model.ScopeKind(scopeID) != model.KindOrg {
		return "", nil
	}
	if api == nil {
		return "", errors.New("workspace: no API")
	}
	c, err := api.Customer(ctx, scopeID)
	if err != nil {
		return "", quiet(err, "customer", scopeID)
	}
	return c, nil
}

// quiet drops a denial, logging it, and returns any other error.
func quiet(err error, what, scope string) error {
	if errors.Is(err, ErrDenied) {
		slog.Info("workspace: not readable; showing nothing", "what", what, "scope", scope, "err", err)
		return nil
	}
	return err
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

var (
	_ model.ResourceProvider = Users{}
	_ model.ResourceProvider = Groups{}
	_ model.RelatedLister    = Groups{}
)
