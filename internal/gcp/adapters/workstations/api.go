package workstations

import (
	"context"
	"errors"
	"time"
)

// ErrDenied marks a list the caller lacks permission for. Live wraps the
// Google error with it, so the provider can fall back to the workstations
// the caller can use and gcp.Classify still reads the original.
var ErrDenied = errors.New("workstations: permission denied")

// Any stands for every cluster or config in a ListUsable parent.
const Any = "-"

// Cluster is the Google-free slice of a workstation cluster.
type Cluster struct {
	Name              string // short name
	Location          string
	DisplayName       string
	Network           string // full name
	Subnetwork        string // full name
	ControlPlaneIP    string
	PrivateEndpoint   bool
	Hostname          string // private cluster hostname
	ServiceAttachment string
	Domain            string
	Degraded          bool
	Reconciling       bool
	Conditions        []string // condition messages
	Created           time.Time
	Updated           time.Time
	Labels            map[string]string
}

// Config is the Google-free slice of a workstation configuration.
type Config struct {
	Name            string // short name
	Cluster         string // short name
	Location        string
	DisplayName     string
	MachineType     string
	PoolSize        int64
	PooledInstances int64
	BootDiskGB      int64
	ServiceAccount  string
	NoPublicIP      bool
	Accelerators    []string // "nvidia-tesla-t4 ×1"
	Image           string
	Env             map[string]string // container environment
	IdleTimeout     string            // API duration, "1200s"
	RunningTimeout  string
	IdleAction      string // STOP or SUSPEND; "" for STOP
	Directories     []Directory
	ReplicaZones    []string
	KMSKey          string
	MaxUsable       int64    // workstations a user may use; 0 is unlimited
	ShieldedVM      []string // "secure boot", "vTPM", "integrity monitoring"
	Confidential    bool
	NoSSH           bool
	NestedVirt      bool
	AllowedPorts    []string // "22", "1024-65535"
	NoTCP           bool     // plain TCP connections (SSH) are off
	AuditAgent      bool
	Boosts          []string // "gpu  n1-standard-8, nvidia-tesla-t4 ×1"
	Degraded        bool
	Reconciling     bool
	Conditions      []string
	Created         time.Time
	Updated         time.Time
	Labels          map[string]string
}

// Directory is a persistent directory a config mounts.
type Directory struct {
	MountPath     string
	SizeGB        int64
	DiskType      string // pd-standard, pd-ssd, hyperdisk-balanced-ha
	ReclaimPolicy string // DELETE or RETAIN
}

// Workstation is the Google-free slice of a workstation.
type Workstation struct {
	Name        string // short name
	Config      string // short name
	Cluster     string // short name
	Location    string
	DisplayName string
	State       string // STATE_RUNNING, STATE_STOPPED, …
	Reconciling bool
	Host        string
	Env         map[string]string
	KMSKey      string
	Source      string // full name of the workstation it was copied from
	VM          string // the running workstation's VM: "zone/name"
	Directories []Directory
	Started     time.Time
	Created     time.Time
	Updated     time.Time
	Labels      map[string]string
}

// Binding is one IAM role binding on a workstation config or workstation.
type Binding struct {
	Role      string
	Members   []string
	Condition string // condition title, "" when unconditional
}

// API is the Workstations ↔ Resource boundary. Tests inject a fake; live
// wraps the Workstations REST client. The List methods wrap a permission
// error with ErrDenied.
type API interface {
	// Locations lists the locations Cloud Workstations serves for project.
	Locations(ctx context.Context, project string) ([]string, error)
	ListClusters(ctx context.Context, project, location string) ([]Cluster, error)
	ListConfigs(ctx context.Context, project, location, cluster string) ([]Config, error)
	ListWorkstations(ctx context.Context, project, location, cluster, config string) ([]Workstation, error)
	// ListUsable lists the workstations under cluster and config that the
	// caller may use. Either may be Any.
	ListUsable(ctx context.Context, project, location, cluster, config string) ([]Workstation, error)
	// GetConfigIAMPolicy reads the bindings set directly on a workstation
	// configuration.
	GetConfigIAMPolicy(ctx context.Context, project, location, cluster, config string) ([]Binding, error)
	// GetWorkstationIAMPolicy reads the bindings set directly on a
	// workstation.
	GetWorkstationIAMPolicy(ctx context.Context, project, location, cluster, config, workstation string) ([]Binding, error)
}
