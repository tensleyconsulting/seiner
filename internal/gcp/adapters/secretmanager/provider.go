package secretmanager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/display"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// maxVersions caps a secret's versions. The explorer shows "more not shown"
// past it.
const maxVersions = 100

// Provider implements model.ResourceProvider for Secret Manager secrets. A
// secret expands to its versions. Payloads are never read.
type Provider struct{ api API }

func New(api API) Provider { return Provider{api: api} }

func (p Provider) Service() model.ServiceType { return model.ServiceSecretManager }

// ListPermissions is what List needs.
func (p Provider) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "secretmanager.secrets.list")
}

func (p Provider) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("secretmanager: no API")
	}
	secrets, err := p.api.ListSecrets(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(secrets))
	for _, s := range secrets {
		out = append(out, secretResource(project, s))
	}
	return out, nil
}

// ListRelated lists a secret's versions or IAM bindings.
func (p Provider) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceSecretManager || (facetID != model.FacetVersions && facetID != model.FacetPermissions) {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Kind != "secrets" || len(n.Rest) != 0 {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("secretmanager: no API")
	}
	if facetID == model.FacetPermissions {
		bindings, err := p.api.GetIAMPolicy(ctx, n.Project, n.Name)
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
	versions, truncated, err := p.api.ListVersions(ctx, n.Project, n.Name, maxVersions)
	if err != nil {
		return nil, false, err
	}
	out := make([]model.Resource, 0, len(versions))
	for _, v := range versions {
		r := versionResource(parent.ID, v)
		r.ScopeID = parent.ScopeID
		out = append(out, r)
	}
	return out, truncated, nil
}

func secretResource(project string, s Secret) model.Resource {
	md := map[string]string{}
	repl := s.Replication
	if len(s.Locations) > 0 {
		repl += " (" + strings.Join(s.Locations, ", ") + ")"
	}
	set(md, "replication", repl)
	set(md, "kmsKey", s.KMSKey)
	set(md, "rotation", duration(s.RotationPeriod))
	set(md, "nextRotation", display.Stamp(s.NextRotation))
	set(md, "expires", display.Stamp(s.Expires))
	set(md, "topics", strings.Join(s.Topics, ", "))
	set(md, "destroyDelay", duration(s.DestroyTTL))
	set(md, "type", s.Type)
	if len(s.Annotations) > 0 {
		md["annotations"] = strconv.Itoa(len(s.Annotations))
	}
	r := model.Resource{
		ID:       "projects/" + project + "/secrets/" + url.PathEscape(s.Name),
		Name:     s.Name,
		Service:  model.ServiceSecretManager,
		ScopeID:  "projects/" + project,
		Location: "global",
		Status:   secretStatus(s.Expires),
		Created:  s.Created,
		Labels:   maps.Clone(s.Labels),
		Metadata: md,
		Fields:   model.FieldsOf(md, "type", "replication", "kmsKey", "rotation", "nextRotation", "expires", "topics", "destroyDelay", "annotations"),
		Facets: []model.Facet{
			{ID: model.FacetVersions, Label: "Versions"},
			{ID: model.FacetPermissions, Label: "Permissions", Optional: true},
		},
	}
	if len(s.Aliases) > 0 {
		items := make([]string, 0, len(s.Aliases))
		for _, k := range slices.Sorted(maps.Keys(s.Aliases)) {
			items = append(items, k+" → "+s.Aliases[k])
		}
		r.Lists = []model.List{{ID: "aliases", Label: "Aliases", Items: items}}
	}
	if len(s.Annotations) > 0 {
		items := make([]string, 0, len(s.Annotations))
		for _, k := range slices.Sorted(maps.Keys(s.Annotations)) {
			items = append(items, k+": "+s.Annotations[k])
		}
		r.Lists = append(r.Lists, model.List{ID: "annotations", Label: "Annotations", Items: items})
	}
	return r
}

func versionResource(parentID string, v Version) model.Resource {
	md := map[string]string{}
	set(md, "destroyed", display.Stamp(v.Destroyed))
	set(md, "scheduledDestroy", display.Stamp(v.ScheduledDestroy))
	set(md, "kmsKeyVersion", strings.Join(v.KMSKeyVersions, ", "))
	return model.Resource{
		ID:       parentID + "/versions/" + url.PathEscape(v.Name),
		Name:     v.Name,
		Service:  model.ServiceSecretVersion,
		Location: "global",
		Status:   versionStatus(v.State),
		State:    v.State,
		Created:  v.Created,
		Metadata: md,
		Fields:   model.FieldsOf(md, "destroyed", "scheduledDestroy", "kmsKeyVersion"),
		Badge:    v.State,
		ParentID: parentID,
		FacetID:  model.FacetVersions,
	}
}

// secretStatus warns once a secret's expiry has passed. A secret has no
// state of its own, so it is otherwise unknown.
func secretStatus(expires time.Time) model.ResourceStatus {
	if !expires.IsZero() && expires.Before(time.Now()) {
		return model.StatusWarning
	}
	return model.StatusUnknown
}

func versionStatus(s string) model.ResourceStatus {
	switch s {
	case "ENABLED":
		return model.StatusHealthy
	case "DISABLED":
		return model.StatusWarning
	default: // DESTROYED, or unknown
		return model.StatusUnknown
	}
}

// duration turns the API's "2592000s" into "30 days" (or "36h0m0s" when not
// whole days); other strings are returned as-is.
func duration(s string) string {
	if s == "" {
		return ""
	}
	secs, err := strconv.ParseFloat(strings.TrimSuffix(s, "s"), 64)
	if err != nil || !strings.HasSuffix(s, "s") {
		return s
	}
	d := time.Duration(secs * float64(time.Second))
	if d > 0 && d%(24*time.Hour) == 0 {
		days := int(d / (24 * time.Hour))
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	return d.String()
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

var (
	_ model.ResourceProvider = Provider{}
	_ model.RelatedLister    = Provider{}
)
