package fake

import (
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

var (
	instanceFields = []string{"machineType", "cpuPlatform", "accelerators", "internalIP", "externalIP", "network", "subnetwork",
		"interfaces", "ipForwarding", "disks", "serviceAccount", "scopes", "networkTags", "provisioning", "maintenance",
		"shieldedVM", "confidentialVM", "osLogin", "deletionProtection", "lastStarted", "lastStopped"}
	subnetFields         = []string{"cidr", "gateway", "network", "secondaryRanges", "purpose", "stackType", "privateGoogleAccess", "flowLogs"}
	addressFields        = []string{"address", "addressType", "ipVersion", "networkTier", "purpose", "network", "subnetwork", "usedBy"}
	forwardingRuleFields = []string{"ipAddress", "protocol", "ports", "scheme", "target", "backendService",
		"network", "subnetwork", "networkTier", "globalAccess"}
	pscEndpointFields = []string{"ipAddress", "targetType", "target", "pscConnectionId", "network", "subnetwork", "globalAccess"}
	diskFields        = []string{"type", "size", "attachedTo", "sourceImage", "sourceSnapshot", "replicaZones",
		"provisionedIops", "provisionedThroughput", "architecture", "encryption", "lastAttached"}
	templateFields = []string{"machineType", "bootImage", "bootDisk", "disks", "network", "subnetwork",
		"externalIP", "serviceAccount", "networkTags", "provisioning"}
	groupFields = []string{"targetSize", "instances", "template", "autoscaler", "autoscaling", "recommendedSize",
		"updatePolicy", "updateLimits", "minimalAction", "replacementMethod", "autoHealing", "autoHealingDelay",
		"zones", "namedPorts", "baseInstanceName"}
	routeFields = []string{"destination", "priority", "nextHop", "network", "instanceTags", "routeType", "warnings"}
	natFields   = []string{"ipAllocation", "natIPs", "sourceRanges", "subnetworks", "minPortsPerVm", "maxPortsPerVm",
		"dynamicPorts", "endpointIndependentMapping", "logging"}
	lbFields               = []string{"type", "frontends", "backends", "hosts", "certificates", "certificateMap", "sslPolicy", "cloudArmor", "cdn"}
	backendFields          = []string{"protocol", "scheme", "portName", "backends", "healthChecks", "cloudArmor", "edgeSecurityPolicy", "iap", "cdn", "sessionAffinity", "timeout", "connectionDraining", "logging"}
	healthCheckFields      = []string{"type", "port", "requestPath", "interval", "timeout", "thresholds", "logging"}
	attachmentFields       = []string{"targetService", "connectionPreference", "endpoints", "natSubnets", "proxyProtocol", "domainNames"}
	regionalEndpointFields = []string{"targetGoogleApi", "accessType", "ipAddress", "address", "network", "subnetwork", "forwardingRule", "updated"}
	connectionPolicyFields = []string{"serviceClass", "connections", "network", "subnetworks", "producerScope", "infrastructure", "updated"}
	pscConnectionFields    = []string{"pscConnectionId", "consumerProject", "forwardingRule", "consumerAddress", "subnetwork", "producerInstanceId", "serviceClass", "ipVersion", "errorType", "error"}
	firewallFields         = []string{"direction", "priority", "network", "allowed", "denied",
		"sourceRanges", "sourceTags", "sourceServiceAccounts", "destinationRanges", "targetTags", "targetServiceAccounts", "logging", "disabled"}
)

func vpc(id, name, scope, desc string, md map[string]string) model.Resource {
	r := res(id, name, model.ServiceVPC, scope, "global", "", desc, md)
	r.Facets = []model.Facet{
		{ID: model.FacetSubnets, Label: "Subnets"},
		{ID: model.FacetFirewalls, Label: "Firewalls"},
		{ID: model.FacetRoutes, Label: "Routes"},
	}
	return r
}

const (
	webLB = "projects/demo-project/global/urlMaps/web-map"
	apiLB = "projects/demo-project/regions/us-central1/backendServices/api-backend"
)

// routerFix is a Cloud Router with its NAT Gateways facet and BGP peers.
func routerFix(id, name, desc string, md map[string]string) model.Resource {
	n, _ := model.ParseName(id)
	r := res(id, name, model.ServiceRouter, "projects/demo-project", n.Location, "", desc, md)
	r.Facets = []model.Facet{{ID: model.FacetNATs, Label: "NAT Gateways"}}
	r.Lists = []model.List{{ID: "peers", Label: "BGP peers", Items: []string{"onprem-peer  169.254.10.2  AS65010"}}}
	return r
}

// lbFix is a load balancer with its Frontends, Backends, and Health Checks
// facets.
func lbFix(id, name, loc, desc string, md map[string]string) model.Resource {
	r := res(id, name, model.ServiceLoadBalancer, "projects/demo-project", loc, "", desc, md)
	r.Facets = []model.Facet{
		{ID: model.FacetFrontends, Label: "Frontends"},
		{ID: model.FacetBackends, Label: "Backends"},
		{ID: model.FacetHealthChecks, Label: "Health Checks"},
	}
	return r
}

func webHTTPSRule() model.Resource {
	return extra(ordered(res("projects/demo-project/global/forwardingRules/web-https", "web-https", model.ServiceForwardingRule, "projects/demo-project", "global", "", "Public HTTPS frontend", map[string]string{"ipAddress": "203.0.113.110", "protocol": "TCP", "ports": "443-443", "scheme": "EXTERNAL_MANAGED", "target": "targetHttpsProxies/web-proxy", "networkTier": "PREMIUM"}), forwardingRuleFields...), "", "2025-01-15T18:31:00Z", map[string]string{"env": "prod"})
}

func apiILBRule() model.Resource {
	return extra(ordered(res("projects/demo-project/regions/us-central1/forwardingRules/api-ilb", "api-ilb", model.ServiceForwardingRule, "projects/demo-project", "us-central1", "", "Internal API load balancer", map[string]string{"ipAddress": "10.128.0.50", "protocol": "TCP", "ports": "8080", "scheme": "INTERNAL", "backendService": "api-backend", "network": "default", "subnetwork": "default", "networkTier": "PREMIUM", "globalAccess": "on"}), forwardingRuleFields...), "", "2025-03-04T10:01:00Z", nil)
}

func webBackend() model.Resource {
	r := res("projects/demo-project/global/backendServices/web-backend", "web-backend", model.ServiceBackendService, "projects/demo-project", "global", "", "Frontend VMs", map[string]string{"protocol": "HTTP", "scheme": "EXTERNAL_MANAGED", "portName": "http", "backends": "1", "healthChecks": "web-hc", "cloudArmor": "web-armor", "iap": "off", "cdn": "on, CACHE_ALL_STATIC", "sessionAffinity": "NONE", "timeout": "30s", "connectionDraining": "300s", "logging": "on, 50% sampled"})
	r.Lists = []model.List{{ID: "backends", Label: "Backends", Items: []string{"instanceGroups/web-ig (us-central1-a) UTILIZATION 100%"}}}
	return extra(ordered(r, backendFields...), "", "2025-01-15T18:10:00Z", nil)
}

func apiBackend() model.Resource {
	r := res(apiLB, "api-backend", model.ServiceBackendService, "projects/demo-project", "us-central1", "", "Internal API backends", map[string]string{"protocol": "TCP", "scheme": "INTERNAL", "backends": "1", "healthChecks": "api-hc", "cdn": "off", "sessionAffinity": "CLIENT_IP", "timeout": "30s", "logging": "off"})
	r.Lists = []model.List{{ID: "backends", Label: "Backends", Items: []string{"networkEndpointGroups/api-neg (us-central1-a) CONNECTION"}}}
	return extra(ordered(r, backendFields...), "", "2025-03-04T09:55:00Z", nil)
}

func webHealthCheck() model.Resource {
	return extra(ordered(res("projects/demo-project/global/healthChecks/web-hc", "web-hc", model.ServiceHealthCheck, "projects/demo-project", "global", "", "", map[string]string{"type": "HTTP", "port": "80", "requestPath": "/healthz", "interval": "5s", "timeout": "5s", "thresholds": "healthy 2, unhealthy 2", "logging": "off"}), healthCheckFields...), "", "2025-01-15T18:05:00Z", nil)
}

func apiHealthCheck() model.Resource {
	return extra(ordered(res("projects/demo-project/regions/us-central1/healthChecks/api-hc", "api-hc", model.ServiceHealthCheck, "projects/demo-project", "us-central1", "", "", map[string]string{"type": "TCP", "port": "8080", "interval": "10s", "timeout": "5s", "thresholds": "healthy 2, unhealthy 3", "logging": "on"}), healthCheckFields...), "", "2025-03-04T09:50:00Z", nil)
}

// armorFix is a Cloud Armor policy with its rules, in priority order.
func armorFix(id, name, desc string, md map[string]string, rules ...string) model.Resource {
	r := res(id, name, model.ServiceCloudArmor, "projects/demo-project", "global", "", desc, md)
	r.Lists = []model.List{{ID: "rules", Label: "Rules", Items: rules}}
	return ordered(r, "type", "rules", "adaptiveProtection", "usedBy")
}

// sslPolicyFix is an SSL policy with the target proxies that apply it. The
// location comes from id.
func sslPolicyFix(id, name, desc string, md map[string]string, usedBy ...string) model.Resource {
	n, _ := model.ParseName(id)
	loc := n.Location
	if loc == "" {
		loc = "global"
	}
	r := res(id, name, model.ServiceSSLPolicy, "projects/demo-project", loc, "", desc, md)
	if len(usedBy) > 0 {
		r.Lists = []model.List{{ID: "targets", Label: "Used by", Items: usedBy}}
	}
	r.Created = time.Date(2025, 1, 15, 18, 10, 0, 0, time.UTC)
	return ordered(r, "profile", "minTlsVersion", "targets")
}

// addressFix is a reserved IP address. The location comes from id; users
// fill the "Used by" list as the live adapter does.
func addressFix(id, name, desc, state string, md map[string]string, users ...string) model.Resource {
	loc := "global"
	if n, ok := model.ParseName(id); ok && n.Location != "" {
		loc = n.Location
	}
	r := res(id, name, model.ServiceAddress, "projects/demo-project", loc, model.StatusHealthy, desc, md)
	if len(users) > 0 {
		r.Lists = []model.List{{ID: "users", Label: "Used by", Items: users}}
	}
	return ordered(r, addressFields...)
}

// attachmentFix is a regional PSC service attachment with its endpoint and
// consumer lists.
func attachmentFix(id, name, desc string, md map[string]string, endpoints, accept []string) model.Resource {
	n, _ := model.ParseName(id)
	r := res(id, name, model.ServiceServiceAttachment, "projects/demo-project", n.Location, model.StatusHealthy, desc, md)
	r.Lists = []model.List{
		{ID: "endpoints", Label: "Connected endpoints", Items: endpoints},
		{ID: "accept", Label: "Accepted consumers", Items: accept},
	}
	r.Created = time.Date(2025, 3, 4, 10, 5, 0, 0, time.UTC)
	return ordered(r, attachmentFields...)
}

const webIG = "projects/demo-project/zones/us-central1-a/instanceGroupManagers/web-ig"

// mig is a managed instance group that expands to its instances.
func mig(r model.Resource) model.Resource {
	r.Facets = []model.Facet{{ID: model.FacetInstances, Label: "Instances", Inline: true}}
	return ordered(r, groupFields...)
}

// managedInstance is one instance of a group, under a MIG's Instances or a
// node pool's Nodes.
func managedInstance(project, parent, facet, group, zone, name, status, action, health string) model.Resource {
	md := map[string]string{"instanceStatus": status, "currentAction": action, "group": group,
		"vm": "projects/" + project + "/zones/" + zone + "/instances/" + name}
	if health != "" {
		md["health"] = health
	}
	st := model.StatusHealthy
	switch {
	case action != "NONE":
		st = model.StatusWarning
	case status != "RUNNING":
		st = model.StatusWarning
	}
	r := res(parent+"/instances/"+name, name, model.ServiceManagedInstance, "projects/"+project, zone, st, "", md)
	if action != "NONE" {
		r.Badge = action
	}
	r.State = status
	return under(ordered(r, "instanceStatus", "targetStatus", "currentAction", "health", "template", "version", "group", "vm", "errors"), parent, facet)
}

const wsCluster = "projects/demo-project/locations/us-central1/workstationClusters/dev-cluster"

// workstationFixtures are a cluster with one configuration and its
// workstations, built as the live adapter builds them.
func workstationFixtures() []model.Resource {
	cluster := res(wsCluster, "dev-cluster", model.ServiceWorkstationCluster, "projects/demo-project", "us-central1", "", "",
		map[string]string{"displayName": "Developer workstations", "network": "default", "subnetwork": "default", "controlPlaneIp": "10.128.0.60", "updated": "2025-04-02 10:00 UTC"})
	cluster.Facets = []model.Facet{{ID: model.FacetConfigs, Label: "Configurations", Inline: true}}
	cluster = extra(ordered(cluster, "displayName", "network", "subnetwork", "privateEndpoint", "hostname",
		"serviceAttachment", "controlPlaneIp", "domain", "condition", "updated"), "", "2025-04-01T09:00:00Z", map[string]string{"team": "platform"})

	configID := wsCluster + "/workstationConfigs/vscode-standard"
	config := res(configID, "vscode-standard", model.ServiceWorkstationConfig, "projects/demo-project", "us-central1", "", "",
		map[string]string{"displayName": "Code OSS, e2-standard-8", "machineType": "e2-standard-8", "pool": "1 (1 ready)", "bootDisk": "50 GB",
			"image":          "us-central1-docker.pkg.dev/cloud-workstations-images/predefined/code-oss:latest",
			"serviceAccount": "workstations@demo-project.iam.gserviceaccount.com", "publicIp": "off", "shieldedVM": "secure boot, vTPM, integrity monitoring", "allowedPorts": "22, 80, 1024-65535", "idleTimeout": "20m0s",
			"runningTimeout": "12h0m0s", "persistentDirectories": "/home 200 GB pd-balanced, RETAIN", "replicaZones": "us-central1-a, us-central1-b"})
	config.Facets = []model.Facet{{ID: model.FacetWorkstations, Label: "Workstations", Inline: true}}
	config.Lists = []model.List{{ID: "env", Label: "Environment", Items: []string{"GOPROXY=https://proxy.golang.org", "TEAM=platform"}}}
	config = under(extra(ordered(config, "displayName", "machineType", "pool", "bootDisk", "accelerators", "boostConfigs", "image",
		"serviceAccount", "publicIp", "ssh", "tcpConnections", "allowedPorts", "shieldedVM", "confidentialVM",
		"nestedVirtualization", "auditAgent", "idleTimeout", "idleAction", "runningTimeout", "persistentDirectories",
		"replicaZones", "encryption", "maxUsable", "condition", "updated"), "", "2025-04-01T09:30:00Z", nil), wsCluster, model.FacetConfigs)

	workstation := func(name, display, state string, st model.ResourceStatus, started string) model.Resource {
		md := map[string]string{"displayName": display, "host": name + ".cluster-abc123.cloudworkstations.dev", "persistentDirectories": "/home 200 GB"}
		if started != "" {
			md["started"] = started
			md["vm"] = "us-central1-a/workstations-" + name
		}
		r := res(configID+"/workstations/"+name, name, model.ServiceWorkstation, "projects/demo-project", "us-central1", st, "", md)
		r.Lists = []model.List{{ID: "env", Label: "Environment", Items: []string{"EDITOR=vim"}}}
		r = ordered(r, "displayName", "host", "vm", "started", "persistentDirectories", "source", "encryption", "updated")
		return under(extra(r, state, "2025-04-03T08:00:00Z", map[string]string{"owner": name}), configID, model.FacetWorkstations)
	}
	return []model.Resource{
		cluster,
		config,
		workstation("alice", "Alice's workstation", "RUNNING", model.StatusHealthy, "2026-10-27 07:55 UTC"),
		workstation("bob", "Bob's workstation", "STOPPED", "", ""),
	}
}

func networkConnectivityFixtures() []model.Resource {
	sqlPolicyID := "projects/demo-project/locations/us-central1/serviceConnectionPolicies/default-cloud-sql"
	sqlPolicy := res(sqlPolicyID, "default-cloud-sql", model.ServiceConnectionPolicy, "projects/demo-project", "us-central1", model.StatusHealthy, "Cloud SQL PSC endpoints in default VPC",
		map[string]string{
			"serviceClass":   "google-cloud-sql",
			"connections":    "1 / 8",
			"network":        "default",
			"subnetworks":    "default",
			"producerScope":  "folders/prod",
			"infrastructure": "PSC",
		})
	sqlPolicy.Facets = []model.Facet{{ID: model.FacetConnections, Label: "Connections", Inline: true}}

	sqlConn := res(sqlPolicyID+"/connections/10000000000000001", "demo-project/sca-auto-fr-0a1b2c3d", model.ServicePSCConnection, "projects/demo-project", "us-central1", model.StatusHealthy, "",
		map[string]string{
			"pscConnectionId": "10000000000000001",
			"consumerProject": "demo-project",
			"forwardingRule":  "sca-auto-fr-0a1b2c3d",
			"consumerAddress": "sca-auto-addr-4e5f6a7b",
			"subnetwork":      "default",
			"serviceClass":    "google-cloud-sql",
		})
	sqlConn.Badge = "ACTIVE"

	memPolicy := res("projects/demo-project/locations/us-central1/serviceConnectionPolicies/default-memorystore", "default-memorystore", model.ServiceConnectionPolicy, "projects/demo-project", "us-central1", model.StatusUnknown, "Memorystore PSC endpoints in default VPC",
		map[string]string{
			"serviceClass":   "gcp-memorystore",
			"connections":    "0 / 4",
			"network":        "default",
			"subnetworks":    "default",
			"producerScope":  "folders/prod",
			"infrastructure": "PSC",
		})

	telemetryREP := res("projects/demo-project/locations/us-central1/regionalEndpoints/telemetry-rep", "telemetry-rep", model.ServiceRegionalEndpoint, "projects/demo-project", "us-central1", model.StatusUnknown, "Regional endpoint for telemetry.us-central1.rep.googleapis.com",
		map[string]string{
			"targetGoogleApi": "telemetry.us-central1.rep.googleapis.com",
			"accessType":      "REGIONAL",
			"ipAddress":       "10.128.0.61",
			"network":         "default",
			"subnetwork":      "default",
			"forwardingRule":  "rep-autogen-fr-telemetry-rep",
		})
	telemetryREP.Badge = "REGIONAL"

	aiREP := res("projects/demo-project/locations/us-central1/regionalEndpoints/aiplatform-us-rep", "aiplatform-us-rep", model.ServiceRegionalEndpoint, "projects/demo-project", "us-central1", model.StatusUnknown, "Multi-regional endpoint for aiplatform.us.rep.googleapis.com",
		map[string]string{
			"targetGoogleApi": "aiplatform.us.rep.googleapis.com",
			"accessType":      "GLOBAL",
			"ipAddress":       "10.128.0.62",
			"network":         "default",
			"subnetwork":      "default",
			"forwardingRule":  "rep-autogen-fr-aiplatform-us-rep",
		})
	aiREP.Badge = "GLOBAL"

	return []model.Resource{
		extra(ordered(telemetryREP, regionalEndpointFields...), "", "2025-11-05T16:43:42Z", nil),
		extra(ordered(aiREP, regionalEndpointFields...), "", "2025-11-04T19:54:33Z", nil),
		extra(ordered(sqlPolicy, connectionPolicyFields...), "", "2025-10-01T19:23:49Z", nil),
		under(extra(ordered(sqlConn, pscConnectionFields...), "ACTIVE", "", nil), sqlPolicyID, model.FacetConnections),
		extra(ordered(memPolicy, connectionPolicyFields...), "", "2025-10-01T19:23:48Z", nil),
	}
}
