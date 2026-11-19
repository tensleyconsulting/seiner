package workstations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/fanout"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// locationConcurrency bounds the per-location list calls, as in certmanager.
const locationConcurrency = 24

// usableNote marks a cluster or configuration known only from the caller's
// usable workstations: the caller may not list it, so it has no details.
const usableNote = "only the workstations you can use"

// Provider implements model.ResourceProvider for Cloud Workstations. It
// lists clusters; a cluster expands to its configurations and a
// configuration to its workstations.
//
// A workstation user may hold no list permission at all, only use of their
// own workstation. Where a list is denied, the provider lists the
// workstations the caller can use instead (listUsable) and builds the
// clusters and configurations above them from their names. So it declares
// no ListPermissions: the catalog's project-level check would hide those
// users' workstations.
type Provider struct{ api API }

func New(api API) Provider { return Provider{api: api} }

func (p Provider) Service() model.ServiceType { return model.ServiceWorkstationCluster }

// List lists clusters in every Workstations location. Locations that fail
// are ignored so restricted or unreachable regions never surface as errors;
// List fails only when every location fails or ctx ends.
func (p Provider) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("workstations: no API")
	}
	locs, err := p.api.Locations(ctx, project)
	if err != nil {
		return nil, err
	}
	items, locErrs := fanout.Regions(ctx, locs, locationConcurrency, func(ctx context.Context, loc string) ([]model.Resource, error) {
		clusters, err := p.api.ListClusters(ctx, project, loc)
		if errors.Is(err, ErrDenied) {
			ws, err := p.api.ListUsable(ctx, project, loc, Any, Any)
			if err != nil {
				return nil, err
			}
			return usableClusters(project, loc, ws), nil
		}
		if err != nil {
			return nil, err
		}
		out := make([]model.Resource, 0, len(clusters))
		for _, c := range clusters {
			out = append(out, clusterResource(project, c))
		}
		return out, nil
	})
	if len(locErrs) > 0 {
		if len(locErrs) == len(locs) {
			return nil, fmt.Errorf("workstations: every location failed: %w", locErrs[0])
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		slog.Debug("workstations: some locations failed", "project", project, "failed", len(locErrs), "locations", len(locs), "err", locErrs[0])
	}
	return items, nil
}

// ListRelated lists a cluster's configurations or a configuration's
// workstations.
func (p Provider) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	var want model.ServiceType
	switch facetID {
	case model.FacetConfigs:
		want = model.ServiceWorkstationCluster
	case model.FacetWorkstations:
		want = model.ServiceWorkstationConfig
	default:
		return nil, false, nil
	}
	if parent.Service != want {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "workstationClusters" {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("workstations: no API")
	}
	var out []model.Resource
	if facetID == model.FacetConfigs {
		if len(n.Rest) != 0 {
			return nil, false, nil
		}
		configs, err := p.api.ListConfigs(ctx, n.Project, n.Location, n.Name)
		switch {
		case errors.Is(err, ErrDenied):
			ws, err := p.api.ListUsable(ctx, n.Project, n.Location, n.Name, Any)
			if err != nil {
				return nil, false, err
			}
			out = usableConfigs(n.Project, n.Location, ws)
		case err != nil:
			return nil, false, err
		default:
			for _, c := range configs {
				out = append(out, configResource(n.Project, c))
			}
		}
	} else {
		if len(n.Rest) != 2 || n.Rest[0] != "workstationConfigs" {
			return nil, false, nil
		}
		config := n.Rest[1]
		ws, err := p.api.ListWorkstations(ctx, n.Project, n.Location, n.Name, config)
		if errors.Is(err, ErrDenied) {
			ws, err = p.api.ListUsable(ctx, n.Project, n.Location, n.Name, config)
		}
		if err != nil {
			return nil, false, err
		}
		for _, w := range ws {
			out = append(out, workstationResource(n.Project, w))
		}
	}
	for i := range out {
		out[i].ParentID = parent.ID
		out[i].FacetID = facetID
	}
	return out, false, nil
}

func clusterID(project, location, cluster string) string {
	return "projects/" + project + "/locations/" + url.PathEscape(location) + "/workstationClusters/" + url.PathEscape(cluster)
}

func configID(project, location, cluster, config string) string {
	return clusterID(project, location, cluster) + "/workstationConfigs/" + url.PathEscape(config)
}

var configsFacet = []model.Facet{{ID: model.FacetConfigs, Label: "Configurations", Inline: true}}

var workstationsFacet = []model.Facet{{ID: model.FacetWorkstations, Label: "Workstations", Inline: true}}

func clusterResource(project string, c Cluster) model.Resource {
	md := map[string]string{}
	set(md, "displayName", c.DisplayName)
	set(md, "network", lastSeg(c.Network))
	set(md, "subnetwork", lastSeg(c.Subnetwork))
	if c.PrivateEndpoint {
		md["privateEndpoint"] = "on"
		set(md, "hostname", c.Hostname)
		set(md, "serviceAttachment", c.ServiceAttachment)
	}
	set(md, "controlPlaneIp", c.ControlPlaneIP)
	set(md, "domain", c.Domain)
	set(md, "condition", strings.Join(c.Conditions, "; "))
	set(md, "updated", timeWord(c.Updated))
	return model.Resource{
		ID:       clusterID(project, c.Location, c.Name),
		Name:     c.Name,
		Service:  model.ServiceWorkstationCluster,
		ScopeID:  "projects/" + project,
		Location: c.Location,
		Status:   reconcileStatus(c.Degraded, c.Reconciling),
		Created:  c.Created,
		Labels:   maps.Clone(c.Labels),
		Metadata: md,
		Fields: model.FieldsOf(md, "displayName", "network", "subnetwork", "privateEndpoint", "hostname",
			"serviceAttachment", "controlPlaneIp", "domain", "condition", "updated"),
		Facets: configsFacet,
	}
}

func configResource(project string, c Config) model.Resource {
	md := map[string]string{}
	set(md, "displayName", c.DisplayName)
	set(md, "machineType", c.MachineType)
	if c.PoolSize > 0 {
		md["pool"] = fmt.Sprintf("%d (%d ready)", c.PoolSize, c.PooledInstances)
	}
	if c.BootDiskGB > 0 {
		md["bootDisk"] = strconv.FormatInt(c.BootDiskGB, 10) + " GB"
	}
	set(md, "accelerators", strings.Join(c.Accelerators, ", "))
	set(md, "image", c.Image)
	set(md, "serviceAccount", c.ServiceAccount)
	md["publicIp"] = onOff(!c.NoPublicIP)
	if c.NoSSH {
		md["ssh"] = "off"
	}
	if c.NoTCP {
		md["tcpConnections"] = "off"
	}
	set(md, "allowedPorts", strings.Join(c.AllowedPorts, ", "))
	set(md, "shieldedVM", strings.Join(c.ShieldedVM, ", "))
	if c.Confidential {
		md["confidentialVM"] = "on"
	}
	if c.NestedVirt {
		md["nestedVirtualization"] = "on"
	}
	if c.AuditAgent {
		md["auditAgent"] = "on"
	}
	if len(c.Boosts) > 0 {
		md["boostConfigs"] = strconv.Itoa(len(c.Boosts))
	}
	set(md, "idleTimeout", duration(c.IdleTimeout))
	set(md, "idleAction", c.IdleAction)
	set(md, "runningTimeout", duration(c.RunningTimeout))
	set(md, "persistentDirectories", directories(c.Directories))
	set(md, "replicaZones", strings.Join(c.ReplicaZones, ", "))
	set(md, "encryption", c.KMSKey)
	if c.MaxUsable > 0 {
		md["maxUsable"] = strconv.FormatInt(c.MaxUsable, 10)
	}
	set(md, "condition", strings.Join(c.Conditions, "; "))
	set(md, "updated", timeWord(c.Updated))
	r := model.Resource{
		ID:       configID(project, c.Location, c.Cluster, c.Name),
		Name:     c.Name,
		Service:  model.ServiceWorkstationConfig,
		ScopeID:  "projects/" + project,
		Location: c.Location,
		Status:   reconcileStatus(c.Degraded, c.Reconciling),
		Created:  c.Created,
		Labels:   maps.Clone(c.Labels),
		Metadata: md,
		Fields: model.FieldsOf(md, "displayName", "machineType", "pool", "bootDisk", "accelerators", "boostConfigs", "image",
			"serviceAccount", "publicIp", "ssh", "tcpConnections", "allowedPorts", "shieldedVM", "confidentialVM",
			"nestedVirtualization", "auditAgent", "idleTimeout", "idleAction", "runningTimeout", "persistentDirectories",
			"replicaZones", "encryption", "maxUsable", "condition", "updated"),
		Facets: workstationsFacet,
	}
	if l, ok := envList(c.Env); ok {
		r.Lists = []model.List{l}
	}
	if len(c.Boosts) > 0 {
		r.Lists = append(r.Lists, model.List{ID: "boostConfigs", Label: "Boost configs", Items: slices.Clone(c.Boosts)})
	}
	return r
}

func workstationResource(project string, w Workstation) model.Resource {
	md := map[string]string{}
	set(md, "displayName", w.DisplayName)
	set(md, "host", w.Host)
	set(md, "vm", w.VM)
	set(md, "started", timeWord(w.Started))
	set(md, "persistentDirectories", directories(w.Directories))
	set(md, "source", w.Source)
	set(md, "encryption", w.KMSKey)
	set(md, "updated", timeWord(w.Updated))
	state := strings.TrimPrefix(w.State, "STATE_")
	if state == "UNSPECIFIED" {
		state = ""
	}
	r := model.Resource{
		ID:       configID(project, w.Location, w.Cluster, w.Config) + "/workstations/" + url.PathEscape(w.Name),
		Name:     w.Name,
		Service:  model.ServiceWorkstation,
		ScopeID:  "projects/" + project,
		Location: w.Location,
		Status:   workstationStatus(state, w.Reconciling),
		State:    state,
		Created:  w.Created,
		Labels:   maps.Clone(w.Labels),
		Metadata: md,
		Fields:   model.FieldsOf(md, "displayName", "host", "vm", "started", "persistentDirectories", "source", "encryption", "updated"),
	}
	if l, ok := envList(w.Env); ok {
		r.Lists = []model.List{l}
	}
	return r
}

// usableClusters is one row per cluster holding a usable workstation, in
// first-seen order.
func usableClusters(project, location string, ws []Workstation) []model.Resource {
	var out []model.Resource
	seen := map[string]bool{}
	for _, w := range ws {
		if seen[w.Cluster] {
			continue
		}
		seen[w.Cluster] = true
		out = append(out, usableNode(clusterID(project, location, w.Cluster), w.Cluster, model.ServiceWorkstationCluster, project, location, configsFacet))
	}
	return out
}

// usableConfigs is one row per configuration holding a usable workstation.
func usableConfigs(project, location string, ws []Workstation) []model.Resource {
	var out []model.Resource
	seen := map[string]bool{}
	for _, w := range ws {
		if seen[w.Config] {
			continue
		}
		seen[w.Config] = true
		out = append(out, usableNode(configID(project, location, w.Cluster, w.Config), w.Config, model.ServiceWorkstationConfig, project, location, workstationsFacet))
	}
	return out
}

func usableNode(id, name string, svc model.ServiceType, project, location string, facets []model.Facet) model.Resource {
	md := map[string]string{"listing": usableNote}
	return model.Resource{
		ID:       id,
		Name:     name,
		Service:  svc,
		ScopeID:  "projects/" + project,
		Location: location,
		Metadata: md,
		Fields:   model.FieldsOf(md),
		Facets:   facets,
	}
}

// reconcileStatus is a cluster's or configuration's health: an error while
// degraded, a warning while reconciling. Neither has a state of its own.
func reconcileStatus(degraded, reconciling bool) model.ResourceStatus {
	switch {
	case degraded:
		return model.StatusError
	case reconciling:
		return model.StatusWarning
	}
	return ""
}

// workstationStatus maps a workstation's state, without the STATE_
// prefix. Stopped and suspended are at rest, not unhealthy.
func workstationStatus(state string, reconciling bool) model.ResourceStatus {
	switch state {
	case "STARTING", "STOPPING", "SUSPENDING":
		return model.StatusWarning
	case "RUNNING":
		if reconciling {
			return model.StatusWarning
		}
		return model.StatusHealthy
	case "STOPPED", "SUSPENDED":
		return ""
	}
	return model.StatusUnknown
}

// envList is env as "KEY=value" lines sorted by key.
func envList(env map[string]string) (model.List, bool) {
	if len(env) == 0 {
		return model.List{}, false
	}
	items := make([]string, 0, len(env))
	for _, k := range slices.Sorted(maps.Keys(env)) {
		items = append(items, k+"="+env[k])
	}
	return model.List{ID: "env", Label: "Environment", Items: items}, true
}

// directories is "/home 200 GB pd-balanced, RETAIN" per directory.
func directories(ds []Directory) string {
	parts := make([]string, 0, len(ds))
	for _, d := range ds {
		s := d.MountPath
		if d.SizeGB > 0 {
			s += " " + strconv.FormatInt(d.SizeGB, 10) + " GB"
		}
		if d.DiskType != "" {
			s += " " + d.DiskType
		}
		if d.ReclaimPolicy != "" {
			s += ", " + d.ReclaimPolicy
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

// duration turns the API's "1200s" into "20m0s"; anything else is kept.
func duration(s string) string {
	if s == "" {
		return ""
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return s
	}
	if d == 0 {
		return "off"
	}
	return d.String()
}

func timeWord(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

var (
	_ model.ResourceProvider = Provider{}
	_ model.RelatedLister    = Provider{}
)
