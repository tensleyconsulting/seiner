package gke

import (
	"context"
	"time"
)

// Cluster is the Google-free slice of a GKE cluster the adapter needs.
type Cluster struct {
	Name           string
	Location       string // region or zone
	Description    string
	Status         string // the API's enum word, e.g. "RUNNING"
	StatusMessage  string
	Autopilot      bool
	MasterVersion  string
	ReleaseChannel string // "" or UNSPECIFIED when the cluster has none
	NodeCount      int64
	NodePools      []string // pool names, in API order
	Endpoint       string
	PrivateNodes   bool
	Network        string
	Subnetwork     string
	PodRange       string
	ServicesRange  string
	WorkloadPool   string
	Created        time.Time
	Labels         map[string]string

	NodeVersion        string // current node version; differs from MasterVersion mid-upgrade
	EtcdKey            string // CMEK for etcd (application-layer secrets encryption); "" when off
	Maintenance        string // the maintenance window, as read: "daily 03:00 UTC (4h)", ...
	Exclusions         []string
	AuthorizedNetworks bool // master authorized networks are enforced
	AuthorizedCIDRs    []string
	PublicEndpoint     string // "on", "off", or "" when the API does not say
	DNSEndpoint        string
	DNSExternal        bool   // the DNS endpoint accepts traffic from outside Google Cloud
	Dataplane          string // DatapathProvider: ADVANCED_DATAPATH (Dataplane V2), LEGACY_DATAPATH
	ShieldedNodes      bool
	BinAuthz           string // Binary Authorization evaluation mode
}

// NodePool is one node pool of a cluster.
type NodePool struct {
	Name          string
	Status        string // the API's enum word, e.g. "RUNNING"
	StatusMessage string
	Version       string
	MachineType   string
	Accelerators  []Accelerator
	// Autoscaling is on when Autoscaling is true. MinNodes/MaxNodes are per
	// zone; TotalMin/TotalMax are across zones and win when TotalMax > 0.
	Autoscaling    bool
	MinNodes       int64
	MaxNodes       int64
	TotalMin       int64
	TotalMax       int64
	InitialNodes   int64 // per zone, at creation; resizes do not update it
	Zones          []string
	DiskSizeGB     int64
	DiskType       string
	ImageType      string
	Spot           bool
	Preemptible    bool
	ServiceAccount string
	AutoUpgrade    bool
	AutoRepair     bool
	MaxPodsPerNode int64
	// InstanceGroups are the pool's instance group managers, one per zone,
	// as relative names (projects/p/zones/z/instanceGroupManagers/n).
	InstanceGroups []string

	Taints         []string // key=value:EFFECT
	NodeLabels     []string // Kubernetes node labels, key=value
	KMSKey         string   // boot disk CMEK
	MetadataServer string   // GKE_METADATA or GCE_METADATA
	Sandbox        string   // GVISOR
	SecureBoot     bool
	Integrity      bool   // integrity monitoring
	UpgradeType    string // SURGE or BLUE_GREEN
	MaxSurge       int64
	MaxUnavailable int64
}

// Accelerator is a GPU or TPU attached to each node of a pool.
type Accelerator struct {
	Type  string
	Count int64
}

// API is the GKE ↔ Resource boundary. Tests inject a fake; live wraps the
// Container REST client.
type API interface {
	// ListClusters lists clusters in every location of the project.
	ListClusters(ctx context.Context, project string) ([]Cluster, error)
	ListNodePools(ctx context.Context, project, location, cluster string) ([]NodePool, error)
}
