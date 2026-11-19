package memorystore

import (
	"context"
	"time"
)

// RedisInstance is the Google-free slice of a Memorystore for Redis instance
// the adapter needs.
type RedisInstance struct {
	Name              string // short name, not the full resource name
	Region            string
	Zone              string // zone serving the primary
	AlternativeZone   string // standby zone of a Standard tier instance
	DisplayName       string
	State             string // the API's enum word, e.g. "READY"
	StatusMessage     string
	Tier              string // BASIC or STANDARD_HA
	MemoryGB          int64
	Version           string // REDIS_7_2
	Host              string
	Port              int64
	ReadEndpoint      string
	ReadEndpointPort  int64
	ReplicaCount      int64
	Network           string // network name
	ConnectMode       string // DIRECT_PEERING or PRIVATE_SERVICE_ACCESS
	ReservedIPRange   string
	AuthEnabled       bool
	TransitEncryption string // SERVER_AUTHENTICATION or DISABLED
	Persistence       string // DISABLED or RDB
	CMEK              string
	Created           time.Time
	Labels            map[string]string

	Maintenance     string // "SUNDAY 03:00 UTC (1h)"; windows joined with ", "
	NextMaintenance time.Time
	RDBPeriod       string // ONE_HOUR, SIX_HOURS, TWELVE_HOURS, TWENTY_FOUR_HOURS
	NextSnapshot    time.Time
}

// ValkeyInstance is the Google-free slice of a Memorystore for Valkey
// instance the adapter needs.
type ValkeyInstance struct {
	Name               string // short name, not the full resource name
	Region             string
	State              string // the API's enum word, e.g. "ACTIVE"
	EngineVersion      string // VALKEY_8_0
	Mode               string // CLUSTER, CLUSTER_DISABLED
	NodeType           string // HIGHMEM_MEDIUM
	NodeSizeGB         float64
	ShardCount         int32
	ReplicaCount       int32
	ZoneMode           string // MULTI_ZONE or SINGLE_ZONE
	Zone               string // the zone of a SINGLE_ZONE instance
	Authorization      string // AUTH_DISABLED, IAM_AUTH
	TransitEncryption  string // TRANSIT_ENCRYPTION_DISABLED, SERVER_AUTHENTICATION
	Persistence        string // DISABLED, RDB, AOF
	DeletionProtection bool
	Endpoints          []Endpoint
	Created            time.Time
	Labels             map[string]string

	KMSKey          string
	Backups         string // "daily 03:00 UTC, keep 7 days", or "" when automated backups are off
	Maintenance     string // "SUNDAY 03:00 UTC"; windows joined with ", "
	NextMaintenance time.Time
	ReplicationRole string // PRIMARY, SECONDARY, or NONE
	ReplicationPeer string // the primary of a secondary, or the secondaries of a primary
}

// Endpoint is one address clients connect to.
type Endpoint struct {
	Type    string // DISCOVERY, PRIMARY, READER
	Address string
	Port    int32
	Network string // network name
}

// RedisAPI is the Memorystore for Redis ↔ Resource boundary. Tests inject a
// fake; live wraps the Redis REST client.
type RedisAPI interface {
	// ListRedisInstances lists instances in every location of the project.
	ListRedisInstances(ctx context.Context, project string) ([]RedisInstance, error)
}

// ValkeyAPI is the Memorystore for Valkey ↔ Resource boundary. Tests inject
// a fake; live wraps the Memorystore client.
type ValkeyAPI interface {
	// ListValkeyInstances lists instances in every location of the project.
	ListValkeyInstances(ctx context.Context, project string) ([]ValkeyInstance, error)
}
