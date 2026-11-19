package networkconnectivity

import (
	"cmp"
	"context"
	"log/slog"
	"maps"
	"strings"
	"time"

	nc "google.golang.org/api/networkconnectivity/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct{ svc *nc.Service }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as that service's
// ProviderError instead. Share the result across RegionalEndpoints and
// ConnectionPolicies so the client is built once.
func NewLazy(opts ...option.ClientOption) API {
	return lazyAPI{l: lazy.New(func(ctx context.Context) (*nc.Service, error) {
		return nc.NewService(ctx, opts...)
	})}
}

type lazyAPI struct{ l *lazy.Value[*nc.Service] }

func (a lazyAPI) ListRegionalEndpoints(ctx context.Context, project string) ([]RegionalEndpoint, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListRegionalEndpoints(ctx, project)
}

func (a lazyAPI) ListConnectionPolicies(ctx context.Context, project string) ([]ConnectionPolicy, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListConnectionPolicies(ctx, project)
}

func (a lazyAPI) GetConnectionPolicy(ctx context.Context, project, location, name string) (ConnectionPolicy, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return ConnectionPolicy{}, err
	}
	return liveAPI{svc: svc}.GetConnectionPolicy(ctx, project, location, name)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

// ListRegionalEndpoints uses the "-" location wildcard so one paginated call
// covers every region. Unreachable regions are logged and ignored.
func (a liveAPI) ListRegionalEndpoints(ctx context.Context, project string) ([]RegionalEndpoint, error) {
	var out []RegionalEndpoint
	var missed unreachable
	parent := "projects/" + project + "/locations/-"
	err := a.svc.Projects.Locations.RegionalEndpoints.List(parent).Pages(ctx, func(resp *nc.ListRegionalEndpointsResponse) error {
		missed.add(resp.Unreachable)
		for _, e := range resp.RegionalEndpoints {
			if e != nil {
				out = append(out, fromRegionalEndpoint(e))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	missed.log("regional endpoint", project)
	return out, nil
}

// ListConnectionPolicies uses the "-" location wildcard so one paginated call
// covers every region. Unreachable regions are logged and ignored.
func (a liveAPI) ListConnectionPolicies(ctx context.Context, project string) ([]ConnectionPolicy, error) {
	var out []ConnectionPolicy
	var missed unreachable
	parent := "projects/" + project + "/locations/-"
	err := a.svc.Projects.Locations.ServiceConnectionPolicies.List(parent).Pages(ctx, func(resp *nc.ListServiceConnectionPoliciesResponse) error {
		missed.add(resp.Unreachable)
		for _, p := range resp.ServiceConnectionPolicies {
			if p != nil {
				out = append(out, fromPolicy(p))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	missed.log("service connection policy", project)
	return out, nil
}

func (a liveAPI) GetConnectionPolicy(ctx context.Context, project, location, name string) (ConnectionPolicy, error) {
	fullName := "projects/" + project + "/locations/" + location + "/serviceConnectionPolicies/" + name
	p, err := a.svc.Projects.Locations.ServiceConnectionPolicies.Get(fullName).Context(ctx).Do()
	if err != nil {
		return ConnectionPolicy{}, err
	}
	return fromPolicy(p), nil
}

// unreachable tracks location strings that failed to respond across paginated
// wildcard list responses so each is logged once per call.
type unreachable struct {
	seen  map[string]bool
	items []string
}

func (u *unreachable) add(locs []string) {
	for _, loc := range locs {
		if loc == "" || u.seen[loc] {
			continue
		}
		if u.seen == nil {
			u.seen = map[string]bool{}
		}
		u.seen[loc] = true
		u.items = append(u.items, loc)
	}
}

func (u unreachable) log(kind, project string) {
	if len(u.items) > 0 {
		slog.Debug("networkconnectivity: some locations did not respond", "kind", kind, "project", project, "locations", u.items)
	}
}

func fromRegionalEndpoint(e *nc.RegionalEndpoint) RegionalEndpoint {
	ipAddr := e.IpAddress
	var addrName string
	// Live responses put the allocated IP literal in e.Address and leave
	// e.IpAddress empty; handle both IP literals and Compute address URLs.
	if strings.Contains(e.Address, "/") {
		addrName = lastSeg(e.Address)
	} else {
		ipAddr = cmp.Or(e.IpAddress, e.Address)
	}
	return RegionalEndpoint{
		Name:            lastSeg(e.Name),
		Location:        locationOf(e.Name),
		TargetGoogleAPI: e.TargetGoogleApi,
		AccessType:      unspecified(e.AccessType),
		IPAddress:       ipAddr,
		AddressName:     addrName,
		Network:         lastSeg(e.Network),
		Subnetwork:      lastSeg(e.Subnetwork),
		ForwardingRule:  lastSeg(e.PscForwardingRule),
		Description:     e.Description,
		Created:         parseTime(e.CreateTime),
		Updated:         parseTime(e.UpdateTime),
		Labels:          maps.Clone(e.Labels),
	}
}

func fromPolicy(p *nc.ServiceConnectionPolicy) ConnectionPolicy {
	out := ConnectionPolicy{
		Name:           lastSeg(p.Name),
		Location:       locationOf(p.Name),
		ServiceClass:   p.ServiceClass,
		Infrastructure: unspecified(p.Infrastructure),
		Network:        lastSeg(p.Network),
		Description:    p.Description,
		Created:        parseTime(p.CreateTime),
		Updated:        parseTime(p.UpdateTime),
		Labels:         maps.Clone(p.Labels),
	}
	if cfg := p.PscConfig; cfg != nil {
		out.ConnectionLimit = cfg.Limit
		out.ProducerLocation = unspecified(cfg.ProducerInstanceLocation)
		for _, s := range cfg.Subnetworks {
			if name := lastSeg(s); name != "" {
				out.Subnetworks = append(out.Subnetworks, name)
			}
		}
		out.AllowedProducers = append([]string(nil), cfg.AllowedGoogleProducersResourceHierarchyLevel...)
	}
	for _, c := range p.PscConnections {
		if c != nil {
			out.Connections = append(out.Connections, fromConnection(c))
		}
	}
	return out
}

func fromConnection(c *nc.PscConnection) Connection {
	consumerProj := c.ConsumerTargetProject
	if consumerProj == "" {
		consumerProj = projectOf(c.ConsumerForwardingRule)
	}
	var errMsg string
	if c.Error != nil {
		errMsg = c.Error.Message
	}
	if c.ErrorInfo != nil && c.ErrorInfo.Reason != "" {
		if errMsg != "" {
			errMsg = c.ErrorInfo.Reason + ": " + errMsg
		} else {
			errMsg = c.ErrorInfo.Reason
		}
	}
	return Connection{
		ID:                 c.PscConnectionId,
		State:              unspecified(c.State),
		ConsumerProject:    consumerProj,
		ForwardingRule:     lastSeg(c.ConsumerForwardingRule),
		ConsumerAddress:    lastSeg(c.ConsumerAddress),
		Subnetwork:         lastSeg(c.SelectedSubnetwork),
		ProducerInstanceID: c.ProducerInstanceId,
		ServiceClass:       c.ServiceClass,
		IPVersion:          unspecified(c.IpVersion),
		ErrorType:          unspecified(c.ErrorType),
		Error:              errMsg,
	}
}

func unspecified(s string) string {
	if strings.HasSuffix(s, "_UNSPECIFIED") {
		return ""
	}
	return s
}

func lastSeg(name string) string {
	name = strings.TrimSuffix(name, "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}

func locationOf(name string) string {
	segs := strings.Split(name, "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "locations" {
			return segs[i+1]
		}
	}
	return ""
}

func projectOf(name string) string {
	segs := strings.Split(name, "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "projects" {
			return segs[i+1]
		}
	}
	return ""
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
