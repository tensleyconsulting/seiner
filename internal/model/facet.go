package model

// Facet is a named drill off a resource (permissions, objects, …).
// Adapters set these; the UI must not infer them from ServiceType.
type Facet struct {
	ID    string
	Label string
	// Nested marks a facet whose children can themselves have children of
	// the same facet (GCS object prefixes). The UI nests rows under the
	// facet it belongs to instead of a flat list.
	Nested bool
	// Inline lists the facet's children directly under the resource, with
	// no facet row in between: the resource itself expands to them. It
	// applies only when the facet is the resource's sole facet.
	Inline bool
	// Optional hides the facet row in the explorer when it loads with no
	// children and no errors. Expanding a resource with Optional facets
	// prefetches them so empty ones never appear.
	Optional bool
}

// InlineFacet returns r's facet when r lists its children inline: r has
// exactly one facet and it is Inline.
func (r Resource) InlineFacet() (Facet, bool) {
	if len(r.Facets) == 1 && r.Facets[0].Inline {
		return r.Facets[0], true
	}
	return Facet{}, false
}

const (
	FacetPermissions            = "permissions"
	FacetObjects                = "objects"
	FacetSubnets                = "subnets"
	FacetFirewalls              = "firewalls"
	FacetNodePools              = "node-pools"
	FacetNodes                  = "nodes"
	FacetInstances              = "instances"
	FacetEnabledServices        = "enabled-services"
	FacetPackages               = "packages"
	FacetVersions               = "versions"
	FacetSignatureTags          = "signature-tags"
	FacetSignatures             = "signatures"
	FacetArtifacts              = "artifacts"
	FacetVulnerabilities        = "vulnerabilities"
	FacetFrontends              = "frontends"
	FacetBackends               = "backends"
	FacetConnections            = "connections"
	FacetHealthChecks           = "health-checks"
	FacetRoutes                 = "routes"
	FacetNATs                   = "nats"
	FacetRecords                = "records"
	FacetDatabases              = "databases"
	FacetUsers                  = "users"
	FacetBackups                = "backups"
	FacetKeys                   = "keys"
	FacetImportJobs             = "import-jobs"
	FacetAttestation            = "attestation"
	FacetRules                  = "rules"
	FacetURLLists               = "url-lists"
	FacetViews                  = "views"
	FacetLinks                  = "links"
	FacetPerimeters             = "perimeters"
	FacetAccessLevels           = "access-levels"
	FacetIngressRules           = "ingress-rules"
	FacetEgressRules            = "egress-rules"
	FacetMapEntries             = "map-entries"
	FacetPolicyValues           = "policy-values"
	FacetConfigs                = "configs"
	FacetWorkstations           = "workstations"
	FacetTables                 = "tables"
	FacetRoutines               = "routines"
	FacetModels                 = "models"
	FacetAssignments            = "assignments"
	FacetMembers                = "members"
	FacetCertificateAuthorities = "certificate-authorities"
	FacetCertificates           = "certificates"
	FacetCRLs                   = "crls"
	// FacetRolePermissions is the permissions an IAM role grants, on a
	// custom role or a role binding. FacetPermissions is a resource's
	// own bindings.
	FacetRolePermissions = "role-permissions"
)

// ListInstanceGroups lists the managed instance groups behind a resource
// (a GKE node pool) as full relative names
// (projects/p/zones/z/instanceGroupManagers/n). Compute lists their
// instances for the resource's FacetNodes.
const ListInstanceGroups = "instanceGroups"

// ListURLs is a Secure Web Proxy URL list's pattern entries.
const ListURLs = "values"

// Children returns nested resources for a parent facet.
func Children(resources []Resource, parentID, facetID string) []Resource {
	out := make([]Resource, 0)
	for _, r := range resources {
		if r.ParentID != parentID {
			continue
		}
		if facetID != "" && r.FacetID != facetID {
			continue
		}
		out = append(out, r)
	}
	return out
}
