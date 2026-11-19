package memorystore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/gcp/display"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// Redis implements model.ResourceProvider for Memorystore for Redis
// instances.
type Redis struct{ api RedisAPI }

func NewRedis(api RedisAPI) Redis { return Redis{api: api} }

func (p Redis) Service() model.ServiceType { return model.ServiceRedis }

// ListPermissions is what List needs.
func (p Redis) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "redis.instances.list")
}

func (p Redis) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("memorystore: no Redis API")
	}
	// A model.Partial error comes back with the instances that loaded.
	insts, err := p.api.ListRedisInstances(ctx, project)
	if err != nil && !model.IsPartial(err) {
		return nil, err
	}
	out := make([]model.Resource, 0, len(insts))
	for _, in := range insts {
		out = append(out, redisResource(project, in))
	}
	return out, err
}

// Valkey implements model.ResourceProvider for Memorystore for Valkey
// instances.
type Valkey struct{ api ValkeyAPI }

func NewValkey(api ValkeyAPI) Valkey { return Valkey{api: api} }

func (p Valkey) Service() model.ServiceType { return model.ServiceValkey }

// ListPermissions is what List needs.
func (p Valkey) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "memorystore.instances.list")
}

func (p Valkey) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("memorystore: no Valkey API")
	}
	// A model.Partial error comes back with the instances that loaded.
	insts, err := p.api.ListValkeyInstances(ctx, project)
	if err != nil && !model.IsPartial(err) {
		return nil, err
	}
	out := make([]model.Resource, 0, len(insts))
	for _, in := range insts {
		out = append(out, valkeyResource(project, in))
	}
	return out, err
}

func instanceID(project, region, name string) string {
	return "projects/" + project + "/locations/" + url.PathEscape(region) + "/instances/" + url.PathEscape(name)
}

func redisResource(project string, in RedisInstance) model.Resource {
	md := map[string]string{}
	set(md, "statusMessage", in.StatusMessage)
	set(md, "displayName", in.DisplayName)
	set(md, "version", in.Version)
	set(md, "tier", in.Tier)
	if in.MemoryGB > 0 {
		md["capacity"] = strconv.FormatInt(in.MemoryGB, 10) + " GB"
	}
	zone := in.Zone
	if in.AlternativeZone != "" {
		zone += " (standby " + in.AlternativeZone + ")"
	}
	set(md, "zone", zone)
	if in.Host != "" {
		md["primaryEndpoint"] = hostPort(in.Host, in.Port)
	}
	if in.ReadEndpoint != "" {
		md["readEndpoint"] = hostPort(in.ReadEndpoint, in.ReadEndpointPort)
	}
	if in.ReplicaCount > 0 {
		md["replicas"] = strconv.FormatInt(in.ReplicaCount, 10)
	}
	set(md, "network", in.Network)
	set(md, "connectMode", in.ConnectMode)
	set(md, "reservedIpRange", in.ReservedIPRange)
	md["auth"] = onOff(in.AuthEnabled)
	set(md, "transitEncryption", in.TransitEncryption)
	persistence := in.Persistence
	if in.RDBPeriod != "" {
		persistence += ", every " + strings.ToLower(strings.ReplaceAll(in.RDBPeriod, "_", " "))
	}
	set(md, "persistence", persistence)
	set(md, "nextSnapshot", display.Stamp(in.NextSnapshot))
	set(md, "cmek", in.CMEK)
	set(md, "maintenance", in.Maintenance)
	set(md, "nextMaintenance", display.Stamp(in.NextMaintenance))
	return model.Resource{
		ID:       instanceID(project, in.Region, in.Name),
		Name:     in.Name,
		Service:  model.ServiceRedis,
		ScopeID:  "projects/" + project,
		Location: in.Region,
		Status:   redisStatus(in.State),
		State:    in.State,
		Created:  in.Created,
		Labels:   maps.Clone(in.Labels),
		Metadata: md,
		Fields: model.FieldsOf(md, "statusMessage", "displayName", "version", "tier", "capacity", "zone",
			"primaryEndpoint", "readEndpoint", "replicas", "network", "connectMode", "reservedIpRange", "auth",
			"transitEncryption", "persistence", "nextSnapshot", "cmek", "maintenance", "nextMaintenance"),
	}
}

func valkeyResource(project string, in ValkeyInstance) model.Resource {
	md := map[string]string{}
	set(md, "version", in.EngineVersion)
	set(md, "mode", in.Mode)
	set(md, "nodeType", in.NodeType)
	if in.NodeSizeGB > 0 {
		md["nodeSize"] = strconv.FormatFloat(in.NodeSizeGB, 'f', -1, 64) + " GB"
	}
	if in.ShardCount > 0 {
		md["shards"] = strconv.Itoa(int(in.ShardCount))
	}
	md["replicas"] = strconv.Itoa(int(in.ReplicaCount))
	zones := in.ZoneMode
	if in.Zone != "" {
		zones += " (" + in.Zone + ")"
	}
	set(md, "zones", zones)
	var endpoints, networks []string
	for _, e := range in.Endpoints {
		s := hostPort(e.Address, int64(e.Port))
		if e.Type != "" {
			s = e.Type + "  " + s
		}
		endpoints = append(endpoints, s)
		if e.Network != "" && !slices.Contains(networks, e.Network) {
			networks = append(networks, e.Network)
		}
	}
	if len(endpoints) > 0 {
		md["endpoint"] = hostPortOf(in.Endpoints[0])
	}
	set(md, "network", strings.Join(networks, ", "))
	set(md, "authorization", in.Authorization)
	set(md, "transitEncryption", in.TransitEncryption)
	set(md, "persistence", in.Persistence)
	set(md, "backups", in.Backups)
	set(md, "cmek", in.KMSKey)
	if in.ReplicationRole != "" && in.ReplicationRole != "NONE" {
		role := strings.ToLower(in.ReplicationRole)
		switch {
		case in.ReplicationRole == "SECONDARY" && in.ReplicationPeer != "":
			role += " of " + in.ReplicationPeer
		case in.ReplicationRole == "PRIMARY" && in.ReplicationPeer != "":
			role += " for " + in.ReplicationPeer
		}
		md["replication"] = role
	}
	set(md, "maintenance", in.Maintenance)
	set(md, "nextMaintenance", display.Stamp(in.NextMaintenance))
	md["deletionProtection"] = onOff(in.DeletionProtection)
	r := model.Resource{
		ID:       instanceID(project, in.Region, in.Name),
		Name:     in.Name,
		Service:  model.ServiceValkey,
		ScopeID:  "projects/" + project,
		Location: in.Region,
		Status:   valkeyStatus(in.State),
		State:    in.State,
		Created:  in.Created,
		Labels:   maps.Clone(in.Labels),
		Metadata: md,
		Fields: model.FieldsOf(md, "version", "mode", "nodeType", "nodeSize", "shards", "replicas", "zones",
			"endpoint", "network", "authorization", "transitEncryption", "persistence", "backups", "cmek", "replication",
			"maintenance", "nextMaintenance", "deletionProtection"),
	}
	if len(endpoints) > 1 {
		r.Lists = []model.List{{ID: "endpoints", Label: "Endpoints", Items: endpoints}}
	}
	return r
}

func redisStatus(s string) model.ResourceStatus {
	switch s {
	case "READY":
		return model.StatusHealthy
	case "CREATING", "UPDATING", "DELETING", "REPAIRING", "MAINTENANCE", "IMPORTING", "FAILING_OVER":
		return model.StatusWarning
	default:
		return model.StatusUnknown
	}
}

func valkeyStatus(s string) model.ResourceStatus {
	switch s {
	case "ACTIVE":
		return model.StatusHealthy
	case "CREATING", "UPDATING", "DELETING", "MIGRATING":
		return model.StatusWarning
	default:
		return model.StatusUnknown
	}
}

func hostPort(host string, port int64) string {
	if port == 0 {
		return host
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func hostPortOf(e Endpoint) string { return hostPort(e.Address, int64(e.Port)) }

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
	_ model.ResourceProvider = Redis{}
	_ model.ResourceProvider = Valkey{}
)
