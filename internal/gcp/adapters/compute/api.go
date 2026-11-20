package compute

import "context"

// Binding is one IAM role binding on a Compute Engine resource.
type Binding struct {
	Role      string
	Members   []string
	Condition string // condition title; "" when unconditional
}

// API is the Compute Engine ↔ Resource boundary.
type API interface {
	ListNetworks(ctx context.Context, project string) ([]Network, error)
	ListSubnets(ctx context.Context, project string) ([]Subnet, error)
	ListFirewalls(ctx context.Context, project string) ([]Firewall, error)
	ListInstances(ctx context.Context, project string) ([]Instance, error)
	// Addresses, forwarding rules (PSC endpoints among them), and service
	// attachments list global and regional items together. region is ""
	// for a global item.
	ListAddresses(ctx context.Context, project string) ([]Address, error)
	ListForwardingRules(ctx context.Context, project string) ([]ForwardingRule, error)
	ListServiceAttachments(ctx context.Context, project string) ([]ServiceAttachment, error)
	// Load balancer parts, global and regional together. ListTargetProxies
	// returns every proxy kind (HTTP, HTTPS, TCP, SSL, gRPC).
	ListURLMaps(ctx context.Context, project string) ([]URLMap, error)
	ListBackendServices(ctx context.Context, project string) ([]BackendService, error)
	ListTargetProxies(ctx context.Context, project string) ([]TargetProxy, error)
	ListHealthChecks(ctx context.Context, project string) ([]HealthCheck, error)
	ListSecurityPolicies(ctx context.Context, project string) ([]SecurityPolicy, error)
	ListSSLPolicies(ctx context.Context, project string) ([]SSLPolicy, error)
	// Disks, templates, and groups list zonal, regional, and global items
	// together.
	ListDisks(ctx context.Context, project string) ([]Disk, error)
	ListInstanceTemplates(ctx context.Context, project string) ([]InstanceTemplate, error)
	ListInstanceGroups(ctx context.Context, project string) ([]InstanceGroup, error)
	// ListManagedInstances lists a managed instance group's instances,
	// stopping after limit. Exactly one of zone and region is set.
	ListManagedInstances(ctx context.Context, project, zone, region, group string, limit int) ([]ManagedInstance, bool, error)
	ListRoutes(ctx context.Context, project string) ([]Route, error)
	ListRouters(ctx context.Context, project string) ([]Router, error)
	GetRouter(ctx context.Context, project, region, name string) (Router, error)
	// GetIAMPolicy reads the bindings set directly on a Compute resource.
	// kind is the Compute collection (instances, subnetworks, disks,
	// instanceTemplates, backendServices, serviceAttachments).
	GetIAMPolicy(ctx context.Context, project, kind, zone, region, name string) ([]Binding, error)
}
