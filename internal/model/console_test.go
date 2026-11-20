package model

import (
	"strings"
	"testing"
)

func TestConsoleURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		r    Resource
		want []string
	}{
		{
			name: "bucket",
			r: Resource{
				ID:       "projects/demo-project/buckets/my-app-bucket",
				Name:     "my-app-bucket",
				Service:  ServiceCloudStorage,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{
				"https://console.cloud.google.com/storage/browser/my-app-bucket",
				"project=demo-project",
			},
		},
		{
			name: "gke",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/clusters/production-cluster",
				Name:     "production-cluster",
				Service:  ServiceGKE,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{
				"/kubernetes/clusters/details/us-central1/production-cluster/details",
				"project=demo-project",
			},
		},
		{
			name: "gke node pool",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/clusters/production-cluster/nodePools/default-pool",
				Name:     "default-pool",
				Service:  ServiceGKENodePool,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{
				"/kubernetes/nodepool/us-central1/production-cluster/default-pool",
				"project=demo-project",
			},
		},
		{
			name: "docker repository",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/repositories/app-images",
				Name:     "app-images",
				Service:  ServiceArtifactRegistry,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
				Metadata: map[string]string{MetaArtifactFormat: "DOCKER"},
			},
			want: []string{"https://us-central1-docker.pkg.dev/demo-project/app-images"},
		},
		{
			name: "docker image digest",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/repositories/app-images/packages/web%2Ffrontend/versions/sha256:abc",
				Name:     "latest",
				Service:  ServiceArtifactVersion,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
				Metadata: map[string]string{MetaArtifactFormat: "DOCKER"},
			},
			want: []string{"https://us-central1-docker.pkg.dev/demo-project/app-images/web/frontend@sha256:abc"},
		},
		{
			name: "python package links to its repository",
			r: Resource{
				ID:       "projects/example.com:demo/locations/us/repositories/py/packages/example-utils",
				Name:     "example-utils",
				Service:  ServiceArtifactPackage,
				ScopeID:  "projects/example.com:demo",
				Location: "us",
				Metadata: map[string]string{MetaArtifactFormat: "PYTHON"},
			},
			want: []string{"https://us-python.pkg.dev/example.com/demo/py"},
		},
		{
			name: "artifact without format",
			r: Resource{
				ID:      "projects/demo-project/locations/us/repositories/py",
				Name:    "py",
				Service: ServiceArtifactRegistry,
				ScopeID: "projects/demo-project",
			},
			want: []string{"https://console.cloud.google.com/artifacts?", "project=demo-project"},
		},
		{
			name: "vulnerability",
			r: Resource{
				ID:       "projects/demo-project/occurrences/o1",
				Name:     "CVE-2023-44487",
				Service:  ServiceVulnerability,
				ScopeID:  "projects/demo-project",
				Metadata: map[string]string{MetaArtifactImage: "us-central1-docker.pkg.dev/demo-project/app-images/api@sha256:abc"},
			},
			want: []string{"https://us-central1-docker.pkg.dev/demo-project/app-images/api@sha256:abc"},
		},
		{
			name: "container signature",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/repositories/app-images/packages/web%2Ffrontend/versions/sha256:abc/signatures/sha256:sig",
				Name:     "release@example.com",
				Service:  ServiceArtifactSignature,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
				Metadata: map[string]string{MetaArtifactImage: "us-central1-docker.pkg.dev/demo-project/app-images/web/frontend@sha256:abc"},
			},
			want: []string{"https://us-central1-docker.pkg.dev/demo-project/app-images/web/frontend@sha256:abc"},
		},
		{
			name: "container signature without image",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/repositories/app-images/packages/web%2Ffrontend/versions/sha256:abc/signatures/sha256:sig",
				Name:     "release@example.com",
				Service:  ServiceArtifactSignature,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"https://console.cloud.google.com/artifacts?", "project=demo-project"},
		},
		{
			name: "compute zone",
			r: Resource{
				ID:       "projects/demo-project/zones/us-central1-a/instances/web-1",
				Name:     "web-1",
				Service:  ServiceComputeEngine,
				ScopeID:  "projects/demo-project",
				Location: "us-central1-a",
			},
			want: []string{
				"/compute/instancesDetail/zones/us-central1-a/instances/web-1",
				"project=demo-project",
			},
		},
		{
			name: "subnet",
			r: Resource{
				ID:       "projects/demo-project/regions/us-central1/subnetworks/default",
				Name:     "default",
				Service:  ServiceSubnet,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{
				"/networking/subnetworks/details/us-central1/default",
				"project=demo-project",
			},
		},
		{
			name: "firewall",
			r: Resource{
				ID:      "projects/demo-project/global/firewalls/default-allow-internal",
				Name:    "default-allow-internal",
				Service: ServiceFirewall,
				ScopeID: "projects/demo-project",
			},
			want: []string{
				"/networking/firewalls/details/default-allow-internal",
				"project=demo-project",
			},
		},
		{
			name: "vpc network",
			r: Resource{
				ID:      "projects/demo-project/global/networks/default",
				Name:    "default",
				Service: ServiceVPC,
				ScopeID: "projects/demo-project",
			},
			want: []string{"/networking/networks/details/default?", "project=demo-project"},
		},
		{
			name: "vpc product node",
			r:    ProductGroupResource(HierarchyNode{ID: "projects/demo-project", Name: "demo-project", Kind: KindProject}.Resource(), ServiceVPC, 2),
			want: []string{"/networking/networks/list?", "project=demo-project"},
		},
		{
			name: "compute product node",
			r:    ProductGroupResource(HierarchyNode{ID: "projects/demo-project", Name: "demo-project", Kind: KindProject}.Resource(), ServiceComputeEngine, 1),
			want: []string{"/compute/instances?", "project=demo-project"},
		},
		{
			name: "ip address",
			r: Resource{
				ID:      "projects/demo-project/global/addresses/web-ip",
				Name:    "web-ip",
				Service: ServiceAddress,
				ScopeID: "projects/demo-project",
			},
			want: []string{"/networking/addresses/list?", "project=demo-project"},
		},
		{
			name: "forwarding rule",
			r: Resource{
				ID:       "projects/demo-project/regions/us-central1/forwardingRules/api-ilb",
				Name:     "api-ilb",
				Service:  ServiceForwardingRule,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/net-services/loadbalancing/list/frontends?", "project=demo-project"},
		},
		{
			name: "service attachment",
			r: Resource{
				ID:       "projects/demo-project/regions/us-central1/serviceAttachments/api",
				Name:     "api",
				Service:  ServiceServiceAttachment,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/net-services/psc/list/producers?", "project=demo-project"},
		},
		{
			name: "psc endpoint",
			r: Resource{
				ID:       "projects/demo-project/regions/us-central1/forwardingRules/partner-psc",
				Name:     "partner-psc",
				Service:  ServicePSCEndpoint,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/net-services/psc/list/consumers?", "project=demo-project"},
		},
		{
			name: "ssl policy",
			r: Resource{
				ID:       "projects/demo-project/global/sslPolicies/modern-tls",
				Name:     "modern-tls",
				Service:  ServiceSSLPolicy,
				ScopeID:  "projects/demo-project",
				Location: "global",
			},
			want: []string{"/net-security/sslpolicies/list?", "project=demo-project"},
		},
		{
			name: "load balancer",
			r: Resource{
				ID:      "projects/demo-project/global/urlMaps/web-map",
				Name:    "web-map",
				Service: ServiceLoadBalancer,
				ScopeID: "projects/demo-project",
			},
			want: []string{"/net-services/loadbalancing/list/loadBalancers?", "project=demo-project"},
		},
		{
			name: "health check",
			r: Resource{
				ID:      "projects/demo-project/global/healthChecks/web-hc",
				Name:    "web-hc",
				Service: ServiceHealthCheck,
				ScopeID: "projects/demo-project",
			},
			want: []string{"/compute/healthChecks?", "project=demo-project"},
		},
		{
			name: "cloud armor",
			r: Resource{
				ID:      "projects/demo-project/global/securityPolicies/web-armor",
				Name:    "web-armor",
				Service: ServiceCloudArmor,
				ScopeID: "projects/demo-project",
			},
			want: []string{"/net-security/securitypolicies/list?", "project=demo-project"},
		},
		{
			name: "web proxy",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/gateways/egress-proxy",
				Name:     "egress-proxy",
				Service:  ServiceWebProxy,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/net-security/swp/web-proxies/list?", "project=demo-project"},
		},
		{
			name: "swp policy",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/gatewaySecurityPolicies/egress-policy",
				Name:     "egress-policy",
				Service:  ServiceSWPPolicy,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/net-security/swp/policies/list?", "project=demo-project"},
		},
		{
			name: "swp rule",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/gatewaySecurityPolicies/egress-policy/rules/allow-github",
				Name:     "allow-github",
				Service:  ServiceSWPRule,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/net-security/swp/policies/list?", "project=demo-project"},
		},
		{
			name: "url list",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/urlLists/dev-tools",
				Name:     "dev-tools",
				Service:  ServiceURLList,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/net-security/swp/url-lists/list?", "project=demo-project"},
		},
		{
			name: "certificate",
			r: Resource{
				ID:       "projects/demo-project/locations/global/certificates/web-cert",
				Name:     "web-cert",
				Service:  ServiceCertificate,
				ScopeID:  "projects/demo-project",
				Location: "global",
			},
			want: []string{"/security/ccm/list?", "project=demo-project"},
		},
		{
			name: "certificate map entry",
			r: Resource{
				ID:       "projects/demo-project/locations/global/certificateMaps/web-map/certificateMapEntries/www",
				Name:     "www.example.com",
				Service:  ServiceCertMapEntry,
				ScopeID:  "projects/demo-project",
				Location: "global",
			},
			want: []string{"/security/ccm/list?", "project=demo-project"},
		},
		{
			name: "tls inspection policy",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/tlsInspectionPolicies/egress-tls",
				Name:     "egress-tls",
				Service:  ServiceTLSInspection,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/net-security/tls/policies/list?", "project=demo-project"},
		},
		{
			name: "disk",
			r: Resource{
				ID:       "projects/demo-project/zones/us-central1-a/disks/web-1",
				Name:     "web-1",
				Service:  ServiceDisk,
				ScopeID:  "projects/demo-project",
				Location: "us-central1-a",
			},
			want: []string{"/compute/disks?", "project=demo-project"},
		},
		{
			name: "cloud router",
			r: Resource{
				ID:       "projects/demo-project/regions/us-central1/routers/nat-router",
				Name:     "nat-router",
				Service:  ServiceRouter,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/hybrid/routers/list?", "project=demo-project"},
		},
		{
			name: "cloud nat",
			r: Resource{
				ID:       "projects/demo-project/regions/us-central1/routers/nat-router/nats/egress",
				Name:     "egress",
				Service:  ServiceCloudNAT,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
				ParentID: "projects/demo-project/regions/us-central1/routers/nat-router",
				FacetID:  FacetNATs,
			},
			want: []string{"/net-services/nat/list?", "project=demo-project"},
		},
		{
			name: "cloud sql instance",
			r: Resource{
				ID:       "projects/demo-project/instances/postgres-prod",
				Name:     "postgres-prod",
				Service:  ServiceCloudSQL,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/sql/instances/postgres-prod/overview?", "project=demo-project"},
		},
		{
			name: "cloud sql database",
			r: Resource{
				ID:       "projects/demo-project/instances/postgres-prod/databases/app",
				Name:     "app",
				Service:  ServiceCloudSQLDatabase,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
				ParentID: "projects/demo-project/instances/postgres-prod",
				FacetID:  FacetDatabases,
			},
			want: []string{"/sql/instances/postgres-prod/databases?", "project=demo-project"},
		},
		{
			name: "log-based metric",
			r: Resource{
				ID:       "projects/demo-project/metrics/checkout_errors",
				Name:     "checkout_errors",
				Service:  ServiceLogMetric,
				ScopeID:  "projects/demo-project",
				Location: "global",
			},
			want: []string{"/logs/metrics?", "project=demo-project"},
		},
		{
			name: "log view",
			r: Resource{
				ID:       "projects/demo-project/locations/global/buckets/_Default/views/_AllLogs",
				Name:     "_AllLogs",
				Service:  ServiceLogView,
				ScopeID:  "projects/demo-project",
				Location: "global",
			},
			want: []string{"/logs/storage?", "project=demo-project"},
		},
		{
			name: "managed instance",
			r: Resource{
				ID:       "projects/demo-project/zones/us-central1-a/instanceGroupManagers/web-ig/instances/web-x7k2",
				Name:     "web-x7k2",
				Service:  ServiceManagedInstance,
				ScopeID:  "projects/demo-project",
				Location: "us-central1-a",
			},
			want: []string{"/compute/instancesDetail/zones/us-central1-a/instances/web-x7k2?", "project=demo-project"},
		},
		{
			name: "cloud sql user",
			r: Resource{
				ID:       "projects/demo-project/instances/postgres-prod/users/app",
				Name:     "app",
				Service:  ServiceCloudSQLUser,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
				ParentID: "projects/demo-project/instances/postgres-prod",
				FacetID:  FacetUsers,
			},
			want: []string{"/sql/instances/postgres-prod/users?", "project=demo-project"},
		},
		{
			name: "cloud sql backup",
			r: Resource{
				ID:       "projects/demo-project/instances/postgres-prod/backupRuns/1730257200000",
				Name:     "2026-10-30 03:00 UTC",
				Service:  ServiceCloudSQLBackup,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
				ParentID: "projects/demo-project/instances/postgres-prod",
				FacetID:  FacetBackups,
			},
			want: []string{"/sql/instances/postgres-prod/backups?", "project=demo-project"},
		},
		{
			name: "redis",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/instances/session-cache",
				Name:     "session-cache",
				Service:  ServiceRedis,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/memorystore/redis/instances?", "project=demo-project"},
		},
		{
			name: "valkey",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/instances/valkey-cache",
				Name:     "valkey-cache",
				Service:  ServiceValkey,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/memorystore/valkey/instances?", "project=demo-project"},
		},
		{
			name: "dns zone",
			r: Resource{
				ID:       "projects/demo-project/managedZones/example-com",
				Name:     "example-com",
				Service:  ServiceCloudDNS,
				ScopeID:  "projects/demo-project",
				Location: "global",
			},
			want: []string{"/net-services/dns/zones?", "project=demo-project"},
		},
		{
			name: "dns record set",
			r: Resource{
				ID:       "projects/demo-project/managedZones/example-com/rrsets/www.example.com./CNAME",
				Name:     "www.example.com.",
				Service:  ServiceDNSRecord,
				ScopeID:  "projects/demo-project",
				Location: "global",
				ParentID: "projects/demo-project/managedZones/example-com",
				FacetID:  FacetRecords,
			},
			want: []string{"/net-services/dns/zones?", "project=demo-project"},
		},
		{
			name: "secret",
			r: Resource{
				ID:       "projects/demo-project/secrets/db-password",
				Name:     "db-password",
				Service:  ServiceSecretManager,
				ScopeID:  "projects/demo-project",
				Location: "global",
			},
			want: []string{"/security/secret-manager?", "project=demo-project"},
		},
		{
			name: "kms key",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/keyRings/app-keys/cryptoKeys/secrets",
				Name:     "secrets",
				Service:  ServiceKMSKey,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
				ParentID: "projects/demo-project/locations/us-central1/keyRings/app-keys",
				FacetID:  FacetKeys,
			},
			want: []string{"/security/kms/keyrings?", "project=demo-project"},
		},
		{
			name: "workstation cluster",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/workstationClusters/dev-cluster",
				Name:     "dev-cluster",
				Service:  ServiceWorkstationCluster,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
			},
			want: []string{"/workstations/clusters?", "project=demo-project"},
		},
		{
			name: "workstation config",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/workstationClusters/dev-cluster/workstationConfigs/vscode",
				Name:     "vscode",
				Service:  ServiceWorkstationConfig,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
				ParentID: "projects/demo-project/locations/us-central1/workstationClusters/dev-cluster",
				FacetID:  FacetConfigs,
			},
			want: []string{"/workstations/configurations?", "project=demo-project"},
		},
		{
			name: "workstation",
			r: Resource{
				ID:       "projects/demo-project/locations/us-central1/workstationClusters/dev-cluster/workstationConfigs/vscode/workstations/alice",
				Name:     "alice",
				Service:  ServiceWorkstation,
				ScopeID:  "projects/demo-project",
				Location: "us-central1",
				ParentID: "projects/demo-project/locations/us-central1/workstationClusters/dev-cluster/workstationConfigs/vscode",
				FacetID:  FacetWorkstations,
			},
			want: []string{"/workstations/list?", "project=demo-project"},
		},
		{
			name: "organization log sink",
			r: Resource{
				ID:       "organizations/123/sinks/org-audit",
				Name:     "org-audit",
				Service:  ServiceLogSink,
				ScopeID:  "organizations/123",
				Location: "global",
			},
			want: []string{"/logs/router?", "organizationId=123"},
		},
		{
			name: "project log sink",
			r: Resource{
				ID:       "projects/demo-project/sinks/_Default",
				Name:     "_Default",
				Service:  ServiceLogSink,
				ScopeID:  "projects/demo-project",
				Location: "global",
			},
			want: []string{"/logs/router?", "project=demo-project"},
		},
		{
			name: "log bucket",
			r: Resource{
				ID:       "projects/demo-project/locations/global/buckets/_Default",
				Name:     "_Default",
				Service:  ServiceLogBucket,
				ScopeID:  "projects/demo-project",
				Location: "global",
			},
			want: []string{"/logs/storage?", "project=demo-project"},
		},
		{
			name: "service account",
			r: Resource{
				ID:       "projects/demo-project/serviceAccounts/deployer@demo-project.iam.gserviceaccount.com",
				Name:     "deployer",
				Service:  ServiceServiceAccount,
				ScopeID:  "projects/demo-project",
				Metadata: map[string]string{MetaUniqueID: "104729384756102938475"},
			},
			want: []string{
				"/iam-admin/serviceaccounts/details/104729384756102938475",
				"project=demo-project",
			},
		},
		{
			name: "project custom role",
			r: Resource{
				ID:      "projects/demo-project/roles/deployer",
				Name:    "deployer",
				Service: ServiceCustomRole,
				ScopeID: "projects/demo-project",
			},
			want: []string{"/iam-admin/roles", "project=demo-project"},
		},
		{
			name: "org custom role",
			r: Resource{
				ID:      "organizations/123/roles/securityReviewer",
				Name:    "securityReviewer",
				Service: ServiceCustomRole,
				ScopeID: "organizations/123",
			},
			want: []string{"/iam-admin/roles", "organizationId=123"},
		},
		{
			name: "org iam",
			r: Resource{
				ID:      "organizations/example-org/iam/bindings/org-admins",
				Name:    "org-admins",
				Service: ServiceIAM,
				ScopeID: "organizations/example-org",
			},
			want: []string{
				"/iam-admin/iam",
				"organizationId=example-org",
			},
		},
		{
			name: "folder org policy",
			r: Resource{
				ID:      "folders/platform/policies/compute.restrictVpcPeering",
				Name:    "restrict-vpc-peering",
				Service: ServiceOrgPolicy,
				ScopeID: "folders/platform",
			},
			want: []string{
				"/iam-admin/orgpolicies",
				"folderId=platform",
			},
		},
		{
			name: "organization",
			r: Resource{
				ID:      "organizations/123",
				Name:    "example",
				Service: ServiceOrganization,
				ScopeID: "organizations/123",
			},
			want: []string{"/iam-admin/iam", "organizationId=123"},
		},
		{
			name: "project",
			r: Resource{
				ID:      "projects/demo-project",
				Name:    "demo-project",
				Service: ServiceProject,
				ScopeID: "projects/demo-project",
			},
			want: []string{"/home/dashboard", "project=demo-project"},
		},
		{
			name: "enabled api",
			r: Resource{
				ID:       "projects/demo-project/services/storage.googleapis.com",
				Name:     "Cloud Storage",
				Service:  ServiceCloudStorage,
				ScopeID:  "projects/demo-project",
				ParentID: "projects/demo-project/collections/enabled-services",
				FacetID:  FacetEnabledServices,
				Metadata: map[string]string{"api": "storage.googleapis.com"},
			},
			want: []string{
				"https://console.cloud.google.com/apis/api/storage.googleapis.com/overview",
				"project=demo-project",
			},
		},
		{
			name: "folder",
			r: Resource{
				ID:      "folders/platform",
				Name:    "platform",
				Service: ServiceFolder,
				ScopeID: "folders/platform",
			},
			want: []string{"/cloud-resource-manager", "folderId=platform"},
		},
		{
			name: "org iam numeric",
			r: Resource{
				ID:      "organizations/123/iam/bindings/roles%2Fowner",
				Name:    "owner",
				Service: ServiceIAM,
				ScopeID: "organizations/123",
			},
			want: []string{"/iam-admin/iam", "organizationId=123"},
		},
		{
			name: "bucket binding",
			r: Resource{
				ID:       "projects/demo-project/buckets/my-app-bucket/iam/bindings/roles%2Fstorage.admin",
				Name:     "storage.admin",
				Service:  ServiceIAM,
				ScopeID:  "projects/demo-project",
				ParentID: "projects/demo-project/buckets/my-app-bucket",
				FacetID:  FacetPermissions,
			},
			want: []string{"/storage/browser/my-app-bucket", "project=demo-project"},
		},
		{
			name: "iap app",
			r: Resource{
				ID:       "projects/demo-project/iap_web/compute/services/admin-backend",
				Name:     "admin-backend",
				Service:  ServiceIAPApp,
				ScopeID:  "projects/demo-project",
				Location: "global",
			},
			want: []string{"/security/iap?", "project=demo-project"},
		},
		{
			name: "iap binding",
			r: Resource{
				ID:       "projects/demo-project/iap_web/compute/services/admin-backend/iam/bindings/roles%2Fiap.httpsResourceAccessor",
				Name:     "iap.httpsResourceAccessor",
				Service:  ServiceIAMBinding,
				ScopeID:  "projects/demo-project",
				ParentID: "projects/demo-project/iap_web/compute/services/admin-backend",
				FacetID:  FacetPermissions,
			},
			want: []string{"/security/iap?", "project=demo-project"},
		},
		{
			name: "access binding",
			r: Resource{
				ID:      "organizations/123/gcpUserAccessBindings/a1b2c3d4",
				Name:    "engineering@example.com",
				Service: ServiceAccessBinding,
				ScopeID: "organizations/123",
			},
			want: []string{"/security/access-level?", "organizationId=123"},
		},
		{
			name: "workspace user",
			r: Resource{
				ID:      "organizations/123/users/1001",
				Name:    "alice@example.com",
				Service: ServiceWorkspaceUser,
				ScopeID: "organizations/123",
			},
			want: []string{"https://admin.google.com/ac/users"},
		},
		{
			name: "workspace group",
			r: Resource{
				ID:      "organizations/123/groups/03abc",
				Name:    "eng@example.com",
				Service: ServiceWorkspaceGroup,
				ScopeID: "organizations/123",
			},
			want: []string{"https://admin.google.com/ac/groups"},
		},
		{
			name: "access policy",
			r: Resource{
				ID:      "organizations/123/accessPolicies/558812004321",
				Name:    "default policy",
				Service: ServiceAccessPolicy,
				ScopeID: "organizations/123",
			},
			want: []string{"/security/service-perimeter?", "organizationId=123"},
		},
		{
			name: "org perimeter",
			r: Resource{
				ID:       "organizations/123/accessPolicies/558812004321/servicePerimeters/prod",
				Name:     "prod",
				Service:  ServicePerimeter,
				ScopeID:  "organizations/123",
				ParentID: "organizations/123/accessPolicies/558812004321",
				FacetID:  FacetPerimeters,
			},
			want: []string{"/security/service-perimeter?", "organizationId=123"},
		},
		{
			name: "project perimeter",
			r: Resource{
				ID:      "projects/demo-project/accessPolicies/558812004321/servicePerimeters/prod",
				Name:    "prod",
				Service: ServicePerimeter,
				ScopeID: "projects/demo-project",
			},
			want: []string{"/security/service-perimeter?", "project=demo-project"},
		},
		{
			name: "ingress rule",
			r: Resource{
				ID:       "projects/demo-project/accessPolicies/558812004321/servicePerimeters/prod/ingressRules/1",
				Name:     "CI deploys",
				Service:  ServiceIngressRule,
				ScopeID:  "projects/demo-project",
				ParentID: "projects/demo-project/accessPolicies/558812004321/servicePerimeters/prod",
				FacetID:  FacetIngressRules,
			},
			want: []string{"/security/service-perimeter?", "project=demo-project"},
		},
		{
			name: "access level",
			r: Resource{
				ID:       "organizations/123/accessPolicies/558812004321/accessLevels/corp",
				Name:     "corp",
				Service:  ServiceAccessLevel,
				ScopeID:  "organizations/123",
				ParentID: "organizations/123/accessPolicies/558812004321",
				FacetID:  FacetAccessLevels,
			},
			want: []string{"/security/access-level?", "organizationId=123"},
		},
		{
			name: "org scope fallback",
			r: Resource{
				Name:    "orphan",
				Service: ServiceOrgPolicy,
				ScopeID: "organizations/123",
			},
			want: []string{"/iam-admin/orgpolicies", "organizationId=123"},
		},
		{
			name: "scope fallback",
			r: Resource{
				Name:    "orphan",
				Service: ServiceCloudStorage,
				ScopeID: "projects/demo-project",
			},
			want: []string{
				"/storage/browser/orphan",
				"project=demo-project",
			},
		},
		{
			name: "bigquery product node",
			r:    ProductGroupResource(HierarchyNode{ID: "projects/demo-project", Name: "demo-project", Kind: KindProject}.Resource(), ServiceBigQuery, 3),
			want: []string{"https://console.cloud.google.com/bigquery?project=demo-project"},
		},
		{
			name: "bigquery dataset",
			r:    Resource{ID: "projects/demo-project/datasets/analytics", Name: "analytics", Service: ServiceBigQueryDataset, ScopeID: "projects/demo-project", Location: "US"},
			want: []string{"/bigquery?", "d=analytics", "p=demo-project", "page=dataset", "project=demo-project"},
		},
		{
			name: "bigquery table",
			r:    Resource{ID: "projects/demo-project/datasets/analytics/tables/events", Name: "events", Service: ServiceBigQueryTable, ScopeID: "projects/demo-project", Location: "US"},
			want: []string{"/bigquery?", "d=analytics", "t=events", "page=table"},
		},
		{
			name: "bigquery routine opens its dataset",
			r:    Resource{ID: "projects/demo-project/datasets/analytics/routines/normalize_email", Name: "normalize_email", Service: ServiceBigQueryRoutine, ScopeID: "projects/demo-project", Location: "US"},
			want: []string{"d=analytics", "page=dataset"},
		},
		{
			name: "bigquery reservation",
			r:    Resource{ID: "projects/demo-project/locations/US/reservations/prod-etl", Name: "prod-etl", Service: ServiceBigQueryReservation, ScopeID: "projects/demo-project", Location: "US"},
			want: []string{"/bigquery/admin/reservations", "project=demo-project"},
		},
		{
			name: "bigquery transfer",
			r:    Resource{ID: "projects/demo-project/locations/us/transferConfigs/6530f6d2", Name: "daily-rollup", Service: ServiceBigQueryTransfer, ScopeID: "projects/demo-project", Location: "us"},
			want: []string{"/bigquery/transfers", "project=demo-project"},
		},
		{
			name: "privateca product node",
			r:    ProductGroupResource(HierarchyNode{ID: "projects/demo-project", Name: "demo-project", Kind: KindProject}.Resource(), ServicePrivateCA, 2),
			want: []string{"/security/cas?", "project=demo-project"},
		},
		{
			name: "privateca pool",
			r:    Resource{ID: "projects/demo-project/locations/us-central1/caPools/internal-pool", Name: "internal-pool", Service: ServiceCAPool, ScopeID: "projects/demo-project", Location: "us-central1"},
			want: []string{"/security/cas?", "project=demo-project"},
		},
		{
			name: "privateca authority",
			r:    Resource{ID: "projects/demo-project/locations/us-central1/caPools/internal-pool/certificateAuthorities/root-ca-1", Name: "root-ca-1", Service: ServiceCertificateAuthority, ScopeID: "projects/demo-project", Location: "us-central1"},
			want: []string{"/security/cas?", "project=demo-project"},
		},
		{
			name: "privateca template",
			r:    Resource{ID: "projects/demo-project/locations/us-central1/certificateTemplates/mtls-leaf", Name: "mtls-leaf", Service: ServiceCertificateTemplate, ScopeID: "projects/demo-project", Location: "us-central1"},
			want: []string{"/security/cas?", "project=demo-project"},
		},
		{
			name: "privateca pool binding",
			r:    Resource{ID: "projects/demo-project/locations/us-central1/caPools/internal-pool/iam/bindings/roles%2Fprivateca.certificateManager", Name: "privateca.certificateManager", Service: ServiceIAMBinding, ScopeID: "projects/demo-project", ParentID: "projects/demo-project/locations/us-central1/caPools/internal-pool", FacetID: FacetPermissions},
			want: []string{"/security/cas?", "project=demo-project"},
		},
		{
			name: "psc regional endpoint",
			r:    Resource{ID: "projects/demo-project/locations/us-central1/regionalEndpoints/telemetry-rep", Name: "telemetry-rep", Service: ServiceRegionalEndpoint, ScopeID: "projects/demo-project", Location: "us-central1"},
			want: []string{"/net-services/psc/list/consumers?", "project=demo-project"},
		},
		{
			name: "service connection policy",
			r:    Resource{ID: "projects/demo-project/locations/us-central1/serviceConnectionPolicies/default-cloud-sql", Name: "default-cloud-sql", Service: ServiceConnectionPolicy, ScopeID: "projects/demo-project", Location: "us-central1"},
			want: []string{"/net-services/psc/list/policies?", "project=demo-project"},
		},
		{
			name: "psc connection",
			r:    Resource{ID: "projects/demo-project/locations/us-central1/serviceConnectionPolicies/default-cloud-sql/connections/10000000000000001", Name: "demo-project/sca-auto-fr-0a1b2c3d", Service: ServicePSCConnection, ScopeID: "projects/demo-project", Location: "us-central1", ParentID: "projects/demo-project/locations/us-central1/serviceConnectionPolicies/default-cloud-sql", FacetID: FacetConnections},
			want: []string{"/net-services/psc/list/policies?", "project=demo-project"},
		},
		{
			name: "logging view binding does not link to storage",
			r:    Resource{ID: "projects/demo-project/locations/global/buckets/_Default/views/_AllLogs/iam/bindings/roles%2Flogging.viewAccessor", Name: "logging.viewAccessor", Service: ServiceIAMBinding, ScopeID: "projects/demo-project", ParentID: "projects/demo-project/locations/global/buckets/_Default/views/_AllLogs", FacetID: FacetPermissions},
			want: []string{"/iam-admin/iam?", "project=demo-project"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.r.ConsoleURL()
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("ConsoleURL=%q missing %q", got, w)
				}
			}
			for _, bad := range []string{"=projects%2F", "=organizations%2F", "=folders%2F"} {
				if strings.Contains(got, bad) {
					t.Errorf("ConsoleURL=%q carries a full scope name", got)
				}
			}
			if strings.Contains(got, "organizationId=") && strings.Contains(got, "project=") {
				t.Errorf("org URL should not also set project: %s", got)
			}
		})
	}
}
