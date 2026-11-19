package iam

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/cache"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// ServiceAccounts lists a project's IAM service accounts, plus the service
// agents its IAM policy grants roles to. IAM Admin cannot list service
// agents, so the policy is the only place they show up.
type ServiceAccounts struct {
	api    AdminAPI
	policy API
}

// NewServiceAccounts lists accounts from api and service agents from
// policy. A nil policy lists no service agents.
func NewServiceAccounts(api AdminAPI, policy API) ServiceAccounts {
	return ServiceAccounts{api: api, policy: policy}
}

func (p ServiceAccounts) Service() model.ServiceType { return model.ServiceServiceAccount }

// ListPermissions is what List needs.
func (p ServiceAccounts) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "iam.serviceAccounts.list")
}

func (p ServiceAccounts) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok || model.ScopeKind(scopeID) != model.KindProject {
		return nil, nil // service accounts live only in projects
	}
	if p.api == nil {
		return nil, errors.New("iam: no admin API")
	}
	accounts, err := p.api.ListServiceAccounts(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(accounts))
	listed := make(map[string]bool, len(accounts))
	for _, a := range accounts {
		out = append(out, accountResource(project, a))
		listed[strings.ToLower(a.Email)] = true
	}
	agents, err := p.agents(ctx, project, listed)
	if err != nil {
		return nil, err
	}
	return append(out, agents...), nil
}

// agents lists the service agents project's IAM policy grants roles to,
// skipping accounts already listed. The policy only adds them, so a policy
// that cannot be read is logged and skipped; it fails only when ctx ends.
func (p ServiceAccounts) agents(ctx context.Context, project string, listed map[string]bool) ([]model.Resource, error) {
	if p.policy == nil {
		return nil, nil
	}
	pol, err := p.policy.GetPolicy(ctx, "projects/"+project)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		slog.Warn("iam: project policy unavailable; service agents not listed", "project", project, "err", err)
		return nil, nil
	}
	roles := map[string][]string{}
	var emails []string
	for _, b := range pol.Bindings {
		for _, m := range b.Members {
			email, ok := strings.CutPrefix(m, "serviceAccount:")
			if !ok || listed[strings.ToLower(email)] || !model.GoogleServiceAccount(email) {
				continue
			}
			if _, seen := roles[email]; !seen {
				emails = append(emails, email)
			}
			if !slices.Contains(roles[email], b.Role) {
				roles[email] = append(roles[email], b.Role)
			}
		}
	}
	out := make([]model.Resource, 0, len(emails))
	for _, email := range emails {
		out = append(out, agentResource(project, email, roles[email]))
	}
	return out, nil
}

// agentResource is a service agent found in project's IAM policy, holding
// roles there.
func agentResource(project, email string, roles []string) model.Resource {
	slices.Sort(roles)
	md := map[string]string{model.MetaEmail: email}
	return model.Resource{
		ID:          "projects/" + project + "/serviceAccounts/" + url.PathEscape(email),
		Name:        email,
		Service:     model.ServiceServiceAccount,
		ScopeID:     "projects/" + project,
		Location:    "global",
		Description: "Google-managed service agent",
		Metadata:    md,
		Fields:      model.FieldsOf(md, model.MetaEmail),
		Lists:       []model.List{{ID: model.ListRoles, Label: "Roles in this project", Items: roles}},
	}
}

func accountResource(project string, a ServiceAccount) model.Resource {
	md := map[string]string{model.MetaEmail: a.Email}
	set := func(k, v string) {
		if v != "" {
			md[k] = v
		}
	}
	set("displayName", a.DisplayName)
	set(model.MetaUniqueID, a.UniqueID)
	set("oauth2ClientId", a.OAuth2ClientID)
	status, state := model.StatusHealthy, "ENABLED"
	if a.Disabled {
		status, state = model.StatusWarning, "DISABLED"
	}
	return model.Resource{
		ID:          "projects/" + project + "/serviceAccounts/" + url.PathEscape(a.Email),
		Name:        accountName(project, a.Email),
		Service:     model.ServiceServiceAccount,
		ScopeID:     "projects/" + project,
		Location:    "global",
		Status:      status,
		State:       state,
		Description: a.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, model.MetaEmail, "displayName", model.MetaUniqueID, "oauth2ClientId"),
		Facets:      []model.Facet{{ID: model.FacetKeys, Label: "Keys", Inline: true}},
	}
}

// ListRelated lists a listed account's keys. Service agents carry no
// facet: their keys are Google's and live in Google's projects.
func (p ServiceAccounts) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceServiceAccount || facetID != model.FacetKeys {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Kind != "serviceAccounts" || len(n.Rest) != 0 {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("iam: no admin API")
	}
	keys, err := p.api.ListServiceAccountKeys(ctx, n.Project, n.Name)
	if err != nil {
		return nil, false, err
	}
	sortKeys(keys)
	now := time.Now()
	out := make([]model.Resource, 0, len(keys))
	for _, k := range keys {
		r := keyResource(parent.ID, k, now)
		r.ScopeID = parent.ScopeID
		out = append(out, r)
	}
	return out, false, nil
}

// sortKeys puts user-managed keys first, the ones to watch, then each
// kind newest first.
func sortKeys(keys []ServiceAccountKey) {
	slices.SortStableFunc(keys, func(a, b ServiceAccountKey) int {
		if au, bu := a.Type == "USER_MANAGED", b.Type == "USER_MANAGED"; au != bu {
			if au {
				return -1
			}
			return 1
		}
		return b.ValidAfter.Compare(a.ValidAfter)
	})
}

// noExpiry is the year IAM gives a key that never expires (9999-12-31).
const noExpiry = 9999

// keyResource is a service account key. A disabled or expired key is a
// warning; an enabled key within its validity window is healthy.
func keyResource(accountID string, k ServiceAccountKey, now time.Time) model.Resource {
	md := map[string]string{}
	set := func(key, v string) {
		if v != "" {
			md[key] = v
		}
	}
	set("type", k.Type)
	set("origin", k.Origin)
	set("algorithm", strings.TrimPrefix(k.Algorithm, "KEY_ALG_"))
	expired := false
	switch {
	case k.ValidBefore.IsZero():
	case k.ValidBefore.Year() >= noExpiry:
		md["expires"] = "never"
	default:
		md["expires"] = k.ValidBefore.UTC().Format("2006-01-02 15:04 UTC")
		expired = k.ValidBefore.Before(now)
	}
	status, state, badge := model.StatusHealthy, "ENABLED", ""
	if k.Type == "USER_MANAGED" {
		badge = "USER"
	}
	switch {
	case k.Disabled:
		status, state = model.StatusWarning, "DISABLED"
	case expired:
		status, badge = model.StatusWarning, "EXPIRED"
	}
	return model.Resource{
		ID:       accountID + "/keys/" + url.PathEscape(k.ID),
		Name:     k.ID,
		Service:  model.ServiceServiceAccountKey,
		Location: "global",
		Status:   status,
		State:    state,
		Created:  k.ValidAfter, // a key is valid from its creation
		Metadata: md,
		Fields:   model.FieldsOf(md, "type", "origin", "algorithm", "expires"),
		Badge:    badge,
		ParentID: accountID,
		FacetID:  model.FacetKeys,
	}
}

// accountName is the account id for an account created in project
// (deployer@p.iam.gserviceaccount.com is "deployer"), and the full email
// for Google-managed and other projects' accounts, whose local part alone
// does not say whose they are.
func accountName(project, email string) string {
	if local, ok := strings.CutSuffix(email, "@"+project+".iam.gserviceaccount.com"); ok && local != "" {
		return local
	}
	return email
}

// rolesTTL is how long Roles keeps a role. Predefined roles
// change rarely; a custom role edited meanwhile shows its old permissions
// until then.
const rolesTTL = 5 * time.Minute

// Roles reads IAM roles and keeps them by name for rolesTTL, so a role
// bound many times, or a custom role already listed, is not read again.
// CustomRoles and RolePermissions share one.
type Roles struct {
	api   AdminAPI
	roles *cache.Cache[Role]
}

func NewRoles(api AdminAPI) *Roles {
	return &Roles{api: api, roles: cache.New[Role](rolesTTL)}
}

// list lists the custom roles defined on parent and keeps them.
func (r *Roles) list(ctx context.Context, parent string) ([]Role, error) {
	if r == nil || r.api == nil {
		return nil, errors.New("iam: no admin API")
	}
	roles, err := r.api.ListRoles(ctx, parent)
	if err != nil {
		return nil, err
	}
	for _, role := range roles {
		r.roles.Set(role.Name, role)
	}
	return roles, nil
}

// get reads the role name, once per rolesTTL.
func (r *Roles) get(ctx context.Context, name string) (Role, error) {
	if r == nil || r.api == nil {
		return Role{}, errors.New("iam: no admin API")
	}
	if role, ok := r.roles.Get(name); ok {
		return role, nil
	}
	role, err := r.api.GetRole(ctx, name)
	if err != nil {
		return Role{}, err
	}
	r.roles.Set(name, role)
	return role, nil
}

// CustomRoles lists the custom roles defined on a project or organization.
// Folders cannot hold custom roles.
type CustomRoles struct{ roles *Roles }

func NewCustomRoles(roles *Roles) CustomRoles { return CustomRoles{roles: roles} }

func (p CustomRoles) Service() model.ServiceType { return model.ServiceCustomRole }

// ListPermissions is what List needs: custom roles list in orgs and
// projects.
func (p CustomRoles) ListPermissions(scopeID string) []string {
	switch model.ScopeKind(scopeID) {
	case model.KindOrg, model.KindProject:
		return []string{"iam.roles.list"}
	}
	return nil
}

func (p CustomRoles) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch model.ScopeKind(scopeID) {
	case model.KindProject, model.KindOrg:
	default:
		return nil, nil
	}
	roles, err := p.roles.list(ctx, scopeID)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(roles))
	for _, r := range roles {
		out = append(out, roleResource(scopeID, r))
	}
	return out, nil
}

// roleResource is a custom role. Its permissions list under it, so a long
// list is not clipped in the details pane.
func roleResource(scopeID string, r Role) model.Resource {
	md := map[string]string{
		model.MetaRole:        r.Name,
		model.ListPermissions: strconv.Itoa(len(r.Permissions)),
	}
	if r.Title != "" {
		md["title"] = r.Title
	}
	return model.Resource{
		ID:          r.Name,
		Name:        path.Base(r.Name),
		Service:     model.ServiceCustomRole,
		ScopeID:     scopeID,
		Location:    "global",
		Status:      roleStatus(r.Stage),
		State:       r.Stage,
		Description: r.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "title", model.MetaRole, model.ListPermissions),
		Facets:      []model.Facet{model.RolePermissionsFacet()},
	}
}

// roleStatus maps a role's launch stage onto the coarse UI bucket. A
// disabled role grants nothing, and a deprecated one is on its way out.
// The other stages say nothing about health.
func roleStatus(stage string) model.ResourceStatus {
	switch stage {
	case "DEPRECATED", "DISABLED":
		return model.StatusWarning
	}
	return model.StatusUnknown
}

// RolePermissions lists the permissions a role grants, under a custom
// role or a role binding (model.FacetRolePermissions). It lists nothing
// at a scope.
type RolePermissions struct{ roles *Roles }

func NewRolePermissions(roles *Roles) RolePermissions { return RolePermissions{roles: roles} }

func (p RolePermissions) Service() model.ServiceType { return model.ServiceIAMPermission }

func (p RolePermissions) List(ctx context.Context, _ string) ([]model.Resource, error) {
	return nil, ctx.Err()
}

func (p RolePermissions) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if facetID != model.FacetRolePermissions {
		return nil, false, nil
	}
	role, ok := model.RoleOf(parent)
	if !ok {
		return nil, false, nil
	}
	got, err := p.roles.get(ctx, role)
	if err != nil {
		return nil, false, err
	}
	info := model.RoleInfo{Name: role, Title: got.Title, Description: got.Description}
	out := make([]model.Resource, len(got.Permissions))
	for i, perm := range got.Permissions {
		out[i] = model.PermissionResource(parent, info, perm)
	}
	return out, false, nil
}

var (
	_ model.ResourceProvider = ServiceAccounts{}
	_ model.RelatedLister    = ServiceAccounts{}
	_ model.ResourceProvider = CustomRoles{}
	_ model.ResourceProvider = RolePermissions{}
	_ model.RelatedLister    = RolePermissions{}
)
