package gke

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// Provider implements model.ResourceProvider for GKE clusters. Node pools
// are not listed at the top level; they are the cluster's Node Pools facet.
type Provider struct{ api API }

func New(api API) Provider { return Provider{api: api} }

func (p Provider) Service() model.ServiceType { return model.ServiceGKE }

// ListPermissions is what List needs.
func (p Provider) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "container.clusters.list")
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
		return nil, errors.New("gke: no API")
	}
	// A model.Partial error comes back with the clusters that loaded.
	clusters, err := p.api.ListClusters(ctx, project)
	if err != nil && !model.IsPartial(err) {
		return nil, err
	}
	out := make([]model.Resource, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, clusterResource(project, c))
	}
	return out, err
}

// ListRelated lists a cluster's node pools.
func (p Provider) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceGKE || facetID != model.FacetNodePools {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "clusters" || len(n.Rest) > 0 {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("gke: no API")
	}
	pools, err := p.api.ListNodePools(ctx, n.Project, n.Location, n.Name)
	if err != nil {
		return nil, false, err
	}
	out := make([]model.Resource, 0, len(pools))
	for _, np := range pools {
		out = append(out, nodePoolResource(n.Project, n.Location, parent.ID, np))
	}
	return out, false, nil
}

func clusterID(project, location, name string) string {
	return "projects/" + project + "/locations/" + location + "/clusters/" + name
}

// maxPoolNames caps the node pool names in a cluster's summary field.
const maxPoolNames = 5

func clusterResource(project string, c Cluster) model.Resource {
	md := map[string]string{}
	set(md, "statusMessage", c.StatusMessage)
	if c.Autopilot {
		md["mode"] = "Autopilot"
	} else {
		md["mode"] = "Standard"
	}
	set(md, "version", c.MasterVersion)
	if c.ReleaseChannel != "" && c.ReleaseChannel != "UNSPECIFIED" {
		md["releaseChannel"] = c.ReleaseChannel
	}
	md["locationType"] = locationType(c.Location)
	md["nodes"] = strconv.FormatInt(c.NodeCount, 10)
	md["nodePools"] = poolSummary(c.NodePools)
	set(md, "endpoint", c.Endpoint)
	md["privateNodes"] = onOff(c.PrivateNodes)
	set(md, "network", c.Network)
	set(md, "subnetwork", c.Subnetwork)
	set(md, "podRange", c.PodRange)
	set(md, "servicesRange", c.ServicesRange)
	set(md, "workloadPool", c.WorkloadPool)
	if c.NodeVersion != "" && c.NodeVersion != c.MasterVersion {
		md["nodeVersion"] = c.NodeVersion
	}
	set(md, "publicEndpoint", c.PublicEndpoint)
	if c.DNSEndpoint != "" {
		md["dnsEndpoint"] = c.DNSEndpoint
		if c.DNSExternal {
			md["dnsEndpoint"] += " (external traffic on)"
		}
	}
	var lists []model.List
	if c.AuthorizedNetworks {
		md["authorizedNetworks"] = strconv.Itoa(len(c.AuthorizedCIDRs))
		if len(c.AuthorizedCIDRs) > 0 {
			lists = append(lists, model.List{ID: "authorizedNetworks", Label: "Authorized networks", Items: slices.Clone(c.AuthorizedCIDRs)})
		}
	} else {
		md["authorizedNetworks"] = "off"
	}
	set(md, "etcdEncryption", c.EtcdKey)
	set(md, "maintenance", c.Maintenance)
	if len(c.Exclusions) > 0 {
		md["maintenanceExclusions"] = strconv.Itoa(len(c.Exclusions))
		lists = append(lists, model.List{ID: "maintenanceExclusions", Label: "Maintenance exclusions", Items: slices.Clone(c.Exclusions)})
	}
	switch c.Dataplane {
	case "ADVANCED_DATAPATH":
		md["dataplane"] = "V2"
	case "LEGACY_DATAPATH":
		md["dataplane"] = "legacy"
	}
	md["shieldedNodes"] = onOff(c.ShieldedNodes)
	if c.BinAuthz != "" && c.BinAuthz != "DISABLED" && c.BinAuthz != "EVALUATION_MODE_UNSPECIFIED" {
		md["binaryAuthorization"] = c.BinAuthz
	}
	return model.Resource{
		ID:          clusterID(project, c.Location, c.Name),
		Name:        c.Name,
		Service:     model.ServiceGKE,
		ScopeID:     "projects/" + project,
		Location:    c.Location,
		Status:      clusterStatus(c.Status),
		State:       c.Status,
		Description: c.Description,
		Created:     c.Created,
		Labels:      maps.Clone(c.Labels),
		Metadata:    md,
		Fields: model.FieldsOf(md, "statusMessage", "mode", "version", "nodeVersion", "releaseChannel", "locationType", "nodes",
			"nodePools", "endpoint", "publicEndpoint", "dnsEndpoint", "authorizedNetworks", "privateNodes", "network",
			"subnetwork", "podRange", "servicesRange", "dataplane", "workloadPool", "etcdEncryption", "shieldedNodes",
			"binaryAuthorization", "maintenance", "maintenanceExclusions"),
		Lists:  lists,
		Facets: []model.Facet{{ID: model.FacetNodePools, Label: "Node Pools"}},
	}
}

func nodePoolResource(project, location, parentID string, np NodePool) model.Resource {
	md := map[string]string{}
	set(md, "statusMessage", np.StatusMessage)
	set(md, "machineType", np.MachineType)
	if len(np.Accelerators) > 0 {
		as := make([]string, 0, len(np.Accelerators))
		for _, a := range np.Accelerators {
			as = append(as, fmt.Sprintf("%d× %s", a.Count, a.Type))
		}
		md["accelerators"] = strings.Join(as, ", ")
	}
	set(md, "version", np.Version)
	if np.Autoscaling {
		if np.TotalMax > 0 {
			md["autoscaling"] = fmt.Sprintf("%d–%d total", np.TotalMin, np.TotalMax)
		} else {
			md["autoscaling"] = fmt.Sprintf("%d–%d per zone", np.MinNodes, np.MaxNodes)
		}
	} else {
		md["initialNodes"] = fmt.Sprintf("%d per zone", np.InitialNodes)
	}
	set(md, "zones", strings.Join(np.Zones, ", "))
	if np.DiskSizeGB > 0 {
		d := fmt.Sprintf("%d GB", np.DiskSizeGB)
		if np.DiskType != "" {
			d += " " + np.DiskType
		}
		md["disk"] = d
	}
	set(md, "imageType", np.ImageType)
	switch {
	case np.Spot:
		md["provisioning"] = "SPOT"
	case np.Preemptible:
		md["provisioning"] = "PREEMPTIBLE"
	default:
		md["provisioning"] = "STANDARD"
	}
	set(md, "serviceAccount", np.ServiceAccount)
	md["autoUpgrade"] = onOff(np.AutoUpgrade)
	md["autoRepair"] = onOff(np.AutoRepair)
	if np.MaxPodsPerNode > 0 {
		md["maxPodsPerNode"] = strconv.FormatInt(np.MaxPodsPerNode, 10)
	}
	if np.UpgradeType != "" && np.UpgradeType != "NODE_POOL_UPDATE_STRATEGY_UNSPECIFIED" {
		u := np.UpgradeType
		if u == "SURGE" {
			u += fmt.Sprintf(" (surge %d, unavailable %d)", np.MaxSurge, np.MaxUnavailable)
		}
		md["upgradeStrategy"] = u
	}
	set(md, "kmsKey", np.KMSKey)
	set(md, "metadataServer", np.MetadataServer)
	set(md, "sandbox", np.Sandbox)
	var shielded []string
	if np.SecureBoot {
		shielded = append(shielded, "secure boot")
	}
	if np.Integrity {
		shielded = append(shielded, "integrity monitoring")
	}
	set(md, "shieldedVM", strings.Join(shielded, ", "))
	var lists []model.List
	if len(np.Taints) > 0 {
		md["taints"] = strconv.Itoa(len(np.Taints))
		lists = append(lists, model.List{ID: "taints", Label: "Taints", Items: slices.Clone(np.Taints)})
	}
	if len(np.NodeLabels) > 0 {
		md["nodeLabels"] = strconv.Itoa(len(np.NodeLabels))
		lists = append(lists, model.List{ID: "nodeLabels", Label: "Node labels", Items: slices.Clone(np.NodeLabels)})
	}
	var facets []model.Facet
	if len(np.InstanceGroups) > 0 {
		md["instanceGroups"] = strconv.Itoa(len(np.InstanceGroups))
		lists = append(lists, model.List{ID: model.ListInstanceGroups, Label: "Instance groups", Items: slices.Clone(np.InstanceGroups)})
		facets = []model.Facet{{ID: model.FacetNodes, Label: "Nodes", Inline: true}}
	}
	return model.Resource{
		ID:       parentID + "/nodePools/" + np.Name,
		Name:     np.Name,
		Service:  model.ServiceGKENodePool,
		ScopeID:  "projects/" + project,
		Location: location,
		Status:   nodePoolStatus(np.Status),
		State:    np.Status,
		Metadata: md,
		Fields: model.FieldsOf(md, "statusMessage", "machineType", "accelerators", "version", "autoscaling",
			"initialNodes", "zones", "disk", "kmsKey", "imageType", "provisioning", "sandbox", "serviceAccount",
			"metadataServer", "shieldedVM", "autoUpgrade", "autoRepair", "upgradeStrategy", "maxPodsPerNode",
			"taints", "nodeLabels", "instanceGroups"),
		Lists:    lists,
		ParentID: parentID,
		FacetID:  model.FacetNodePools,
		Facets:   facets,
	}
}

// poolSummary is "2 (default-pool, gpu-pool)", capped at maxPoolNames names.
func poolSummary(names []string) string {
	if len(names) == 0 {
		return "0"
	}
	shown := names
	more := ""
	if len(shown) > maxPoolNames {
		shown = shown[:maxPoolNames]
		more = ", …"
	}
	return fmt.Sprintf("%d (%s%s)", len(names), strings.Join(shown, ", "), more)
}

// locationType is "zonal" for a zone (us-central1-a) and "regional" for a
// region (us-central1).
func locationType(loc string) string {
	if strings.Count(loc, "-") >= 2 {
		return "zonal"
	}
	return "regional"
}

func clusterStatus(s string) model.ResourceStatus {
	switch s {
	case "RUNNING":
		return model.StatusHealthy
	case "PROVISIONING", "RECONCILING", "STOPPING":
		return model.StatusWarning
	case "ERROR", "DEGRADED":
		return model.StatusError
	default:
		return model.StatusUnknown
	}
}

func nodePoolStatus(s string) model.ResourceStatus {
	switch s {
	case "RUNNING":
		return model.StatusHealthy
	case "PROVISIONING", "RECONCILING", "STOPPING", "RUNNING_WITH_ERROR":
		return model.StatusWarning
	case "ERROR":
		return model.StatusError
	default:
		return model.StatusUnknown
	}
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
