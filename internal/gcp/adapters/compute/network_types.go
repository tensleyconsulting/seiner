package compute

import (
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// Address is a reserved IP address. Region is empty for a global address.
type Address struct {
	Name         string
	Region       string
	Address      string
	AddressType  string // EXTERNAL or INTERNAL
	IPVersion    string
	NetworkTier  string
	Purpose      string
	Status       string // RESERVING, RESERVED, IN_USE
	Network      string
	Subnetwork   string
	PrefixLength int64
	Users        []string // kind/name of each resource using the address
	Description  string
	Created      time.Time
	Labels       map[string]string
}

// ForwardingRule is a load balancer frontend or a PSC endpoint. Region is
// empty for a global rule.
type ForwardingRule struct {
	Name                string
	Region              string
	IPAddress           string
	IPProtocol          string
	Ports               string // port range, port list, or "all"
	Scheme              string // load balancing scheme
	Target              string // kind/name, or a PSC target such as all-apis
	BackendService      string
	Network             string
	Subnetwork          string
	NetworkTier         string
	GlobalAccess        bool
	PSCConnectionStatus string
	PSCConnectionID     string // set on a PSC endpoint once it connects
	// TargetRef and BackendServiceRef are refs (see refOf) that resolve the
	// rule to its load balancer. Empty when the rule has neither.
	TargetRef         string
	BackendServiceRef string
	Description       string
	Created           time.Time
	Labels            map[string]string
}

// ServiceAttachment publishes a producer service over Private Service Connect.
type ServiceAttachment struct {
	Name                 string
	Region               string
	TargetService        string // kind/name of the producer forwarding rule
	ConnectionPreference string
	NATSubnets           []string
	ProxyProtocol        bool
	DomainNames          []string
	AcceptList           []string // project or network, with its connection limit
	RejectList           []string
	Endpoints            []ConnectedEndpoint
	Description          string
	Created              time.Time
}

// ConnectedEndpoint is one consumer endpoint on a service attachment.
type ConnectedEndpoint struct {
	Endpoint        string // kind/name of the consumer forwarding rule
	ConsumerNetwork string
	Status          string
}

// kindName keeps the last two segments of a self link
// (targetHttpsProxies/web-proxy), so the kind of a target stays readable.
func kindName(link string) string {
	segs := strings.Split(strings.TrimSuffix(link, "/"), "/")
	if len(segs) < 2 {
		return link
	}
	return segs[len(segs)-2] + "/" + segs[len(segs)-1]
}

// projectAndName reads a self link as project/name, so a consumer endpoint
// or network names the project it belongs to.
func projectAndName(link string) string {
	segs := strings.Split(strings.TrimSuffix(link, "/"), "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "projects" {
			return segs[i+1] + "/" + segs[len(segs)-1]
		}
	}
	return lastSeg(link)
}

// regionalID is the resource name for a global (region "") or regional item.
func regionalID(project, region, kind, name string) string {
	if region == "" {
		return "projects/" + project + "/global/" + kind + "/" + name
	}
	return "projects/" + project + "/regions/" + region + "/" + kind + "/" + name
}

func locationOf(region string) string {
	if region == "" {
		return "global"
	}
	return region
}

func addressResource(project string, a Address) model.Resource {
	md := map[string]string{}
	addr := a.Address
	if addr != "" && a.PrefixLength > 0 {
		addr += "/" + strconv.FormatInt(a.PrefixLength, 10)
	}
	set(md, "address", addr)
	set(md, "addressType", a.AddressType)
	set(md, "ipVersion", a.IPVersion)
	set(md, "networkTier", a.NetworkTier)
	set(md, "purpose", a.Purpose)
	set(md, "network", a.Network)
	set(md, "subnetwork", a.Subnetwork)
	set(md, "usedBy", strings.Join(a.Users, ", "))
	r := model.Resource{
		ID:          regionalID(project, a.Region, "addresses", a.Name),
		Name:        a.Name,
		Service:     model.ServiceAddress,
		ScopeID:     "projects/" + project,
		Location:    locationOf(a.Region),
		Status:      addressStatus(a.Status),
		State:       a.Status,
		Description: a.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "address", "addressType", "ipVersion", "networkTier", "purpose", "network", "subnetwork", "usedBy"),
		Created:     a.Created,
		Labels:      maps.Clone(a.Labels),
	}
	if len(a.Users) > 0 {
		r.Lists = []model.List{{ID: "users", Label: "Used by", Items: append([]string(nil), a.Users...)}}
	}
	return r
}

func addressStatus(s string) model.ResourceStatus {
	switch strings.ToUpper(s) {
	case "IN_USE", "RESERVED":
		return model.StatusHealthy
	case "RESERVING":
		return model.StatusWarning
	default:
		return model.StatusUnknown
	}
}

func forwardingRuleResource(project string, f ForwardingRule) model.Resource {
	md := map[string]string{}
	set(md, "ipAddress", f.IPAddress)
	set(md, "protocol", f.IPProtocol)
	set(md, "ports", f.Ports)
	set(md, "scheme", f.Scheme)
	set(md, "target", f.Target)
	set(md, "backendService", f.BackendService)
	set(md, "network", f.Network)
	set(md, "subnetwork", f.Subnetwork)
	set(md, "networkTier", f.NetworkTier)
	if f.GlobalAccess {
		md["globalAccess"] = "on"
	}
	return model.Resource{
		ID:          regionalID(project, f.Region, "forwardingRules", f.Name),
		Name:        f.Name,
		Service:     model.ServiceForwardingRule,
		ScopeID:     "projects/" + project,
		Location:    locationOf(f.Region),
		Status:      pscStatus(f.PSCConnectionStatus),
		State:       f.PSCConnectionStatus,
		Description: f.Description,
		Metadata:    md,
		Fields: model.FieldsOf(md, "ipAddress", "protocol", "ports", "scheme", "target", "backendService",
			"network", "subnetwork", "networkTier", "globalAccess"),
		Created: f.Created,
		Labels:  maps.Clone(f.Labels),
	}
}

// pscEndpoint reports whether f is a Private Service Connect endpoint: a
// rule that targets a service attachment or a Google APIs bundle, or that
// carries a PSC connection. Those list as Connected Endpoints under Private
// Service Connect, not as Forwarding Rules.
func (f ForwardingRule) pscEndpoint() bool {
	return f.PSCConnectionStatus != "" || f.PSCConnectionID != "" ||
		f.Target == "all-apis" || f.Target == "vpc-sc" || strings.HasPrefix(f.Target, "serviceAttachments/")
}

// pscEndpointResource maps a PSC endpoint. Its id is the forwarding rule's,
// so the Console and the API name it the same way.
func pscEndpointResource(project string, f ForwardingRule) model.Resource {
	md := map[string]string{}
	set(md, "ipAddress", f.IPAddress)
	target := f.Target
	if strings.HasPrefix(target, "serviceAttachments/") {
		md["targetType"] = "Published service"
	} else if target != "" {
		md["targetType"] = "Google APIs"
	}
	set(md, "target", target)
	set(md, "pscConnectionId", f.PSCConnectionID)
	set(md, "network", f.Network)
	set(md, "subnetwork", f.Subnetwork)
	if f.GlobalAccess {
		md["globalAccess"] = "on"
	}
	return model.Resource{
		ID:          regionalID(project, f.Region, "forwardingRules", f.Name),
		Name:        f.Name,
		Service:     model.ServicePSCEndpoint,
		ScopeID:     "projects/" + project,
		Location:    locationOf(f.Region),
		Status:      pscStatus(f.PSCConnectionStatus),
		State:       f.PSCConnectionStatus,
		Description: f.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "ipAddress", "targetType", "target", "pscConnectionId", "network", "subnetwork", "globalAccess"),
		Created:     f.Created,
		Labels:      maps.Clone(f.Labels),
	}
}

// pscStatus maps a PSC connection status onto the UI bucket. A rule that is
// not a PSC endpoint has no status.
func pscStatus(s string) model.ResourceStatus {
	switch strings.ToUpper(s) {
	case "ACCEPTED":
		return model.StatusHealthy
	case "PENDING", "NEEDS_ATTENTION":
		return model.StatusWarning
	case "REJECTED", "CLOSED":
		return model.StatusError
	default:
		return model.StatusUnknown
	}
}

func serviceAttachmentResource(project string, s ServiceAttachment) model.Resource {
	md := map[string]string{}
	set(md, "targetService", s.TargetService)
	set(md, "connectionPreference", s.ConnectionPreference)
	set(md, "natSubnets", strings.Join(s.NATSubnets, ", "))
	md["proxyProtocol"] = onOff(s.ProxyProtocol)
	set(md, "domainNames", strings.Join(s.DomainNames, ", "))
	md["endpoints"] = strconv.Itoa(len(s.Endpoints))
	// The attachment has no state of its own: it is healthy once an
	// endpoint is accepted, and a warning while any is pending.
	var st model.ResourceStatus
	var endpoints []string
	for _, e := range s.Endpoints {
		line := e.Endpoint
		if e.ConsumerNetwork != "" {
			line += " (" + e.ConsumerNetwork + ")"
		}
		if e.Status != "" {
			line += " " + e.Status
		}
		endpoints = append(endpoints, line)
		switch es := pscStatus(e.Status); {
		case es == model.StatusWarning:
			st = es
		case es == model.StatusHealthy && st == "":
			st = es
		}
	}
	var lists []model.List
	if len(endpoints) > 0 {
		lists = append(lists, model.List{ID: "endpoints", Label: "Connected endpoints", Items: endpoints})
	}
	if len(s.AcceptList) > 0 {
		lists = append(lists, model.List{ID: "accept", Label: "Accepted consumers", Items: append([]string(nil), s.AcceptList...)})
	}
	if len(s.RejectList) > 0 {
		lists = append(lists, model.List{ID: "reject", Label: "Rejected consumers", Items: append([]string(nil), s.RejectList...)})
	}
	return model.Resource{
		ID:          regionalID(project, s.Region, "serviceAttachments", s.Name),
		Name:        s.Name,
		Service:     model.ServiceServiceAttachment,
		ScopeID:     "projects/" + project,
		Location:    locationOf(s.Region),
		Status:      st,
		Description: s.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "targetService", "connectionPreference", "endpoints", "natSubnets", "proxyProtocol", "domainNames"),
		Lists:       lists,
		Created:     s.Created,
		Facets:      permissionsFacet,
	}
}
