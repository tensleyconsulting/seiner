package compute

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// Network is the Google-free VPC network slice.
type Network struct {
	Name        string
	ID          uint64
	Description string
	AutoCreate  bool
	RoutingMode string
	MTU         int64
	Subnets     int
	Peerings    []Peering
	Created     time.Time
}

// Peering is one VPC network peering.
type Peering struct {
	Name  string
	State string
}

// Subnet is a regional subnetwork.
type Subnet struct {
	Name                string
	Region              string
	Network             string
	CIDR                string
	Gateway             string
	PrivateGoogleAccess bool
	Description         string
	Purpose             string
	StackType           string
	State               string
	SecondaryRanges     []SecondaryRange
	FlowLogs            bool
	Created             time.Time
}

// SecondaryRange is a named alias IP range on a subnet.
type SecondaryRange struct {
	Name string
	CIDR string
}

// Firewall is a VPC firewall rule.
type Firewall struct {
	Name                  string
	Network               string
	Direction             string
	Priority              int64
	Disabled              bool
	Logging               bool
	SourceRanges          []string
	SourceTags            []string
	DestinationRanges     []string
	Allowed               string
	Denied                string
	TargetTags            []string
	TargetServiceAccounts []string
	SourceServiceAccounts []string
	Description           string
	Created               time.Time
}

// Instance is a Compute Engine VM.
type Instance struct {
	Name               string
	Zone               string
	Status             string
	MachineType        string
	CPUPlatform        string
	Network            string
	Subnetwork         string
	InternalIP         string
	ExternalIP         string
	Interfaces         int
	Disks              int
	BootDiskGB         int64
	ServiceAccount     string
	Tags               []string
	ProvisioningModel  string
	DeletionProtection bool
	LastStarted        time.Time
	LastStopped        time.Time
	Description        string
	Created            time.Time
	Labels             map[string]string

	SecureBoot        bool
	VTPM              bool
	Integrity         bool   // integrity monitoring
	Confidential      string // confidential instance type (SEV, SEV_SNP, TDX), or "on"
	Accelerators      []string
	IPForwarding      bool
	Scopes            []string // the service account's OAuth scopes, short form
	OSLogin           string   // the enable-oslogin metadata value, lowercased; "" when unset
	OnHostMaintenance string   // MIGRATE or TERMINATE
	AutoRestart       *bool
	TerminationAction string // STOP or DELETE, for Spot and time-limited VMs
}

func lastSeg(link string) string {
	link = strings.TrimSuffix(link, "/")
	if i := strings.LastIndexByte(link, '/'); i >= 0 && i+1 < len(link) {
		return link[i+1:]
	}
	return link
}

func networkResource(project string, n Network) model.Resource {
	md := map[string]string{}
	if n.AutoCreate {
		md["subnetMode"] = "auto"
	} else {
		md["subnetMode"] = "custom"
	}
	if n.RoutingMode != "" {
		md["routingMode"] = n.RoutingMode
	}
	if n.MTU > 0 {
		md["mtu"] = strconv.FormatInt(n.MTU, 10)
	}
	md["subnets"] = strconv.Itoa(n.Subnets)
	if len(n.Peerings) > 0 {
		ps := make([]string, 0, len(n.Peerings))
		for _, p := range n.Peerings {
			if p.State != "" {
				ps = append(ps, p.Name+" ("+p.State+")")
			} else {
				ps = append(ps, p.Name)
			}
		}
		md["peerings"] = strings.Join(ps, ", ")
	}
	if n.ID != 0 {
		md["networkId"] = strconv.FormatUint(n.ID, 10)
	}
	return model.Resource{
		ID:          "projects/" + project + "/global/networks/" + n.Name,
		Name:        n.Name,
		Service:     model.ServiceVPC,
		ScopeID:     "projects/" + project,
		Location:    "global",
		Description: n.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "subnetMode", "subnets", "routingMode", "mtu", "peerings", "networkId"),
		Created:     n.Created,
		Facets: []model.Facet{
			{ID: model.FacetSubnets, Label: "Subnets"},
			{ID: model.FacetFirewalls, Label: "Firewalls"},
			{ID: model.FacetRoutes, Label: "Routes"},
		},
	}
}

func subnetResource(project string, s Subnet, parentID string) model.Resource {
	md := map[string]string{}
	set(md, "cidr", s.CIDR)
	set(md, "gateway", s.Gateway)
	set(md, "network", s.Network)
	if len(s.SecondaryRanges) > 0 {
		rs := make([]string, 0, len(s.SecondaryRanges))
		for _, r := range s.SecondaryRanges {
			rs = append(rs, r.Name+"="+r.CIDR)
		}
		md["secondaryRanges"] = strings.Join(rs, ", ")
	}
	set(md, "purpose", s.Purpose)
	set(md, "stackType", s.StackType)
	md["privateGoogleAccess"] = onOff(s.PrivateGoogleAccess)
	md["flowLogs"] = onOff(s.FlowLogs)
	var st model.ResourceStatus
	switch s.State {
	case "":
	case "READY":
		st = model.StatusHealthy
	default:
		st = model.StatusWarning
	}
	r := model.Resource{
		ID:          "projects/" + project + "/regions/" + s.Region + "/subnetworks/" + s.Name,
		Name:        s.Name,
		Service:     model.ServiceSubnet,
		ScopeID:     "projects/" + project,
		Location:    s.Region,
		Status:      st,
		State:       s.State,
		Description: s.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "cidr", "gateway", "network", "secondaryRanges", "purpose", "stackType", "privateGoogleAccess", "flowLogs"),
		Created:     s.Created,
		Facets:      permissionsFacet,
	}
	if parentID != "" {
		r.ParentID = parentID
		r.FacetID = model.FacetSubnets
	}
	return r
}

func firewallResource(project string, f Firewall, parentID string) model.Resource {
	md := map[string]string{}
	set(md, "network", f.Network)
	set(md, "direction", f.Direction)
	if f.Priority > 0 {
		md["priority"] = strconv.FormatInt(f.Priority, 10)
	}
	set(md, "allowed", f.Allowed)
	set(md, "denied", f.Denied)
	if f.Disabled {
		md["disabled"] = "true"
	}
	md["logging"] = onOff(f.Logging)
	set(md, "sourceRanges", strings.Join(f.SourceRanges, ","))
	set(md, "sourceTags", strings.Join(f.SourceTags, ","))
	set(md, "destinationRanges", strings.Join(f.DestinationRanges, ","))
	set(md, "targetTags", strings.Join(f.TargetTags, ","))
	set(md, "targetServiceAccounts", strings.Join(f.TargetServiceAccounts, ","))
	set(md, "sourceServiceAccounts", strings.Join(f.SourceServiceAccounts, ","))
	var st model.ResourceStatus
	if f.Disabled {
		st = model.StatusWarning
	}
	r := model.Resource{
		ID:          "projects/" + project + "/global/firewalls/" + f.Name,
		Name:        f.Name,
		Service:     model.ServiceFirewall,
		ScopeID:     "projects/" + project,
		Location:    "global",
		Status:      st,
		Description: f.Description,
		Metadata:    md,
		Fields: model.FieldsOf(md, "direction", "priority", "network", "allowed", "denied",
			"sourceRanges", "sourceTags", "sourceServiceAccounts", "destinationRanges", "targetTags", "targetServiceAccounts", "logging", "disabled"),
		Created: f.Created,
	}
	if parentID != "" {
		r.ParentID = parentID
		r.FacetID = model.FacetFirewalls
	}
	return r
}

func instanceResource(project string, inst Instance) model.Resource {
	md := map[string]string{}
	set(md, "machineType", inst.MachineType)
	set(md, "cpuPlatform", inst.CPUPlatform)
	set(md, "internalIP", inst.InternalIP)
	set(md, "externalIP", inst.ExternalIP)
	set(md, "network", inst.Network)
	set(md, "subnetwork", inst.Subnetwork)
	if inst.Interfaces > 1 {
		md["interfaces"] = strconv.Itoa(inst.Interfaces)
	}
	if inst.Disks > 0 {
		d := strconv.Itoa(inst.Disks)
		if inst.BootDiskGB > 0 {
			d += fmt.Sprintf(" (boot %d GB)", inst.BootDiskGB)
		}
		md["disks"] = d
	}
	set(md, "serviceAccount", inst.ServiceAccount)
	set(md, "networkTags", strings.Join(inst.Tags, ","))
	set(md, "provisioning", inst.ProvisioningModel)
	if inst.DeletionProtection {
		md["deletionProtection"] = "on"
	}
	if !inst.LastStarted.IsZero() {
		md["lastStarted"] = inst.LastStarted.UTC().Format("2006-01-02 15:04 UTC")
	}
	if !inst.LastStopped.IsZero() {
		md["lastStopped"] = inst.LastStopped.UTC().Format("2006-01-02 15:04 UTC")
	}
	set(md, "accelerators", strings.Join(inst.Accelerators, ", "))
	var shielded []string
	if inst.SecureBoot {
		shielded = append(shielded, "secure boot")
	}
	if inst.VTPM {
		shielded = append(shielded, "vTPM")
	}
	if inst.Integrity {
		shielded = append(shielded, "integrity monitoring")
	}
	set(md, "shieldedVM", strings.Join(shielded, ", "))
	set(md, "confidentialVM", inst.Confidential)
	if inst.IPForwarding {
		md["ipForwarding"] = "on"
	}
	set(md, "osLogin", inst.OSLogin)
	set(md, "maintenance", hostMaintenance(inst))
	var lists []model.List
	if len(inst.Scopes) > 0 {
		md["scopes"] = strconv.Itoa(len(inst.Scopes))
		lists = []model.List{{ID: "scopes", Label: "Access scopes", Items: slices.Clone(inst.Scopes)}}
	}
	return model.Resource{
		ID:          "projects/" + project + "/zones/" + inst.Zone + "/instances/" + inst.Name,
		Name:        inst.Name,
		Service:     model.ServiceComputeEngine,
		ScopeID:     "projects/" + project,
		Location:    inst.Zone,
		Status:      instanceStatus(inst.Status),
		State:       inst.Status,
		Description: inst.Description,
		Metadata:    md,
		Fields: model.FieldsOf(md, "machineType", "cpuPlatform", "accelerators", "internalIP", "externalIP", "network", "subnetwork",
			"interfaces", "ipForwarding", "disks", "serviceAccount", "scopes", "networkTags", "provisioning", "maintenance",
			"shieldedVM", "confidentialVM", "osLogin", "deletionProtection", "lastStarted", "lastStopped"),
		Lists:   lists,
		Created: inst.Created,
		Labels:  maps.Clone(inst.Labels),
		Facets:  permissionsFacet,
	}
}

// hostMaintenance reads "MIGRATE, auto restart" or "TERMINATE, STOP on
// preemption"; "" when the API says nothing.
func hostMaintenance(inst Instance) string {
	parts := []string{}
	if inst.OnHostMaintenance != "" {
		parts = append(parts, inst.OnHostMaintenance)
	}
	if inst.AutoRestart != nil {
		if *inst.AutoRestart {
			parts = append(parts, "auto restart")
		} else {
			parts = append(parts, "no auto restart")
		}
	}
	if inst.TerminationAction != "" && inst.TerminationAction != "INSTANCE_TERMINATION_ACTION_UNSPECIFIED" {
		parts = append(parts, inst.TerminationAction+" on preemption")
	}
	return strings.Join(parts, ", ")
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

func instanceStatus(s string) model.ResourceStatus {
	switch strings.ToUpper(s) {
	case "RUNNING":
		return model.StatusHealthy
	case "STAGING", "PROVISIONING", "STOPPING", "SUSPENDING", "REPAIRING":
		return model.StatusWarning
	case "TERMINATED", "STOPPED", "SUSPENDED":
		return model.StatusWarning
	default:
		return model.StatusUnknown
	}
}

func joinAllowed(protocol string, ports []string) string {
	if len(ports) == 0 {
		return protocol
	}
	return fmt.Sprintf("%s:%s", protocol, strings.Join(ports, ","))
}
