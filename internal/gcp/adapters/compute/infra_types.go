package compute

import (
	"fmt"
	"maps"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// Disk is a zonal or regional persistent disk. Exactly one of Zone and
// Region is set.
type Disk struct {
	Name           string
	Zone           string
	Region         string
	Type           string // pd-balanced, pd-ssd, hyperdisk-balanced, ...
	SizeGB         int64
	Status         string // CREATING, RESTORING, FAILED, READY, DELETING
	Users          []string
	SourceImage    string
	SourceSnapshot string
	ReplicaZones   []string
	IOPS           int64
	Throughput     int64
	Architecture   string
	KMSKey         string // empty for Google-managed encryption
	LastAttach     time.Time
	Description    string
	Created        time.Time
	Labels         map[string]string
}

// InstanceTemplate is a VM template. Region is empty for a global template.
type InstanceTemplate struct {
	Name           string
	Region         string
	MachineType    string
	BootImage      string
	BootDiskGB     int64
	BootDiskType   string
	Disks          int
	Network        string
	Subnetwork     string
	ExternalIP     bool
	ServiceAccount string
	Tags           []string
	Provisioning   string
	Description    string
	Created        time.Time
	Labels         map[string]string
}

// InstanceGroup is a managed instance group. Exactly one of Zone and Region
// is set.
type InstanceGroup struct {
	Name             string
	Zone             string
	Region           string
	BaseInstanceName string
	TargetSize       int64
	Templates        []string // one per version
	Stable           bool
	Autoscaler       string
	Actions          map[string]int64 // current action -> instance count
	UpdateType       string           // PROACTIVE or OPPORTUNISTIC
	AutoHealing      []string         // health check names
	NamedPorts       []string         // name:port
	Description      string
	Created          time.Time

	MaxSurge          string // "3" or "20%"
	MaxUnavailable    string
	MinimalAction     string   // REPLACE, RESTART, REFRESH, NONE
	ReplacementMethod string   // SUBSTITUTE or RECREATE
	AutoHealDelay     int64    // seconds before autohealing checks a new instance
	Zones             []string // a regional group's distribution zones
	TargetShape       string   // EVEN, BALANCED, ANY, ANY_SINGLE_ZONE

	// From the group's autoscaler, when it has one and it could be read.
	AutoscaleMode   string // ON, OFF, ONLY_SCALE_OUT
	AutoscaleMin    int64
	AutoscaleMax    int64
	AutoscaleCPU    float64 // CPU utilization target, 0..1; 0 when not set
	AutoscaleStatus string  // ACTIVE, PENDING, DELETING, ERROR
	Recommended     int64   // the autoscaler's recommended size
}

// ManagedInstance is one instance of a managed instance group.
type ManagedInstance struct {
	Name         string
	Zone         string
	Status       string // the instance's status: RUNNING, STAGING, ...
	TargetStatus string
	Action       string   // the group's current action on it: NONE, CREATING, ...
	Template     string   // the version's instance template
	Version      string   // the version's name, when the group names versions
	Health       []string // "HEALTHY (web-hc)", one per health check
	Errors       []string // the last attempt's errors
}

// Route is a VPC route.
type Route struct {
	Name        string
	Network     string
	DestRange   string
	Priority    int64
	NextHop     string // kind/name, an IP, or default-internet-gateway
	Tags        []string
	RouteType   string
	Warnings    []string
	Description string
	Created     time.Time
}

// Router is a Cloud Router.
type Router struct {
	Name          string
	Region        string
	Network       string
	ASN           int64
	AdvertiseMode string
	Interfaces    int
	Peers         []BGPPeer
	NATs          []NAT
	Description   string
	Created       time.Time
}

// BGPPeer is one BGP session on a Cloud Router.
type BGPPeer struct {
	Name    string
	PeerIP  string
	PeerASN int64
	Enabled bool
}

// NAT is a Cloud NAT gateway, configured on a Cloud Router.
type NAT struct {
	Name                string
	IPAllocation        string // AUTO_ONLY or MANUAL_ONLY
	NATIPs              []string
	SourceRanges        string // ALL_SUBNETWORKS_ALL_IP_RANGES, LIST_OF_SUBNETWORKS, ...
	Subnetworks         []string
	MinPortsPerVM       int64
	MaxPortsPerVM       int64
	DynamicPorts        bool
	EndpointIndependent bool
	Logging             string // filter (ERRORS_ONLY, ALL, ...) or "" when off
}

func zonalID(project, zone, region, kind, name string) string {
	if zone != "" {
		return "projects/" + project + "/zones/" + zone + "/" + kind + "/" + name
	}
	return regionalID(project, region, kind, name)
}

func zoneOrRegion(zone, region string) string {
	if zone != "" {
		return zone
	}
	return locationOf(region)
}

func diskResource(project string, d Disk) model.Resource {
	md := map[string]string{}
	set(md, "type", d.Type)
	if d.SizeGB > 0 {
		md["size"] = strconv.FormatInt(d.SizeGB, 10) + " GB"
	}
	if len(d.Users) > 0 {
		md["attachedTo"] = strings.Join(d.Users, ", ")
	} else {
		md["attachedTo"] = "none"
	}
	set(md, "sourceImage", d.SourceImage)
	set(md, "sourceSnapshot", d.SourceSnapshot)
	set(md, "replicaZones", strings.Join(d.ReplicaZones, ", "))
	if d.IOPS > 0 {
		md["provisionedIops"] = strconv.FormatInt(d.IOPS, 10)
	}
	if d.Throughput > 0 {
		md["provisionedThroughput"] = strconv.FormatInt(d.Throughput, 10) + " MB/s"
	}
	set(md, "architecture", d.Architecture)
	if d.KMSKey != "" {
		md["encryption"] = d.KMSKey
	} else {
		md["encryption"] = "Google-managed"
	}
	if !d.LastAttach.IsZero() {
		md["lastAttached"] = d.LastAttach.UTC().Format("2006-01-02 15:04 UTC")
	}
	return model.Resource{
		ID:          zonalID(project, d.Zone, d.Region, "disks", d.Name),
		Name:        d.Name,
		Service:     model.ServiceDisk,
		ScopeID:     "projects/" + project,
		Location:    zoneOrRegion(d.Zone, d.Region),
		Status:      diskStatus(d.Status),
		State:       d.Status,
		Description: d.Description,
		Metadata:    md,
		Fields: model.FieldsOf(md, "type", "size", "attachedTo", "sourceImage", "sourceSnapshot", "replicaZones",
			"provisionedIops", "provisionedThroughput", "architecture", "encryption", "lastAttached"),
		Created: d.Created,
		Labels:  maps.Clone(d.Labels),
		Facets:  permissionsFacet,
	}
}

func diskStatus(s string) model.ResourceStatus {
	switch strings.ToUpper(s) {
	case "READY":
		return model.StatusHealthy
	case "CREATING", "RESTORING", "DELETING":
		return model.StatusWarning
	case "FAILED":
		return model.StatusError
	default:
		return model.StatusUnknown
	}
}

func instanceTemplateResource(project string, t InstanceTemplate) model.Resource {
	md := map[string]string{}
	set(md, "machineType", t.MachineType)
	set(md, "bootImage", t.BootImage)
	if t.BootDiskGB > 0 || t.BootDiskType != "" {
		boot := strings.TrimSpace(fmt.Sprintf("%s %s", sizeGB(t.BootDiskGB), t.BootDiskType))
		md["bootDisk"] = boot
	}
	if t.Disks > 1 {
		md["disks"] = strconv.Itoa(t.Disks)
	}
	set(md, "network", t.Network)
	set(md, "subnetwork", t.Subnetwork)
	md["externalIP"] = onOff(t.ExternalIP)
	set(md, "serviceAccount", t.ServiceAccount)
	set(md, "networkTags", strings.Join(t.Tags, ","))
	set(md, "provisioning", t.Provisioning)
	r := model.Resource{
		ID:          regionalID(project, t.Region, "instanceTemplates", t.Name),
		Name:        t.Name,
		Service:     model.ServiceInstanceTemplate,
		ScopeID:     "projects/" + project,
		Location:    locationOf(t.Region),
		Description: t.Description,
		Metadata:    md,
		Fields: model.FieldsOf(md, "machineType", "bootImage", "bootDisk", "disks", "network", "subnetwork",
			"externalIP", "serviceAccount", "networkTags", "provisioning"),
		Created: t.Created,
		Labels:  maps.Clone(t.Labels),
	}
	if t.Region == "" {
		r.Facets = permissionsFacet
	}
	return r
}

func sizeGB(n int64) string {
	if n <= 0 {
		return ""
	}
	return strconv.FormatInt(n, 10) + " GB"
}

// actionOrder is the order the Console lists a group's current actions.
var actionOrder = []string{"none", "creating", "creatingWithoutRetries", "recreating", "deleting", "abandoning",
	"restarting", "refreshing", "verifying", "starting", "stopping", "suspending", "resuming"}

func instanceGroupResource(project string, g InstanceGroup) model.Resource {
	md := map[string]string{}
	md["targetSize"] = strconv.FormatInt(g.TargetSize, 10)
	set(md, "template", strings.Join(g.Templates, ", "))
	var actions []string
	for _, a := range actionOrder {
		if n := g.Actions[a]; n > 0 {
			actions = append(actions, fmt.Sprintf("%s %d", a, n))
		}
	}
	set(md, "instances", strings.Join(actions, ", "))
	set(md, "autoscaler", g.Autoscaler)
	set(md, "updatePolicy", g.UpdateType)
	set(md, "autoHealing", strings.Join(g.AutoHealing, ", "))
	set(md, "namedPorts", strings.Join(g.NamedPorts, ", "))
	set(md, "baseInstanceName", g.BaseInstanceName)
	set(md, "autoscaling", autoscaling(g))
	if g.Recommended > 0 && g.AutoscaleMode != "" {
		md["recommendedSize"] = strconv.FormatInt(g.Recommended, 10)
	}
	var surge []string
	if g.MaxSurge != "" {
		surge = append(surge, "surge "+g.MaxSurge)
	}
	if g.MaxUnavailable != "" {
		surge = append(surge, "unavailable "+g.MaxUnavailable)
	}
	set(md, "updateLimits", strings.Join(surge, ", "))
	set(md, "minimalAction", g.MinimalAction)
	set(md, "replacementMethod", g.ReplacementMethod)
	if g.AutoHealDelay > 0 && len(g.AutoHealing) > 0 {
		md["autoHealingDelay"] = (time.Duration(g.AutoHealDelay) * time.Second).String()
	}
	if len(g.Zones) > 0 {
		z := strings.Join(g.Zones, ", ")
		if g.TargetShape != "" {
			z += " (" + g.TargetShape + ")"
		}
		md["zones"] = z
	}
	st, state := model.StatusHealthy, "STABLE"
	if !g.Stable {
		st, state = model.StatusWarning, "UPDATING"
	}
	return model.Resource{
		ID:          zonalID(project, g.Zone, g.Region, "instanceGroupManagers", g.Name),
		Name:        g.Name,
		Service:     model.ServiceInstanceGroup,
		ScopeID:     "projects/" + project,
		Location:    zoneOrRegion(g.Zone, g.Region),
		Status:      st,
		State:       state,
		Description: g.Description,
		Metadata:    md,
		Fields: model.FieldsOf(md, "targetSize", "instances", "template", "autoscaler", "autoscaling", "recommendedSize",
			"updatePolicy", "updateLimits", "minimalAction", "replacementMethod", "autoHealing", "autoHealingDelay",
			"zones", "namedPorts", "baseInstanceName"),
		Created: g.Created,
		Facets:  []model.Facet{{ID: model.FacetInstances, Label: "Instances", Inline: true}},
	}
}

// autoscaling reads "2–10, CPU 60%" from the group's autoscaler; the mode
// is added unless it is ON.
func autoscaling(g InstanceGroup) string {
	if g.AutoscaleMode == "" {
		return ""
	}
	if g.AutoscaleMode == "OFF" {
		return "off"
	}
	s := fmt.Sprintf("%d–%d", g.AutoscaleMin, g.AutoscaleMax)
	if g.AutoscaleCPU > 0 {
		s += fmt.Sprintf(", CPU %d%%", int(math.Round(g.AutoscaleCPU*100)))
	}
	if g.AutoscaleMode != "ON" {
		s += ", " + g.AutoscaleMode
	}
	if g.AutoscaleStatus == "ERROR" {
		s += ", autoscaler error"
	}
	return s
}

// managedInstanceResource is one instance of a group (a MIG, or a GKE node
// pool's groups). Its id nests under the row it lists under
// (…/instanceGroupManagers/g/instances/vm), so it never collides with the
// same VM's GCE Instance row; the VM's own name is its vm field. A
// managed instance that has no zone yet (still being created) takes the
// group's location.
func managedInstanceResource(project, parentID, facetID, group, groupLocation string, m ManagedInstance) model.Resource {
	md := map[string]string{}
	location := m.Zone
	if location == "" {
		location = groupLocation
	} else {
		md["vm"] = "projects/" + project + "/zones/" + m.Zone + "/instances/" + m.Name
	}
	set(md, "instanceStatus", m.Status)
	if m.TargetStatus != "" && m.TargetStatus != m.Status {
		md["targetStatus"] = m.TargetStatus
	}
	set(md, "currentAction", m.Action)
	set(md, "template", m.Template)
	set(md, "version", m.Version)
	set(md, "health", strings.Join(m.Health, ", "))
	set(md, "group", group)
	var lists []model.List
	if len(m.Errors) > 0 {
		md["errors"] = strconv.Itoa(len(m.Errors))
		lists = []model.List{{ID: "errors", Label: "Last attempt errors", Items: slices.Clone(m.Errors)}}
	}
	return model.Resource{
		ID:       parentID + "/instances/" + url.PathEscape(m.Name),
		Name:     m.Name,
		Service:  model.ServiceManagedInstance,
		ScopeID:  "projects/" + project,
		Location: location,
		Status:   managedInstanceStatus(m),
		State:    m.Status,
		Metadata: md,
		Fields:   model.FieldsOf(md, "instanceStatus", "targetStatus", "currentAction", "health", "template", "version", "group", "vm", "errors"),
		Lists:    lists,
		Badge:    actionBadge(m.Action),
		ParentID: parentID,
		FacetID:  facetID,
	}
}

// managedInstanceStatus: the last attempt's errors or an unhealthy check
// are errors; a group action under way is a warning; a running instance
// with nothing else to say is healthy.
func managedInstanceStatus(m ManagedInstance) model.ResourceStatus {
	if len(m.Errors) > 0 {
		return model.StatusError
	}
	for _, h := range m.Health {
		if strings.HasPrefix(h, "UNHEALTHY") || strings.HasPrefix(h, "TIMEOUT") {
			return model.StatusError
		}
	}
	if m.Action != "" && m.Action != "NONE" {
		return model.StatusWarning
	}
	return instanceStatus(m.Status)
}

// actionBadge shows what the group is doing to the instance, if anything.
func actionBadge(action string) string {
	if action == "NONE" {
		return ""
	}
	return action
}

func routeResource(project string, r Route, parentID string) model.Resource {
	md := map[string]string{}
	set(md, "destination", r.DestRange)
	if r.Priority > 0 {
		md["priority"] = strconv.FormatInt(r.Priority, 10)
	}
	set(md, "nextHop", r.NextHop)
	set(md, "network", r.Network)
	set(md, "instanceTags", strings.Join(r.Tags, ","))
	set(md, "routeType", r.RouteType)
	set(md, "warnings", strings.Join(r.Warnings, "; "))
	var st model.ResourceStatus
	if len(r.Warnings) > 0 {
		st = model.StatusWarning
	}
	res := model.Resource{
		ID:          "projects/" + project + "/global/routes/" + r.Name,
		Name:        r.Name,
		Service:     model.ServiceRoute,
		ScopeID:     "projects/" + project,
		Location:    "global",
		Status:      st,
		Description: r.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "destination", "priority", "nextHop", "network", "instanceTags", "routeType", "warnings"),
		Created:     r.Created,
	}
	if parentID != "" {
		res.ParentID = parentID
		res.FacetID = model.FacetRoutes
	}
	return res
}

func routerResource(project string, r Router) model.Resource {
	md := map[string]string{}
	set(md, "network", r.Network)
	if r.ASN > 0 {
		md["asn"] = strconv.FormatInt(r.ASN, 10)
	}
	set(md, "advertiseMode", r.AdvertiseMode)
	md["interfaces"] = strconv.Itoa(r.Interfaces)
	md["bgpPeers"] = strconv.Itoa(len(r.Peers))
	md["natGateways"] = strconv.Itoa(len(r.NATs))
	res := model.Resource{
		ID:          regionalID(project, r.Region, "routers", r.Name),
		Name:        r.Name,
		Service:     model.ServiceRouter,
		ScopeID:     "projects/" + project,
		Location:    locationOf(r.Region),
		Description: r.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "network", "asn", "advertiseMode", "interfaces", "bgpPeers", "natGateways"),
		Created:     r.Created,
		Facets:      []model.Facet{{ID: model.FacetNATs, Label: "NAT Gateways"}},
	}
	if len(r.Peers) > 0 {
		items := make([]string, 0, len(r.Peers))
		for _, p := range r.Peers {
			line := p.Name
			if p.PeerIP != "" {
				line += "  " + p.PeerIP
			}
			if p.PeerASN > 0 {
				line += "  AS" + strconv.FormatInt(p.PeerASN, 10)
			}
			if !p.Enabled {
				line += "  (disabled)"
			}
			items = append(items, line)
		}
		res.Lists = []model.List{{ID: "peers", Label: "BGP peers", Items: items}}
	}
	return res
}

// natResource is a NAT gateway under its router. The API names NATs only
// inside their router, so the id extends the router's.
func natResource(router model.Resource, n NAT) model.Resource {
	md := map[string]string{}
	set(md, "ipAllocation", n.IPAllocation)
	set(md, "natIPs", strings.Join(n.NATIPs, ", "))
	set(md, "sourceRanges", n.SourceRanges)
	set(md, "subnetworks", strings.Join(n.Subnetworks, ", "))
	if n.MinPortsPerVM > 0 {
		md["minPortsPerVm"] = strconv.FormatInt(n.MinPortsPerVM, 10)
	}
	if n.MaxPortsPerVM > 0 {
		md["maxPortsPerVm"] = strconv.FormatInt(n.MaxPortsPerVM, 10)
	}
	md["dynamicPorts"] = onOff(n.DynamicPorts)
	md["endpointIndependentMapping"] = onOff(n.EndpointIndependent)
	if n.Logging != "" {
		md["logging"] = n.Logging
	} else {
		md["logging"] = "off"
	}
	return model.Resource{
		ID:       router.ID + "/nats/" + n.Name,
		Name:     n.Name,
		Service:  model.ServiceCloudNAT,
		ScopeID:  router.ScopeID,
		Location: router.Location,
		Metadata: md,
		Fields: model.FieldsOf(md, "ipAllocation", "natIPs", "sourceRanges", "subnetworks", "minPortsPerVm", "maxPortsPerVm",
			"dynamicPorts", "endpointIndependentMapping", "logging"),
		ParentID: router.ID,
		FacetID:  model.FacetNATs,
	}
}
