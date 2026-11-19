package fake

import (
	"context"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/iam"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// Static is an in-memory ResourceProvider. Err, if set, is returned from List.
type Static struct {
	Svc   model.ServiceType
	Items []model.Resource
	Err   error
	// Permissions are the list permissions it declares on project scopes.
	Permissions []string
}

func (s Static) Service() model.ServiceType { return s.Svc }

func (s Static) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, s.Permissions...)
}

func (s Static) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Err != nil {
		return nil, s.Err
	}
	out := make([]model.Resource, 0, len(s.Items))
	for _, r := range s.Items {
		if r.ParentID != "" {
			continue
		}
		if scopeID != "" && r.ScopeID != "" && r.ScopeID != scopeID {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (s Static) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if s.Err != nil {
		return nil, false, s.Err
	}
	return model.Children(s.Items, parent.ID, facetID), false, nil
}

// Providers splits fixture resources into one Static provider per service.
func Providers() []model.ResourceProvider {
	grouped := make(map[model.ServiceType][]model.Resource)
	for _, r := range Resources() {
		grouped[r.Service] = append(grouped[r.Service], r)
	}
	out := make([]model.ResourceProvider, 0, len(grouped))
	for _, svc := range model.KnownServices() {
		items, ok := grouped[svc]
		if !ok {
			continue
		}
		st := Static{Svc: svc, Items: items, Permissions: fixturePermissions[svc]}
		switch svc {
		case model.ServiceIAMBinding:
			out = append(out, bindings{Static: st, iam: iam.New(policies{})})
			continue
		case model.ServiceIAMPermission:
			// Bindings bind predefined roles too, which have no fixture
			// children; the live adapter reads every role through roles.
			out = append(out, iam.NewRolePermissions(iam.NewRoles(roles{})))
			continue
		}
		out = append(out, st)
	}
	return out
}

// bindings is the fixture IAM binding provider. List goes through the live
// adapter's Provider over the fixture policies and hierarchy, so projects
// and folders show the bindings they inherit as they do live. Facet
// children (bucket permissions) come from Static.
type bindings struct {
	Static
	iam iam.Provider
}

func (b bindings) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return b.iam.List(ctx, scopeID)
}

// policies is the fixture iam.API: each CRM node's policy is its binding
// fixtures, and nodes come from Hierarchy.
type policies struct{}

func (policies) GetPolicy(ctx context.Context, resource string) (iam.Policy, error) {
	if err := ctx.Err(); err != nil {
		return iam.Policy{}, err
	}
	var pol iam.Policy
	for _, r := range Resources() {
		if !r.IsBinding() || r.ParentID != "" || !strings.HasPrefix(r.ID, resource+"/iam/bindings/") {
			continue
		}
		pol.Bindings = append(pol.Bindings, iam.Binding{
			Role:      r.Metadata["role"],
			Members:   r.List(model.ListMembers),
			Condition: r.Metadata["condition"],
		})
	}
	return pol, nil
}

func (policies) GetNode(ctx context.Context, name string) (iam.Node, error) {
	if err := ctx.Err(); err != nil {
		return iam.Node{}, err
	}
	n, ok := Hierarchy().Find(name)
	if !ok {
		return iam.Node{}, model.ErrNotFound
	}
	return iam.Node{DisplayName: n.Name, Parent: n.ParentID}, nil
}

// EnabledAPIs is the fixture Service Usage provider.
func EnabledAPIs() model.ResourceProvider {
	return enabledProvider{}
}

type enabledProvider struct{}

func (enabledProvider) Service() model.ServiceType { return model.ServiceUsage }

func (enabledProvider) List(ctx context.Context, _ string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (enabledProvider) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if facetID != model.FacetEnabledServices && !parent.IsEnabledGroup() {
		return nil, false, nil
	}
	project, ok := model.ProjectID(parent.ScopeID)
	if !ok {
		return nil, false, nil
	}
	return enabledResources(project, parent.ID), false, nil
}

// EnabledAPIs reports the fixture enabled set, the same for every project.
func (enabledProvider) EnabledAPIs(ctx context.Context, _ string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(enabledAPIs))
	for _, a := range enabledAPIs {
		out = append(out, a.name)
	}
	return out, nil
}

// enabledAPIs must list every API a fixture project resource needs, or the
// catalog gates that resource away.
var enabledAPIs = []struct{ name, title string }{
	{"storage.googleapis.com", "Cloud Storage"},
	{"compute.googleapis.com", "Compute Engine"},
	{"container.googleapis.com", "Kubernetes Engine"},
	{"sqladmin.googleapis.com", "Cloud SQL Admin API"},
	{"redis.googleapis.com", "Google Cloud Memorystore for Redis API"},
	{"memorystore.googleapis.com", "Memorystore API"},
	{"dns.googleapis.com", "Cloud DNS API"},
	{"networkservices.googleapis.com", "Network Services API"},
	{"networksecurity.googleapis.com", "Network Security API"},
	{"networkconnectivity.googleapis.com", "Network Connectivity API"},
	{"accesscontextmanager.googleapis.com", "Access Context Manager API"},
	{"iap.googleapis.com", "Cloud Identity-Aware Proxy API"},
	{"certificatemanager.googleapis.com", "Certificate Manager API"},
	{"bigquery.googleapis.com", "BigQuery"},
	{"bigqueryreservation.googleapis.com", "BigQuery Reservation API"},
	{"bigquerydatatransfer.googleapis.com", "BigQuery Data Transfer API"},
	{"bigqueryconnection.googleapis.com", "BigQuery Connection API"},
	{"run.googleapis.com", "Cloud Run"},
	{"pubsub.googleapis.com", "Cloud Pub/Sub"},
	{"firestore.googleapis.com", "Cloud Firestore"},
	{"cloudfunctions.googleapis.com", "Cloud Functions"},
	{"workstations.googleapis.com", "Cloud Workstations API"},
	{"artifactregistry.googleapis.com", "Artifact Registry API"},
	{"containeranalysis.googleapis.com", "Container Analysis API"},
	{"secretmanager.googleapis.com", "Secret Manager API"},
	{"cloudkms.googleapis.com", "Cloud Key Management Service (KMS) API"},
	{"privateca.googleapis.com", "Certificate Authority API"},
	{"logging.googleapis.com", "Cloud Logging API"},
	{"iam.googleapis.com", "Identity and Access Management (IAM)"},
	{"orgpolicy.googleapis.com", "Organization Policy API"},
}

func enabledResources(project, parentID string) []model.Resource {
	out := make([]model.Resource, 0, len(enabledAPIs))
	for _, a := range enabledAPIs {
		svc, ok := model.ServiceForAPI(a.name)
		if !ok {
			svc = model.ServiceUsage
		}
		out = append(out, model.Resource{
			ID:       "projects/" + project + "/services/" + a.name,
			Name:     a.title,
			Service:  svc,
			ScopeID:  "projects/" + project,
			Location: "global",
			ParentID: parentID,
			FacetID:  model.FacetEnabledServices,
			Metadata: map[string]string{"api": a.name, "title": a.title},
		})
	}
	return out
}

var _ model.ResourceProvider = Static{}
var _ model.RelatedLister = Static{}
var _ model.ResourceProvider = enabledProvider{}
var _ model.RelatedLister = enabledProvider{}
var _ model.EnabledLister = enabledProvider{}
