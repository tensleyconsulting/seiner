package networkconnectivity

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/gcp/display"
	"github.com/tensleyconsulting/seiner/internal/model"
)

var (
	regionalEndpointFields = []string{
		"targetGoogleApi", "accessType", "ipAddress", "address",
		"network", "subnetwork", "forwardingRule", "updated",
	}
	connectionPolicyFields = []string{
		"serviceClass", "connections", "network", "subnetworks",
		"producerScope", "infrastructure", "updated",
	}
	pscConnectionFields = []string{
		"pscConnectionId", "consumerProject", "forwardingRule", "consumerAddress",
		"subnetwork", "producerInstanceId", "serviceClass", "ipVersion", "errorType", "error",
	}
)

// RegionalEndpoints implements model.ResourceProvider for Network Connectivity
// Regional Endpoints.
type RegionalEndpoints struct{ api API }

// NewRegionalEndpoints constructs a RegionalEndpoints provider over api.
func NewRegionalEndpoints(api API) RegionalEndpoints { return RegionalEndpoints{api: api} }

func (p RegionalEndpoints) Service() model.ServiceType { return model.ServiceRegionalEndpoint }

// ListPermissions is what List needs.
func (p RegionalEndpoints) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "networkconnectivity.regionalEndpoints.list")
}

// List lists Regional Endpoints across every location in project.
func (p RegionalEndpoints) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("networkconnectivity: no API")
	}
	endpoints, err := p.api.ListRegionalEndpoints(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(endpoints))
	for _, e := range endpoints {
		out = append(out, regionalEndpointResource(project, e))
	}
	return out, nil
}

// ConnectionPolicies implements model.ResourceProvider and model.RelatedLister
// for Service Connection Policies and their inline PSC connections.
type ConnectionPolicies struct{ api API }

// NewConnectionPolicies constructs a ConnectionPolicies provider over api.
func NewConnectionPolicies(api API) ConnectionPolicies { return ConnectionPolicies{api: api} }

func (p ConnectionPolicies) Service() model.ServiceType { return model.ServiceConnectionPolicy }

// ListPermissions is what List needs.
func (p ConnectionPolicies) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "networkconnectivity.serviceConnectionPolicies.list")
}

// List lists Service Connection Policies across every location in project.
func (p ConnectionPolicies) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("networkconnectivity: no API")
	}
	policies, err := p.api.ListConnectionPolicies(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(policies))
	for _, pol := range policies {
		out = append(out, policyResource(project, pol))
	}
	return out, nil
}

// ListRelated lists a Service Connection Policy's active/pending/failed PSC
// connections inline under the policy.
func (p ConnectionPolicies) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceConnectionPolicy || facetID != model.FacetConnections {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "serviceConnectionPolicies" || len(n.Rest) != 0 {
		return nil, false, nil
	}
	loc, err := url.PathUnescape(n.Location)
	if err != nil {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("networkconnectivity: no API")
	}
	pol, err := p.api.GetConnectionPolicy(ctx, n.Project, loc, n.Name)
	if err != nil {
		return nil, false, err
	}
	out := make([]model.Resource, 0, len(pol.Connections))
	for i, c := range pol.Connections {
		out = append(out, connectionResource(parent, c, i))
	}
	return out, false, nil
}

func locatedID(project, location, kind, name string) string {
	return "projects/" + project + "/locations/" + url.PathEscape(location) + "/" + kind + "/" + url.PathEscape(name)
}

func regionalEndpointResource(project string, e RegionalEndpoint) model.Resource {
	md := map[string]string{}
	set(md, "targetGoogleApi", e.TargetGoogleAPI)
	set(md, "accessType", e.AccessType)
	set(md, "ipAddress", e.IPAddress)
	set(md, "address", e.AddressName)
	set(md, "network", e.Network)
	set(md, "subnetwork", e.Subnetwork)
	set(md, "forwardingRule", e.ForwardingRule)
	set(md, "updated", display.Stamp(e.Updated))

	return model.Resource{
		ID:          locatedID(project, e.Location, "regionalEndpoints", e.Name),
		Name:        e.Name,
		Service:     model.ServiceRegionalEndpoint,
		ScopeID:     "projects/" + project,
		Location:    e.Location,
		Status:      model.StatusUnknown,
		Description: e.Description,
		Created:     e.Created,
		Labels:      maps.Clone(e.Labels),
		Metadata:    md,
		Fields:      model.FieldsOf(md, regionalEndpointFields...),
		Badge:       e.AccessType,
	}
}

func policyResource(project string, p ConnectionPolicy) model.Resource {
	md := map[string]string{}
	set(md, "serviceClass", p.ServiceClass)
	connSummary := strconv.Itoa(len(p.Connections))
	if p.ConnectionLimit > 0 {
		connSummary += " / " + strconv.FormatInt(p.ConnectionLimit, 10)
	}
	md["connections"] = connSummary
	set(md, "network", p.Network)
	set(md, "subnetworks", strings.Join(p.Subnetworks, ", "))
	switch {
	case len(p.AllowedProducers) > 0:
		md["producerScope"] = strings.Join(p.AllowedProducers, ", ")
	case p.ProducerLocation != "":
		md["producerScope"] = p.ProducerLocation
	}
	set(md, "infrastructure", p.Infrastructure)
	set(md, "updated", display.Stamp(p.Updated))

	var lists []model.List
	if len(p.AllowedProducers) > 1 {
		lists = append(lists, model.List{
			ID:    "allowedProducers",
			Label: "Allowed producer scopes",
			Items: slices.Clone(p.AllowedProducers),
		})
	}

	var facets []model.Facet
	if len(p.Connections) > 0 {
		facets = []model.Facet{{ID: model.FacetConnections, Label: "Connections", Inline: true}}
	}

	return model.Resource{
		ID:          locatedID(project, p.Location, "serviceConnectionPolicies", p.Name),
		Name:        p.Name,
		Service:     model.ServiceConnectionPolicy,
		ScopeID:     "projects/" + project,
		Location:    p.Location,
		Status:      policyStatus(p),
		Description: p.Description,
		Created:     p.Created,
		Labels:      maps.Clone(p.Labels),
		Metadata:    md,
		Fields:      model.FieldsOf(md, connectionPolicyFields...),
		Lists:       lists,
		Facets:      facets,
	}
}

func connectionResource(parent model.Resource, c Connection, index int) model.Resource {
	md := map[string]string{}
	set(md, "pscConnectionId", c.ID)
	set(md, "consumerProject", c.ConsumerProject)
	set(md, "forwardingRule", c.ForwardingRule)
	set(md, "consumerAddress", c.ConsumerAddress)
	set(md, "subnetwork", c.Subnetwork)
	set(md, "producerInstanceId", c.ProducerInstanceID)
	set(md, "serviceClass", c.ServiceClass)
	set(md, "ipVersion", c.IPVersion)
	set(md, "errorType", c.ErrorType)
	set(md, "error", c.Error)

	name := connectionName(c, index)
	cid := cmp.Or(c.ID, c.ForwardingRule, strconv.Itoa(index+1))

	return model.Resource{
		ID:       parent.ID + "/connections/" + url.PathEscape(cid),
		Name:     name,
		Service:  model.ServicePSCConnection,
		ScopeID:  parent.ScopeID,
		Location: parent.Location,
		Status:   connectionStatus(c.State, c.Error),
		State:    c.State,
		Metadata: md,
		Fields:   model.FieldsOf(md, pscConnectionFields...),
		Badge:    c.State,
		ParentID: parent.ID,
		FacetID:  model.FacetConnections,
	}
}

func connectionName(c Connection, index int) string {
	if c.ConsumerProject != "" && c.ForwardingRule != "" {
		return c.ConsumerProject + "/" + c.ForwardingRule
	}
	if c.ForwardingRule != "" {
		return c.ForwardingRule
	}
	if c.ID != "" {
		return c.ID
	}
	return fmt.Sprintf("connection-%d", index+1)
}

func policyStatus(p ConnectionPolicy) model.ResourceStatus {
	hasWarning := p.ConnectionLimit > 0 && int64(len(p.Connections)) >= p.ConnectionLimit
	hasHealthy := false
	for _, c := range p.Connections {
		switch connectionStatus(c.State, c.Error) {
		case model.StatusError:
			return model.StatusError
		case model.StatusWarning:
			hasWarning = true
		case model.StatusHealthy:
			hasHealthy = true
		}
	}
	switch {
	case hasWarning:
		return model.StatusWarning
	case hasHealthy:
		return model.StatusHealthy
	default:
		return model.StatusUnknown
	}
}

func connectionStatus(state, errMsg string) model.ResourceStatus {
	if errMsg != "" || state == "FAILED" {
		return model.StatusError
	}
	switch state {
	case "ACTIVE":
		return model.StatusHealthy
	case "CREATING", "DELETING", "PRODUCER_REPAIRING", "PRODUCER_DELETING":
		return model.StatusWarning
	default:
		return model.StatusUnknown
	}
}

func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

var (
	_ model.ResourceProvider   = RegionalEndpoints{}
	_ model.PermissionDeclarer = RegionalEndpoints{}
	_ model.ResourceProvider   = ConnectionPolicies{}
	_ model.PermissionDeclarer = ConnectionPolicies{}
	_ model.RelatedLister      = ConnectionPolicies{}
)
