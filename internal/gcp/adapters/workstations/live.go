package workstations

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	ws "google.golang.org/api/workstations/v1"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/gcp/regions"
)

type liveAPI struct{ svc *ws.Service }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as this service's
// ProviderError instead.
func NewLazy(opts ...option.ClientOption) Provider {
	svc := lazy.New(func(ctx context.Context) (*ws.Service, error) {
		return ws.NewService(ctx, opts...)
	})
	return New(lazyAPI{
		svc: svc,
		locs: regions.Cached(func(ctx context.Context, project string) ([]string, error) {
			c, err := svc.Get(ctx)
			if err != nil {
				return nil, err
			}
			return liveAPI{svc: c}.Locations(ctx, project)
		}),
	})
}

// lazyAPI builds the client on first use. The location list is cached for
// the session, since it changes rarely and every list starts with it.
type lazyAPI struct {
	svc  *lazy.Value[*ws.Service]
	locs regions.Lister
}

func (a lazyAPI) Locations(ctx context.Context, project string) ([]string, error) {
	return a.locs.Regions(ctx, project)
}

func (a lazyAPI) ListClusters(ctx context.Context, project, location string) ([]Cluster, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.ListClusters(ctx, project, location)
}

func (a lazyAPI) ListConfigs(ctx context.Context, project, location, cluster string) ([]Config, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.ListConfigs(ctx, project, location, cluster)
}

func (a lazyAPI) ListWorkstations(ctx context.Context, project, location, cluster, config string) ([]Workstation, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.ListWorkstations(ctx, project, location, cluster, config)
}

func (a lazyAPI) ListUsable(ctx context.Context, project, location, cluster, config string) ([]Workstation, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.ListUsable(ctx, project, location, cluster, config)
}

func (a lazyAPI) GetConfigIAMPolicy(ctx context.Context, project, location, cluster, config string) ([]Binding, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.GetConfigIAMPolicy(ctx, project, location, cluster, config)
}

func (a lazyAPI) GetWorkstationIAMPolicy(ctx context.Context, project, location, cluster, config, workstation string) ([]Binding, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.GetWorkstationIAMPolicy(ctx, project, location, cluster, config, workstation)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

func (a liveAPI) Locations(ctx context.Context, project string) ([]string, error) {
	var out []string
	err := a.svc.Projects.Locations.List("projects/"+project).Pages(ctx, func(resp *ws.ListLocationsResponse) error {
		for _, l := range resp.Locations {
			if l.LocationId != "" {
				out = append(out, l.LocationId)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListClusters(ctx context.Context, project, location string) ([]Cluster, error) {
	var out []Cluster
	parent := "projects/" + project + "/locations/" + location
	err := a.svc.Projects.Locations.WorkstationClusters.List(parent).Pages(ctx, func(resp *ws.ListWorkstationClustersResponse) error {
		for _, c := range resp.WorkstationClusters {
			out = append(out, fromCluster(c))
		}
		return nil
	})
	if err != nil {
		return nil, denied(err)
	}
	return out, nil
}

func (a liveAPI) ListConfigs(ctx context.Context, project, location, cluster string) ([]Config, error) {
	var out []Config
	parent := clusterParent(project, location, cluster)
	err := a.svc.Projects.Locations.WorkstationClusters.WorkstationConfigs.List(parent).Pages(ctx, func(resp *ws.ListWorkstationConfigsResponse) error {
		for _, c := range resp.WorkstationConfigs {
			out = append(out, fromConfig(c))
		}
		return nil
	})
	if err != nil {
		return nil, denied(err)
	}
	return out, nil
}

func (a liveAPI) ListWorkstations(ctx context.Context, project, location, cluster, config string) ([]Workstation, error) {
	var out []Workstation
	parent := clusterParent(project, location, cluster) + "/workstationConfigs/" + config
	err := a.svc.Projects.Locations.WorkstationClusters.WorkstationConfigs.Workstations.List(parent).Pages(ctx, func(resp *ws.ListWorkstationsResponse) error {
		for _, w := range resp.Workstations {
			out = append(out, fromWorkstation(w))
		}
		return nil
	})
	if err != nil {
		return nil, denied(err)
	}
	return out, nil
}

// ListUsable calls listUsable with "-" standing for any cluster or config,
// as gcloud workstations list-usable does by default.
func (a liveAPI) ListUsable(ctx context.Context, project, location, cluster, config string) ([]Workstation, error) {
	var out []Workstation
	parent := clusterParent(project, location, cluster) + "/workstationConfigs/" + config
	err := a.svc.Projects.Locations.WorkstationClusters.WorkstationConfigs.Workstations.ListUsable(parent).Pages(ctx, func(resp *ws.ListUsableWorkstationsResponse) error {
		for _, w := range resp.Workstations {
			out = append(out, fromWorkstation(w))
		}
		return nil
	})
	if err != nil {
		return nil, denied(err)
	}
	return out, nil
}

func clusterParent(project, location, cluster string) string {
	return "projects/" + project + "/locations/" + location + "/workstationClusters/" + cluster
}

func (a liveAPI) GetConfigIAMPolicy(ctx context.Context, project, location, cluster, config string) ([]Binding, error) {
	name := clusterParent(project, location, cluster) + "/workstationConfigs/" + config
	policy, err := a.svc.Projects.Locations.WorkstationClusters.WorkstationConfigs.GetIamPolicy(name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return fromBindings(policy.Bindings), nil
}

func (a liveAPI) GetWorkstationIAMPolicy(ctx context.Context, project, location, cluster, config, workstation string) ([]Binding, error) {
	name := clusterParent(project, location, cluster) + "/workstationConfigs/" + config + "/workstations/" + workstation
	policy, err := a.svc.Projects.Locations.WorkstationClusters.WorkstationConfigs.Workstations.GetIamPolicy(name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return fromBindings(policy.Bindings), nil
}

func fromBindings(bindings []*ws.Binding) []Binding {
	var out []Binding
	for _, b := range bindings {
		out = append(out, Binding{Role: b.Role, Members: append([]string(nil), b.Members...), Condition: conditionTitle(b.Condition)})
	}
	return out
}

func conditionTitle(e *ws.Expr) string {
	if e == nil {
		return ""
	}
	if e.Title != "" {
		return e.Title
	}
	return e.Expression
}

// denied wraps a 403 with ErrDenied, keeping the Google error for Classify.
func denied(err error) error {
	var gerr *googleapi.Error
	if errors.As(err, &gerr) && gerr.Code == http.StatusForbidden {
		return fmt.Errorf("%w: %w", ErrDenied, err)
	}
	return err
}

func fromCluster(c *ws.WorkstationCluster) Cluster {
	n := splitName(c.Name)
	out := Cluster{
		Name:           n.cluster,
		Location:       n.location,
		DisplayName:    c.DisplayName,
		Network:        c.Network,
		Subnetwork:     c.Subnetwork,
		ControlPlaneIP: c.ControlPlaneIp,
		Degraded:       c.Degraded,
		Reconciling:    c.Reconciling,
		Conditions:     conditions(c.Conditions),
		Created:        parseTime(c.CreateTime),
		Updated:        parseTime(c.UpdateTime),
		Labels:         maps.Clone(c.Labels),
	}
	if p := c.PrivateClusterConfig; p != nil {
		out.PrivateEndpoint = p.EnablePrivateEndpoint
		out.Hostname = p.ClusterHostname
		out.ServiceAttachment = p.ServiceAttachmentUri
	}
	if d := c.DomainConfig; d != nil {
		out.Domain = d.Domain
	}
	return out
}

func fromConfig(c *ws.WorkstationConfig) Config {
	n := splitName(c.Name)
	out := Config{
		Name:           n.config,
		Cluster:        n.cluster,
		Location:       n.location,
		DisplayName:    c.DisplayName,
		IdleTimeout:    c.IdleTimeout,
		RunningTimeout: c.RunningTimeout,
		IdleAction:     c.IdleAction,
		ReplicaZones:   c.ReplicaZones,
		MaxUsable:      c.MaxUsableWorkstations,
		Degraded:       c.Degraded,
		Reconciling:    c.Reconciling,
		Conditions:     conditions(c.Conditions),
		Created:        parseTime(c.CreateTime),
		Updated:        parseTime(c.UpdateTime),
		Labels:         maps.Clone(c.Labels),
	}
	if h := c.Host; h != nil && h.GceInstance != nil {
		g := h.GceInstance
		out.MachineType = g.MachineType
		out.PoolSize = g.PoolSize
		out.PooledInstances = g.PooledInstances
		out.BootDiskGB = g.BootDiskSizeGb
		out.ServiceAccount = g.ServiceAccount
		out.NoPublicIP = g.DisablePublicIpAddresses
		for _, acc := range g.Accelerators {
			out.Accelerators = append(out.Accelerators, acc.Type+" ×"+strconv.FormatInt(acc.Count, 10))
		}
		if sh := g.ShieldedInstanceConfig; sh != nil {
			for _, f := range []struct {
				on   bool
				name string
			}{{sh.EnableSecureBoot, "secure boot"}, {sh.EnableVtpm, "vTPM"}, {sh.EnableIntegrityMonitoring, "integrity monitoring"}} {
				if f.on {
					out.ShieldedVM = append(out.ShieldedVM, f.name)
				}
			}
		}
		out.Confidential = g.ConfidentialInstanceConfig != nil && g.ConfidentialInstanceConfig.EnableConfidentialCompute
		out.NoSSH = g.DisableSsh
		out.NestedVirt = g.EnableNestedVirtualization
		for _, b := range g.BoostConfigs {
			s := b.Id + "  " + b.MachineType
			for _, acc := range b.Accelerators {
				s += ", " + acc.Type + " ×" + strconv.FormatInt(acc.Count, 10)
			}
			out.Boosts = append(out.Boosts, strings.TrimSpace(s))
		}
	}
	for _, pr := range c.AllowedPorts {
		if pr.First == pr.Last {
			out.AllowedPorts = append(out.AllowedPorts, strconv.FormatInt(pr.First, 10))
		} else {
			out.AllowedPorts = append(out.AllowedPorts, strconv.FormatInt(pr.First, 10)+"-"+strconv.FormatInt(pr.Last, 10))
		}
	}
	out.NoTCP = c.DisableTcpConnections
	out.AuditAgent = c.EnableAuditAgent
	if ct := c.Container; ct != nil {
		out.Image = ct.Image
		out.Env = maps.Clone(ct.Env)
	}
	for _, d := range c.PersistentDirectories {
		dir := Directory{MountPath: d.MountPath}
		switch {
		case d.GcePd != nil:
			dir.SizeGB, dir.DiskType, dir.ReclaimPolicy = d.GcePd.SizeGb, d.GcePd.DiskType, d.GcePd.ReclaimPolicy
		case d.GceHd != nil:
			dir.SizeGB, dir.DiskType, dir.ReclaimPolicy = d.GceHd.SizeGb, "hyperdisk-balanced-ha", d.GceHd.ReclaimPolicy
		}
		out.Directories = append(out.Directories, dir)
	}
	if k := c.EncryptionKey; k != nil {
		out.KMSKey = k.KmsKey
	}
	return out
}

func fromWorkstation(w *ws.Workstation) Workstation {
	n := splitName(w.Name)
	out := Workstation{
		Name:        n.workstation,
		Config:      n.config,
		Cluster:     n.cluster,
		Location:    n.location,
		DisplayName: w.DisplayName,
		State:       w.State,
		Reconciling: w.Reconciling,
		Host:        w.Host,
		Env:         maps.Clone(w.Env),
		KMSKey:      w.KmsKey,
		Source:      w.SourceWorkstation,
		Started:     parseTime(w.StartTime),
		Created:     parseTime(w.CreateTime),
		Updated:     parseTime(w.UpdateTime),
		Labels:      maps.Clone(w.Labels),
	}
	for _, d := range w.PersistentDirectories {
		out.Directories = append(out.Directories, Directory{MountPath: d.MountPath, SizeGB: d.SizeGb})
	}
	if rh := w.RuntimeHost; rh != nil && rh.GceInstanceHost != nil && rh.GceInstanceHost.Name != "" {
		out.VM = lastSeg(rh.GceInstanceHost.Zone) + "/" + rh.GceInstanceHost.Name
	}
	return out
}

// name is the segments of
// projects/p/locations/l/workstationClusters/c/workstationConfigs/k/workstations/w.
type name struct{ location, cluster, config, workstation string }

func splitName(s string) name {
	var n name
	segs := strings.Split(s, "/")
	for i := 0; i+1 < len(segs); i += 2 {
		switch segs[i] {
		case "locations":
			n.location = segs[i+1]
		case "workstationClusters":
			n.cluster = segs[i+1]
		case "workstationConfigs":
			n.config = segs[i+1]
		case "workstations":
			n.workstation = segs[i+1]
		}
	}
	return n
}

func conditions(cs []*ws.Status) []string {
	var out []string
	for _, c := range cs {
		if c != nil && c.Message != "" {
			out = append(out, c.Message)
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
