package clouddns

import (
	"context"
	"time"
)

// Zone is the Google-free slice of a Cloud DNS managed zone the adapter needs.
type Zone struct {
	Name        string // the zone's resource name (example-com), not its DNS name
	DNSName     string // example.com.
	Description string
	Visibility  string // "public" or "private"
	NameServers []string
	DNSSEC      string   // "on", "off", or "transfer"; "" when not configured
	Networks    []string // network names a private zone is visible to
	GKEClusters []string // cluster names a private zone is visible to
	Forwarding  []string // forwarding target addresses
	PeerNetwork string   // network a peering zone resolves through
	Logging     bool
	Created     time.Time
	Labels      map[string]string
}

// Record is one resource record set in a zone.
type Record struct {
	Name    string // fully qualified, with the trailing dot (www.example.com.)
	Type    string // A, AAAA, CNAME, MX, ...
	TTL     int64
	Data    []string // rrdatas, in API order
	Routing string   // "geo", "weighted round robin", "failover"; "" for plain data
}

// API is the Cloud DNS ↔ Resource boundary. Tests inject a fake; live wraps
// the Cloud DNS REST client.
type API interface {
	ListZones(ctx context.Context, project string) ([]Zone, error)
	// ListRecords stops after limit record sets and reports whether more
	// were left.
	ListRecords(ctx context.Context, project, zone string, limit int) ([]Record, bool, error)
}
