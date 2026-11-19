package model

// ServiceType is the semantic identity of a Google Cloud product.
// UI code requests icons and labels by this value, never by terminal graphics.
type ServiceType string

const (
	ServiceCloudStorage              ServiceType = "cloud-storage"
	ServiceGKE                       ServiceType = "gke"
	ServiceGKENodePool               ServiceType = "gke-node-pool"
	ServiceCloudSQL                  ServiceType = "cloud-sql"
	ServiceCloudSQLDatabase          ServiceType = "cloud-sql-database"
	ServiceCloudSQLUser              ServiceType = "cloud-sql-user"
	ServiceCloudSQLBackup            ServiceType = "cloud-sql-backup"
	ServiceMemorystore               ServiceType = "memorystore"
	ServiceRedis                     ServiceType = "memorystore-redis"
	ServiceValkey                    ServiceType = "memorystore-valkey"
	ServiceCloudDNS                  ServiceType = "cloud-dns"
	ServiceDNSRecord                 ServiceType = "cloud-dns-record"
	ServiceBigQuery                  ServiceType = "bigquery"
	ServiceBigQueryDataset           ServiceType = "bigquery-dataset"
	ServiceBigQueryTable             ServiceType = "bigquery-table"
	ServiceBigQueryRoutine           ServiceType = "bigquery-routine"
	ServiceBigQueryModel             ServiceType = "bigquery-model"
	ServiceBigQueryReservation       ServiceType = "bigquery-reservation"
	ServiceBigQueryAssignment        ServiceType = "bigquery-assignment"
	ServiceBigQueryCommitment        ServiceType = "bigquery-commitment"
	ServiceBigQueryTransfer          ServiceType = "bigquery-transfer"
	ServiceBigQueryConnection        ServiceType = "bigquery-connection"
	ServiceCloudRun                  ServiceType = "cloud-run"
	ServicePubSub                    ServiceType = "pubsub"
	ServiceFirestore                 ServiceType = "firestore"
	ServiceComputeEngine             ServiceType = "compute-engine"
	ServiceVPC                       ServiceType = "vpc"
	ServiceSubnet                    ServiceType = "vpc-subnet"
	ServiceFirewall                  ServiceType = "compute-firewall"
	ServiceAddress                   ServiceType = "compute-address"
	ServiceNetworkServices           ServiceType = "network-services"
	ServiceLoadBalancer              ServiceType = "compute-load-balancer"
	ServiceBackendService            ServiceType = "compute-backend-service"
	ServiceHealthCheck               ServiceType = "compute-health-check"
	ServiceCloudArmor                ServiceType = "cloud-armor"
	ServiceDisk                      ServiceType = "compute-disk"
	ServiceInstanceTemplate          ServiceType = "compute-instance-template"
	ServiceInstanceGroup             ServiceType = "compute-instance-group"
	ServiceManagedInstance           ServiceType = "compute-managed-instance"
	ServiceRoute                     ServiceType = "compute-route"
	ServiceNetworkConnectivity       ServiceType = "network-connectivity"
	ServiceRouter                    ServiceType = "compute-router"
	ServiceCloudNAT                  ServiceType = "cloud-nat"
	ServiceForwardingRule            ServiceType = "compute-forwarding-rule"
	ServiceServiceAttachment         ServiceType = "compute-service-attachment"
	ServicePSC                       ServiceType = "private-service-connect"
	ServicePSCEndpoint               ServiceType = "compute-psc-endpoint"
	ServiceRegionalEndpoint          ServiceType = "psc-regional-endpoint"
	ServiceConnectionPolicy          ServiceType = "psc-connection-policy"
	ServicePSCConnection             ServiceType = "psc-connection"
	ServiceSSLPolicy                 ServiceType = "compute-ssl-policy"
	ServiceNetworkSecurity           ServiceType = "network-security"
	ServiceSecureWebProxy            ServiceType = "secure-web-proxy"
	ServiceWebProxy                  ServiceType = "swp-web-proxy"
	ServiceSWPPolicy                 ServiceType = "swp-policy"
	ServiceSWPRule                   ServiceType = "swp-rule"
	ServiceURLList                   ServiceType = "swp-url-list"
	ServiceTLSInspection             ServiceType = "tls-inspection-policy"
	ServiceCertificateManager        ServiceType = "certificate-manager"
	ServiceCertificate               ServiceType = "certificate-manager-certificate"
	ServiceCertificateMap            ServiceType = "certificate-manager-map"
	ServiceCertMapEntry              ServiceType = "certificate-manager-map-entry"
	ServiceDNSAuthorization          ServiceType = "certificate-manager-dns-authorization"
	ServiceIssuanceConfig            ServiceType = "certificate-manager-issuance-config"
	ServiceTrustConfig               ServiceType = "certificate-manager-trust-config"
	ServiceIAPApp                    ServiceType = "iap-app"
	ServiceACM                       ServiceType = "access-context-manager"
	ServiceAccessBinding             ServiceType = "acm-access-binding"
	ServiceVPCSC                     ServiceType = "vpc-service-controls"
	ServiceAccessPolicy              ServiceType = "vpcsc-access-policy"
	ServicePerimeter                 ServiceType = "vpcsc-perimeter"
	ServiceAccessLevel               ServiceType = "vpcsc-access-level"
	ServiceIngressRule               ServiceType = "vpcsc-ingress-rule"
	ServiceEgressRule                ServiceType = "vpcsc-egress-rule"
	ServiceSecretManager             ServiceType = "secret-manager"
	ServiceSecretVersion             ServiceType = "secret-version"
	ServiceKMS                       ServiceType = "cloud-kms"
	ServiceKMSKey                    ServiceType = "cloud-kms-key"
	ServiceKMSKeyVersion             ServiceType = "cloud-kms-key-version"
	ServiceKMSImportJob              ServiceType = "cloud-kms-import-job"
	ServiceKMSAttestation            ServiceType = "cloud-kms-attestation"
	ServiceKMSCertChain              ServiceType = "cloud-kms-cert-chain"
	ServicePrivateCA                 ServiceType = "private-ca"
	ServiceCAPool                    ServiceType = "private-ca-pool"
	ServiceCertificateAuthority      ServiceType = "private-ca-authority"
	ServiceCACertificate             ServiceType = "private-ca-certificate"
	ServiceCertificateRevocationList ServiceType = "private-ca-crl"
	ServiceCertificateTemplate       ServiceType = "private-ca-template"
	ServiceLogging                   ServiceType = "logging"
	ServiceLogSink                   ServiceType = "logging-sink"
	ServiceLogBucket                 ServiceType = "logging-bucket"
	ServiceLogMetric                 ServiceType = "logging-metric"
	ServiceLogView                   ServiceType = "logging-view"
	ServiceLogLink                   ServiceType = "logging-link"
	ServiceCloudFunctions            ServiceType = "cloud-functions"
	ServiceWorkstationCluster        ServiceType = "workstation-cluster"
	ServiceWorkstationConfig         ServiceType = "workstation-config"
	ServiceWorkstation               ServiceType = "workstation"
	ServiceArtifactRegistry          ServiceType = "artifact-registry"
	ServiceArtifactPackage           ServiceType = "artifact-package"
	ServiceArtifactVersion           ServiceType = "artifact-version"
	ServiceArtifactSignature         ServiceType = "artifact-signature"
	ServiceVulnerability             ServiceType = "vulnerability"
	ServiceIAM                       ServiceType = "iam"
	ServiceIAMBinding                ServiceType = "iam-binding"
	ServiceServiceAccount            ServiceType = "iam-service-account"
	ServiceServiceAccountKey         ServiceType = "iam-service-account-key"
	ServiceCustomRole                ServiceType = "iam-custom-role"
	ServiceIAMPermission             ServiceType = "iam-permission"
	ServiceOrgPolicy                 ServiceType = "org-policy"
	ServiceOrgPolicyValue            ServiceType = "org-policy-value"
	ServiceCloudPlatform             ServiceType = "cloud-platform"
	ServiceWorkspace                 ServiceType = "google-workspace"
	ServiceWorkspaceUser             ServiceType = "workspace-user"
	ServiceWorkspaceGroup            ServiceType = "workspace-group"
	ServiceWorkspaceMember           ServiceType = "workspace-member"
	ServiceOrganization              ServiceType = "organization"
	ServiceFolder                    ServiceType = "folder"
	ServiceProject                   ServiceType = "project"
	ServiceUsage                     ServiceType = "service-usage"
)

// KnownServices is the registration order for providers and tests.
func KnownServices() []ServiceType {
	out := make([]ServiceType, len(specs))
	for i, s := range specs {
		out[i] = s.Type
	}
	return out
}

// Label is the human-readable product name.
func (s ServiceType) Label() string {
	if spec, ok := Spec(s); ok {
		return spec.Label
	}
	return string(s)
}

// NodeLabel is the name of the product's explorer node.
func (s ServiceType) NodeLabel() string {
	if spec, ok := Spec(s); ok && spec.Node != "" {
		return spec.Node
	}
	return s.Label()
}

// Abbrev is the short product name ("GCS"). Search matches it, and it
// stands in for the icon when the renderer draws none.
func (s ServiceType) Abbrev() string {
	if spec, ok := Spec(s); ok {
		return spec.Abbrev
	}
	return "?"
}

// Glyph is the text-mode icon, an emoji two cells wide.
func (s ServiceType) Glyph() string {
	if spec, ok := Spec(s); ok {
		return spec.Glyph
	}
	return "❓"
}

// CollectionLabel is the explorer group name for a service that has resources.
func (s ServiceType) CollectionLabel() string {
	if spec, ok := Spec(s); ok {
		return spec.Collection
	}
	return s.Label()
}
