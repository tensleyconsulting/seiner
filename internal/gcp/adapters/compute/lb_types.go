package compute

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// Refs name a compute resource across global and regional collections as
// location/kind/name (global/urlMaps/web, us-central1/backendServices/api),
// so a regional and a global resource of the same name stay apart.

// refOf reads a self link or relative name as a ref. It returns "" for a
// link that names no global or regional resource (all-apis, a zonal group).
func refOf(link string) string {
	segs := strings.Split(strings.TrimSuffix(link, "/"), "/")
	for i, s := range segs {
		switch {
		case s == "global" && i+2 < len(segs):
			return "global/" + segs[i+1] + "/" + segs[i+2]
		case s == "regions" && i+3 < len(segs):
			return segs[i+1] + "/" + segs[i+2] + "/" + segs[i+3]
		}
	}
	return ""
}

// refFor is the ref of a resource of kind in region ("" for global).
func refFor(region, kind, name string) string {
	return locationOf(region) + "/" + kind + "/" + name
}

// refName is the name in a ref.
func refName(ref string) string { return lastSeg(ref) }

// URLMap routes an application load balancer's requests to its backends.
type URLMap struct {
	Name        string
	Region      string
	ServiceRefs []string // backend services and buckets it routes to, default first
	Hosts       []string
	Description string
	Created     time.Time
}

// BackendService is a load balancer backend.
type BackendService struct {
	Name               string
	Region             string
	Protocol           string
	Scheme             string
	PortName           string
	Backends           []Backend
	HealthCheckRefs    []string
	SecurityPolicy     string // Cloud Armor backend policy ref
	EdgeSecurityPolicy string // Cloud Armor edge policy ref
	CDN                bool
	SessionAffinity    string
	TimeoutSec         int64
	Logging            bool
	Description        string
	Created            time.Time
	IAP                bool
	CDNCacheMode       string  // CACHE_ALL_STATIC, USE_ORIGIN_HEADERS, FORCE_CACHE_ALL
	DrainingSec        int64   // connection draining timeout
	LogSampleRate      float64 // 0..1, when logging is on
}

// Backend is one instance group or NEG behind a backend service.
type Backend struct {
	Group         string // kind/name
	Location      string // zone or region of the group
	BalancingMode string
	Capacity      float64 // capacity scaler, 0..1
}

// TargetProxy sits between a forwarding rule and a URL map or backend service.
type TargetProxy struct {
	Kind           string // targetHttpsProxies, targetTcpProxies, ...
	Name           string
	Region         string
	URLMapRef      string
	ServiceRef     string
	Certificates   []string
	SSLPolicy      string
	CertificateMap string // Certificate Manager map, on HTTPS proxies
}

// SSLPolicy sets the TLS versions and ciphers a load balancer's target
// proxies negotiate. Region is empty for a global policy.
type SSLPolicy struct {
	Name           string
	Region         string
	Profile        string // COMPATIBLE, MODERN, RESTRICTED, FIPS_202205, CUSTOM
	MinTLSVersion  string // TLS_1_0, TLS_1_1, TLS_1_2, TLS_1_3
	CustomFeatures []string
	Warnings       []string
	Description    string
	Created        time.Time
}

// HealthCheck probes a backend service's backends.
type HealthCheck struct {
	Name               string
	Region             string
	Type               string // HTTP, HTTPS, TCP, ...
	Port               string // number or named port
	RequestPath        string
	IntervalSec        int64
	TimeoutSec         int64
	HealthyThreshold   int64
	UnhealthyThreshold int64
	Logging            bool
	Description        string
	Created            time.Time
}

// SecurityPolicy is a Cloud Armor policy.
type SecurityPolicy struct {
	Name               string
	Region             string
	Type               string // CLOUD_ARMOR, CLOUD_ARMOR_EDGE, CLOUD_ARMOR_NETWORK
	Rules              []SecurityRule
	AdaptiveProtection bool
	Description        string
	Created            time.Time
	Labels             map[string]string
}

// SecurityRule is one Cloud Armor rule.
type SecurityRule struct {
	Priority    int64
	Action      string
	Match       string // CEL expression or source ranges
	Description string
	Preview     bool
}

// defaultRulePriority is the priority of a Cloud Armor policy's default rule.
const defaultRulePriority = 2147483647

// lbInputs is what assembling load balancers needs from the API.
type lbInputs struct {
	maps     []URLMap
	services []BackendService
	proxies  []TargetProxy
	rules    []ForwardingRule
}

// loadBalancer is one load balancer as the Console lists it: a URL map, or
// a backend service that forwarding rules reach without one.
type loadBalancer struct {
	Ref       string
	Name      string
	Region    string
	URLMap    *URLMap
	Frontends []ForwardingRule
	Proxies   []TargetProxy
	Backends  []BackendService
	Others    []string // routed-to refs that are not backend services (buckets)
}

// buildLoadBalancers resolves each forwarding rule through its target proxy
// to a URL map or backend service and groups the rules under it. A backend
// service a URL map routes to is that map's backend, not its own load
// balancer. A backend service no rule reaches is not a load balancer.
func buildLoadBalancers(in lbInputs) []loadBalancer {
	services := map[string]BackendService{}
	for _, b := range in.services {
		services[refFor(b.Region, "backendServices", b.Name)] = b
	}
	proxies := map[string]TargetProxy{}
	for _, p := range in.proxies {
		proxies[refFor(p.Region, p.Kind, p.Name)] = p
	}
	frontends := map[string][]ForwardingRule{}
	viaProxy := map[string][]TargetProxy{}
	for _, r := range in.rules {
		if r.BackendServiceRef != "" {
			frontends[r.BackendServiceRef] = append(frontends[r.BackendServiceRef], r)
			continue
		}
		p, ok := proxies[r.TargetRef]
		if !ok {
			continue // target pool, PSC endpoint, VPN, ...
		}
		root := p.URLMapRef
		if root == "" {
			root = p.ServiceRef
		}
		if root == "" {
			continue
		}
		frontends[root] = append(frontends[root], r)
		if !slices.ContainsFunc(viaProxy[root], func(q TargetProxy) bool { return q.Kind == p.Kind && q.Name == p.Name && q.Region == p.Region }) {
			viaProxy[root] = append(viaProxy[root], p)
		}
	}

	var out []loadBalancer
	claimed := map[string]bool{}
	for _, m := range in.maps {
		ref := refFor(m.Region, "urlMaps", m.Name)
		lb := loadBalancer{Ref: ref, Name: m.Name, Region: m.Region, URLMap: &m, Frontends: frontends[ref], Proxies: viaProxy[ref]}
		for _, s := range m.ServiceRefs {
			claimed[s] = true
			if b, ok := services[s]; ok {
				lb.Backends = append(lb.Backends, b)
			} else {
				lb.Others = append(lb.Others, s)
			}
		}
		out = append(out, lb)
	}
	for _, b := range in.services {
		ref := refFor(b.Region, "backendServices", b.Name)
		if claimed[ref] || len(frontends[ref]) == 0 {
			continue
		}
		out = append(out, loadBalancer{Ref: ref, Name: b.Name, Region: b.Region, Frontends: frontends[ref], Proxies: viaProxy[ref], Backends: []BackendService{b}})
	}
	slices.SortStableFunc(out, func(a, b loadBalancer) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// kindLabel is the Console's load balancer type: Application, Network
// proxy, or Network passthrough, with whether it is external or internal.
func (lb loadBalancer) kindLabel() string {
	kind := "Network passthrough"
	switch {
	case lb.URLMap != nil:
		kind = "Application"
	case len(lb.Proxies) > 0:
		kind = "Network proxy"
	}
	scheme := ""
	if len(lb.Frontends) > 0 {
		scheme = lb.Frontends[0].Scheme
	}
	if scheme == "" && len(lb.Backends) > 0 {
		scheme = lb.Backends[0].Scheme
	}
	switch strings.ToUpper(scheme) {
	case "EXTERNAL_MANAGED":
		return kind + " (external)"
	case "EXTERNAL":
		if lb.URLMap != nil {
			return kind + " (classic)"
		}
		return kind + " (external)"
	case "INTERNAL", "INTERNAL_MANAGED", "INTERNAL_SELF_MANAGED":
		return kind + " (internal)"
	}
	return kind
}

func (lb loadBalancer) securityPolicies() []string {
	var out []string
	for _, b := range lb.Backends {
		for _, ref := range []string{b.SecurityPolicy, b.EdgeSecurityPolicy} {
			if ref != "" && !slices.Contains(out, refName(ref)) {
				out = append(out, refName(ref))
			}
		}
	}
	return out
}

func (lb loadBalancer) healthCheckRefs() []string {
	var out []string
	for _, b := range lb.Backends {
		for _, ref := range b.HealthCheckRefs {
			if !slices.Contains(out, ref) {
				out = append(out, ref)
			}
		}
	}
	return out
}

// lbID is the load balancer's id: its URL map's or backend service's name.
func lbID(project string, lb loadBalancer) string {
	kind := "backendServices"
	if lb.URLMap != nil {
		kind = "urlMaps"
	}
	return regionalID(project, lb.Region, kind, lb.Name)
}

func loadBalancerResource(project string, lb loadBalancer) model.Resource {
	md := map[string]string{}
	md["type"] = lb.kindLabel()
	var fes []string
	for _, f := range lb.Frontends {
		fes = append(fes, frontendSummary(f))
	}
	set(md, "frontends", strings.Join(fes, ", "))
	var bes []string
	for _, b := range lb.Backends {
		bes = append(bes, b.Name)
	}
	for _, o := range lb.Others {
		bes = append(bes, kindName(o))
	}
	set(md, "backends", strings.Join(bes, ", "))
	if lb.URLMap != nil {
		set(md, "hosts", strings.Join(lb.URLMap.Hosts, ", "))
	}
	var certs, ssl, certMaps []string
	for _, p := range lb.Proxies {
		if p.CertificateMap != "" && !slices.Contains(certMaps, p.CertificateMap) {
			certMaps = append(certMaps, p.CertificateMap)
		}
		for _, c := range p.Certificates {
			if !slices.Contains(certs, c) {
				certs = append(certs, c)
			}
		}
		if p.SSLPolicy != "" && !slices.Contains(ssl, p.SSLPolicy) {
			ssl = append(ssl, p.SSLPolicy)
		}
	}
	set(md, "certificates", strings.Join(certs, ", "))
	set(md, "certificateMap", strings.Join(certMaps, ", "))
	set(md, "sslPolicy", strings.Join(ssl, ", "))
	set(md, "cloudArmor", strings.Join(lb.securityPolicies(), ", "))
	if slices.ContainsFunc(lb.Backends, func(b BackendService) bool { return b.CDN }) {
		md["cdn"] = "on"
	}
	var st model.ResourceStatus
	if len(lb.Frontends) == 0 {
		st = model.StatusWarning
		md["frontends"] = "none"
	}
	r := model.Resource{
		ID:       lbID(project, lb),
		Name:     lb.Name,
		Service:  model.ServiceLoadBalancer,
		ScopeID:  "projects/" + project,
		Location: locationOf(lb.Region),
		Status:   st,
		Metadata: md,
		Fields:   model.FieldsOf(md, "type", "frontends", "backends", "hosts", "certificates", "certificateMap", "sslPolicy", "cloudArmor", "cdn"),
		Facets: []model.Facet{
			{ID: model.FacetFrontends, Label: "Frontends"},
			{ID: model.FacetBackends, Label: "Backends"},
			{ID: model.FacetHealthChecks, Label: "Health Checks"},
		},
	}
	if lb.URLMap != nil {
		r.Description = lb.URLMap.Description
		r.Created = lb.URLMap.Created
	} else if len(lb.Backends) > 0 {
		r.Description = lb.Backends[0].Description
		r.Created = lb.Backends[0].Created
	}
	return r
}

// frontendSummary is a rule's address and ports (203.0.113.20:443).
func frontendSummary(f ForwardingRule) string {
	ports := f.Ports
	if lo, hi, ok := strings.Cut(ports, "-"); ok && lo == hi {
		ports = lo
	}
	if ports == "" {
		return f.IPAddress
	}
	return f.IPAddress + ":" + ports
}

func backendServiceResource(project string, b BackendService, parentID string) model.Resource {
	md := map[string]string{}
	set(md, "protocol", b.Protocol)
	set(md, "scheme", b.Scheme)
	set(md, "portName", b.PortName)
	md["backends"] = strconv.Itoa(len(b.Backends))
	var hcs []string
	for _, ref := range b.HealthCheckRefs {
		hcs = append(hcs, refName(ref))
	}
	set(md, "healthChecks", strings.Join(hcs, ", "))
	if b.SecurityPolicy != "" {
		md["cloudArmor"] = refName(b.SecurityPolicy)
	}
	if b.EdgeSecurityPolicy != "" {
		md["edgeSecurityPolicy"] = refName(b.EdgeSecurityPolicy)
	}
	md["cdn"] = onOff(b.CDN)
	if b.CDN && b.CDNCacheMode != "" {
		md["cdn"] = "on, " + b.CDNCacheMode
	}
	md["iap"] = onOff(b.IAP)
	set(md, "sessionAffinity", b.SessionAffinity)
	if b.TimeoutSec > 0 {
		md["timeout"] = strconv.FormatInt(b.TimeoutSec, 10) + "s"
	}
	if b.DrainingSec > 0 {
		md["connectionDraining"] = strconv.FormatInt(b.DrainingSec, 10) + "s"
	}
	md["logging"] = onOff(b.Logging)
	if b.Logging && b.LogSampleRate > 0 && b.LogSampleRate < 1 {
		md["logging"] = fmt.Sprintf("on, %g%% sampled", b.LogSampleRate*100)
	}
	r := model.Resource{
		ID:          regionalID(project, b.Region, "backendServices", b.Name),
		Name:        b.Name,
		Service:     model.ServiceBackendService,
		ScopeID:     "projects/" + project,
		Location:    locationOf(b.Region),
		Description: b.Description,
		Metadata:    md,
		Fields: model.FieldsOf(md, "protocol", "scheme", "portName", "backends", "healthChecks", "cloudArmor",
			"edgeSecurityPolicy", "iap", "cdn", "sessionAffinity", "timeout", "connectionDraining", "logging"),
		Created:  b.Created,
		ParentID: parentID,
	}
	if parentID != "" {
		r.FacetID = model.FacetBackends
	}
	if len(b.Backends) > 0 {
		items := make([]string, 0, len(b.Backends))
		for _, be := range b.Backends {
			line := be.Group
			if be.Location != "" {
				line += " (" + be.Location + ")"
			}
			if be.BalancingMode != "" {
				line += " " + be.BalancingMode
			}
			if be.Capacity > 0 {
				line += fmt.Sprintf(" %g%%", be.Capacity*100)
			}
			items = append(items, line)
		}
		r.Lists = []model.List{{ID: "backends", Label: "Backends", Items: items}}
	}
	return r
}

func healthCheckResource(project string, h HealthCheck, parentID string) model.Resource {
	md := map[string]string{}
	set(md, "type", h.Type)
	set(md, "port", h.Port)
	set(md, "requestPath", h.RequestPath)
	if h.IntervalSec > 0 {
		md["interval"] = strconv.FormatInt(h.IntervalSec, 10) + "s"
	}
	if h.TimeoutSec > 0 {
		md["timeout"] = strconv.FormatInt(h.TimeoutSec, 10) + "s"
	}
	if h.HealthyThreshold > 0 || h.UnhealthyThreshold > 0 {
		md["thresholds"] = fmt.Sprintf("healthy %d, unhealthy %d", h.HealthyThreshold, h.UnhealthyThreshold)
	}
	md["logging"] = onOff(h.Logging)
	r := model.Resource{
		ID:          regionalID(project, h.Region, "healthChecks", h.Name),
		Name:        h.Name,
		Service:     model.ServiceHealthCheck,
		ScopeID:     "projects/" + project,
		Location:    locationOf(h.Region),
		Description: h.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "type", "port", "requestPath", "interval", "timeout", "thresholds", "logging"),
		Created:     h.Created,
		ParentID:    parentID,
	}
	if parentID != "" {
		r.FacetID = model.FacetHealthChecks
	}
	return r
}

// securityPolicyResource maps a Cloud Armor policy. usedBy names the backend
// services it is attached to.
func securityPolicyResource(project string, p SecurityPolicy, usedBy []string) model.Resource {
	md := map[string]string{}
	set(md, "type", p.Type)
	md["rules"] = strconv.Itoa(len(p.Rules))
	md["adaptiveProtection"] = onOff(p.AdaptiveProtection)
	set(md, "usedBy", strings.Join(usedBy, ", "))
	rules := slices.Clone(p.Rules)
	slices.SortStableFunc(rules, func(a, b SecurityRule) int { return cmp.Compare(a.Priority, b.Priority) })
	var lines []string
	for _, r := range rules {
		prio := strconv.FormatInt(r.Priority, 10)
		if r.Priority == defaultRulePriority {
			prio = "default"
		}
		line := prio + "  " + r.Action
		if r.Match != "" {
			line += "  " + r.Match
		}
		if r.Preview {
			line += "  (preview)"
		}
		lines = append(lines, line)
	}
	res := model.Resource{
		ID:          regionalID(project, p.Region, "securityPolicies", p.Name),
		Name:        p.Name,
		Service:     model.ServiceCloudArmor,
		ScopeID:     "projects/" + project,
		Location:    locationOf(p.Region),
		Description: p.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "type", "rules", "adaptiveProtection", "usedBy"),
		Created:     p.Created,
		Labels:      maps.Clone(p.Labels),
	}
	if len(lines) > 0 {
		res.Lists = []model.List{{ID: "rules", Label: "Rules", Items: lines}}
	}
	return res
}

// sslPolicyResource maps an SSL policy. usedBy lists the target proxies
// that apply it (kind/name).
func sslPolicyResource(project string, p SSLPolicy, usedBy []string) model.Resource {
	md := map[string]string{}
	set(md, "profile", p.Profile)
	set(md, "minTlsVersion", p.MinTLSVersion)
	md["targets"] = strconv.Itoa(len(usedBy))
	var lists []model.List
	if len(usedBy) > 0 {
		lists = append(lists, model.List{ID: "targets", Label: "Used by", Items: usedBy})
	}
	if len(p.CustomFeatures) > 0 {
		lists = append(lists, model.List{ID: "features", Label: "Custom features", Items: slices.Clone(p.CustomFeatures)})
	}
	var st model.ResourceStatus
	if len(p.Warnings) > 0 {
		st = model.StatusWarning
		lists = append(lists, model.List{ID: "warnings", Label: "Warnings", Items: slices.Clone(p.Warnings)})
	}
	return model.Resource{
		ID:          regionalID(project, p.Region, "sslPolicies", p.Name),
		Name:        p.Name,
		Service:     model.ServiceSSLPolicy,
		ScopeID:     "projects/" + project,
		Location:    locationOf(p.Region),
		Status:      st,
		Description: p.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "profile", "minTlsVersion", "targets"),
		Lists:       lists,
		Created:     p.Created,
	}
}

// sslPolicyUsers maps each policy, by region and name, to the target
// proxies that apply it.
func sslPolicyUsers(proxies []TargetProxy) map[[2]string][]string {
	out := map[[2]string][]string{}
	for _, p := range proxies {
		if p.SSLPolicy == "" {
			continue
		}
		k := [2]string{p.Region, p.SSLPolicy}
		out[k] = append(out[k], p.Kind+"/"+p.Name)
	}
	return out
}
