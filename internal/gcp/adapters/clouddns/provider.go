package clouddns

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"strconv"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// maxRecords caps a zone's Record Sets. The explorer shows "more not shown"
// past it.
const maxRecords = 500

// Provider implements model.ResourceProvider for Cloud DNS managed zones.
// Record sets are not listed at the top level; a zone expands to them.
type Provider struct{ api API }

func New(api API) Provider { return Provider{api: api} }

func (p Provider) Service() model.ServiceType { return model.ServiceCloudDNS }

// ListPermissions is what List needs.
func (p Provider) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "dns.managedZones.list")
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
		return nil, errors.New("clouddns: no API")
	}
	zones, err := p.api.ListZones(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(zones))
	for _, z := range zones {
		out = append(out, zoneResource(project, z))
	}
	return out, nil
}

// ListRelated lists a zone's record sets.
func (p Provider) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceCloudDNS || facetID != model.FacetRecords {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Kind != "managedZones" || len(n.Rest) != 0 {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("clouddns: no API")
	}
	records, truncated, err := p.api.ListRecords(ctx, n.Project, n.Name, maxRecords)
	if err != nil {
		return nil, false, err
	}
	out := make([]model.Resource, 0, len(records))
	for _, rr := range records {
		r := recordResource(n.Project, parent.ID, rr)
		r.ScopeID = parent.ScopeID
		out = append(out, r)
	}
	return out, truncated, nil
}

func zoneID(project, zone string) string {
	return "projects/" + project + "/managedZones/" + url.PathEscape(zone)
}

func zoneResource(project string, z Zone) model.Resource {
	md := map[string]string{}
	set(md, "dnsName", z.DNSName)
	set(md, "visibility", z.Visibility)
	set(md, "dnssec", z.DNSSEC)
	set(md, "nameServers", strings.Join(z.NameServers, ", "))
	set(md, "networks", strings.Join(z.Networks, ", "))
	set(md, "gkeClusters", strings.Join(z.GKEClusters, ", "))
	set(md, "forwarding", strings.Join(z.Forwarding, ", "))
	set(md, "peerNetwork", z.PeerNetwork)
	md["logging"] = onOff(z.Logging)
	return model.Resource{
		ID:          zoneID(project, z.Name),
		Name:        z.Name,
		Service:     model.ServiceCloudDNS,
		ScopeID:     "projects/" + project,
		Location:    "global",
		Description: z.Description,
		Created:     z.Created,
		Labels:      maps.Clone(z.Labels),
		Metadata:    md,
		Fields: model.FieldsOf(md, "dnsName", "visibility", "dnssec", "nameServers", "networks", "gkeClusters",
			"forwarding", "peerNetwork", "logging"),
		Facets: []model.Facet{{ID: model.FacetRecords, Label: "Record Sets", Inline: true}},
	}
}

// recordResource is a record set under its zone. Its id is the zone's plus
// rrsets/{name}/{type}, since a name holds one record set per type.
func recordResource(project, parentID string, rr Record) model.Resource {
	md := map[string]string{"type": rr.Type, "ttl": strconv.FormatInt(rr.TTL, 10)}
	set(md, "routing", rr.Routing)
	if len(rr.Data) == 1 {
		md["data"] = rr.Data[0]
	} else if len(rr.Data) > 1 {
		md["data"] = strconv.Itoa(len(rr.Data)) + " values"
	}
	r := model.Resource{
		ID:       parentID + "/rrsets/" + url.PathEscape(rr.Name) + "/" + url.PathEscape(rr.Type),
		Name:     rr.Name,
		Service:  model.ServiceDNSRecord,
		ScopeID:  "projects/" + project,
		Location: "global",
		Metadata: md,
		Fields:   model.FieldsOf(md, "type", "ttl", "data", "routing"),
		Badge:    rr.Type,
		ParentID: parentID,
		FacetID:  model.FacetRecords,
	}
	if len(rr.Data) > 1 {
		r.Lists = []model.List{{ID: "data", Label: "Data", Items: rr.Data}}
	}
	return r
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

var (
	_ model.ResourceProvider = Provider{}
	_ model.RelatedLister    = Provider{}
)
