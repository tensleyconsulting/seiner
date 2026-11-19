package clouddns

import (
	"context"
	"errors"
	"strings"
	"time"

	dns "google.golang.org/api/dns/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct{ svc *dns.Service }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as this service's
// ProviderError instead.
func NewLazy(opts ...option.ClientOption) Provider {
	l := lazy.New(func(ctx context.Context) (*dns.Service, error) {
		return dns.NewService(ctx, opts...)
	})
	return New(lazyAPI{l: l})
}

type lazyAPI struct{ l *lazy.Value[*dns.Service] }

func (a lazyAPI) ListZones(ctx context.Context, project string) ([]Zone, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListZones(ctx, project)
}

func (a lazyAPI) ListRecords(ctx context.Context, project, zone string, limit int) ([]Record, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{svc: svc}.ListRecords(ctx, project, zone, limit)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

// errStop ends paging once the record limit is reached.
var errStop = errors.New("stop")

func (a liveAPI) ListZones(ctx context.Context, project string) ([]Zone, error) {
	var out []Zone
	err := a.svc.ManagedZones.List(project).Pages(ctx, func(resp *dns.ManagedZonesListResponse) error {
		for _, z := range resp.ManagedZones {
			out = append(out, fromZone(z))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListRecords(ctx context.Context, project, zone string, limit int) ([]Record, bool, error) {
	var out []Record
	truncated := false
	err := a.svc.ResourceRecordSets.List(project, zone).Pages(ctx, func(resp *dns.ResourceRecordSetsListResponse) error {
		for _, rr := range resp.Rrsets {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromRecord(rr))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

func fromZone(z *dns.ManagedZone) Zone {
	out := Zone{
		Name:        z.Name,
		DNSName:     z.DnsName,
		Description: z.Description,
		Visibility:  z.Visibility,
		NameServers: z.NameServers,
		Created:     parseTime(z.CreationTime),
		Labels:      z.Labels,
	}
	if c := z.DnssecConfig; c != nil {
		out.DNSSEC = c.State
	}
	if c := z.PrivateVisibilityConfig; c != nil {
		for _, n := range c.Networks {
			out.Networks = append(out.Networks, lastSeg(n.NetworkUrl))
		}
		for _, g := range c.GkeClusters {
			out.GKEClusters = append(out.GKEClusters, lastSeg(g.GkeClusterName))
		}
	}
	if c := z.ForwardingConfig; c != nil {
		for _, t := range c.TargetNameServers {
			switch {
			case t.Ipv4Address != "":
				out.Forwarding = append(out.Forwarding, t.Ipv4Address)
			case t.Ipv6Address != "":
				out.Forwarding = append(out.Forwarding, t.Ipv6Address)
			case t.DomainName != "":
				out.Forwarding = append(out.Forwarding, t.DomainName)
			}
		}
	}
	if c := z.PeeringConfig; c != nil && c.TargetNetwork != nil {
		out.PeerNetwork = lastSeg(c.TargetNetwork.NetworkUrl)
	}
	if c := z.CloudLoggingConfig; c != nil {
		out.Logging = c.EnableLogging
	}
	return out
}

func fromRecord(rr *dns.ResourceRecordSet) Record {
	out := Record{Name: rr.Name, Type: rr.Type, TTL: rr.Ttl, Data: rr.Rrdatas}
	if p := rr.RoutingPolicy; p != nil {
		switch {
		case p.Geo != nil:
			out.Routing = "geo"
		case p.Wrr != nil:
			out.Routing = "weighted round robin"
		case p.PrimaryBackup != nil:
			out.Routing = "failover"
		}
	}
	return out
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func lastSeg(link string) string {
	link = strings.TrimSuffix(link, "/")
	if i := strings.LastIndexByte(link, '/'); i >= 0 {
		return link[i+1:]
	}
	return link
}
