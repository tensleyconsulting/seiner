package compute

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	gce "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/display"
	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/model"
)

type liveAPI struct {
	svc *gce.Service
}

// NewLazy returns an API whose Compute Engine client is constructed on first
// use rather than at startup. A construction failure is returned from that
// first call instead of blocking Open or silently dropping the service.
// Callers share one NewLazy result across every compute provider so the
// client is built at most once regardless of which provider is read first.
func NewLazy(opts ...option.ClientOption) API {
	l := lazy.New(func(ctx context.Context) (*gce.Service, error) {
		return gce.NewService(ctx, opts...)
	})
	return lazyAPI{l: l}
}

type lazyAPI struct {
	l *lazy.Value[*gce.Service]
}

func (a lazyAPI) ListNetworks(ctx context.Context, project string) ([]Network, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListNetworks(ctx, project)
}

func (a lazyAPI) ListSubnets(ctx context.Context, project string) ([]Subnet, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListSubnets(ctx, project)
}

func (a lazyAPI) ListFirewalls(ctx context.Context, project string) ([]Firewall, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListFirewalls(ctx, project)
}

func (a lazyAPI) ListInstances(ctx context.Context, project string) ([]Instance, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListInstances(ctx, project)
}

func (a lazyAPI) ListAddresses(ctx context.Context, project string) ([]Address, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListAddresses(ctx, project)
}

func (a lazyAPI) ListForwardingRules(ctx context.Context, project string) ([]ForwardingRule, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListForwardingRules(ctx, project)
}

func (a lazyAPI) ListServiceAttachments(ctx context.Context, project string) ([]ServiceAttachment, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListServiceAttachments(ctx, project)
}

func (a lazyAPI) ListURLMaps(ctx context.Context, project string) ([]URLMap, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListURLMaps(ctx, project)
}

func (a lazyAPI) ListBackendServices(ctx context.Context, project string) ([]BackendService, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListBackendServices(ctx, project)
}

func (a lazyAPI) ListTargetProxies(ctx context.Context, project string) ([]TargetProxy, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListTargetProxies(ctx, project)
}

func (a lazyAPI) ListHealthChecks(ctx context.Context, project string) ([]HealthCheck, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListHealthChecks(ctx, project)
}

func (a lazyAPI) ListSecurityPolicies(ctx context.Context, project string) ([]SecurityPolicy, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListSecurityPolicies(ctx, project)
}

func (a lazyAPI) ListSSLPolicies(ctx context.Context, project string) ([]SSLPolicy, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListSSLPolicies(ctx, project)
}

func (a lazyAPI) ListDisks(ctx context.Context, project string) ([]Disk, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListDisks(ctx, project)
}

func (a lazyAPI) ListInstanceTemplates(ctx context.Context, project string) ([]InstanceTemplate, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListInstanceTemplates(ctx, project)
}

func (a lazyAPI) ListInstanceGroups(ctx context.Context, project string) ([]InstanceGroup, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListInstanceGroups(ctx, project)
}

func (a lazyAPI) ListRoutes(ctx context.Context, project string) ([]Route, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListRoutes(ctx, project)
}

func (a lazyAPI) ListRouters(ctx context.Context, project string) ([]Router, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListRouters(ctx, project)
}

func (a lazyAPI) GetRouter(ctx context.Context, project, region, name string) (Router, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return Router{}, err
	}
	return liveAPI{svc: svc}.GetRouter(ctx, project, region, name)
}

func (a lazyAPI) GetIAMPolicy(ctx context.Context, project, kind, zone, region, name string) ([]Binding, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.GetIAMPolicy(ctx, project, kind, zone, region, name)
}

func (a lazyAPI) ListManagedInstances(ctx context.Context, project, zone, region, group string, limit int) ([]ManagedInstance, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{svc: svc}.ListManagedInstances(ctx, project, zone, region, group, limit)
}

var _ API = lazyAPI{}

func (a liveAPI) ListNetworks(ctx context.Context, project string) ([]Network, error) {
	var out []Network
	err := a.svc.Networks.List(project).Context(ctx).Pages(ctx, func(page *gce.NetworkList) error {
		for _, n := range page.Items {
			out = append(out, fromNetwork(n))
		}
		return nil
	})
	return out, err
}

func (a liveAPI) ListSubnets(ctx context.Context, project string) ([]Subnet, error) {
	var out []Subnet
	err := a.svc.Subnetworks.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.SubnetworkAggregatedList) error {
		for _, scoped := range page.Items {
			for _, s := range scoped.Subnetworks {
				out = append(out, fromSubnet(s))
			}
		}
		return nil
	})
	return out, err
}

func (a liveAPI) ListFirewalls(ctx context.Context, project string) ([]Firewall, error) {
	var out []Firewall
	err := a.svc.Firewalls.List(project).Context(ctx).Pages(ctx, func(page *gce.FirewallList) error {
		for _, f := range page.Items {
			out = append(out, fromFirewall(f))
		}
		return nil
	})
	return out, err
}

func (a liveAPI) ListInstances(ctx context.Context, project string) ([]Instance, error) {
	var out []Instance
	err := a.svc.Instances.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.InstanceAggregatedList) error {
		for _, scoped := range page.Items {
			for _, inst := range scoped.Instances {
				out = append(out, fromInstance(inst))
			}
		}
		return nil
	})
	return out, err
}

func fromNetwork(n *gce.Network) Network {
	if n == nil {
		return Network{}
	}
	mode := ""
	if n.RoutingConfig != nil {
		mode = n.RoutingConfig.RoutingMode
	}
	var peerings []Peering
	for _, p := range n.Peerings {
		if p != nil {
			peerings = append(peerings, Peering{Name: p.Name, State: p.State})
		}
	}
	return Network{
		Name:        n.Name,
		ID:          n.Id,
		Description: n.Description,
		AutoCreate:  n.AutoCreateSubnetworks,
		RoutingMode: mode,
		MTU:         n.Mtu,
		Subnets:     len(n.Subnetworks),
		Peerings:    peerings,
		Created:     parseCreated(n.CreationTimestamp),
	}
}

func fromSubnet(s *gce.Subnetwork) Subnet {
	if s == nil {
		return Subnet{}
	}
	var ranges []SecondaryRange
	for _, r := range s.SecondaryIpRanges {
		if r != nil {
			ranges = append(ranges, SecondaryRange{Name: r.RangeName, CIDR: r.IpCidrRange})
		}
	}
	return Subnet{
		Name:                s.Name,
		Region:              lastSeg(s.Region),
		Network:             lastSeg(s.Network),
		CIDR:                s.IpCidrRange,
		Gateway:             s.GatewayAddress,
		PrivateGoogleAccess: s.PrivateIpGoogleAccess,
		Description:         s.Description,
		Purpose:             s.Purpose,
		StackType:           s.StackType,
		State:               s.State,
		SecondaryRanges:     ranges,
		FlowLogs:            s.EnableFlowLogs || (s.LogConfig != nil && s.LogConfig.Enable),
		Created:             parseCreated(s.CreationTimestamp),
	}
}

func fromFirewall(f *gce.Firewall) Firewall {
	if f == nil {
		return Firewall{}
	}
	var allowed []string
	for _, a := range f.Allowed {
		allowed = append(allowed, joinAllowed(a.IPProtocol, a.Ports))
	}
	var denied []string
	for _, d := range f.Denied {
		denied = append(denied, joinAllowed(d.IPProtocol, d.Ports))
	}
	return Firewall{
		Name:                  f.Name,
		Network:               lastSeg(f.Network),
		Direction:             f.Direction,
		Priority:              f.Priority,
		Disabled:              f.Disabled,
		Logging:               f.LogConfig != nil && f.LogConfig.Enable,
		SourceRanges:          append([]string(nil), f.SourceRanges...),
		SourceTags:            append([]string(nil), f.SourceTags...),
		DestinationRanges:     append([]string(nil), f.DestinationRanges...),
		Allowed:               strings.Join(allowed, ";"),
		Denied:                strings.Join(denied, ";"),
		TargetTags:            append([]string(nil), f.TargetTags...),
		TargetServiceAccounts: append([]string(nil), f.TargetServiceAccounts...),
		SourceServiceAccounts: append([]string(nil), f.SourceServiceAccounts...),
		Description:           f.Description,
		Created:               parseCreated(f.CreationTimestamp),
	}
}

func fromInstance(inst *gce.Instance) Instance {
	if inst == nil {
		return Instance{}
	}
	out := Instance{
		Name:               inst.Name,
		Zone:               lastSeg(inst.Zone),
		Status:             inst.Status,
		MachineType:        lastSeg(inst.MachineType),
		CPUPlatform:        inst.CpuPlatform,
		Interfaces:         len(inst.NetworkInterfaces),
		Disks:              len(inst.Disks),
		DeletionProtection: inst.DeletionProtection,
		LastStarted:        parseCreated(inst.LastStartTimestamp),
		LastStopped:        parseCreated(inst.LastStopTimestamp),
		IPForwarding:       inst.CanIpForward,
		Description:        inst.Description,
		Created:            parseCreated(inst.CreationTimestamp),
		Labels:             maps.Clone(inst.Labels),
	}
	if len(inst.NetworkInterfaces) > 0 {
		ni := inst.NetworkInterfaces[0]
		out.Network = lastSeg(ni.Network)
		out.Subnetwork = lastSeg(ni.Subnetwork)
		out.InternalIP = ni.NetworkIP
		if len(ni.AccessConfigs) > 0 {
			out.ExternalIP = ni.AccessConfigs[0].NatIP
		}
	}
	for _, d := range inst.Disks {
		if d != nil && d.Boot {
			out.BootDiskGB = d.DiskSizeGb
		}
	}
	if len(inst.ServiceAccounts) > 0 && inst.ServiceAccounts[0] != nil {
		out.ServiceAccount = inst.ServiceAccounts[0].Email
		for _, sc := range inst.ServiceAccounts[0].Scopes {
			out.Scopes = append(out.Scopes, strings.TrimPrefix(sc, "https://www.googleapis.com/auth/"))
		}
	}
	if inst.Tags != nil {
		out.Tags = append([]string(nil), inst.Tags.Items...)
	}
	if sc := inst.Scheduling; sc != nil {
		out.ProvisioningModel = sc.ProvisioningModel
		if out.ProvisioningModel == "" && sc.Preemptible {
			out.ProvisioningModel = "PREEMPTIBLE"
		}
		out.OnHostMaintenance = sc.OnHostMaintenance
		out.AutoRestart = sc.AutomaticRestart
		out.TerminationAction = sc.InstanceTerminationAction
	}
	if sh := inst.ShieldedInstanceConfig; sh != nil {
		out.SecureBoot = sh.EnableSecureBoot
		out.VTPM = sh.EnableVtpm
		out.Integrity = sh.EnableIntegrityMonitoring
	}
	if c := inst.ConfidentialInstanceConfig; c != nil {
		switch {
		case c.ConfidentialInstanceType != "" && c.ConfidentialInstanceType != "CONFIDENTIAL_INSTANCE_TYPE_UNSPECIFIED":
			out.Confidential = c.ConfidentialInstanceType
		case c.EnableConfidentialCompute:
			out.Confidential = "on"
		}
	}
	for _, a := range inst.GuestAccelerators {
		if a != nil {
			out.Accelerators = append(out.Accelerators, fmt.Sprintf("%d× %s", a.AcceleratorCount, lastSeg(a.AcceleratorType)))
		}
	}
	// Only enable-oslogin is read from metadata: other keys can hold
	// startup scripts and secrets.
	if md := inst.Metadata; md != nil {
		for _, it := range md.Items {
			if it != nil && it.Key == "enable-oslogin" && it.Value != nil {
				out.OSLogin = strings.ToLower(*it.Value)
			}
		}
	}
	return out
}

func (a liveAPI) ListAddresses(ctx context.Context, project string) ([]Address, error) {
	var out []Address
	err := a.svc.Addresses.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.AddressAggregatedList) error {
		for _, scoped := range page.Items {
			for _, ad := range scoped.Addresses {
				out = append(out, fromAddress(ad))
			}
		}
		return nil
	})
	return out, err
}

func (a liveAPI) ListForwardingRules(ctx context.Context, project string) ([]ForwardingRule, error) {
	var out []ForwardingRule
	err := a.svc.ForwardingRules.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.ForwardingRuleAggregatedList) error {
		for _, scoped := range page.Items {
			for _, f := range scoped.ForwardingRules {
				out = append(out, fromForwardingRule(f))
			}
		}
		return nil
	})
	return out, err
}

func (a liveAPI) ListServiceAttachments(ctx context.Context, project string) ([]ServiceAttachment, error) {
	var out []ServiceAttachment
	err := a.svc.ServiceAttachments.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.ServiceAttachmentAggregatedList) error {
		for _, scoped := range page.Items {
			for _, sa := range scoped.ServiceAttachments {
				out = append(out, fromServiceAttachment(sa))
			}
		}
		return nil
	})
	return out, err
}

func fromAddress(a *gce.Address) Address {
	if a == nil {
		return Address{}
	}
	users := make([]string, 0, len(a.Users))
	for _, u := range a.Users {
		users = append(users, kindName(u))
	}
	return Address{
		Name:         a.Name,
		Region:       lastSeg(a.Region),
		Address:      a.Address,
		AddressType:  a.AddressType,
		IPVersion:    a.IpVersion,
		NetworkTier:  a.NetworkTier,
		Purpose:      a.Purpose,
		Status:       a.Status,
		Network:      lastSeg(a.Network),
		Subnetwork:   lastSeg(a.Subnetwork),
		PrefixLength: a.PrefixLength,
		Users:        users,
		Description:  a.Description,
		Created:      parseCreated(a.CreationTimestamp),
		Labels:       maps.Clone(a.Labels),
	}
}

func fromForwardingRule(f *gce.ForwardingRule) ForwardingRule {
	if f == nil {
		return ForwardingRule{}
	}
	ports := f.PortRange
	switch {
	case f.AllPorts:
		ports = "all"
	case len(f.Ports) > 0:
		ports = strings.Join(f.Ports, ",")
	}
	target := f.Target
	if strings.Contains(target, "/") {
		target = kindName(target)
	}
	return ForwardingRule{
		Name:                f.Name,
		Region:              lastSeg(f.Region),
		IPAddress:           f.IPAddress,
		IPProtocol:          f.IPProtocol,
		Ports:               ports,
		Scheme:              f.LoadBalancingScheme,
		Target:              target,
		BackendService:      lastSeg(f.BackendService),
		Network:             lastSeg(f.Network),
		Subnetwork:          lastSeg(f.Subnetwork),
		NetworkTier:         f.NetworkTier,
		GlobalAccess:        f.AllowGlobalAccess || f.AllowPscGlobalAccess,
		PSCConnectionStatus: f.PscConnectionStatus,
		PSCConnectionID:     pscConnectionID(f.PscConnectionId),
		TargetRef:           refOf(f.Target),
		BackendServiceRef:   refOf(f.BackendService),
		Description:         f.Description,
		Created:             parseCreated(f.CreationTimestamp),
		Labels:              maps.Clone(f.Labels),
	}
}

func pscConnectionID(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}

func fromServiceAttachment(s *gce.ServiceAttachment) ServiceAttachment {
	if s == nil {
		return ServiceAttachment{}
	}
	target := s.TargetService
	if target == "" {
		target = s.ProducerForwardingRule
	}
	out := ServiceAttachment{
		Name:                 s.Name,
		Region:               lastSeg(s.Region),
		TargetService:        kindName(target),
		ConnectionPreference: s.ConnectionPreference,
		ProxyProtocol:        s.EnableProxyProtocol,
		DomainNames:          append([]string(nil), s.DomainNames...),
		RejectList:           append([]string(nil), s.ConsumerRejectLists...),
		Description:          s.Description,
		Created:              parseCreated(s.CreationTimestamp),
	}
	for _, n := range s.NatSubnets {
		out.NATSubnets = append(out.NATSubnets, lastSeg(n))
	}
	for _, l := range s.ConsumerAcceptLists {
		if l == nil {
			continue
		}
		who := l.ProjectIdOrNum
		if who == "" {
			who = kindName(l.NetworkUrl)
		}
		out.AcceptList = append(out.AcceptList, fmt.Sprintf("%s (limit %d)", who, l.ConnectionLimit))
	}
	for _, e := range s.ConnectedEndpoints {
		if e == nil {
			continue
		}
		out.Endpoints = append(out.Endpoints, ConnectedEndpoint{
			Endpoint:        projectAndName(e.Endpoint),
			ConsumerNetwork: projectAndName(e.ConsumerNetwork),
			Status:          e.Status,
		})
	}
	return out
}

func (a liveAPI) ListURLMaps(ctx context.Context, project string) ([]URLMap, error) {
	var out []URLMap
	err := a.svc.UrlMaps.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.UrlMapsAggregatedList) error {
		for _, scoped := range page.Items {
			for _, m := range scoped.UrlMaps {
				out = append(out, fromURLMap(m))
			}
		}
		return nil
	})
	return out, err
}

func (a liveAPI) ListBackendServices(ctx context.Context, project string) ([]BackendService, error) {
	var out []BackendService
	err := a.svc.BackendServices.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.BackendServiceAggregatedList) error {
		for _, scoped := range page.Items {
			for _, b := range scoped.BackendServices {
				out = append(out, fromBackendService(b))
			}
		}
		return nil
	})
	return out, err
}

// ListTargetProxies reads every proxy kind. HTTP, HTTPS, and TCP proxies
// have aggregated lists; SSL and gRPC proxies are global only. A kind that
// fails is skipped, so one denied list does not blank every load balancer:
// the others come back with a model.Partial error. The call fails outright
// only when every kind does or ctx ends.
func (a liveAPI) ListTargetProxies(ctx context.Context, project string) ([]TargetProxy, error) {
	var out []TargetProxy
	var errs []error
	note := func(kind string, err error) {
		if err != nil {
			slog.Warn("compute: target proxies unavailable", "project", project, "kind", kind, "err", err)
			errs = append(errs, err)
		}
	}
	note("targetHttpProxies", a.svc.TargetHttpProxies.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.TargetHttpProxyAggregatedList) error {
		for _, scoped := range page.Items {
			for _, p := range scoped.TargetHttpProxies {
				out = append(out, TargetProxy{Kind: "targetHttpProxies", Name: p.Name, Region: lastSeg(p.Region), URLMapRef: refOf(p.UrlMap)})
			}
		}
		return nil
	}))
	note("targetHttpsProxies", a.svc.TargetHttpsProxies.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.TargetHttpsProxyAggregatedList) error {
		for _, scoped := range page.Items {
			for _, p := range scoped.TargetHttpsProxies {
				out = append(out, TargetProxy{Kind: "targetHttpsProxies", Name: p.Name, Region: lastSeg(p.Region), URLMapRef: refOf(p.UrlMap),
					Certificates: lastSegs(p.SslCertificates), SSLPolicy: lastSeg(p.SslPolicy), CertificateMap: lastSeg(p.CertificateMap)})
			}
		}
		return nil
	}))
	note("targetTcpProxies", a.svc.TargetTcpProxies.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.TargetTcpProxyAggregatedList) error {
		for _, scoped := range page.Items {
			for _, p := range scoped.TargetTcpProxies {
				out = append(out, TargetProxy{Kind: "targetTcpProxies", Name: p.Name, Region: lastSeg(p.Region), ServiceRef: refOf(p.Service)})
			}
		}
		return nil
	}))
	note("targetSslProxies", a.svc.TargetSslProxies.List(project).Context(ctx).Pages(ctx, func(page *gce.TargetSslProxyList) error {
		for _, p := range page.Items {
			out = append(out, TargetProxy{Kind: "targetSslProxies", Name: p.Name, ServiceRef: refOf(p.Service),
				Certificates: lastSegs(p.SslCertificates), SSLPolicy: lastSeg(p.SslPolicy)})
		}
		return nil
	}))
	note("targetGrpcProxies", a.svc.TargetGrpcProxies.List(project).Context(ctx).Pages(ctx, func(page *gce.TargetGrpcProxyList) error {
		for _, p := range page.Items {
			out = append(out, TargetProxy{Kind: "targetGrpcProxies", Name: p.Name, URLMapRef: refOf(p.UrlMap)})
		}
		return nil
	}))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(errs) == 5 {
		return nil, errs[0]
	}
	return out, model.Partial(errors.Join(errs...))
}

func (a liveAPI) ListHealthChecks(ctx context.Context, project string) ([]HealthCheck, error) {
	var out []HealthCheck
	err := a.svc.HealthChecks.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.HealthChecksAggregatedList) error {
		for _, scoped := range page.Items {
			for _, h := range scoped.HealthChecks {
				out = append(out, fromHealthCheck(h))
			}
		}
		return nil
	})
	return out, err
}

func (a liveAPI) ListSecurityPolicies(ctx context.Context, project string) ([]SecurityPolicy, error) {
	var out []SecurityPolicy
	err := a.svc.SecurityPolicies.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.SecurityPoliciesAggregatedList) error {
		for _, scoped := range page.Items {
			for _, p := range scoped.SecurityPolicies {
				out = append(out, fromSecurityPolicy(p))
			}
		}
		return nil
	})
	return out, err
}

func (a liveAPI) ListSSLPolicies(ctx context.Context, project string) ([]SSLPolicy, error) {
	var out []SSLPolicy
	err := a.svc.SslPolicies.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.SslPoliciesAggregatedList) error {
		for _, scoped := range page.Items {
			for _, p := range scoped.SslPolicies {
				out = append(out, fromSSLPolicy(p))
			}
		}
		return nil
	})
	return out, err
}

func fromSSLPolicy(p *gce.SslPolicy) SSLPolicy {
	if p == nil {
		return SSLPolicy{}
	}
	out := SSLPolicy{
		Name:           p.Name,
		Region:         lastSeg(p.Region),
		Profile:        p.Profile,
		MinTLSVersion:  p.MinTlsVersion,
		CustomFeatures: slices.Clone(p.CustomFeatures),
		Description:    p.Description,
		Created:        parseCreated(p.CreationTimestamp),
	}
	for _, w := range p.Warnings {
		if w != nil && w.Message != "" {
			out.Warnings = append(out.Warnings, w.Message)
		}
	}
	return out
}

func lastSegs(links []string) []string {
	var out []string
	for _, l := range links {
		out = append(out, lastSeg(l))
	}
	return out
}

func fromURLMap(m *gce.UrlMap) URLMap {
	if m == nil {
		return URLMap{}
	}
	out := URLMap{
		Name:        m.Name,
		Region:      lastSeg(m.Region),
		Description: m.Description,
		Created:     parseCreated(m.CreationTimestamp),
	}
	add := func(link string) {
		if ref := refOf(link); ref != "" && !slices.Contains(out.ServiceRefs, ref) {
			out.ServiceRefs = append(out.ServiceRefs, ref)
		}
	}
	addAction := func(ra *gce.HttpRouteAction) {
		if ra == nil {
			return
		}
		for _, w := range ra.WeightedBackendServices {
			if w != nil {
				add(w.BackendService)
			}
		}
	}
	add(m.DefaultService)
	addAction(m.DefaultRouteAction)
	for _, pm := range m.PathMatchers {
		if pm == nil {
			continue
		}
		add(pm.DefaultService)
		addAction(pm.DefaultRouteAction)
		for _, pr := range pm.PathRules {
			if pr != nil {
				add(pr.Service)
				addAction(pr.RouteAction)
			}
		}
		for _, rr := range pm.RouteRules {
			if rr != nil {
				add(rr.Service)
				addAction(rr.RouteAction)
			}
		}
	}
	for _, hr := range m.HostRules {
		if hr != nil {
			out.Hosts = append(out.Hosts, hr.Hosts...)
		}
	}
	return out
}

func fromBackendService(b *gce.BackendService) BackendService {
	if b == nil {
		return BackendService{}
	}
	out := BackendService{
		Name:               b.Name,
		Region:             lastSeg(b.Region),
		Protocol:           b.Protocol,
		Scheme:             b.LoadBalancingScheme,
		PortName:           b.PortName,
		SecurityPolicy:     refOf(b.SecurityPolicy),
		EdgeSecurityPolicy: refOf(b.EdgeSecurityPolicy),
		CDN:                b.EnableCDN,
		SessionAffinity:    b.SessionAffinity,
		TimeoutSec:         b.TimeoutSec,
		Logging:            b.LogConfig != nil && b.LogConfig.Enable,
		Description:        b.Description,
		Created:            parseCreated(b.CreationTimestamp),
		IAP:                b.Iap != nil && b.Iap.Enabled,
	}
	if b.LogConfig != nil && b.LogConfig.Enable {
		out.LogSampleRate = b.LogConfig.SampleRate
	}
	if c := b.CdnPolicy; c != nil && b.EnableCDN {
		out.CDNCacheMode = c.CacheMode
	}
	if d := b.ConnectionDraining; d != nil {
		out.DrainingSec = d.DrainingTimeoutSec
	}
	for _, hc := range b.HealthChecks {
		if ref := refOf(hc); ref != "" {
			out.HealthCheckRefs = append(out.HealthCheckRefs, ref)
		}
	}
	for _, be := range b.Backends {
		if be == nil {
			continue
		}
		segs := strings.Split(be.Group, "/")
		loc := ""
		if len(segs) >= 4 {
			loc = segs[len(segs)-3] // zones/<z>/instanceGroups/<n> or regions/<r>/...
		}
		out.Backends = append(out.Backends, Backend{
			Group:         kindName(be.Group),
			Location:      loc,
			BalancingMode: be.BalancingMode,
			Capacity:      be.CapacityScaler,
		})
	}
	return out
}

func fromHealthCheck(h *gce.HealthCheck) HealthCheck {
	if h == nil {
		return HealthCheck{}
	}
	out := HealthCheck{
		Name:               h.Name,
		Region:             lastSeg(h.Region),
		Type:               h.Type,
		IntervalSec:        h.CheckIntervalSec,
		TimeoutSec:         h.TimeoutSec,
		HealthyThreshold:   h.HealthyThreshold,
		UnhealthyThreshold: h.UnhealthyThreshold,
		Logging:            h.LogConfig != nil && h.LogConfig.Enable,
		Description:        h.Description,
		Created:            parseCreated(h.CreationTimestamp),
	}
	port := func(n int64, name string) string {
		if name != "" {
			return name
		}
		if n > 0 {
			return strconv.FormatInt(n, 10)
		}
		return ""
	}
	switch {
	case h.HttpHealthCheck != nil:
		out.Port, out.RequestPath = port(h.HttpHealthCheck.Port, h.HttpHealthCheck.PortName), h.HttpHealthCheck.RequestPath
	case h.HttpsHealthCheck != nil:
		out.Port, out.RequestPath = port(h.HttpsHealthCheck.Port, h.HttpsHealthCheck.PortName), h.HttpsHealthCheck.RequestPath
	case h.Http2HealthCheck != nil:
		out.Port, out.RequestPath = port(h.Http2HealthCheck.Port, h.Http2HealthCheck.PortName), h.Http2HealthCheck.RequestPath
	case h.TcpHealthCheck != nil:
		out.Port = port(h.TcpHealthCheck.Port, h.TcpHealthCheck.PortName)
	case h.SslHealthCheck != nil:
		out.Port = port(h.SslHealthCheck.Port, h.SslHealthCheck.PortName)
	case h.GrpcHealthCheck != nil:
		out.Port = port(h.GrpcHealthCheck.Port, h.GrpcHealthCheck.PortName)
	}
	return out
}

func fromSecurityPolicy(p *gce.SecurityPolicy) SecurityPolicy {
	if p == nil {
		return SecurityPolicy{}
	}
	out := SecurityPolicy{
		Name:        p.Name,
		Region:      lastSeg(p.Region),
		Type:        p.Type,
		Description: p.Description,
		Created:     parseCreated(p.CreationTimestamp),
		Labels:      maps.Clone(p.Labels),
	}
	if ap := p.AdaptiveProtectionConfig; ap != nil && ap.Layer7DdosDefenseConfig != nil {
		out.AdaptiveProtection = ap.Layer7DdosDefenseConfig.Enable
	}
	for _, r := range p.Rules {
		if r == nil {
			continue
		}
		rule := SecurityRule{Priority: r.Priority, Action: r.Action, Description: r.Description, Preview: r.Preview}
		if m := r.Match; m != nil {
			switch {
			case m.Expr != nil && m.Expr.Expression != "":
				rule.Match = m.Expr.Expression
			case m.Config != nil && len(m.Config.SrcIpRanges) > 0:
				rule.Match = "src " + strings.Join(m.Config.SrcIpRanges, ",")
			}
		}
		out.Rules = append(out.Rules, rule)
	}
	return out
}

func (a liveAPI) ListDisks(ctx context.Context, project string) ([]Disk, error) {
	var out []Disk
	err := a.svc.Disks.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.DiskAggregatedList) error {
		for _, scoped := range page.Items {
			for _, d := range scoped.Disks {
				out = append(out, fromDisk(d))
			}
		}
		return nil
	})
	return out, err
}

func (a liveAPI) ListInstanceTemplates(ctx context.Context, project string) ([]InstanceTemplate, error) {
	var out []InstanceTemplate
	err := a.svc.InstanceTemplates.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.InstanceTemplateAggregatedList) error {
		for _, scoped := range page.Items {
			for _, t := range scoped.InstanceTemplates {
				out = append(out, fromInstanceTemplate(t))
			}
		}
		return nil
	})
	return out, err
}

// ListInstanceGroups lists every group, then the project's autoscalers to
// add each group's limits. The autoscalers only add detail, so a failed
// list is logged and leaves the groups without them.
func (a liveAPI) ListInstanceGroups(ctx context.Context, project string) ([]InstanceGroup, error) {
	var (
		out   []InstanceGroup
		links []string
	)
	err := a.svc.InstanceGroupManagers.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.InstanceGroupManagerAggregatedList) error {
		for _, scoped := range page.Items {
			for _, g := range scoped.InstanceGroupManagers {
				out = append(out, fromInstanceGroup(g))
				links = append(links, display.RelLink(g.SelfLink))
			}
		}
		return nil
	})
	// Status.Autoscaler names a group's autoscaler, so the autoscaler list
	// is skipped when no group has one.
	if err != nil || !slices.ContainsFunc(out, func(g InstanceGroup) bool { return g.Autoscaler != "" }) {
		return out, err
	}
	scalers := map[string]*gce.Autoscaler{}
	err = a.svc.Autoscalers.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.AutoscalerAggregatedList) error {
		for _, scoped := range page.Items {
			for _, as := range scoped.Autoscalers {
				if as != nil {
					scalers[display.RelLink(as.Target)] = as
				}
			}
		}
		return nil
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		slog.Warn("compute: autoscalers unavailable; groups listed without limits", "project", project, "err", err)
		return out, nil
	}
	for i := range out {
		if as := scalers[links[i]]; as != nil {
			withAutoscaler(&out[i], as)
		}
	}
	return out, nil
}

func withAutoscaler(g *InstanceGroup, as *gce.Autoscaler) {
	g.AutoscaleStatus = as.Status
	g.Recommended = as.RecommendedSize
	if p := as.AutoscalingPolicy; p != nil {
		g.AutoscaleMode = p.Mode
		if g.AutoscaleMode == "" {
			g.AutoscaleMode = "ON"
		}
		g.AutoscaleMin = p.MinNumReplicas
		g.AutoscaleMax = p.MaxNumReplicas
		if c := p.CpuUtilization; c != nil {
			g.AutoscaleCPU = c.UtilizationTarget
		}
	}
}

// errStop ends paging once a limit is reached.
var errStop = errors.New("stop")

func (a liveAPI) ListManagedInstances(ctx context.Context, project, zone, region, group string, limit int) ([]ManagedInstance, bool, error) {
	var out []ManagedInstance
	truncated := false
	add := func(items []*gce.ManagedInstance) error {
		for _, m := range items {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			if m == nil {
				continue
			}
			out = append(out, fromManagedInstance(m))
		}
		return nil
	}
	var err error
	if zone != "" {
		err = a.svc.InstanceGroupManagers.ListManagedInstances(project, zone, group).Pages(ctx, func(resp *gce.InstanceGroupManagersListManagedInstancesResponse) error {
			return add(resp.ManagedInstances)
		})
	} else {
		err = a.svc.RegionInstanceGroupManagers.ListManagedInstances(project, region, group).Pages(ctx, func(resp *gce.RegionInstanceGroupManagersListInstancesResponse) error {
			return add(resp.ManagedInstances)
		})
	}
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

func fromManagedInstance(m *gce.ManagedInstance) ManagedInstance {
	if m == nil {
		return ManagedInstance{}
	}
	out := ManagedInstance{
		Name:         lastSeg(m.Instance),
		Zone:         zoneOfLink(m.Instance),
		Status:       m.InstanceStatus,
		TargetStatus: m.TargetStatus,
		Action:       m.CurrentAction,
	}
	if out.Name == "" {
		out.Name = m.Name
	}
	if v := m.Version; v != nil {
		out.Template = lastSeg(v.InstanceTemplate)
		out.Version = v.Name
	}
	for _, h := range m.InstanceHealth {
		if h == nil || h.DetailedHealthState == "" {
			continue
		}
		s := h.DetailedHealthState
		if hc := lastSeg(h.HealthCheck); hc != "" {
			s += " (" + hc + ")"
		}
		out.Health = append(out.Health, s)
	}
	if la := m.LastAttempt; la != nil && la.Errors != nil {
		for _, e := range la.Errors.Errors {
			if e != nil {
				out.Errors = append(out.Errors, strings.TrimSpace(e.Code+": "+e.Message))
			}
		}
	}
	return out
}

// zoneOfLink reads the zone from an instance link
// (…/projects/p/zones/z/instances/vm).
func zoneOfLink(link string) string {
	_, rest, ok := strings.Cut(link, "/zones/")
	if !ok {
		return ""
	}
	zone, _, _ := strings.Cut(rest, "/")
	return zone
}

func (a liveAPI) ListRoutes(ctx context.Context, project string) ([]Route, error) {
	var out []Route
	err := a.svc.Routes.List(project).Context(ctx).Pages(ctx, func(page *gce.RouteList) error {
		for _, r := range page.Items {
			out = append(out, fromRoute(r))
		}
		return nil
	})
	return out, err
}

func (a liveAPI) ListRouters(ctx context.Context, project string) ([]Router, error) {
	var out []Router
	err := a.svc.Routers.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.RouterAggregatedList) error {
		for _, scoped := range page.Items {
			for _, r := range scoped.Routers {
				out = append(out, fromRouter(r))
			}
		}
		return nil
	})
	return out, err
}

func (a liveAPI) GetRouter(ctx context.Context, project, region, name string) (Router, error) {
	r, err := a.svc.Routers.Get(project, region, name).Context(ctx).Do()
	if err != nil {
		return Router{}, err
	}
	return fromRouter(r), nil
}

func (a liveAPI) GetIAMPolicy(ctx context.Context, project, kind, zone, region, name string) ([]Binding, error) {
	var (
		pol *gce.Policy
		err error
	)
	switch kind {
	case "instances":
		pol, err = a.svc.Instances.GetIamPolicy(project, zone, name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	case "subnetworks":
		pol, err = a.svc.Subnetworks.GetIamPolicy(project, region, name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	case "disks":
		if zone != "" {
			pol, err = a.svc.Disks.GetIamPolicy(project, zone, name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
		} else {
			pol, err = a.svc.RegionDisks.GetIamPolicy(project, region, name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
		}
	case "instanceTemplates":
		pol, err = a.svc.InstanceTemplates.GetIamPolicy(project, name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	case "backendServices":
		if region != "" {
			pol, err = a.svc.RegionBackendServices.GetIamPolicy(project, region, name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
		} else {
			pol, err = a.svc.BackendServices.GetIamPolicy(project, name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
		}
	case "serviceAttachments":
		pol, err = a.svc.ServiceAttachments.GetIamPolicy(project, region, name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	default:
		return nil, fmt.Errorf("compute: unsupported IAM policy kind %q", kind)
	}
	if err != nil {
		return nil, err
	}
	var out []Binding
	for _, b := range pol.Bindings {
		cond := ""
		if b.Condition != nil {
			cond = b.Condition.Title
			if cond == "" {
				cond = b.Condition.Expression
			}
		}
		out = append(out, Binding{Role: b.Role, Members: append([]string(nil), b.Members...), Condition: cond})
	}
	return out, nil
}

func fromDisk(d *gce.Disk) Disk {
	if d == nil {
		return Disk{}
	}
	out := Disk{
		Name:           d.Name,
		Zone:           lastSeg(d.Zone),
		Region:         lastSeg(d.Region),
		Type:           lastSeg(d.Type),
		SizeGB:         d.SizeGb,
		Status:         d.Status,
		Users:          lastSegs(d.Users),
		SourceImage:    lastSeg(d.SourceImage),
		SourceSnapshot: lastSeg(d.SourceSnapshot),
		ReplicaZones:   lastSegs(d.ReplicaZones),
		IOPS:           d.ProvisionedIops,
		Throughput:     d.ProvisionedThroughput,
		Architecture:   d.Architecture,
		LastAttach:     parseCreated(d.LastAttachTimestamp),
		Description:    d.Description,
		Created:        parseCreated(d.CreationTimestamp),
		Labels:         maps.Clone(d.Labels),
	}
	if k := d.DiskEncryptionKey; k != nil && k.KmsKeyName != "" {
		out.KMSKey = strings.Split(k.KmsKeyName, "/cryptoKeyVersions/")[0]
	}
	return out
}

func fromInstanceTemplate(t *gce.InstanceTemplate) InstanceTemplate {
	if t == nil {
		return InstanceTemplate{}
	}
	out := InstanceTemplate{
		Name:        t.Name,
		Region:      lastSeg(t.Region),
		Description: t.Description,
		Created:     parseCreated(t.CreationTimestamp),
	}
	p := t.Properties
	if p == nil {
		return out
	}
	out.MachineType = lastSeg(p.MachineType)
	out.Labels = maps.Clone(p.Labels)
	out.Disks = len(p.Disks)
	for _, d := range p.Disks {
		if d != nil && d.Boot && d.InitializeParams != nil {
			out.BootImage = lastSeg(d.InitializeParams.SourceImage)
			out.BootDiskGB = d.InitializeParams.DiskSizeGb
			out.BootDiskType = lastSeg(d.InitializeParams.DiskType)
		}
	}
	if len(p.NetworkInterfaces) > 0 && p.NetworkInterfaces[0] != nil {
		ni := p.NetworkInterfaces[0]
		out.Network = lastSeg(ni.Network)
		out.Subnetwork = lastSeg(ni.Subnetwork)
		out.ExternalIP = len(ni.AccessConfigs) > 0
	}
	if len(p.ServiceAccounts) > 0 && p.ServiceAccounts[0] != nil {
		out.ServiceAccount = p.ServiceAccounts[0].Email
	}
	if p.Tags != nil {
		out.Tags = append([]string(nil), p.Tags.Items...)
	}
	if sc := p.Scheduling; sc != nil {
		out.Provisioning = sc.ProvisioningModel
		if out.Provisioning == "" && sc.Preemptible {
			out.Provisioning = "PREEMPTIBLE"
		}
	}
	return out
}

func fromInstanceGroup(g *gce.InstanceGroupManager) InstanceGroup {
	if g == nil {
		return InstanceGroup{}
	}
	out := InstanceGroup{
		Name:             g.Name,
		Zone:             lastSeg(g.Zone),
		Region:           lastSeg(g.Region),
		BaseInstanceName: g.BaseInstanceName,
		TargetSize:       g.TargetSize,
		Description:      g.Description,
		Created:          parseCreated(g.CreationTimestamp),
	}
	for _, v := range g.Versions {
		if v != nil && v.InstanceTemplate != "" {
			out.Templates = append(out.Templates, lastSeg(v.InstanceTemplate))
		}
	}
	if len(out.Templates) == 0 && g.InstanceTemplate != "" {
		out.Templates = []string{lastSeg(g.InstanceTemplate)}
	}
	if st := g.Status; st != nil {
		out.Stable = st.IsStable
		out.Autoscaler = lastSeg(st.Autoscaler)
	}
	if a := g.CurrentActions; a != nil {
		out.Actions = map[string]int64{
			"none": a.None, "creating": a.Creating, "creatingWithoutRetries": a.CreatingWithoutRetries,
			"recreating": a.Recreating, "deleting": a.Deleting, "abandoning": a.Abandoning,
			"restarting": a.Restarting, "refreshing": a.Refreshing, "verifying": a.Verifying,
			"starting": a.Starting, "stopping": a.Stopping, "suspending": a.Suspending, "resuming": a.Resuming,
		}
	}
	if u := g.UpdatePolicy; u != nil {
		out.UpdateType = u.Type
		out.MaxSurge = fixedOrPercent(u.MaxSurge)
		out.MaxUnavailable = fixedOrPercent(u.MaxUnavailable)
		out.MinimalAction = u.MinimalAction
		out.ReplacementMethod = u.ReplacementMethod
	}
	for _, h := range g.AutoHealingPolicies {
		if h != nil && h.HealthCheck != "" {
			out.AutoHealing = append(out.AutoHealing, lastSeg(h.HealthCheck))
			out.AutoHealDelay = max(out.AutoHealDelay, h.InitialDelaySec)
		}
	}
	if d := g.DistributionPolicy; d != nil && g.Region != "" {
		for _, z := range d.Zones {
			if z != nil {
				out.Zones = append(out.Zones, lastSeg(z.Zone))
			}
		}
		out.TargetShape = d.TargetShape
	}
	for _, np := range g.NamedPorts {
		if np != nil {
			out.NamedPorts = append(out.NamedPorts, fmt.Sprintf("%s:%d", np.Name, np.Port))
		}
	}
	return out
}

func fromRoute(r *gce.Route) Route {
	if r == nil {
		return Route{}
	}
	out := Route{
		Name:        r.Name,
		Network:     lastSeg(r.Network),
		DestRange:   r.DestRange,
		Priority:    r.Priority,
		Tags:        append([]string(nil), r.Tags...),
		RouteType:   r.RouteType,
		Description: r.Description,
		Created:     parseCreated(r.CreationTimestamp),
	}
	switch {
	case r.NextHopGateway != "":
		out.NextHop = lastSeg(r.NextHopGateway)
	case r.NextHopInstance != "":
		out.NextHop = kindName(r.NextHopInstance)
	case r.NextHopIlb != "":
		if strings.Contains(r.NextHopIlb, "/") {
			out.NextHop = kindName(r.NextHopIlb)
		} else {
			out.NextHop = r.NextHopIlb
		}
	case r.NextHopVpnTunnel != "":
		out.NextHop = kindName(r.NextHopVpnTunnel)
	case r.NextHopInterconnectAttachment != "":
		out.NextHop = kindName(r.NextHopInterconnectAttachment)
	case r.NextHopPeering != "":
		out.NextHop = "peering/" + r.NextHopPeering
	case r.NextHopNetwork != "":
		out.NextHop = kindName(r.NextHopNetwork)
	case r.NextHopIp != "":
		out.NextHop = r.NextHopIp
	}
	for _, w := range r.Warnings {
		if w != nil {
			out.Warnings = append(out.Warnings, w.Code)
		}
	}
	return out
}

func fromRouter(r *gce.Router) Router {
	if r == nil {
		return Router{}
	}
	out := Router{
		Name:        r.Name,
		Region:      lastSeg(r.Region),
		Network:     lastSeg(r.Network),
		Interfaces:  len(r.Interfaces),
		Description: r.Description,
		Created:     parseCreated(r.CreationTimestamp),
	}
	if b := r.Bgp; b != nil {
		out.ASN = b.Asn
		out.AdvertiseMode = b.AdvertiseMode
	}
	for _, p := range r.BgpPeers {
		if p != nil {
			out.Peers = append(out.Peers, BGPPeer{Name: p.Name, PeerIP: p.PeerIpAddress, PeerASN: p.PeerAsn, Enabled: p.Enable != "FALSE"})
		}
	}
	for _, n := range r.Nats {
		if n == nil {
			continue
		}
		nat := NAT{
			Name:                n.Name,
			IPAllocation:        n.NatIpAllocateOption,
			NATIPs:              lastSegs(n.NatIps),
			SourceRanges:        n.SourceSubnetworkIpRangesToNat,
			MinPortsPerVM:       n.MinPortsPerVm,
			MaxPortsPerVM:       n.MaxPortsPerVm,
			DynamicPorts:        n.EnableDynamicPortAllocation,
			EndpointIndependent: n.EnableEndpointIndependentMapping,
		}
		for _, sn := range n.Subnetworks {
			if sn != nil {
				nat.Subnetworks = append(nat.Subnetworks, lastSeg(sn.Name))
			}
		}
		if lc := n.LogConfig; lc != nil && lc.Enable {
			nat.Logging = lc.Filter
			if nat.Logging == "" {
				nat.Logging = "on"
			}
		}
		out.NATs = append(out.NATs, nat)
	}
	return out
}

// fixedOrPercent reads "3" or "20%"; "" when unset.
func fixedOrPercent(f *gce.FixedOrPercent) string {
	switch {
	case f == nil:
		return ""
	case f.Percent > 0:
		return strconv.FormatInt(f.Percent, 10) + "%"
	default:
		return strconv.FormatInt(f.Fixed, 10)
	}
}

// parseCreated reads a Compute RFC 3339 creationTimestamp. It returns the
// zero time when ts is empty or malformed.
func parseCreated(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		slog.Debug("compute: bad creationTimestamp", "value", ts, "err", err)
		return time.Time{}
	}
	return t
}
