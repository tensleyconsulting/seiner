package memorystore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	ms "cloud.google.com/go/memorystore/apiv1"
	"cloud.google.com/go/memorystore/apiv1/memorystorepb"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	redis "google.golang.org/api/redis/v1"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveRedisAPI struct{ svc *redis.Service }

// The Valkey client uses REST: the gRPC frontend cannot route the "-"
// location wildcard ListValkeyInstances relies on and answers it with an
// HTTP 404, which gRPC reports as Unimplemented. REST accepts it.
type liveValkeyAPI struct{ client *ms.Client }

// NewLazyRedis defers client construction to the first call, so a slow or
// failing constructor cannot block startup; the failure surfaces as this
// service's ProviderError instead.
func NewLazyRedis(opts ...option.ClientOption) Redis {
	l := lazy.New(func(ctx context.Context) (*redis.Service, error) {
		return redis.NewService(ctx, opts...)
	})
	return NewRedis(lazyRedisAPI{l: l})
}

// NewLazyValkey is NewLazyRedis for the Memorystore (Valkey) client,
// which g closes once built.
func NewLazyValkey(g *lazy.Group, opts ...option.ClientOption) Valkey {
	l := lazy.NewIn(g, func(ctx context.Context) (*ms.Client, error) {
		return ms.NewRESTClient(ctx, opts...)
	})
	return NewValkey(lazyValkeyAPI{l: l})
}

type lazyRedisAPI struct{ l *lazy.Value[*redis.Service] }

func (a lazyRedisAPI) ListRedisInstances(ctx context.Context, project string) ([]RedisInstance, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveRedisAPI{svc: svc}.ListRedisInstances(ctx, project)
}

type lazyValkeyAPI struct{ l *lazy.Value[*ms.Client] }

func (a lazyValkeyAPI) ListValkeyInstances(ctx context.Context, project string) ([]ValkeyInstance, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveValkeyAPI{client: c}.ListValkeyInstances(ctx, project)
}

var (
	_ RedisAPI  = lazyRedisAPI{}
	_ RedisAPI  = liveRedisAPI{}
	_ ValkeyAPI = lazyValkeyAPI{}
	_ ValkeyAPI = liveValkeyAPI{}
)

// ListRedisInstances uses the "-" location wildcard, so one call covers
// every region. Locations the API could not reach are ignored so restricted
// or unreachable regions never surface as errors.
func (a liveRedisAPI) ListRedisInstances(ctx context.Context, project string) ([]RedisInstance, error) {
	var out []RedisInstance
	var missed unreachable
	err := a.svc.Projects.Locations.Instances.List("projects/"+project+"/locations/-").Pages(ctx, func(resp *redis.ListInstancesResponse) error {
		missed.add(resp.Unreachable)
		for _, in := range resp.Instances {
			out = append(out, fromRedis(in))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	missed.log("Redis", project)
	return out, nil
}

// ListValkeyInstances uses the "-" location wildcard, as ListRedisInstances
// does.
func (a liveValkeyAPI) ListValkeyInstances(ctx context.Context, project string) ([]ValkeyInstance, error) {
	it := a.client.ListInstances(ctx, &memorystorepb.ListInstancesRequest{Parent: "projects/" + project + "/locations/-"})
	var out []ValkeyInstance
	var missed unreachable
	for {
		in, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		// it.Response is the page in hand, so each page's list is read.
		if resp, ok := it.Response.(*memorystorepb.ListInstancesResponse); ok {
			missed.add(resp.GetUnreachable())
		}
		out = append(out, fromValkey(in))
	}
	// A page with no instances can still name unreachable locations.
	if resp, ok := it.Response.(*memorystorepb.ListInstancesResponse); ok {
		missed.add(resp.GetUnreachable())
	}
	missed.log("Valkey", project)
	return out, nil
}

// unreachable collects the locations a wildcard list's pages could not
// reach, without repeats.
type unreachable []string

func (u *unreachable) add(locs []string) {
	for _, l := range locs {
		if !slices.Contains(*u, l) {
			*u = append(*u, l)
		}
	}
}

func (u unreachable) log(product, project string) {
	if len(u) > 0 {
		slog.Debug("memorystore: some "+product+" locations did not respond", "project", project, "locations", []string(u))
	}
}

func fromRedis(in *redis.Instance) RedisInstance {
	out := RedisInstance{
		Name:              lastSeg(in.Name),
		Region:            regionOf(in.Name),
		Zone:              in.CurrentLocationId,
		DisplayName:       in.DisplayName,
		State:             in.State,
		StatusMessage:     in.StatusMessage,
		Tier:              in.Tier,
		MemoryGB:          in.MemorySizeGb,
		Version:           in.RedisVersion,
		Host:              in.Host,
		Port:              in.Port,
		ReadEndpoint:      in.ReadEndpoint,
		ReadEndpointPort:  in.ReadEndpointPort,
		ReplicaCount:      in.ReplicaCount,
		Network:           lastSeg(in.AuthorizedNetwork),
		ConnectMode:       in.ConnectMode,
		ReservedIPRange:   in.ReservedIpRange,
		AuthEnabled:       in.AuthEnabled,
		TransitEncryption: in.TransitEncryptionMode,
		CMEK:              in.CustomerManagedKey,
		Created:           parseTime(in.CreateTime),
		Labels:            in.Labels,
	}
	if out.Zone == "" {
		out.Zone = in.LocationId
	}
	if in.Tier == "STANDARD_HA" {
		out.AlternativeZone = in.AlternativeLocationId
	}
	if p := in.PersistenceConfig; p != nil {
		out.Persistence = p.PersistenceMode
		if p.PersistenceMode == "RDB" {
			out.RDBPeriod = p.RdbSnapshotPeriod
			out.NextSnapshot = parseTime(p.RdbNextSnapshotTime)
		}
	}
	if mp := in.MaintenancePolicy; mp != nil {
		var ws []string
		for _, w := range mp.WeeklyMaintenanceWindow {
			s := w.Day
			if t := w.StartTime; t != nil {
				s += fmt.Sprintf(" %02d:%02d UTC", t.Hours, t.Minutes)
			}
			if d, err := time.ParseDuration(strings.ToLower(w.Duration)); err == nil && d > 0 {
				s += " (" + hoursMinutes(d) + ")"
			}
			ws = append(ws, s)
		}
		out.Maintenance = strings.Join(ws, ", ")
	}
	if ms := in.MaintenanceSchedule; ms != nil {
		out.NextMaintenance = parseTime(ms.StartTime)
	}
	return out
}

func fromValkey(in *memorystorepb.Instance) ValkeyInstance {
	out := ValkeyInstance{
		Name:               lastSeg(in.GetName()),
		Region:             regionOf(in.GetName()),
		State:              in.GetState().String(),
		EngineVersion:      in.GetEngineVersion(),
		Mode:               unspecified(in.GetMode().String()),
		NodeType:           unspecified(in.GetNodeType().String()),
		NodeSizeGB:         in.GetNodeConfig().GetSizeGb(),
		ShardCount:         in.GetShardCount(),
		ReplicaCount:       in.GetReplicaCount(),
		ZoneMode:           unspecified(in.GetZoneDistributionConfig().GetMode().String()),
		Zone:               in.GetZoneDistributionConfig().GetZone(),
		Authorization:      unspecified(in.GetAuthorizationMode().String()),
		TransitEncryption:  unspecified(in.GetTransitEncryptionMode().String()),
		Persistence:        unspecified(in.GetPersistenceConfig().GetMode().String()),
		DeletionProtection: in.GetDeletionProtectionEnabled(),
		Labels:             in.GetLabels(),
	}
	if t := in.GetCreateTime(); t != nil {
		out.Created = t.AsTime()
	}
	out.KMSKey = in.GetKmsKey()
	if b := in.GetAutomatedBackupConfig(); b != nil && b.GetAutomatedBackupMode().String() == "ENABLED" {
		parts := []string{"on"}
		if t := b.GetFixedFrequencySchedule().GetStartTime(); t != nil {
			parts = append(parts, fmt.Sprintf("daily %02d:%02d UTC", t.GetHours(), t.GetMinutes()))
		}
		if r := b.GetRetention(); r != nil {
			if days := int(r.AsDuration() / (24 * time.Hour)); days > 0 {
				parts = append(parts, fmt.Sprintf("keep %d days", days))
			}
		}
		out.Backups = strings.Join(parts, ", ")
	}
	var ws []string
	for _, w := range in.GetMaintenancePolicy().GetWeeklyMaintenanceWindow() {
		s := w.GetDay().String()
		if t := w.GetStartTime(); t != nil {
			s += fmt.Sprintf(" %02d:%02d UTC", t.GetHours(), t.GetMinutes())
		}
		ws = append(ws, s)
	}
	out.Maintenance = strings.Join(ws, ", ")
	if t := in.GetMaintenanceSchedule().GetStartTime(); t != nil {
		out.NextMaintenance = t.AsTime()
	}
	if c := in.GetCrossInstanceReplicationConfig(); c != nil {
		out.ReplicationRole = unspecified(c.GetInstanceRole().String())
		switch out.ReplicationRole {
		case "SECONDARY":
			out.ReplicationPeer = relName(c.GetPrimaryInstance().GetInstance())
		case "PRIMARY":
			var peers []string
			for _, s := range c.GetSecondaryInstances() {
				peers = append(peers, relName(s.GetInstance()))
			}
			out.ReplicationPeer = strings.Join(peers, ", ")
		}
	}
	for _, ep := range in.GetEndpoints() {
		for _, c := range ep.GetConnections() {
			if pc := c.GetPscAutoConnection(); pc != nil {
				out.Endpoints = append(out.Endpoints, Endpoint{
					Type:    connType(pc.GetConnectionType().String()),
					Address: pc.GetIpAddress(),
					Port:    pc.GetPort(),
					Network: lastSeg(pc.GetNetwork()),
				})
			} else if pc := c.GetPscConnection(); pc != nil {
				out.Endpoints = append(out.Endpoints, Endpoint{
					Type:    connType(pc.GetConnectionType().String()),
					Address: pc.GetIpAddress(),
					Port:    pc.GetPort(),
					Network: lastSeg(pc.GetNetwork()),
				})
			}
		}
	}
	// Older instances report only discovery endpoints.
	if len(out.Endpoints) == 0 {
		for _, d := range in.GetDiscoveryEndpoints() {
			out.Endpoints = append(out.Endpoints, Endpoint{Type: "DISCOVERY", Address: d.GetAddress(), Port: d.GetPort(), Network: lastSeg(d.GetNetwork())})
		}
	}
	return out
}

// hoursMinutes reads a duration as "1h", "30m", or "1h30m".
func hoursMinutes(d time.Duration) string {
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case m == 0:
		return fmt.Sprintf("%dh", h)
	case h == 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%dh%dm", h, m)
	}
}

// relName shortens projects/p/locations/l/instances/i to l/i.
func relName(name string) string {
	if _, rest, ok := strings.Cut(name, "/locations/"); ok {
		return strings.Replace(rest, "/instances/", "/", 1)
	}
	return name
}

// unspecified drops the proto zero value (MODE_UNSPECIFIED).
func unspecified(s string) string {
	if strings.HasSuffix(s, "_UNSPECIFIED") {
		return ""
	}
	return s
}

// connType turns CONNECTION_TYPE_PRIMARY into PRIMARY.
func connType(s string) string {
	return strings.TrimPrefix(unspecified(s), "CONNECTION_TYPE_")
}

// regionOf reads the location from projects/p/locations/l/instances/i.
func regionOf(name string) string {
	segs := strings.Split(name, "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "locations" {
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

func lastSeg(link string) string {
	link = strings.TrimSuffix(link, "/")
	if i := strings.LastIndexByte(link, '/'); i >= 0 {
		return link[i+1:]
	}
	return link
}
