package fake

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func res(id, name string, svc model.ServiceType, scope, loc string, st model.ResourceStatus, desc string, md map[string]string) model.Resource {
	if md == nil {
		md = map[string]string{}
	}
	return model.Resource{
		ID:          id,
		Name:        name,
		Service:     svc,
		ScopeID:     scope,
		Location:    loc,
		Status:      st,
		Description: desc,
		Metadata:    md,
	}
}

// Resources is the fixture catalog. No network, no credentials.
func Resources() []model.Resource {
	return append([]model.Resource{
		extra(ordered(bucket("projects/demo-project/buckets/my-app-bucket", "my-app-bucket", "projects/demo-project", "us-central1", "Application object storage", map[string]string{"storageClass": "STANDARD", "locationType": "region", "access": "uniform", "publicAccessPrevention": "enforced", "versioning": "enabled", "softDelete": "7 days", "lifecycleRules": "2", "updated": "2025-08-19 14:22 UTC"}), bucketFields...), "", "2024-02-11T09:30:00Z", map[string]string{"env": "prod", "team": "web"}),
		extra(ordered(bucket("projects/demo-project/buckets/audit-logs", "audit-logs", "projects/demo-project", "us-east1", "Audit log archive", map[string]string{"storageClass": "NEARLINE", "locationType": "region", "access": "uniform", "publicAccessPrevention": "enforced", "versioning": "disabled", "retention": "2555 days (locked)", "softDelete": "7 days", "defaultEventBasedHold": "enabled", "kmsKey": "projects/demo-project/locations/us-east1/keyRings/audit/cryptoKeys/logs"}), bucketFields...), "", "2023-07-01T00:00:00Z", map[string]string{"retention": "7y"}),
		extra(cluster("projects/demo-project/locations/us-central1/clusters/production-cluster", "production-cluster", "projects/demo-project", "us-central1", "Primary GKE cluster", map[string]string{"mode": "Standard", "version": "1.31.5-gke.100", "releaseChannel": "REGULAR", "locationType": "regional", "nodes": "7", "nodePools": "2 (default-pool, gpu-pool)", "endpoint": "198.51.100.40", "privateNodes": "on", "network": "default", "subnetwork": "default", "podRange": "10.4.0.0/14", "servicesRange": "10.8.0.0/20", "workloadPool": "demo-project.svc.id.goog", "publicEndpoint": "off", "authorizedNetworks": "1", "dataplane": "V2", "etcdEncryption": "projects/demo-project/locations/us-central1/keyRings/app-keys/cryptoKeys/secrets", "shieldedNodes": "on", "maintenance": "FREQ=WEEKLY;BYDAY=SA,SU, 02:00–06:00 UTC", "maintenanceExclusions": "1"}, "authorizedNetworks", "corp-vpn 203.0.113.0/24", "maintenanceExclusions", "year-end-freeze  2026-12-20 – 2027-01-04"), "RUNNING", "2024-11-04T16:20:00Z", map[string]string{"env": "prod", "team": "platform"}),
		extra(ordered(res("projects/demo-project/zones/us-central1-a/instances/web-1", "web-1", model.ServiceComputeEngine, "projects/demo-project", "us-central1-a", model.StatusHealthy, "Frontend VM", map[string]string{"machineType": "e2-medium", "cpuPlatform": "Intel Broadwell", "internalIP": "10.128.0.2", "externalIP": "198.51.100.67", "network": "default", "subnetwork": "default", "disks": "1 (boot 20 GB)", "serviceAccount": "123456789012-compute@developer.gserviceaccount.com", "networkTags": "http-server,https-server", "provisioning": "STANDARD", "maintenance": "MIGRATE, auto restart", "shieldedVM": "secure boot, vTPM, integrity monitoring", "osLogin": "true", "deletionProtection": "on", "lastStarted": "2025-09-30 07:12 UTC"}), instanceFields...), "RUNNING", "2025-01-15T18:04:12Z", map[string]string{"env": "prod", "team": "web"}),
		res("projects/demo-project/locations/us-central1/services/api-gateway", "api-gateway", model.ServiceCloudRun, "projects/demo-project", "us-central1", model.StatusWarning, "Public API", map[string]string{"revision": "api-gateway-00042", "cpu": "2"}),
		extra(sqlInstance("postgres-prod", map[string]string{"engine": "POSTGRES_16", "edition": "ENTERPRISE_PLUS", "tier": "db-perf-optimized-N-4", "role": "primary", "availability": "REGIONAL", "zone": "us-central1-a (standby us-central1-c)", "connectionName": "demo-project:us-central1:postgres-prod", "privateIP": "10.20.0.3", "network": "default", "storage": "250 GB PD_SSD, auto-resize", "backups": "on, point-in-time recovery", "deletionProtection": "on", "replicas": "postgres-prod-replica", "publicAccess": "off", "ssl": "ENCRYPTED_ONLY", "kmsKey": "projects/demo-project/locations/us-central1/keyRings/app-keys/cryptoKeys/secrets", "backupWindow": "02:00 UTC", "backupRetention": "14 backups", "logRetention": "7 days", "maintenance": "Sun 03:00 UTC, stable", "nextMaintenance": "2026-11-08 03:00 UTC", "queryInsights": "on", "flags": "2"}, "max_connections=500", "log_min_duration_statement=250"), "RUNNABLE", "2024-05-02T08:00:00Z", map[string]string{"env": "prod", "team": "platform"}),
		sqlDatabase("postgres-prod", "postgres", "UTF8", "en_US.UTF8"),
		sqlDatabase("postgres-prod", "app", "UTF8", "en_US.UTF8"),
		sqlDatabase("postgres-prod", "analytics", "UTF8", "en_US.UTF8"),
		sqlUser("postgres-prod", "postgres", "", "", map[string]string{"type": "BUILT_IN", "roles": "1"}, "cloudsqlsuperuser"),
		sqlUser("postgres-prod", "web-app@demo-project.iam", "IAM SA", "", map[string]string{"type": "CLOUD_IAM_SERVICE_ACCOUNT", "iamEmail": "web-app@demo-project.iam.gserviceaccount.com"}),
		sqlUser("postgres-prod", "reporting", "", "LOCKED", map[string]string{"type": "BUILT_IN", "passwordExpires": "2026-12-01 00:00 UTC"}),
		sqlBackup("postgres-prod", "1761789600000", "2026-10-30T02:00:05Z", "SUCCESSFUL", "", map[string]string{"type": "AUTOMATED", "backupKind": "SNAPSHOT", "location": "us", "ended": "2026-10-30 02:04 UTC", "duration": "4m30s", "id": "1761789600000"}),
		sqlBackup("postgres-prod", "1761703200000", "2026-10-29T02:00:04Z", "SUCCESSFUL", "", map[string]string{"type": "AUTOMATED", "backupKind": "SNAPSHOT", "location": "us", "ended": "2026-10-29 02:04 UTC", "duration": "4m12s", "id": "1761703200000"}),
		sqlBackup("postgres-prod", "1761650000000", "2026-10-28T11:13:20Z", "FAILED", "Before schema migration", map[string]string{"type": "ON_DEMAND", "backupKind": "SNAPSHOT", "error": "Backup failed: instance was under maintenance", "id": "1761650000000"}),
		extra(sqlInstance("postgres-prod-replica", map[string]string{"engine": "POSTGRES_16", "edition": "ENTERPRISE_PLUS", "tier": "db-perf-optimized-N-4", "role": "read replica of postgres-prod", "availability": "ZONAL", "zone": "us-central1-b", "connectionName": "demo-project:us-central1:postgres-prod-replica", "privateIP": "10.20.0.7", "network": "default", "storage": "250 GB PD_SSD, auto-resize", "backups": "off", "deletionProtection": "off"}), "RUNNABLE", "2024-05-09T12:30:00Z", map[string]string{"env": "prod"}),
		sqlDatabase("postgres-prod-replica", "postgres", "UTF8", "en_US.UTF8"),
		sqlDatabase("postgres-prod-replica", "app", "UTF8", "en_US.UTF8"),
		sqlUser("postgres-prod-replica", "postgres", "", "", map[string]string{"type": "BUILT_IN", "roles": "1"}, "cloudsqlsuperuser"),
		status(extra(sqlInstance("mysql-legacy", map[string]string{"engine": "MYSQL_8_0", "edition": "ENTERPRISE", "tier": "db-n1-standard-1", "role": "primary", "availability": "ZONAL", "zone": "us-central1-f", "serving": "stopped", "connectionName": "demo-project:us-central1:mysql-legacy", "publicIP": "198.51.100.90", "storage": "20 GB PD_HDD", "backups": "on", "deletionProtection": "off", "publicAccess": "on", "authorizedNetworks": "2", "backupWindow": "06:00 UTC", "backupRetention": "7 backups"}), "RUNNABLE", "2021-09-14T17:45:00Z", nil), model.StatusWarning),
		sqlDatabase("mysql-legacy", "wordpress", "utf8mb4", "utf8mb4_0900_ai_ci"),
		sqlUser("mysql-legacy", "root@%", "", "", map[string]string{"type": "BUILT_IN", "host": "%"}),
		sqlUser("mysql-legacy", "wordpress@%", "", "", map[string]string{"type": "BUILT_IN", "host": "%"}),
		sqlBackup("mysql-legacy", "1761804000000", "2026-10-30T06:00:03Z", "SUCCESSFUL", "", map[string]string{"type": "AUTOMATED", "backupKind": "SNAPSHOT", "location": "us", "ended": "2026-10-30 06:01 UTC", "duration": "1m7s", "id": "1761804000000"}),
		extra(ordered(res("projects/demo-project/locations/us-central1/instances/session-cache", "session-cache", model.ServiceRedis, "projects/demo-project", "us-central1", model.StatusHealthy, "", map[string]string{"displayName": "Web session cache", "version": "REDIS_7_2", "tier": "STANDARD_HA", "capacity": "5 GB", "zone": "us-central1-a (standby us-central1-b)", "primaryEndpoint": "10.30.0.4:6379", "readEndpoint": "10.30.0.5:6379", "replicas": "1", "network": "default", "connectMode": "PRIVATE_SERVICE_ACCESS", "reservedIpRange": "10.30.0.0/29", "auth": "on", "transitEncryption": "SERVER_AUTHENTICATION", "persistence": "RDB"}), redisFields...), "READY", "2024-08-20T13:00:00Z", map[string]string{"env": "prod", "team": "web"}),
		valkeyFix(),
		res("projects/demo-project/databases/app-db", "app-db", model.ServiceFirestore, "projects/demo-project", "nam5", "", "Firestore native database", map[string]string{"type": "FIRESTORE_NATIVE"}),
		res("projects/demo-project/topics/events", "events", model.ServicePubSub, "projects/demo-project", "global", "", "Application event bus", map[string]string{"subscriptions": "3"}),
		extra(ordered(vpc("projects/demo-project/global/networks/default", "default", "projects/demo-project", "Default network for the project", map[string]string{"subnetMode": "auto", "subnets": "1", "routingMode": "REGIONAL", "mtu": "1460", "peerings": "servicenetworking-googleapis-com (ACTIVE)", "networkId": "4817392027461829374"}), "subnetMode", "subnets", "routingMode", "mtu", "peerings", "networkId"), "", "2023-06-30T12:00:00Z", nil),
		res("projects/demo-project/locations/us-central1/functions/resize-images", "resize-images", model.ServiceCloudFunctions, "projects/demo-project", "us-central1", model.StatusError, "Image thumbnailer", map[string]string{"runtime": "go122", "trigger": "gcs"}),
		binding("projects/demo-project", "roles/owner", "", "user:alice@example.com", "user:bob@example.com", "group:platform@example.com"),
		binding("projects/demo-project", "roles/editor", "", "group:platform@example.com", "serviceAccount:123456789012@cloudservices.gserviceaccount.com"),
		binding("projects/demo-project", "roles/viewer", "", "user:carol@example.com", "group:auditors@example.com", "domain:example.com"),
		binding("projects/demo-project", "roles/container.developer", "", "group:platform@example.com", "serviceAccount:deployer@demo-project.iam.gserviceaccount.com"),
		binding("projects/demo-project", "roles/storage.objectAdmin", "Business hours", "user:alice@example.com", "serviceAccount:deployer@demo-project.iam.gserviceaccount.com"),
		binding("projects/demo-project", "roles/iam.workloadIdentityUser", "", "principal://iam.googleapis.com/projects/123456789012/locations/global/workloadIdentityPools/github/subject/repo:example/web:ref:refs/heads/main"),
		binding("projects/demo-project", "projects/demo-project/roles/deployer", "", "serviceAccount:deployer@demo-project.iam.gserviceaccount.com"),
		serviceAccount("demo-project", "deployer@demo-project.iam.gserviceaccount.com", "CI deployer", "Deploys web and API releases from GitHub Actions", "104729384756102938475", false),
		serviceAccount("demo-project", "gke-nodes@demo-project.iam.gserviceaccount.com", "GKE nodes", "Node identity for production-cluster", "117384950283746501928", false),
		serviceAccount("demo-project", "legacy-batch@demo-project.iam.gserviceaccount.com", "Legacy batch", "Retired nightly batch job; kept for audit", "109283746501928374650", true),
		serviceAccount("demo-project", "123456789012-compute@developer.gserviceaccount.com", "Compute Engine default service account", "", "112233445566778899001", false),
		saKey("demo-project", "deployer@demo-project.iam.gserviceaccount.com", "3f9a1c7e5b2d8f4a6c0e9b1d3f5a7c9e2b4d6f8a", "USER_MANAGED", "2026-02-14T10:00:00Z", "never", model.StatusHealthy, "USER"),
		saKey("demo-project", "deployer@demo-project.iam.gserviceaccount.com", "8c2e4a6f0b1d3e5f7a9c1b3d5e7f9a0c2e4b6d8f", "USER_MANAGED", "2025-03-01T09:00:00Z", "2026-03-01 09:00 UTC", model.StatusWarning, "EXPIRED"),
		saKey("demo-project", "deployer@demo-project.iam.gserviceaccount.com", "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", "SYSTEM_MANAGED", "2026-10-28T00:00:00Z", "2026-11-13 00:00 UTC", model.StatusHealthy, ""),
		saKey("demo-project", "gke-nodes@demo-project.iam.gserviceaccount.com", "b2c3d4e5f60718293a4b5c6d7e8f901234567890", "SYSTEM_MANAGED", "2026-10-25T00:00:00Z", "2026-11-10 00:00 UTC", model.StatusHealthy, ""),
		saKey("demo-project", "legacy-batch@demo-project.iam.gserviceaccount.com", "5d7f9b1a3c5e7092b4d6f8a0c2e4f6a8b0d2e4f6", "USER_MANAGED", "2022-06-01T00:00:00Z", "never", model.StatusWarning, "USER"),
		saKey("demo-project", "123456789012-compute@developer.gserviceaccount.com", "c3d4e5f60718293a4b5c6d7e8f9012345678901a", "SYSTEM_MANAGED", "2026-10-20T00:00:00Z", "2026-11-05 00:00 UTC", model.StatusHealthy, ""),
		serviceAgent("demo-project", "123456789012@cloudservices.gserviceaccount.com", "roles/editor"),

		binding("organizations/example-org", "roles/resourcemanager.organizationAdmin", "", "user:alice@example.com", "group:gcp-org-admins@example.com", "group:security@example.com", "serviceAccount:terraform@seed-project.iam.gserviceaccount.com"),
		binding("organizations/example-org", "roles/billing.admin", "", "user:dana@example.com", "group:finance@example.com"),
		logSink("organizations/example-org", "org-audit-archive", "All audit logs to the archive bucket", map[string]string{"destinationType": "Cloud Storage", "destination": "storage.googleapis.com/example-audit-archive", "filter": "logName:\"cloudaudit.googleapis.com\"", "includeChildren": "on", "writerIdentity": "serviceAccount:o123456789012-111111@gcp-sa-logging.iam.gserviceaccount.com"}, false),
		orgPolicy("organizations/example-org", "compute.vmExternalIpAccess", map[string]string{"enforcement": "deny all", "inheritFromParent": "off", "updated": "2024-01-15 10:00 UTC"}),
		dryRun(orgPolicy("organizations/example-org", "compute.requireOsLogin", map[string]string{"enforcement": "enforced", "dryRun": "not enforced", "updated": "2024-01-15 10:05 UTC"})),

		binding("folders/platform", "roles/resourcemanager.folderAdmin", "", "group:platform@example.com", "user:bob@example.com"),
		listPolicy(orgPolicy("folders/platform", "compute.restrictVpcPeering", map[string]string{"enforcement": "allow under:organizations/123456789012", "inheritFromParent": "off", "updated": "2024-06-02 09:30 UTC"})),
		policyValue("folders/platform", "compute.restrictVpcPeering", 1, "allow", "under:organizations/123456789012", "", false),

		binding("folders/prod", "roles/owner", "", "group:prod-admins@example.com", "user:alice@example.com", "serviceAccount:terraform@seed-project.iam.gserviceaccount.com"),
		binding("folders/prod-apps", "roles/editor", "", "group:app-teams@example.com", "group:platform@example.com"),
		binding("folders/prod-data", "roles/bigquery.admin", "", "group:data-eng@example.com", "user:erin@example.com"),
		listPolicy(orgPolicy("folders/staging", "iam.allowedPolicyMemberDomains", map[string]string{"enforcement": "allow C03xxxxxx", "inheritFromParent": "on", "updated": "2024-03-20 14:00 UTC"})),
		policyValue("folders/staging", "iam.allowedPolicyMemberDomains", 1, "allow", "C03xxxxxx", "", false),
		listPolicy(orgPolicy("folders/sandbox", "gcp.resourceLocations", map[string]string{"enforcement": "2 rules", "inheritFromParent": "off", "updated": "2025-02-11 16:45 UTC"}, "allow in:us-locations", "allow in:europe-locations  when Sandbox EU team")),
		policyValue("folders/sandbox", "gcp.resourceLocations", 1, "allow", "in:us-locations", "", true),
		policyValue("folders/sandbox", "gcp.resourceLocations", 2, "allow", "in:europe-locations", "Sandbox EU team", true),
		binding("folders/security", "roles/iam.securityAdmin", "", "group:security@example.com"),

		serviceAccount("checkout-project", "checkout-api@checkout-project.iam.gserviceaccount.com", "Checkout API", "Runs the checkout service", "118273645501928374655", false),
		serviceAccount("checkout-project", "210987654321-compute@developer.gserviceaccount.com", "Compute Engine default service account", "", "118273645501928374656", false),
		saKey("checkout-project", "checkout-api@checkout-project.iam.gserviceaccount.com", "d4e5f60718293a4b5c6d7e8f9012345678901a2b", "SYSTEM_MANAGED", "2026-10-26T00:00:00Z", "2026-11-11 00:00 UTC", model.StatusHealthy, ""),
		saKey("checkout-project", "210987654321-compute@developer.gserviceaccount.com", "e5f60718293a4b5c6d7e8f9012345678901a2b3c", "SYSTEM_MANAGED", "2026-10-26T00:00:00Z", "2026-11-11 00:00 UTC", model.StatusHealthy, ""),
		res("projects/checkout-project/buckets/checkout-assets", "checkout-assets", model.ServiceCloudStorage, "projects/checkout-project", "us-central1", "", "Checkout static assets", map[string]string{"storageClass": "STANDARD"}),
		extra(cluster("projects/checkout-project/locations/us-central1/clusters/checkout", "checkout", "projects/checkout-project", "us-central1", "Checkout GKE cluster", map[string]string{"mode": "Autopilot", "version": "1.32.2-gke.1182", "releaseChannel": "REGULAR", "locationType": "regional", "nodes": "3", "nodePools": "1 (default-pool)", "endpoint": "198.51.100.47", "privateNodes": "off", "network": "default", "subnetwork": "default", "workloadPool": "checkout-project.svc.id.goog", "authorizedNetworks": "off", "dataplane": "V2", "shieldedNodes": "on", "maintenance": "daily 05:00 UTC (4h)"}), "RUNNING", "2025-03-18T10:05:00Z", map[string]string{"env": "prod", "team": "checkout"}),
		extra(pool(gcsChild(checkoutPool, "default-pool", "projects/checkout-project", "projects/checkout-project/locations/us-central1/clusters/checkout", model.FacetNodePools, model.ServiceGKENodePool, map[string]string{"machineType": "ek-standard-8", "version": "1.32.2-gke.1182", "autoscaling": "0–1000 total", "zones": "us-central1-a, us-central1-b, us-central1-f", "disk": "100 GB pd-balanced", "imageType": "COS_CONTAINERD", "provisioning": "STANDARD", "metadataServer": "GKE_METADATA", "shieldedVM": "secure boot, integrity monitoring", "autoUpgrade": "on", "autoRepair": "on", "upgradeStrategy": "SURGE (surge 1, unavailable 0)", "maxPodsPerNode": "32"}), "checkout-project", "us-central1-a/gke-checkout-default-pool-3f1a2b9c-grp"), "RUNNING", "", nil),
		managedInstance("checkout-project", checkoutPool, model.FacetNodes, "gke-checkout-default-pool-3f1a2b9c-grp", "us-central1-a", "gke-checkout-default-pool-3f1a2b9c-h7wq", "RUNNING", "NONE", ""),
		res("projects/staging-project/locations/us-central1/services/staging-api", "staging-api", model.ServiceCloudRun, "projects/staging-project", "us-central1", "", "Staging API", map[string]string{"revision": "staging-api-00007"}),
		binding("projects/security-project", "roles/viewer", "", "group:security@example.com", "group:auditors@example.com"),
		extra(ordered(res("projects/playground-project/zones/us-west1-a/instances/toy-vm", "toy-vm", model.ServiceComputeEngine, "projects/playground-project", "us-west1-a", model.StatusWarning, "Throwaway VM", map[string]string{"machineType": "e2-small", "internalIP": "10.138.0.4", "network": "default", "subnetwork": "default", "disks": "1 (boot 10 GB)", "provisioning": "SPOT"}), instanceFields...), "TERMINATED", "2025-09-02T08:00:00Z", map[string]string{"owner": "sandbox"}),

		gcsChild("projects/demo-project/buckets/my-app-bucket/objects/logos%2Flogo.png", "logos/logo.png", "projects/demo-project", "projects/demo-project/buckets/my-app-bucket", model.FacetObjects, model.ServiceCloudStorage, map[string]string{"contentType": "image/png", "size": "2.0 KiB", "storageClass": "STANDARD", "updated": "2024-03-02 10:15 UTC", "generation": "1709374500123456"}),
		gcsChild("projects/demo-project/buckets/my-app-bucket/objects/data%2Fexport.csv", "data/export.csv", "projects/demo-project", "projects/demo-project/buckets/my-app-bucket", model.FacetObjects, model.ServiceCloudStorage, map[string]string{"contentType": "text/csv", "size": "4.0 KiB", "storageClass": "STANDARD", "updated": "2025-08-19 14:22 UTC", "generation": "1724077320654321"}),
		bucketBinding("projects/demo-project/buckets/my-app-bucket", "roles/storage.admin", "group:web-admins@example.com", "serviceAccount:deployer@demo-project.iam.gserviceaccount.com"),
		bucketBinding("projects/demo-project/buckets/my-app-bucket", "roles/storage.objectViewer", "allUsers"),

		gcsChild("projects/demo-project/buckets/audit-logs/objects/2025%2F09%2Faudit.log", "2025/09/audit.log", "projects/demo-project", "projects/demo-project/buckets/audit-logs", model.FacetObjects, model.ServiceCloudStorage, map[string]string{"contentType": "text/plain", "size": "148.0 KiB", "storageClass": "NEARLINE", "updated": "2025-09-30 23:59 UTC", "generation": "1727740740000001"}),
		bucketBinding("projects/demo-project/buckets/audit-logs", "roles/storage.objectViewer", "group:auditors@example.com"),

		gcsChild("projects/demo-project/regions/us-central1/subnetworks/default", "default", "projects/demo-project", "projects/demo-project/global/networks/default", model.FacetSubnets, model.ServiceSubnet, map[string]string{"cidr": "10.128.0.0/20", "gateway": "10.128.0.1", "network": "default", "secondaryRanges": "gke-pods=10.4.0.0/14, gke-services=10.8.0.0/20", "purpose": "PRIVATE", "stackType": "IPV4_ONLY", "privateGoogleAccess": "on", "flowLogs": "off"}),
		extra(pool(gcsChild(prodPool+"default-pool", "default-pool", "projects/demo-project", "projects/demo-project/locations/us-central1/clusters/production-cluster", model.FacetNodePools, model.ServiceGKENodePool, map[string]string{"machineType": "e2-standard-4", "version": "1.31.5-gke.100", "autoscaling": "1–3 per zone", "zones": "us-central1-a, us-central1-b, us-central1-c", "disk": "100 GB pd-balanced", "imageType": "COS_CONTAINERD", "provisioning": "STANDARD", "serviceAccount": "gke-nodes@demo-project.iam.gserviceaccount.com", "metadataServer": "GKE_METADATA", "shieldedVM": "secure boot, integrity monitoring", "autoUpgrade": "on", "autoRepair": "on", "upgradeStrategy": "SURGE (surge 1, unavailable 0)", "maxPodsPerNode": "110", "nodeLabels": "1"}), "demo-project", "us-central1-a/gke-production-cluster-default-pool-1c2d3e4f-grp", "us-central1-b/gke-production-cluster-default-pool-5a6b7c8d-grp", "us-central1-c/gke-production-cluster-default-pool-9e0f1a2b-grp"), "RUNNING", "", nil),
		managedInstance("demo-project", prodPool+"default-pool", model.FacetNodes, "gke-production-cluster-default-pool-1c2d3e4f-grp", "us-central1-a", "gke-production-cluster-default-pool-1c2d3e4f-k2vn", "RUNNING", "NONE", ""),
		managedInstance("demo-project", prodPool+"default-pool", model.FacetNodes, "gke-production-cluster-default-pool-5a6b7c8d-grp", "us-central1-b", "gke-production-cluster-default-pool-5a6b7c8d-r9tx", "RUNNING", "NONE", ""),
		managedInstance("demo-project", prodPool+"default-pool", model.FacetNodes, "gke-production-cluster-default-pool-9e0f1a2b-grp", "us-central1-c", "gke-production-cluster-default-pool-9e0f1a2b-m4zp", "RUNNING", "NONE", ""),
		status(extra(pool(gcsChild(prodPool+"gpu-pool", "gpu-pool", "projects/demo-project", "projects/demo-project/locations/us-central1/clusters/production-cluster", model.FacetNodePools, model.ServiceGKENodePool, map[string]string{"machineType": "g2-standard-8", "accelerators": "1× nvidia-l4", "version": "1.31.5-gke.100", "initialNodes": "1 per zone", "zones": "us-central1-a", "disk": "200 GB pd-ssd", "imageType": "COS_CONTAINERD", "provisioning": "SPOT", "serviceAccount": "gke-nodes@demo-project.iam.gserviceaccount.com", "metadataServer": "GKE_METADATA", "autoUpgrade": "on", "autoRepair": "on", "maxPodsPerNode": "110", "taints": "1"}), "demo-project", "us-central1-a/gke-production-cluster-gpu-pool-7d8e9f0a-grp"), "RECONCILING", "", nil), model.StatusWarning),
		managedInstance("demo-project", prodPool+"gpu-pool", model.FacetNodes, "gke-production-cluster-gpu-pool-7d8e9f0a-grp", "us-central1-a", "gke-production-cluster-gpu-pool-7d8e9f0a-b5qs", "STAGING", "CREATING", ""),
		extra(artifactRepo(appImagesID, "app-images", "Application container images", map[string]string{"format": "DOCKER", "mode": "Standard", "registryUri": "us-central1-docker.pkg.dev/demo-project/app-images", "size": "1.3 GiB", "vulnerabilityScanning": "active", "immutableTags": "off", "cleanupPolicies": "2", "updated": "2025-10-01 12:40 UTC"}), "", "2024-05-20T08:00:00Z", map[string]string{"team": "platform"}),
		extra(artifactRepo(pyPackagesID, "py-packages", "Internal Python packages", map[string]string{"format": "PYTHON", "mode": "Standard", "registryUri": "us-python.pkg.dev/demo-project/py-packages", "size": "18.4 MiB", "vulnerabilityScanning": "unsupported", "updated": "2025-09-12 09:05 UTC"}), "", "2024-08-02T15:30:00Z", nil),
		extra(artifactPackage("projects/demo-project/locations/us-central1/repositories/app-images/packages/web%2Ffrontend", "web/frontend", "DOCKER", map[string]string{"updated": "2025-10-01 12:40 UTC"}), "", "2024-05-21T10:00:00Z", nil),
		extra(artifactPackage("projects/demo-project/locations/us-central1/repositories/app-images/packages/api", "api", "DOCKER", map[string]string{"updated": "2025-09-28 17:22 UTC"}), "", "2024-05-21T10:05:00Z", nil),
		extra(artifactPackage("projects/demo-project/locations/us/repositories/py-packages/packages/example-utils", "example-utils", "PYTHON", map[string]string{"updated": "2025-09-12 09:05 UTC"}), "", "2024-08-02T15:40:00Z", nil),
		extra(artifactVersion("projects/demo-project/locations/us-central1/repositories/app-images/packages/web%2Ffrontend/versions/sha256:3f9a1c2b7d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8", "v1.4.2, latest", "DOCKER", map[string]string{"tags": "v1.4.2, latest", "digest": "sha256:3f9a1c2b7d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8", "uri": "us-central1-docker.pkg.dev/demo-project/app-images/web/frontend@sha256:3f9a1c2b7d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8", "size": "48.2 MiB", "mediaType": "application/vnd.oci.image.manifest.v1+json", "built": "2025-10-01 12:31 UTC", "updated": "2025-10-01 12:40 UTC"}), "", "2025-10-01T12:40:00Z", nil),
		extra(artifactSigTagVersion("projects/demo-project/locations/us-central1/repositories/app-images/packages/web%2Ffrontend/versions/sha256:e4a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e80", "sha256-3f9a1c2b7d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8.sig", map[string]string{"tags": "sha256-3f9a1c2b7d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8.sig", "digest": "sha256:e4a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e80", "signedDigest": "sha256:3f9a1c2b7d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8", "uri": "us-central1-docker.pkg.dev/demo-project/app-images/web/frontend@sha256:e4a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e80", "size": "1.2 KiB", "mediaType": "application/vnd.oci.image.manifest.v1+json", "updated": "2025-10-01 12:41 UTC"}), "", "2025-10-01T12:41:00Z", nil),
		extra(artifactVersion("projects/demo-project/locations/us-central1/repositories/app-images/packages/api/versions/sha256:9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c", "sha256:9b8c7d6e5f4a", "DOCKER", map[string]string{"digest": "sha256:9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c", "uri": "us-central1-docker.pkg.dev/demo-project/app-images/api@sha256:9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c", "size": "21.7 MiB", "mediaType": "application/vnd.docker.distribution.manifest.v2+json", "built": "2025-09-28 17:15 UTC", "updated": "2025-09-28 17:22 UTC"}), "", "2025-09-28T17:22:00Z", nil),
		extra(artifactSigTagVersion("projects/demo-project/locations/us-central1/repositories/app-images/packages/api/versions/sha256:7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e", "sha256-9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c.sig", map[string]string{"tags": "sha256-9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c.sig", "digest": "sha256:7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e", "signedDigest": "sha256:9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c", "uri": "us-central1-docker.pkg.dev/demo-project/app-images/api@sha256:7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e", "size": "980 B", "mediaType": "application/vnd.oci.image.manifest.v1+json", "updated": "2025-09-28 17:23 UTC"}), "", "2025-09-28T17:23:00Z", nil),
		extra(artifactVersion("projects/demo-project/locations/us/repositories/py-packages/packages/example-utils/versions/2.3.0", "2.3.0", "PYTHON", map[string]string{"version": "2.3.0", "updated": "2025-09-12 09:05 UTC"}), "", "2025-09-12T09:05:00Z", nil),
		artifactSignature(webFrontendDigest, "sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90", "https://github.com/example/web/.github/workflows/release.yml@refs/heads/main", "KEYLESS", "2025-10-01T12:41:00Z",
			map[string]string{
				"signing":          "keyless",
				"identity":         "https://github.com/example/web/.github/workflows/release.yml@refs/heads/main",
				"issuer":           "https://token.actions.githubusercontent.com",
				"source":           "OCI referrer",
				"artifactType":     "application/vnd.dev.sigstore.bundle.v0.3+json",
				"predicateType":    "https://sigstore.dev/cosign/sign/v1",
				"manifestDigest":   "sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90",
				"payloadDigest":    "sha256:11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff",
				"rekorLogIndex":    "142857910",
				"integratedTime":   "2025-10-01 12:41 UTC",
				"certificateValid": "2025-10-01 12:40 UTC to 2025-10-01 12:50 UTC",
				"verified":         "no (seiner does not verify signatures)",
			},
			[]model.Copy{
				{Title: "Signature", Label: "signature", Text: "demo-fixture-signature-base64"},
				{Title: "Certificate (PEM)", Label: "certificate", Text: "demo fixture: not a real PEM document\n"},
				{Title: "Sigstore bundle (JSON)", Label: "Sigstore bundle", Text: `{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}`},
			}),
		artifactSignature(webFrontendDigest, "sha256:e4a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e80", "gcpkms://projects/demo-project/locations/us-central1/keyRings/app-keys/cryptoKeys/release-signing", "KEY", "",
			map[string]string{
				"signing":        "key",
				"keyHint":        "gcpkms://projects/demo-project/locations/us-central1/keyRings/app-keys/cryptoKeys/release-signing",
				"source":         "tag (sha256-3f9a1c2b7d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8.sig)",
				"artifactType":   "application/vnd.dev.cosign.simplesigning.v1+json",
				"manifestDigest": "sha256:e4a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e80",
				"verified":       "no (seiner does not verify signatures)",
			},
			[]model.Copy{
				{Title: "Signature", Label: "signature", Text: "demo-fixture-kms-signature-base64"},
			}),
		artifactSignature(apiDigest, "sha256:7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e", "ci-deployer@demo-project.iam.gserviceaccount.com", "KEYLESS", "2025-09-28T17:23:00Z",
			map[string]string{
				"signing":        "keyless",
				"identity":       "ci-deployer@demo-project.iam.gserviceaccount.com",
				"issuer":         "https://accounts.google.com",
				"source":         "tag (sha256-9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c.sig)",
				"artifactType":   "application/vnd.dev.cosign.simplesigning.v1+json",
				"manifestDigest": "sha256:7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e",
				"rekorLogIndex":  "141902344",
				"integratedTime": "2025-09-28 17:23 UTC",
				"verified":       "no (seiner does not verify signatures)",
			},
			[]model.Copy{
				{Title: "Signature", Label: "signature", Text: "demo-fixture-api-signature-base64"},
				{Title: "Certificate (PEM)", Label: "certificate", Text: "demo fixture: not a real PEM document\n"},
			}),
		vulnerability("projects/demo-project/occurrences/7c1e9a52-0d3b-4f7e-9a61-2b8f4c3d5e01", "CVE-2024-45337", webFrontendDigest, "CRITICAL", "Misuse of ServerConfig.PublicKeyCallback may cause authorization bypass in golang.org/x/crypto", map[string]string{"cvss": "9.1 (v3)", "fixAvailable": "yes", "package": "golang.org/x/crypto", "packageType": "GO", "installed": "v0.28.0", "fixedIn": "v0.31.0", "path": "/app/server"}),
		vulnerability("projects/demo-project/occurrences/1f4b2c8d-6e9a-4b3c-8d21-5a7e0c9f1b02", "CVE-2023-44487", webFrontendDigest, "HIGH", "HTTP/2 rapid reset can cause excessive work in net/http and golang.org/x/net", map[string]string{"cvss": "7.5 (v3)", "fixAvailable": "yes", "package": "golang.org/x/net", "packageType": "GO", "installed": "v0.15.0", "fixedIn": "v0.17.0", "path": "/app/server"}),
		vulnerability("projects/demo-project/occurrences/5e2a7c91-3b6d-4f0e-a8c4-9d1b2e3f4a05", "CVE-2023-5678", webFrontendDigest, "MEDIUM", "Generating or checking excessively long X9.42 DH keys or parameters may be very slow in OpenSSL", map[string]string{"cvss": "5.3 (v3)", "fixAvailable": "yes", "package": "openssl", "packageType": "OS", "installed": "3.0.11-1~deb12u1", "fixedIn": "3.0.13-1~deb12u1"}),
		vulnerability("projects/demo-project/occurrences/a93d5e17-2c4f-4e8b-b0d6-8f1a3c5e7d03", "CVE-2011-3374", apiDigest, "LOW", "apt does not correctly validate repository keys", map[string]string{"cvss": "3.7 (v3)", "fixAvailable": "no", "package": "apt", "packageType": "OS", "installed": "2.6.1"}),
		under(model.BindingResource(appImagesID, "projects/demo-project", "roles/artifactregistry.reader", []string{"serviceAccount:gke-nodes@demo-project.iam.gserviceaccount.com"}, ""), appImagesID, model.FacetPermissions),
		under(model.BindingResource(appImagesID, "projects/demo-project", "roles/artifactregistry.writer", []string{"serviceAccount:ci-deployer@demo-project.iam.gserviceaccount.com"}, ""), appImagesID, model.FacetPermissions),
		under(model.BindingResource(pyPackagesID, "projects/demo-project", "roles/artifactregistry.reader", []string{"group:developers@example.com"}, ""), pyPackagesID, model.FacetPermissions),
		gcsChild("projects/demo-project/global/firewalls/default-allow-internal", "default-allow-internal", "projects/demo-project", "projects/demo-project/global/networks/default", model.FacetFirewalls, model.ServiceFirewall, map[string]string{"network": "default", "direction": "INGRESS", "priority": "65534", "allowed": "tcp:0-65535;udp:0-65535;icmp", "sourceRanges": "10.128.0.0/9", "logging": "off"}),
		gcsChild("projects/demo-project/global/firewalls/default-allow-ssh", "default-allow-ssh", "projects/demo-project", "projects/demo-project/global/networks/default", model.FacetFirewalls, model.ServiceFirewall, map[string]string{"network": "default", "direction": "INGRESS", "priority": "65534", "allowed": "tcp:22", "sourceRanges": "0.0.0.0/0", "logging": "on"}),

		extra(addressFix("projects/demo-project/global/addresses/web-ip", "web-ip", "Frontend anycast IP", "IN_USE", map[string]string{"address": "203.0.113.110", "addressType": "EXTERNAL", "ipVersion": "IPV4", "networkTier": "PREMIUM", "usedBy": "forwardingRules/web-https"}, "forwardingRules/web-https"), "IN_USE", "2025-01-15T18:30:00Z", map[string]string{"env": "prod"}),
		extra(addressFix("projects/demo-project/regions/us-central1/addresses/api-ilb-ip", "api-ilb-ip", "Internal API frontend", "IN_USE", map[string]string{"address": "10.128.0.50", "addressType": "INTERNAL", "purpose": "GCE_ENDPOINT", "subnetwork": "default", "usedBy": "forwardingRules/api-ilb"}, "forwardingRules/api-ilb"), "IN_USE", "2025-03-04T10:00:00Z", nil),
		extra(addressFix("projects/demo-project/global/addresses/google-managed-services-default", "google-managed-services-default", "Private services access range", "RESERVED", map[string]string{"address": "10.60.0.0/16", "addressType": "INTERNAL", "purpose": "VPC_PEERING", "network": "default"}), "RESERVED", "2023-06-30T12:10:00Z", nil),
		webHTTPSRule(),
		apiILBRule(),
		extra(ordered(res("projects/demo-project/regions/us-central1/forwardingRules/partner-psc", "partner-psc", model.ServicePSCEndpoint, "projects/demo-project", "us-central1", model.StatusWarning, "Endpoint for a partner service", map[string]string{"ipAddress": "10.128.0.60", "targetType": "Published service", "target": "serviceAttachments/partner-api", "network": "default", "subnetwork": "default"}), pscEndpointFields...), "PENDING", "2025-09-20T09:00:00Z", nil),
		extra(ordered(res("projects/demo-project/global/forwardingRules/googleapis", "googleapis", model.ServicePSCEndpoint, "projects/demo-project", "global", "", "Private access to Google APIs", map[string]string{"ipAddress": "10.255.0.2", "targetType": "Google APIs", "target": "all-apis", "network": "default"}), pscEndpointFields...), "", "2024-02-11T08:00:00Z", nil),
		sslPolicyFix("projects/demo-project/global/sslPolicies/modern-tls", "modern-tls", "TLS 1.2+ for public frontends", map[string]string{"profile": "MODERN", "minTlsVersion": "TLS_1_2", "targets": "1"}, "targetHttpsProxies/web-proxy"),
		sslPolicyFix("projects/demo-project/regions/us-central1/sslPolicies/legacy-clients", "legacy-clients", "Kept for old partner clients", map[string]string{"profile": "COMPATIBLE", "minTlsVersion": "TLS_1_0", "targets": "0"}),
		extra(ordered(lbFix(webLB, "web-map", "global", "Public web load balancer", map[string]string{"type": "Application (external)", "frontends": "203.0.113.110:443", "backends": "web-backend, backendBuckets/static-assets", "hosts": "example.com, www.example.com", "certificates": "web-cert", "sslPolicy": "modern-tls", "cloudArmor": "web-armor", "cdn": "on"}), lbFields...), "", "2025-01-15T18:20:00Z", nil),
		extra(ordered(lbFix(apiLB, "api-backend", "us-central1", "Internal API backends", map[string]string{"type": "Network passthrough (internal)", "frontends": "10.128.0.50:8080", "backends": "api-backend"}), lbFields...), "", "2025-03-04T09:55:00Z", nil),
		under(webHTTPSRule(), webLB, model.FacetFrontends),
		under(apiILBRule(), apiLB, model.FacetFrontends),
		under(webBackend(), webLB, model.FacetBackends),
		under(apiBackend(), apiLB, model.FacetBackends),
		webHealthCheck(),
		apiHealthCheck(),
		under(webHealthCheck(), webLB, model.FacetHealthChecks),
		under(apiHealthCheck(), apiLB, model.FacetHealthChecks),
		extra(armorFix("projects/demo-project/global/securityPolicies/web-armor", "web-armor", "Edge protection for the public site",
			map[string]string{"type": "CLOUD_ARMOR", "rules": "4", "adaptiveProtection": "on", "usedBy": "backendServices/web-backend"},
			"900  deny(403)  origin.region_code == 'XX'",
			"1000  deny(403)  evaluatePreconfiguredWaf('sqli-v33-stable')",
			"2000  throttle  true  (preview)",
			"default  allow  src *"), "", "2025-01-20T11:00:00Z", map[string]string{"team": "web"}),
		gcsChild("projects/demo-project/global/routes/default-route-internet", "default-route-internet", "projects/demo-project", "projects/demo-project/global/networks/default", model.FacetRoutes, model.ServiceRoute, map[string]string{"destination": "0.0.0.0/0", "priority": "1000", "nextHop": "default-internet-gateway", "network": "default", "routeType": "STATIC"}),
		gcsChild("projects/demo-project/global/routes/onprem-via-vpn", "onprem-via-vpn", "projects/demo-project", "projects/demo-project/global/networks/default", model.FacetRoutes, model.ServiceRoute, map[string]string{"destination": "192.168.0.0/16", "priority": "100", "nextHop": "vpnTunnels/onprem-tunnel", "network": "default", "routeType": "STATIC"}),
		extra(ordered(res("projects/demo-project/zones/us-central1-a/disks/web-1", "web-1", model.ServiceDisk, "projects/demo-project", "us-central1-a", model.StatusHealthy, "", map[string]string{"type": "pd-balanced", "size": "20 GB", "attachedTo": "web-1", "sourceImage": "debian-12-bookworm-v20250910", "architecture": "X86_64", "encryption": "Google-managed", "lastAttached": "2025-01-15 18:04 UTC"}), diskFields...), "READY", "2025-01-15T18:04:00Z", map[string]string{"env": "prod"}),
		extra(ordered(res("projects/demo-project/regions/us-central1/disks/shared-data", "shared-data", model.ServiceDisk, "projects/demo-project", "us-central1", model.StatusHealthy, "Regional data disk", map[string]string{"type": "pd-ssd", "size": "200 GB", "attachedTo": "none", "replicaZones": "us-central1-a, us-central1-b", "encryption": "projects/demo-project/locations/us-central1/keyRings/data/cryptoKeys/disks"}), diskFields...), "READY", "2025-05-02T12:00:00Z", nil),
		extra(ordered(res("projects/demo-project/global/instanceTemplates/web-template-v3", "web-template-v3", model.ServiceInstanceTemplate, "projects/demo-project", "global", "", "Frontend VM template", map[string]string{"machineType": "e2-medium", "bootImage": "debian-12-bookworm-v20250910", "bootDisk": "20 GB pd-balanced", "network": "default", "subnetwork": "default", "externalIP": "off", "serviceAccount": "web@demo-project.iam.gserviceaccount.com", "networkTags": "http-server", "provisioning": "STANDARD"}), templateFields...), "", "2025-09-10T08:00:00Z", map[string]string{"team": "web"}),
		extra(mig(res(webIG, "web-ig", model.ServiceInstanceGroup, "projects/demo-project", "us-central1-a", model.StatusWarning, "Frontend pool", map[string]string{"targetSize": "3", "instances": "none 2, creating 1", "template": "web-template-v3", "autoscaler": "web-ig-autoscaler", "autoscaling": "2–6, CPU 65%", "recommendedSize": "3", "updatePolicy": "PROACTIVE", "updateLimits": "surge 1, unavailable 0", "minimalAction": "REPLACE", "replacementMethod": "SUBSTITUTE", "autoHealing": "web-hc", "autoHealingDelay": "5m0s", "namedPorts": "http:80", "baseInstanceName": "web"})), "UPDATING", "2025-01-15T18:00:00Z", nil),
		managedInstance("demo-project", webIG, model.FacetInstances, "web-ig", "us-central1-a", "web-x7k2", "RUNNING", "NONE", "HEALTHY (web-hc)"),
		managedInstance("demo-project", webIG, model.FacetInstances, "web-ig", "us-central1-a", "web-p3m9", "RUNNING", "NONE", "HEALTHY (web-hc)"),
		managedInstance("demo-project", webIG, model.FacetInstances, "web-ig", "us-central1-a", "web-q8d4", "STAGING", "CREATING", ""),
		orgPolicy("projects/demo-project", "storage.uniformBucketLevelAccess", map[string]string{"enforcement": "enforced", "updated": "2024-02-11 09:00 UTC"}),
		listPolicy(orgPolicy("projects/demo-project", "compute.vmExternalIpAccess", map[string]string{"enforcement": "allow projects/demo-project/zones/us-central1-a/instances/web-1", "inheritFromParent": "off", "updated": "2025-01-15 18:00 UTC"})),
		policyValue("projects/demo-project", "compute.vmExternalIpAccess", 1, "allow", "projects/demo-project/zones/us-central1-a/instances/web-1", "", false),
		secretFix("db-password", map[string]string{"replication": "automatic", "rotation": "30 days", "nextRotation": "2026-11-01 00:00 UTC", "topics": "secret-events"}, "2024-05-02T08:10:00Z", map[string]string{"env": "prod", "app": "web"}, "current → 3"),
		secretVersion("db-password", "3", "ENABLED", "2026-10-01T00:00:00Z", nil),
		secretVersion("db-password", "2", "DISABLED", "2026-09-01T00:00:00Z", nil),
		secretVersion("db-password", "1", "DESTROYED", "2024-05-02T08:10:00Z", map[string]string{"destroyed": "2025-01-10 12:00 UTC"}),
		secretFix("stripe-api-key", map[string]string{"replication": "user-managed (us-central1, us-east1)", "kmsKey": "projects/demo-project/locations/us-central1/keyRings/app-keys/cryptoKeys/secrets", "destroyDelay": "7 days"}, "2025-03-04T11:00:00Z", map[string]string{"app": "checkout"}),
		secretVersion("stripe-api-key", "1", "ENABLED", "2025-03-04T11:00:00Z", nil),
		kmsRing("us-central1", "app-keys", "2024-04-01T09:00:00Z"),
		kmsKey("us-central1", "app-keys", "secrets", "ENABLED", map[string]string{"purpose": "ENCRYPT_DECRYPT", "algorithm": "GOOGLE_SYMMETRIC_ENCRYPTION", "protectionLevel": "SOFTWARE", "primaryVersion": "4", "rotation": "90 days", "nextRotation": "2026-12-01 00:00 UTC", "destroyDelay": "30 days"}),
		kmsKey("us-central1", "app-keys", "gcs-cmek", "ENABLED", map[string]string{"purpose": "ENCRYPT_DECRYPT", "algorithm": "GOOGLE_SYMMETRIC_ENCRYPTION", "protectionLevel": "HSM", "primaryVersion": "1", "destroyDelay": "30 days"}),
		kmsKey("us-central1", "app-keys", "release-signing", "", map[string]string{"purpose": "ASYMMETRIC_SIGN", "algorithm": "EC_SIGN_P256_SHA256", "protectionLevel": "HSM", "versions": "1 enabled, 1 destroy scheduled", "template": "EC_SIGN_P384_SHA384 HSM", "destroyDelay": "30 days"}),
		kmsVersion("us-central1", "app-keys", "secrets", "4", "ENABLED", true, "2026-09-02T00:00:00Z", map[string]string{"algorithm": "GOOGLE_SYMMETRIC_ENCRYPTION", "protectionLevel": "SOFTWARE", "generated": "2026-09-02 00:00 UTC"}),
		kmsVersion("us-central1", "app-keys", "secrets", "3", "ENABLED", false, "2026-06-04T00:00:00Z", map[string]string{"algorithm": "GOOGLE_SYMMETRIC_ENCRYPTION", "protectionLevel": "SOFTWARE", "generated": "2026-06-04 00:00 UTC"}),
		kmsVersion("us-central1", "app-keys", "secrets", "2", "DESTROY_SCHEDULED", false, "2026-03-06T00:00:00Z", map[string]string{"algorithm": "GOOGLE_SYMMETRIC_ENCRYPTION", "protectionLevel": "SOFTWARE", "destroyScheduled": "2026-11-20 00:00 UTC"}),
		kmsVersion("us-central1", "app-keys", "secrets", "1", "DESTROYED", false, "2024-04-01T09:00:00Z", map[string]string{"algorithm": "GOOGLE_SYMMETRIC_ENCRYPTION", "protectionLevel": "SOFTWARE", "destroyed": "2025-01-10 12:00 UTC"}),
		kmsVersion("us-central1", "app-keys", "gcs-cmek", "1", "ENABLED", true, "2024-04-01T09:00:00Z", map[string]string{"algorithm": "GOOGLE_SYMMETRIC_ENCRYPTION", "protectionLevel": "HSM", "generated": "2024-04-01 09:00 UTC"}),
		kmsVersion("us-central1", "app-keys", "release-signing", "2", "ENABLED", false, "2025-08-01T00:00:00Z", map[string]string{"algorithm": "EC_SIGN_P256_SHA256", "protectionLevel": "HSM", "generated": "2025-08-01 00:00 UTC"}),
		kmsVersion("us-central1", "app-keys", "release-signing", "1", "DESTROY_SCHEDULED", false, "2024-04-01T09:00:00Z", map[string]string{"algorithm": "EC_SIGN_P256_SHA256", "protectionLevel": "HSM", "destroyScheduled": "2026-11-15 00:00 UTC"}),
		kmsImportJob("us-central1", "app-keys", "migrate-legacy", "EXPIRED", "2025-02-01T10:00:00Z", map[string]string{"method": "RSA_OAEP_3072_SHA256", "protectionLevel": "HSM", "generated": "2025-02-01 10:00 UTC", "expires": "2025-02-04 10:00 UTC", "expired": "2025-02-04 10:00 UTC"}),
		kmsRingBinding("us-central1", "app-keys", "roles/cloudkms.admin", "group:security@example.com"),
		kmsKeyBinding("us-central1", "app-keys", "secrets", "roles/cloudkms.cryptoKeyEncrypterDecrypter", "serviceAccount:service-123456789012@gcp-sa-secretmanager.iam.gserviceaccount.com"),
		kmsKeyBinding("us-central1", "app-keys", "gcs-cmek", "roles/cloudkms.cryptoKeyEncrypterDecrypter", "serviceAccount:service-123456789012@gs-project-accounts.iam.gserviceaccount.com"),
		kmsKeyBinding("us-central1", "app-keys", "release-signing", "roles/cloudkms.signerVerifier", "serviceAccount:ci-deployer@demo-project.iam.gserviceaccount.com"),
		kmsRing("us-east1", "audit-keys", "2023-07-01T00:00:00Z"),
		kmsKey("us-east1", "audit-keys", "logs", "DISABLED", map[string]string{"purpose": "ENCRYPT_DECRYPT", "algorithm": "GOOGLE_SYMMETRIC_ENCRYPTION", "protectionLevel": "SOFTWARE", "primaryVersion": "2", "destroyDelay": "30 days"}),
		kmsVersion("us-east1", "audit-keys", "logs", "2", "DISABLED", true, "2024-07-01T00:00:00Z", map[string]string{"algorithm": "GOOGLE_SYMMETRIC_ENCRYPTION", "protectionLevel": "SOFTWARE", "generated": "2024-07-01 00:00 UTC"}),
		kmsVersion("us-east1", "audit-keys", "logs", "1", "IMPORT_FAILED", false, "2023-07-01T00:00:00Z", map[string]string{"algorithm": "GOOGLE_SYMMETRIC_ENCRYPTION", "protectionLevel": "SOFTWARE", "importJob": "bootstrap", "failure": "wrapped key length does not match the algorithm", "reimportEligible": "yes"}),
		kmsImportJob("us-east1", "audit-keys", "bootstrap", "ACTIVE", "2026-10-30T08:00:00Z", map[string]string{"method": "RSA_OAEP_4096_SHA256_AES_256", "protectionLevel": "SOFTWARE", "generated": "2026-10-30 08:00 UTC", "expires": "2026-11-02 08:00 UTC"}),
		kmsRingBinding("us-east1", "audit-keys", "roles/cloudkms.viewer", "group:auditors@example.com"),
		kmsKeyBinding("us-east1", "audit-keys", "logs", "roles/cloudkms.cryptoKeyEncrypterDecrypter", "serviceAccount:service-123456789012@gcp-sa-logging.iam.gserviceaccount.com"),
		logSink("projects/demo-project", "_Required", "", map[string]string{"destinationType": "Log bucket", "destination": "logging.googleapis.com/projects/demo-project/locations/global/buckets/_Required", "filter": "LOG_ID(\"cloudaudit.googleapis.com/activity\") OR LOG_ID(\"externalaudit.googleapis.com/activity\") OR LOG_ID(\"cloudaudit.googleapis.com/system_event\")"}, false),
		logSink("projects/demo-project", "_Default", "", map[string]string{"destinationType": "Log bucket", "destination": "logging.googleapis.com/projects/demo-project/locations/global/buckets/_Default", "filter": "NOT LOG_ID(\"cloudaudit.googleapis.com/activity\") AND NOT LOG_ID(\"externalaudit.googleapis.com/activity\")", "exclusions": "1"}, false, "drop-health-checks  httpRequest.requestUrl=\"/healthz\""),
		logSink("projects/demo-project", "audit-to-bq", "Audit logs to BigQuery", map[string]string{"destinationType": "BigQuery", "destination": "bigquery.googleapis.com/projects/demo-project/datasets/audit", "filter": "logName:\"cloudaudit.googleapis.com\"", "writerIdentity": "serviceAccount:service-123456789012@gcp-sa-logging.iam.gserviceaccount.com"}, false),
		logSink("projects/demo-project", "debug-to-pubsub", "Old debug stream", map[string]string{"destinationType": "Pub/Sub", "destination": "pubsub.googleapis.com/projects/demo-project/topics/debug-logs", "filter": "severity>=DEBUG"}, true),
		logBucket("global", "_Required", "Audit bucket", "400 days", "on", "off", "2023-01-01T00:00:00Z"),
		logBucket("global", "_Default", "Default bucket", "30 days", "off", "on", "2023-01-01T00:00:00Z"),
		logBucket("us-central1", "app-logs", "Application logs, 90-day retention", "90 days", "off", "on", "2024-09-12T15:00:00Z"),
		logView("global", "_Required", "_AllLogs", "All logs in the bucket", "(all logs)"),
		logView("global", "_Default", "_AllLogs", "All logs in the bucket", "(all logs)"),
		logLink("global", "_Default", "default_logs", "2025-06-02T10:00:00Z"),
		logView("us-central1", "app-logs", "_AllLogs", "All logs in the bucket", "(all logs)"),
		logView("us-central1", "app-logs", "web-only", "Frontend logs for the web team", `resource.type="gce_instance" AND labels.app="web"`),
		logLink("us-central1", "app-logs", "app_logs_analytics", "2025-01-08T09:30:00Z"),
		logMetric("checkout_errors", "Checkout 5xx responses", map[string]string{"filter": `resource.type="k8s_container" AND httpRequest.status>=500`, "type": "DELTA INT64", "unit": "1", "updated": "2025-04-02 11:00 UTC"}, false, "route  EXTRACT(httpRequest.requestUrl)", "status  EXTRACT(httpRequest.status)"),
		logMetric("batch_latency", "Nightly batch duration (retired)", map[string]string{"filter": `logName="projects/demo-project/logs/batch"`, "type": "DELTA DISTRIBUTION", "unit": "ms", "valueExtractor": "EXTRACT(jsonPayload.duration_ms)"}, true),
		dnsZone("example-com", "Public zone for example.com", map[string]string{"dnsName": "example.com.", "visibility": "public", "dnssec": "on", "nameServers": "ns-cloud-a1.googledomains.com., ns-cloud-a2.googledomains.com., ns-cloud-a3.googledomains.com., ns-cloud-a4.googledomains.com.", "logging": "off"}, "2023-02-10T09:00:00Z"),
		dnsRecord("example-com", "example.com.", "SOA", 21600, "ns-cloud-a1.googledomains.com. cloud-dns-hostmaster.google.com. 1 21600 3600 259200 300"),
		dnsRecord("example-com", "example.com.", "NS", 21600, "ns-cloud-a1.googledomains.com.", "ns-cloud-a2.googledomains.com.", "ns-cloud-a3.googledomains.com.", "ns-cloud-a4.googledomains.com."),
		dnsRecord("example-com", "example.com.", "A", 300, "203.0.113.110"),
		dnsRecord("example-com", "example.com.", "MX", 3600, "1 smtp.google.com."),
		dnsRecord("example-com", "example.com.", "TXT", 3600, "\"v=spf1 include:_spf.google.com ~all\"", "\"google-site-verification=abc123\""),
		dnsRecord("example-com", "www.example.com.", "CNAME", 300, "example.com."),
		dnsRecord("example-com", "api.example.com.", "A", 300, "203.0.113.111"),
		dnsRecord("example-com", "api.example.com.", "AAAA", 300, "2600:1901:0:1234::"),
		dnsZone("corp-internal", "Private zone for internal services", map[string]string{"dnsName": "corp.internal.", "visibility": "private", "networks": "default", "logging": "on"}, "2024-03-01T10:30:00Z"),
		dnsRecord("corp-internal", "postgres.corp.internal.", "A", 300, "10.20.0.3"),
		dnsRecord("corp-internal", "redis.corp.internal.", "A", 300, "10.30.0.4"),
		dnsRecord("corp-internal", "api.corp.internal.", "A", 60, "10.128.0.50"),
		extra(ordered(routerFix("projects/demo-project/regions/us-central1/routers/nat-router", "nat-router", "Egress and on-prem BGP", map[string]string{"network": "default", "asn": "64512", "advertiseMode": "DEFAULT", "interfaces": "1", "bgpPeers": "1", "natGateways": "1"}), "network", "asn", "advertiseMode", "interfaces", "bgpPeers", "natGateways"), "", "2024-03-01T10:00:00Z", nil),
		ordered(gcsChild("projects/demo-project/regions/us-central1/routers/nat-router/nats/egress-nat", "egress-nat", "projects/demo-project", "projects/demo-project/regions/us-central1/routers/nat-router", model.FacetNATs, model.ServiceCloudNAT, map[string]string{"ipAllocation": "AUTO_ONLY", "sourceRanges": "ALL_SUBNETWORKS_ALL_IP_RANGES", "minPortsPerVm": "64", "dynamicPorts": "off", "endpointIndependentMapping": "off", "logging": "ERRORS_ONLY"}), natFields...),
		attachmentFix("projects/demo-project/regions/us-central1/serviceAttachments/api-attachment", "api-attachment", "Publishes the internal API to consumers",
			map[string]string{"targetService": "forwardingRules/api-ilb", "connectionPreference": "ACCEPT_MANUAL", "endpoints": "1", "natSubnets": "psc-nat", "proxyProtocol": "off"},
			[]string{"consumer-project/api-endpoint (consumer-project/default) ACCEPTED"}, []string{"consumer-project (limit 10)"}),
	}, slices.Concat(roleFixtures(), bigQueryFixtures(), swpFixtures(), iapFixtures(), certFixtures(), privatecaFixtures(), acmFixtures(), vpcscFixtures(), workstationFixtures(), workspaceFixtures(), networkConnectivityFixtures(),
		kmsAttestation("us-central1", "app-keys", "gcs-cmek", "1"), kmsAttestation("us-central1", "app-keys", "release-signing", "2"),
		kmsAttestation("us-central1", "app-keys", "release-signing", "1"))...)
}

var (
	bucketFields = []string{"storageClass", "locationType", "dataLocations", "turboReplication", "access",
		"publicAccessPrevention", "versioning", "softDelete", "retention", "objectRetention", "defaultEventBasedHold",
		"lifecycleRules", "autoclass", "hierarchicalNamespace", "requesterPays", "kmsKey", "accessLogs", "updated"}
	clusterFields = []string{"statusMessage", "mode", "version", "nodeVersion", "releaseChannel", "locationType", "nodes",
		"nodePools", "endpoint", "publicEndpoint", "dnsEndpoint", "authorizedNetworks", "privateNodes", "network",
		"subnetwork", "podRange", "servicesRange", "dataplane", "workloadPool", "etcdEncryption", "shieldedNodes",
		"binaryAuthorization", "maintenance", "maintenanceExclusions"}
	nodePoolFields = []string{"statusMessage", "machineType", "accelerators", "version", "autoscaling",
		"initialNodes", "zones", "disk", "kmsKey", "imageType", "provisioning", "sandbox", "serviceAccount",
		"metadataServer", "shieldedVM", "autoUpgrade", "autoRepair", "upgradeStrategy", "maxPodsPerNode",
		"taints", "nodeLabels", "instanceGroups"}
	zoneFields   = []string{"dnsName", "visibility", "dnssec", "nameServers", "networks", "gkeClusters", "forwarding", "peerNetwork", "logging"}
	objectFields = []string{"size", "contentType", "storageClass", "updated", "generation"}
	repoFields   = []string{"format", "mode", "registryUri", "upstream", "remoteSource", "upstreamCredentials",
		"upstreamValidation", "upstreams", "size", "vulnerabilityScanning", "immutableTags", "versionPolicy",
		"snapshotOverwrites", "cleanupPolicies", "kmsKey", "updated"}
	packageFields = []string{"format", "displayName", "updated"}
	versionFields = []string{"format", "tags", "digest", "signedDigest", "version", "uri", "size", "mediaType", "artifactType", "built", "updated"}
	sigFields     = []string{"signing", "identity", "issuer", "keyHint", "source", "artifactType", "predicateType",
		"manifestDigest", "payloadDigest", "rekorLogIndex", "rekorLogId", "integratedTime", "certificateValid",
		"serial", model.MetaArtifactImage, "verified"}
	vulnFields = []string{"cvss", "noteSeverity", "fixAvailable", "package", "packageType", "installed",
		"fixedIn", "path", "image", "published", "updated"}
)

const (
	webFrontendDigest = "projects/demo-project/locations/us-central1/repositories/app-images/packages/web%2Ffrontend/versions/sha256:3f9a1c2b7d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8"
	apiDigest         = "projects/demo-project/locations/us-central1/repositories/app-images/packages/api/versions/sha256:9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c"
)

// ordered sets r's display order to match the live adapter's.
func ordered(r model.Resource, keys ...string) model.Resource {
	r.Fields = model.FieldsOf(r.Metadata, keys...)
	return r
}

// extra sets the typed fields that only some fixtures carry. created is
// RFC 3339 or "".
func extra(r model.Resource, state, created string, labels map[string]string) model.Resource {
	r.State = state
	r.Labels = labels
	if created != "" {
		t, err := time.Parse(time.RFC3339, created)
		if err != nil {
			panic(err)
		}
		r.Created = t
	}
	return r
}

// status sets the coarse bucket of a fixture whose state reports health.
func status(r model.Resource, st model.ResourceStatus) model.Resource {
	r.Status = st
	return r
}

// under places a copy of r under parent's facet, as a load balancer lists
// its frontends, backends, and health checks.
func under(r model.Resource, parent, facet string) model.Resource {
	r.ParentID = parent
	r.FacetID = facet
	return r
}

const dnsPrefix = "projects/demo-project/managedZones/"

// dnsZone is a managed zone that expands to its record sets.
func dnsZone(name, desc string, md map[string]string, created string) model.Resource {
	r := res(dnsPrefix+name, name, model.ServiceCloudDNS, "projects/demo-project", "global", "", desc, md)
	r.Facets = []model.Facet{{ID: model.FacetRecords, Label: "Record Sets", Inline: true}}
	return extra(ordered(r, zoneFields...), "", created, nil)
}

func dnsRecord(zone, name, typ string, ttl int, data ...string) model.Resource {
	md := map[string]string{"type": typ, "ttl": strconv.Itoa(ttl)}
	if len(data) == 1 {
		md["data"] = data[0]
	} else {
		md["data"] = strconv.Itoa(len(data)) + " values"
	}
	r := res(dnsPrefix+zone+"/rrsets/"+name+"/"+typ, name, model.ServiceDNSRecord, "projects/demo-project", "global", "", "", md)
	r.Badge = typ
	if len(data) > 1 {
		r.Lists = []model.List{{ID: "data", Label: "Data", Items: data}}
	}
	return under(ordered(r, "type", "ttl", "data", "routing"), dnsPrefix+zone, model.FacetRecords)
}

// orgPolicy is a policy set directly on scope. rules fill the Rules list
// as the live adapter does for conditional or multi-rule policies.
func orgPolicy(scope, constraint string, md map[string]string, rules ...string) model.Resource {
	r := res(scope+"/policies/"+constraint, constraint, model.ServiceOrgPolicy, scope, "global", "", "", md)
	if len(rules) > 0 {
		r.Lists = []model.List{{ID: "rules", Label: "Rules", Items: rules}}
	}
	return ordered(r, "enforcement", "inheritFromParent", "dryRun", "dryRunInheritFromParent", "updated")
}

// dryRun badges a policy that has a dry-run spec.
func dryRun(r model.Resource) model.Resource {
	r.Badge = "DRY-RUN"
	return r
}

// listPolicy is a policy with allowed or denied values, which it expands
// to as the live adapter does.
func listPolicy(r model.Resource) model.Resource {
	r.Facets = []model.Facet{{ID: model.FacetPolicyValues, Label: "Values", Inline: true}}
	return r
}

// policyValue is one allowed or denied value of rule n of the policy on
// scope. numbered names the rule, as the live adapter does when a policy
// has more than one.
func policyValue(scope, constraint string, n int, action, value, condition string, numbered bool) model.Resource {
	policy := scope + "/policies/" + constraint
	md := map[string]string{"action": action}
	if condition != "" {
		md["condition"] = condition
	}
	if numbered {
		md["rule"] = strconv.Itoa(n)
	}
	r := res(policy+"/rules/"+strconv.Itoa(n)+"/"+action+"/"+url.PathEscape(value), value, model.ServiceOrgPolicyValue, scope, "global", "", "", md)
	r.Badge = strings.ToUpper(action)
	return under(ordered(r, "action", "condition", "rule"), policy, model.FacetPolicyValues)
}

// resourceBinding is a demo-project binding set on the resource id, listed
// under its Permissions facet.
func resourceBinding(id, role string, members ...string) model.Resource {
	return under(model.BindingResource(id, "projects/demo-project", role, members, ""), id, model.FacetPermissions)
}

// logSink is a sink on scope; exclusions are "name  filter" lines.
func logSink(scope, name, desc string, md map[string]string, disabled bool, exclusions ...string) model.Resource {
	r := res(scope+"/sinks/"+name, name, model.ServiceLogSink, scope, "global", "", desc, md)
	if disabled {
		r.Status, r.State, r.Badge = model.StatusWarning, "DISABLED", "off"
	}
	if len(exclusions) > 0 {
		r.Lists = []model.List{{ID: "exclusions", Label: "Exclusions", Items: exclusions}}
	}
	return ordered(r, "destinationType", "destination", "filter", "exclusions", "includeChildren", "interceptChildren", "writerIdentity", "updated")
}

func logBucketID(loc, name string) string {
	return "projects/demo-project/locations/" + loc + "/buckets/" + name
}

// logBucket has Views, and Linked Datasets when Log Analytics is on.
func logBucket(loc, name, desc, retention, locked, analytics, created string) model.Resource {
	r := res(logBucketID(loc, name), name, model.ServiceLogBucket, "projects/demo-project", loc, model.StatusHealthy, desc, map[string]string{"retention": retention, "locked": locked, "analytics": analytics})
	r.Facets = []model.Facet{{ID: model.FacetViews, Label: "Views"}}
	if analytics == "on" {
		r.Facets = append(r.Facets, model.Facet{ID: model.FacetLinks, Label: "Linked Datasets"})
	}
	return extra(ordered(r, "retention", "locked", "analytics", "kmsKey", "kmsKeyVersion", "indexedFields", "restrictedFields", "updated"), "ACTIVE", created, nil)
}

func logView(loc, bucket, name, desc, filter string) model.Resource {
	r := res(logBucketID(loc, bucket)+"/views/"+name, name, model.ServiceLogView, "projects/demo-project", loc, "", desc, map[string]string{"filter": filter})
	return under(ordered(r, "filter", "updated"), logBucketID(loc, bucket), model.FacetViews)
}

func logLink(loc, bucket, name, created string) model.Resource {
	r := res(logBucketID(loc, bucket)+"/links/"+name, name, model.ServiceLogLink, "projects/demo-project", loc, model.StatusHealthy, "", map[string]string{"dataset": "demo-project." + name})
	return under(extra(ordered(r, "dataset"), "ACTIVE", created, nil), logBucketID(loc, bucket), model.FacetLinks)
}

// logMetric is a log-based metric; labels are "key  extractor" lines.
func logMetric(name, desc string, md map[string]string, disabled bool, labels ...string) model.Resource {
	r := res("projects/demo-project/metrics/"+name, name, model.ServiceLogMetric, "projects/demo-project", "global", "", desc, md)
	if disabled {
		r.Status, r.State, r.Badge = model.StatusWarning, "DISABLED", "off"
	}
	if len(labels) > 0 {
		r.Metadata["labels"] = strconv.Itoa(len(labels))
		r.Lists = []model.List{{ID: "labels", Label: "Label extractors", Items: labels}}
	}
	return ordered(r, "filter", "type", "unit", "valueExtractor", "labels", "bucket", "updated")
}

const (
	prodPool     = "projects/demo-project/locations/us-central1/clusters/production-cluster/nodePools/"
	checkoutPool = "projects/checkout-project/locations/us-central1/clusters/checkout/nodePools/default-pool"
)

// pool gives a node pool its instance groups ("zone/name") and the Nodes
// facet that lists their instances. Taints and node labels get sample
// lists when their counts are set.
func pool(r model.Resource, project string, groups ...string) model.Resource {
	items := make([]string, len(groups))
	for i, g := range groups {
		zone, name, _ := strings.Cut(g, "/")
		items[i] = "projects/" + project + "/zones/" + zone + "/instanceGroupManagers/" + name
	}
	r.Metadata["instanceGroups"] = strconv.Itoa(len(items))
	if r.Metadata["taints"] != "" {
		r.Lists = append(r.Lists, model.List{ID: "taints", Label: "Taints", Items: []string{"nvidia.com/gpu=present:NO_SCHEDULE"}})
	}
	if r.Metadata["nodeLabels"] != "" {
		r.Lists = append(r.Lists, model.List{ID: "nodeLabels", Label: "Node labels", Items: []string{"workload=web"}})
	}
	r.Lists = append(r.Lists, model.List{ID: model.ListInstanceGroups, Label: "Instance groups", Items: items})
	r.Facets = []model.Facet{{ID: model.FacetNodes, Label: "Nodes", Inline: true}}
	return ordered(r, nodePoolFields...)
}

func cluster(id, name, scope, loc, desc string, md map[string]string, lists ...string) model.Resource {
	r := res(id, name, model.ServiceGKE, scope, loc, model.StatusHealthy, desc, md)
	r.Facets = []model.Facet{{ID: model.FacetNodePools, Label: "Node Pools"}}
	// lists alternates a list id and its one item.
	for i := 0; i+1 < len(lists); i += 2 {
		label := map[string]string{"authorizedNetworks": "Authorized networks", "maintenanceExclusions": "Maintenance exclusions"}[lists[i]]
		r.Lists = append(r.Lists, model.List{ID: lists[i], Label: label, Items: []string{lists[i+1]}})
	}
	return ordered(r, clusterFields...)
}

func bucket(id, name, scope, loc, desc string, md map[string]string) model.Resource {
	r := res(id, name, model.ServiceCloudStorage, scope, loc, "", desc, md)
	r.Facets = []model.Facet{
		{ID: model.FacetPermissions, Label: "Permissions"},
		{ID: model.FacetObjects, Label: "Objects", Nested: true},
	}
	return r
}

func gcsChild(id, name, scope, parent, facet string, svc model.ServiceType, md map[string]string) model.Resource {
	loc := "global"
	if n, ok := model.ParseName(id); ok && n.Location != "" {
		loc = n.Location
	}
	r := res(id, name, svc, scope, loc, "", "", md)
	r.ParentID = parent
	r.FacetID = facet
	switch facet {
	case model.FacetObjects:
		return ordered(r, objectFields...)
	case model.FacetSubnets:
		return ordered(r, subnetFields...)
	case model.FacetFirewalls:
		return ordered(r, firewallFields...)
	case model.FacetRoutes:
		return ordered(r, routeFields...)
	case model.FacetNodePools:
		return ordered(r, nodePoolFields...)
	}
	return r
}

// Artifact Registry repositories with bindings in the fixtures.
const (
	appImagesID  = "projects/demo-project/locations/us-central1/repositories/app-images"
	pyPackagesID = "projects/demo-project/locations/us/repositories/py-packages"
)

// artifactRepo is an Artifact Registry repository with its Permissions and
// Packages facets, Packages labelled Images for Docker as the live adapter
// does.
func artifactRepo(id, name, desc string, md map[string]string) model.Resource {
	n, _ := model.ParseName(id)
	r := res(id, name, model.ServiceArtifactRegistry, n.Scope, n.Location, "", desc, md)
	label := "Packages"
	if md[model.MetaArtifactFormat] == "DOCKER" {
		label = "Images"
	}
	r.Facets = []model.Facet{{ID: model.FacetPermissions, Label: "Permissions"}, {ID: model.FacetPackages, Label: label}}
	if md["cleanupPolicies"] == "2" {
		r.Lists = []model.List{{ID: "cleanupPolicies", Label: "Cleanup policies", Items: []string{
			"delete-untagged  DELETE  untagged, older than 14d",
			"keep-releases  KEEP  10 most recent, packages web/",
		}}}
	}
	return ordered(r, repoFields...)
}

// artifactPackage is a repository's Packages facet child. Its parent is the
// repository named by the id prefix.
func artifactPackage(id, name, format string, md map[string]string) model.Resource {
	parent, _, _ := strings.Cut(id, "/packages/")
	md[model.MetaArtifactFormat] = format
	r := artifactChild(id, name, parent, model.FacetPackages, model.ServiceArtifactPackage, md, packageFields)
	if format == "DOCKER" {
		r.Facets = []model.Facet{
			{ID: model.FacetVersions, Label: "Digests"},
			{ID: model.FacetSignatureTags, Label: "Signature Tags", Optional: true},
		}
	} else {
		r.Facets = []model.Facet{{ID: model.FacetVersions, Label: "Versions", Inline: true}}
	}
	return r
}

// artifactVersion is a package's Versions facet child. Docker versions have
// Signatures and Vulnerabilities facets.
func artifactVersion(id, name, format string, md map[string]string) model.Resource {
	parent, _, _ := strings.Cut(id, "/versions/")
	md[model.MetaArtifactFormat] = format
	r := artifactChild(id, name, parent, model.FacetVersions, model.ServiceArtifactVersion, md, versionFields)
	if format == "DOCKER" {
		r.Facets = []model.Facet{
			{ID: model.FacetSignatures, Label: "Signatures", Optional: true},
			{ID: model.FacetVulnerabilities, Label: "Vulnerabilities", Optional: true},
		}
	}
	return r
}

// artifactSigTagVersion is a Docker package's .sig tag artifact under Signature Tags.
func artifactSigTagVersion(id, name string, md map[string]string) model.Resource {
	parent, _, _ := strings.Cut(id, "/versions/")
	md[model.MetaArtifactFormat] = "DOCKER"
	return artifactChild(id, name, parent, model.FacetSignatureTags, model.ServiceArtifactVersion, md, versionFields)
}

// artifactSignature is a Docker version's Signatures facet child.
func artifactSignature(version, sigID, name, badge, created string, md map[string]string, copies []model.Copy) model.Resource {
	md[model.MetaArtifactImage] = imageOf(version)
	r := artifactChild(version+"/signatures/"+url.PathEscape(sigID), name, version, model.FacetSignatures, model.ServiceArtifactSignature, md, sigFields)
	r.Badge = badge
	r.Copies = copies
	return extra(r, "", created, nil)
}

// vulnerability is a Docker version's Vulnerabilities facet child, with the
// severity as its state.
func vulnerability(id, cve, version, severity, summary string, md map[string]string) model.Resource {
	md[model.MetaArtifactImage] = imageOf(version)
	r := artifactChild(id, cve, version, model.FacetVulnerabilities, model.ServiceVulnerability, md, vulnFields)
	n, _ := model.ParseName(version)
	r.Location = n.Location
	r.Description = summary
	r.State = severity
	r.Badge = severity
	r.Lists = []model.List{{ID: "references", Label: "References", Items: []string{"https://nvd.nist.gov/vuln/detail/" + cve}}}
	switch severity {
	case "CRITICAL", "HIGH":
		r.Status = model.StatusError
	default:
		r.Status = model.StatusWarning
	}
	return r
}

func artifactChild(id, name, parent, facet string, svc model.ServiceType, md map[string]string, fields []string) model.Resource {
	n, _ := model.ParseName(id)
	r := res(id, name, svc, n.Scope, n.Location, "", "", md)
	r.ParentID = parent
	r.FacetID = facet
	return ordered(r, fields...)
}

// imageOf is the image address of a Docker version id, as the live adapter
// builds it.
func imageOf(versionID string) string {
	n, ok := model.ParseName(versionID)
	if !ok || len(n.Rest) != 4 {
		panic("not a version id: " + versionID)
	}
	return model.ArtifactRepositoryURI(n.Project, n.Location, "docker", n.Name) + "/" + n.Rest[1] + "@" + n.Rest[3]
}

// binding is an IAM role binding on a CRM node, built as the live adapter
// builds it.
func binding(scope, role, condition string, members ...string) model.Resource {
	return model.BindingResource(scope, scope, role, members, condition)
}

// serviceAccount is a project's IAM service account, built as the live
// adapter builds it.
func serviceAccount(project, email, display, desc, uniqueID string, disabled bool) model.Resource {
	md := map[string]string{"email": email, "displayName": display, model.MetaUniqueID: uniqueID}
	name, ok := strings.CutSuffix(email, "@"+project+".iam.gserviceaccount.com")
	if !ok {
		name = email
	}
	r := res("projects/"+project+"/serviceAccounts/"+email, name, model.ServiceServiceAccount, "projects/"+project, "global", model.StatusHealthy, desc, md)
	r.State = "ENABLED"
	if disabled {
		r.Status, r.State = model.StatusWarning, "DISABLED"
	}
	r.Facets = []model.Facet{{ID: model.FacetKeys, Label: "Keys", Inline: true}}
	return ordered(r, "email", "displayName", model.MetaUniqueID)
}

// saKey is a service account key, built as the live adapter builds it.
// expires is "never", a time, or "" when unknown; badge is USER, EXPIRED,
// or "".
func saKey(project, email, id, keyType, created, expires string, status model.ResourceStatus, badge string) model.Resource {
	md := map[string]string{"type": keyType, "origin": "GOOGLE_PROVIDED", "algorithm": "RSA_2048"}
	if expires != "" {
		md["expires"] = expires
	}
	account := "projects/" + project + "/serviceAccounts/" + email
	r := res(account+"/keys/"+id, id, model.ServiceServiceAccountKey, "projects/"+project, "global", status, "", md)
	r.Badge = badge
	state := "ENABLED"
	if status == model.StatusWarning && badge != "EXPIRED" {
		state = "DISABLED"
	}
	return under(extra(ordered(r, "type", "origin", "algorithm", "expires"), state, created, nil), account, model.FacetKeys)
}

// serviceAgent is a service agent that project's IAM policy grants roles,
// built as the live adapter builds it.
func serviceAgent(project, email string, roles ...string) model.Resource {
	r := res("projects/"+project+"/serviceAccounts/"+email, email, model.ServiceServiceAccount, "projects/"+project, "global", "", "Google-managed service agent", map[string]string{model.MetaEmail: email})
	r.Lists = []model.List{{ID: model.ListRoles, Label: "Roles in this project", Items: roles}}
	return ordered(r, model.MetaEmail)
}

// bucketBinding is a bucket's Permissions facet child.
func bucketBinding(bucket, role string, members ...string) model.Resource {
	scope, _, _ := strings.Cut(bucket, "/buckets/")
	r := model.BindingResource(bucket, scope, role, members, "")
	r.ParentID = bucket
	r.FacetID = model.FacetPermissions
	return r
}

// ResourcesFor returns fixtures attached to a single CRM node.
func ResourcesFor(scopeID string) []model.Resource {
	var out []model.Resource
	for _, r := range Resources() {
		if r.ScopeID == scopeID {
			out = append(out, r)
		}
	}
	return out
}
