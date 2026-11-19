package gke

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	container "google.golang.org/api/container/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/display"
	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct{ svc *container.Service }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as this service's
// ProviderError instead.
func NewLazy(opts ...option.ClientOption) Provider {
	l := lazy.New(func(ctx context.Context) (*container.Service, error) {
		return container.NewService(ctx, opts...)
	})
	return New(lazyAPI{l: l})
}

type lazyAPI struct {
	l *lazy.Value[*container.Service]
}

func (a lazyAPI) ListClusters(ctx context.Context, project string) ([]Cluster, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListClusters(ctx, project)
}

func (a lazyAPI) ListNodePools(ctx context.Context, project, location, cluster string) ([]NodePool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListNodePools(ctx, project, location, cluster)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

// ListClusters uses the "-" location wildcard, so one call covers every
// region and zone. Zones the API could not reach come back in MissingZones
// and are ignored so restricted or unreachable zones never surface as errors.
func (a liveAPI) ListClusters(ctx context.Context, project string) ([]Cluster, error) {
	resp, err := a.svc.Projects.Locations.Clusters.List("projects/" + project + "/locations/-").Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	out := make([]Cluster, 0, len(resp.Clusters))
	for _, c := range resp.Clusters {
		out = append(out, fromCluster(c))
	}
	if len(resp.MissingZones) > 0 {
		slog.Debug("gke: some zones did not respond", "project", project, "zones", resp.MissingZones)
	}
	return out, nil
}

func (a liveAPI) ListNodePools(ctx context.Context, project, location, cluster string) ([]NodePool, error) {
	resp, err := a.svc.Projects.Locations.Clusters.NodePools.List(clusterID(project, location, cluster)).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	out := make([]NodePool, 0, len(resp.NodePools))
	for _, np := range resp.NodePools {
		out = append(out, fromNodePool(np))
	}
	return out, nil
}

func fromCluster(c *container.Cluster) Cluster {
	out := Cluster{
		Name:          c.Name,
		Location:      c.Location,
		Description:   c.Description,
		Status:        c.Status,
		StatusMessage: c.StatusMessage,
		MasterVersion: c.CurrentMasterVersion,
		NodeCount:     c.CurrentNodeCount,
		Endpoint:      c.Endpoint,
		Network:       c.Network,
		Subnetwork:    c.Subnetwork,
		PodRange:      c.ClusterIpv4Cidr,
		ServicesRange: c.ServicesIpv4Cidr,
		Created:       parseTime(c.CreateTime),
		Labels:        maps.Clone(c.ResourceLabels),
	}
	if c.Autopilot != nil {
		out.Autopilot = c.Autopilot.Enabled
	}
	if c.ReleaseChannel != nil {
		out.ReleaseChannel = c.ReleaseChannel.Channel
	}
	if c.NetworkConfig != nil {
		out.PrivateNodes = c.NetworkConfig.DefaultEnablePrivateNodes
	}
	if c.PrivateClusterConfig != nil && c.PrivateClusterConfig.EnablePrivateNodes {
		out.PrivateNodes = true
	}
	if p := c.IpAllocationPolicy; p != nil {
		if p.ClusterIpv4CidrBlock != "" {
			out.PodRange = p.ClusterIpv4CidrBlock
		}
		if p.ServicesIpv4CidrBlock != "" {
			out.ServicesRange = p.ServicesIpv4CidrBlock
		}
	}
	if c.WorkloadIdentityConfig != nil {
		out.WorkloadPool = c.WorkloadIdentityConfig.WorkloadPool
	}
	for _, np := range c.NodePools {
		out.NodePools = append(out.NodePools, np.Name)
	}
	out.NodeVersion = c.CurrentNodeVersion
	if e := c.DatabaseEncryption; e != nil && e.State == "ENCRYPTED" {
		out.EtcdKey = e.KeyName
	}
	if mp := c.MaintenancePolicy; mp != nil && mp.Window != nil {
		out.Maintenance = maintenanceWindow(mp.Window)
		for _, name := range slices.Sorted(maps.Keys(mp.Window.MaintenanceExclusions)) {
			w := mp.Window.MaintenanceExclusions[name]
			out.Exclusions = append(out.Exclusions, name+"  "+day(w.StartTime)+" – "+day(w.EndTime))
		}
	}
	authorized := c.MasterAuthorizedNetworksConfig
	if pc := c.PrivateClusterConfig; pc != nil && pc.EnablePrivateNodes {
		out.PublicEndpoint = onOffWord(!pc.EnablePrivateEndpoint)
	}
	if cp := c.ControlPlaneEndpointsConfig; cp != nil {
		if ip := cp.IpEndpointsConfig; ip != nil {
			if ip.Enabled {
				out.PublicEndpoint = onOffWord(ip.EnablePublicEndpoint)
			}
			if ip.AuthorizedNetworksConfig != nil {
				authorized = ip.AuthorizedNetworksConfig
			}
		}
		if d := cp.DnsEndpointConfig; d != nil {
			out.DNSEndpoint = d.Endpoint
			out.DNSExternal = d.AllowExternalTraffic
		}
	}
	if authorized != nil && authorized.Enabled {
		out.AuthorizedNetworks = true
		for _, b := range authorized.CidrBlocks {
			s := b.CidrBlock
			if b.DisplayName != "" {
				s = b.DisplayName + " " + s
			}
			out.AuthorizedCIDRs = append(out.AuthorizedCIDRs, s)
		}
	}
	if nc := c.NetworkConfig; nc != nil && nc.DatapathProvider != "DATAPATH_PROVIDER_UNSPECIFIED" {
		out.Dataplane = nc.DatapathProvider
	}
	out.ShieldedNodes = c.ShieldedNodes != nil && c.ShieldedNodes.Enabled
	if b := c.BinaryAuthorization; b != nil {
		out.BinAuthz = b.EvaluationMode
		if out.BinAuthz == "" && b.Enabled {
			out.BinAuthz = "PROJECT_SINGLETON_POLICY_ENFORCE"
		}
	}
	return out
}

// maintenanceWindow reads a daily or recurring window. Recurring windows
// keep their RRULE, which is already terse.
func maintenanceWindow(w *container.MaintenanceWindow) string {
	switch {
	case w.DailyMaintenanceWindow != nil:
		d := w.DailyMaintenanceWindow
		s := "daily " + d.StartTime + " UTC"
		if dur := isoDuration(d.Duration); dur != "" {
			s += " (" + dur + ")"
		}
		return s
	case w.RecurringWindow != nil && w.RecurringWindow.Window != nil:
		start, end := parseTime(w.RecurringWindow.Window.StartTime), parseTime(w.RecurringWindow.Window.EndTime)
		if start.IsZero() || end.IsZero() {
			return w.RecurringWindow.Recurrence
		}
		return w.RecurringWindow.Recurrence + ", " + start.UTC().Format("15:04") + "–" + end.UTC().Format("15:04") + " UTC"
	default:
		return ""
	}
}

// isoDuration reads the API's ISO 8601 duration (PT4H30M0S) as "4h30m";
// "" when it is empty or does not parse.
func isoDuration(s string) string {
	d, err := time.ParseDuration(strings.TrimPrefix(strings.ToLower(s), "pt"))
	if err != nil || d <= 0 {
		return ""
	}
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case m == 0:
		return strconv.Itoa(h) + "h"
	case h == 0:
		return strconv.Itoa(m) + "m"
	default:
		return strconv.Itoa(h) + "h" + strconv.Itoa(m) + "m"
	}
}

// day is an RFC 3339 time's date; the string itself when it does not parse.
func day(s string) string {
	if t := parseTime(s); !t.IsZero() {
		return t.UTC().Format("2006-01-02")
	}
	return s
}

func onOffWord(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func fromNodePool(np *container.NodePool) NodePool {
	out := NodePool{
		Name:          np.Name,
		Status:        np.Status,
		StatusMessage: np.StatusMessage,
		Version:       np.Version,
		InitialNodes:  np.InitialNodeCount,
		Zones:         np.Locations,
	}
	if cfg := np.Config; cfg != nil {
		out.MachineType = cfg.MachineType
		out.DiskSizeGB = cfg.DiskSizeGb
		out.DiskType = cfg.DiskType
		if bd := cfg.BootDisk; bd != nil {
			if bd.SizeGb > 0 {
				out.DiskSizeGB = bd.SizeGb
			}
			if bd.DiskType != "" {
				out.DiskType = bd.DiskType
			}
		}
		out.ImageType = cfg.ImageType
		out.Spot = cfg.Spot
		out.Preemptible = cfg.Preemptible
		out.ServiceAccount = cfg.ServiceAccount
		for _, a := range cfg.Accelerators {
			out.Accelerators = append(out.Accelerators, Accelerator{Type: a.AcceleratorType, Count: a.AcceleratorCount})
		}
		for _, t := range cfg.Taints {
			s := t.Key
			if t.Value != "" {
				s += "=" + t.Value
			}
			out.Taints = append(out.Taints, s+":"+t.Effect)
		}
		for _, k := range slices.Sorted(maps.Keys(cfg.Labels)) {
			out.NodeLabels = append(out.NodeLabels, k+"="+cfg.Labels[k])
		}
		out.KMSKey = cfg.BootDiskKmsKey
		if w := cfg.WorkloadMetadataConfig; w != nil && w.Mode != "MODE_UNSPECIFIED" {
			out.MetadataServer = w.Mode
		}
		if sb := cfg.SandboxConfig; sb != nil && sb.Type != "UNSPECIFIED" {
			out.Sandbox = sb.Type
		}
		if sh := cfg.ShieldedInstanceConfig; sh != nil {
			out.SecureBoot = sh.EnableSecureBoot
			out.Integrity = sh.EnableIntegrityMonitoring
		}
	}
	if u := np.UpgradeSettings; u != nil {
		out.UpgradeType = u.Strategy
		out.MaxSurge = u.MaxSurge
		out.MaxUnavailable = u.MaxUnavailable
	}
	if as := np.Autoscaling; as != nil {
		out.Autoscaling = as.Enabled
		out.MinNodes = as.MinNodeCount
		out.MaxNodes = as.MaxNodeCount
		out.TotalMin = as.TotalMinNodeCount
		out.TotalMax = as.TotalMaxNodeCount
	}
	if m := np.Management; m != nil {
		out.AutoUpgrade = m.AutoUpgrade
		out.AutoRepair = m.AutoRepair
	}
	if mp := np.MaxPodsConstraint; mp != nil {
		out.MaxPodsPerNode = mp.MaxPodsPerNode
	}
	for _, u := range np.InstanceGroupUrls {
		out.InstanceGroups = append(out.InstanceGroups, display.RelLink(u))
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
