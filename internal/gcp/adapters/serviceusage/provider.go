package serviceusage

import (
	"context"
	"errors"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/gcp/cache"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// APIService is one enabled API, Google-free.
type APIService struct {
	Name    string // storage.googleapis.com
	Title   string
	Summary string // one-line description from the service's documentation
	DocsURL string // documentation root URL
}

// API is the Service Usage ↔ Resource boundary.
type API interface {
	ListEnabled(ctx context.Context, project string) ([]APIService, error)
}

// Provider lists enabled APIs as related children of the Enabled Services
// node and reports them to the catalog for gating. Both read through one
// per-project cache. Provider is a value type; copies share the cache.
type Provider struct {
	api   API
	cache *cache.Flight[[]APIService]
}

// New returns a Provider over api with a fresh enabled-API cache.
func New(api API) Provider {
	return Provider{api: api, cache: newEnabledCache()}
}

func (p Provider) Service() model.ServiceType { return model.ServiceUsage }

func (p Provider) List(ctx context.Context, _ string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (p Provider) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if facetID != model.FacetEnabledServices && !parent.IsEnabledGroup() {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("serviceusage: no API")
	}
	project, ok := model.ProjectID(parent.ScopeID)
	if !ok {
		project, ok = model.ProjectID(parent.ParentID)
	}
	if !ok {
		return nil, false, nil
	}
	svcs, err := p.enabled(ctx, project)
	if err != nil {
		return nil, false, err
	}
	out := make([]model.Resource, 0, len(svcs))
	for _, s := range svcs {
		out = append(out, ResourceFrom(project, parent.ID, s))
	}
	return out, false, nil
}

// EnabledAPIs reports the enabled API names for project.
func (p Provider) EnabledAPIs(ctx context.Context, project string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.api == nil {
		return nil, errors.New("serviceusage: no API")
	}
	svcs, err := p.enabled(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(svcs))
	for _, s := range svcs {
		out = append(out, s.Name)
	}
	return out, nil
}

func (p Provider) enabled(ctx context.Context, project string) ([]APIService, error) {
	fetch := func(ctx context.Context) ([]APIService, error) { return p.api.ListEnabled(ctx, project) }
	if p.cache == nil {
		return fetch(ctx)
	}
	return p.cache.Do(ctx, project, fetch)
}

// InvalidateEnabled drops project's cached enabled set, so the next lookup
// asks Service Usage again. The catalog calls it on a manual refresh.
func (p Provider) InvalidateEnabled(project string) {
	if p.cache != nil {
		p.cache.Forget(project)
	}
}

// ResourceFrom maps an enabled API onto the UI model.
func ResourceFrom(project, parentID string, s APIService) model.Resource {
	name := s.Title
	if name == "" {
		name = s.Name
	}
	svc, ok := model.ServiceForAPI(s.Name)
	if !ok {
		svc = model.ServiceUsage
	}
	md := map[string]string{"api": s.Name}
	if s.Title != "" {
		md["title"] = s.Title
	}
	if s.DocsURL != "" {
		md["documentation"] = s.DocsURL
	}
	return model.Resource{
		ID:          "projects/" + project + "/services/" + s.Name,
		Name:        name,
		Service:     svc,
		ScopeID:     "projects/" + project,
		Location:    "global",
		Description: strings.TrimSpace(s.Summary),
		ParentID:    parentID,
		FacetID:     model.FacetEnabledServices,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "api", "title", "documentation"),
	}
}

var _ model.ResourceProvider = Provider{}
var _ model.RelatedLister = Provider{}
var _ model.EnabledLister = Provider{}
