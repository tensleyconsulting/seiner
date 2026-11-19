package networkconnectivity

import (
	"context"
	"time"
)

// RegionalEndpoint is the Google-free slice of a PSC Regional Endpoint for
// regional or multi-regional Google APIs.
type RegionalEndpoint struct {
	Name            string // short name, not the full resource name
	Location        string
	TargetGoogleAPI string
	AccessType      string // REGIONAL, GLOBAL
	IPAddress       string
	AddressName     string // backing Compute address name, when Address is a resource URL
	Network         string
	Subnetwork      string
	ForwardingRule  string
	Description     string
	Created         time.Time
	Updated         time.Time
	Labels          map[string]string
}

// ConnectionPolicy is the Google-free slice of a Service Connection Policy
// that authorizes managed service producers to create PSC endpoints.
type ConnectionPolicy struct {
	Name             string // short name, not the full resource name
	Location         string
	ServiceClass     string
	Infrastructure   string // PSC
	Network          string
	Subnetworks      []string
	ConnectionLimit  int64
	ProducerLocation string // CUSTOM_RESOURCE_HIERARCHY_LEVELS, PRODUCER_INSTANCE_LOCATION_UNSPECIFIED, ...
	AllowedProducers []string
	Connections      []Connection
	Description      string
	Created          time.Time
	Updated          time.Time
	Labels           map[string]string
}

// Connection is one producer-created PSC connection recorded on a
// ServiceConnectionPolicy.
type Connection struct {
	ID                 string // pscConnectionId
	State              string // ACTIVE, CREATING, DELETING, FAILED, PRODUCER_REPAIRING, PRODUCER_DELETING
	ConsumerProject    string
	ForwardingRule     string
	ConsumerAddress    string
	Subnetwork         string
	ProducerInstanceID string
	ServiceClass       string
	IPVersion          string
	ErrorType          string
	Error              string
}

// API is the Network Connectivity ↔ Resource boundary. Tests inject a fake;
// live wraps the Network Connectivity v1 REST client.
type API interface {
	ListRegionalEndpoints(ctx context.Context, project string) ([]RegionalEndpoint, error)
	ListConnectionPolicies(ctx context.Context, project string) ([]ConnectionPolicy, error)
	GetConnectionPolicy(ctx context.Context, project, location, name string) (ConnectionPolicy, error)
}
