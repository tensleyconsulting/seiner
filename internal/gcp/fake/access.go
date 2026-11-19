package fake

import (
	"slices"
	"strconv"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/model"
)

const (
	iapAdminID   = "projects/demo-project/iap_web/compute/services/admin-backend"
	iapGrafanaID = "projects/demo-project/iap_web/compute-us-central1/services/grafana"
)

// iapFixtures is two IAP-secured backends with their access bindings; the
// global one also has settings.
func iapFixtures() []model.Resource {
	admin := res(iapAdminID, "admin-backend", model.ServiceIAPApp, "projects/demo-project", "global", "", "Internal admin UI", map[string]string{"loadBalancingScheme": "EXTERNAL_MANAGED", "protocol": "HTTPS", "oauthClient": "Google-managed", "reauth": "SECURE_KEY every 1h0m0s (minimum)", "allowedDomains": "example.com", "accessDeniedPage": "https://example.com/access-denied"})
	admin.Lists = []model.List{{ID: "programmaticClients", Label: "Programmatic clients", Items: []string{"123456789012-cli.apps.googleusercontent.com"}}}
	grafana := res(iapGrafanaID, "grafana", model.ServiceIAPApp, "projects/demo-project", "us-central1", "", "Dashboards for on-call", map[string]string{"loadBalancingScheme": "INTERNAL_MANAGED", "protocol": "HTTP", "oauthClient": "Google-managed"})
	out := []model.Resource{
		extra(iapApp(admin), "", "2025-02-03T10:00:00Z", nil),
		extra(iapApp(grafana), "", "2025-04-22T14:30:00Z", nil),
	}
	for _, b := range []struct {
		app, role, condition string
		members              []string
	}{
		{iapAdminID, "roles/iap.httpsResourceAccessor", "", []string{"group:platform@example.com", "user:alice@example.com"}},
		{iapAdminID, "roles/iap.httpsResourceAccessor", "Corp devices", []string{"group:contractors@example.com"}},
		{iapAdminID, "roles/iap.settingsAdmin", "", []string{"group:platform@example.com"}},
	} {
		r := model.BindingResource(b.app, "projects/demo-project", b.role, b.members, b.condition)
		out = append(out, under(r, b.app, model.FacetPermissions))
	}
	// grafana has no bindings of its own; everything it has is inherited,
	// from IAP's levels above it and IAP roles on its project and org.
	for _, b := range []struct {
		app     string
		from    model.Ancestor
		origin  model.ServiceType
		role    string
		members []string
	}{
		{iapAdminID, model.Ancestor{Name: "projects/123456789012/iap_web", DisplayName: "All Web Services", Level: 2}, model.ServiceIAPApp, "roles/iap.httpsResourceAccessor", []string{"group:sre@example.com"}},
		{iapGrafanaID, model.Ancestor{Name: "projects/123456789012/iap_web/compute-us-central1", DisplayName: "Backend Services (us-central1)", Level: 1}, model.ServiceIAPApp, "roles/iap.httpsResourceAccessor", []string{"group:oncall@example.com"}},
		{iapGrafanaID, model.Ancestor{Name: "projects/123456789012/iap_web", DisplayName: "All Web Services", Level: 2}, model.ServiceIAPApp, "roles/iap.httpsResourceAccessor", []string{"group:sre@example.com"}},
		{iapGrafanaID, model.Ancestor{Name: "projects/demo-project", DisplayName: "demo-project", Level: 3}, model.ServiceProject, "roles/iap.httpsResourceAccessor", []string{"user:bob@example.com"}},
		{iapGrafanaID, model.Ancestor{Name: "organizations/example-org", DisplayName: "example-org", Level: 7}, model.ServiceOrganization, "roles/iap.settingsAdmin", []string{"group:security@example.com"}},
	} {
		r := model.InheritedBindingUnder(b.app, "projects/demo-project", b.from, b.origin, b.role, b.members, "")
		out = append(out, under(r, b.app, model.FacetPermissions))
	}
	return out
}

// iapApp orders an IAP-secured app's fields and gives it its Access facet.
func iapApp(r model.Resource) model.Resource {
	r = ordered(r, "loadBalancingScheme", "protocol", "oauthClient", "reauth", "allowedDomains",
		"corsPreflight", "identitySources", "identityPlatformTenants", "loginHint", "cookieDomain", "accessDeniedPage")
	r.Facets = []model.Facet{{ID: model.FacetPermissions, Label: "Access"}}
	return r
}

const (
	swpPrefix   = "projects/demo-project/locations/us-central1/"
	swpProxyID  = swpPrefix + "gateways/egress-proxy"
	swpPolicyID = swpPrefix + "gatewaySecurityPolicies/egress-policy"
)

// swpFixtures is a Secure Web Proxy with its policy, the policy's rules
// (listed under both the policy and the proxy), a URL list, and a TLS
// inspection policy.
func swpFixtures() []model.Resource {
	devTools := urlListFix("dev-tools", "Developer tool hosts", "github.com", "*.github.com", "proxy.golang.org", "registry.npmjs.org")
	out := []model.Resource{
		extra(swpNode(res(swpProxyID, "egress-proxy", model.ServiceWebProxy, "projects/demo-project", "us-central1", "", "Explicit egress proxy for workloads", map[string]string{"policy": "egress-policy", "addresses": "10.128.0.100", "ports": "443", "routingMode": "EXPLICIT", "network": "default", "subnetwork": "default", "certificates": "swp-cert"}), "policy", "addresses", "ports", "routingMode", "network", "subnetwork", "certificates"), "", "2025-06-01T09:00:00Z", map[string]string{"env": "prod", "team": "platform"}),
		extra(swpNode(res(swpPolicyID, "egress-policy", model.ServiceSWPPolicy, "projects/demo-project", "us-central1", "", "Egress allowlist", map[string]string{"tlsInspectionPolicy": "egress-tls"}), "tlsInspectionPolicy"), "", "2025-06-01T08:50:00Z", nil),
		devTools,
		extra(ordered(res(swpPrefix+"tlsInspectionPolicies/egress-tls", "egress-tls", model.ServiceTLSInspection, "projects/demo-project", "us-central1", "", "Inspects uploads to unknown hosts", map[string]string{"caPool": "projects/demo-project/locations/us-central1/caPools/swp-pool", "minTlsVersion": "TLS_1_2", "profile": "MODERN"}), "caPool", "trustConfig", "minTlsVersion", "profile", "excludePublicCAs"), "", "2025-06-01T08:40:00Z", nil),
	}
	for _, parent := range []string{swpPolicyID, swpProxyID} {
		out = append(out,
			swpRule(parent, "allow-dev-tools", 100, "ALLOW", true, map[string]string{"sessionMatcher": "inUrlList(host(), '" + swpPrefix + "urlLists/dev-tools')", "urlLists": "dev-tools"}),
			swpRule(parent, "allow-google-apis", 200, "ALLOW", true, map[string]string{"sessionMatcher": "host().endsWith('.googleapis.com')"}),
			swpRule(parent, "block-uploads", 300, "DENY", false, map[string]string{"sessionMatcher": "true", "applicationMatcher": "request.method == 'POST'", "tlsInspection": "on"}),
		)
	}
	out = append(out, under(devTools, swpPolicyID+"/rules/allow-dev-tools", model.FacetURLLists))
	return out
}

var bindingFields = []string{"principalType", "group", "groupId", "displayName", "principal",
	"accessLevels", "dryRunAccessLevels", "session", "scopedAccess", "binding"}

// acmFixtures is the organization's access bindings: two groups (one
// testing a stricter level in dry run), a service account, and a group
// whose email Cloud Identity would not give, which lists by its id.
func acmFixtures() []model.Resource {
	engineering := accessBinding("a1b2c3d4", "engineering@example.com", map[string]string{"principalType": "group", "group": "engineering@example.com", "groupId": "01d520gv4vjcrht", "displayName": "Engineering", "accessLevels": "corp_devices", "session": "reauth LOGIN every 8h0m0s, idle 30m0s", "scopedAccess": "1"})
	engineering.Lists = []model.List{{ID: "scopedAccess", Label: "Scoped access", Items: []string{"app Google Cloud SDK: levels corp_network"}}}
	contractors := accessBinding("e5f6a7b8", "contractors@example.com", map[string]string{"principalType": "group", "group": "contractors@example.com", "groupId": "02grqrue1fe8m4k", "accessLevels": "corp_network", "dryRunAccessLevels": "corp_devices"})
	contractors.Badge = "DRY-RUN"
	contractors.Status = model.StatusWarning
	terraform := accessBinding("c9d0e1f2", "terraform@seed-project.iam.gserviceaccount.com", map[string]string{"principalType": "service account", "principal": "terraform@seed-project.iam.gserviceaccount.com", "accessLevels": "corp_network"})
	unresolved := accessBinding("a3b4c5d6", "03x8tuzt1hmd2bz", map[string]string{"principalType": "group", "groupId": "03x8tuzt1hmd2bz", "accessLevels": "break_glass"})
	const org = "organizations/example-org"
	return []model.Resource{
		engineering,
		under(corpDevices(acmOrgPolicy, org, "binding"), engineering.ID, model.FacetAccessLevels),
		under(corpNetwork(acmOrgPolicy, org, "app Google Cloud SDK"), engineering.ID, model.FacetAccessLevels),
		contractors,
		under(corpDevices(acmOrgPolicy, org, "dry-run"), contractors.ID, model.FacetAccessLevels),
		under(corpNetwork(acmOrgPolicy, org, "binding"), contractors.ID, model.FacetAccessLevels),
		terraform,
		under(corpNetwork(acmOrgPolicy, org, "binding"), terraform.ID, model.FacetAccessLevels),
		unresolved,
		under(breakGlass(acmOrgPolicy, org, "binding"), unresolved.ID, model.FacetAccessLevels),
	}
}

// accessBinding is a binding that expands inline to the access levels it
// uses.
func accessBinding(id, name string, md map[string]string) model.Resource {
	md["binding"] = id
	r := ordered(res("organizations/example-org/gcpUserAccessBindings/"+id, name, model.ServiceAccessBinding, "organizations/example-org", "global", "", "", md), bindingFields...)
	r.Facets = []model.Facet{{ID: model.FacetAccessLevels, Label: "Access Levels", Inline: true}}
	return r
}

const (
	acmOrgPolicy    = "organizations/example-org/accessPolicies/558812004321"
	acmScopedPolicy = "organizations/example-org/accessPolicies/771203948576"
	acmDemoPolicy   = "projects/demo-project/accessPolicies/558812004321"
	acmPolicyLabel  = "example default policy (558812004321)"
)

var (
	perimeterFields = []string{"title", "type", "policy", "membership", "via", "dryRun", "resources", "restrictedServices",
		"accessLevels", "ingressPolicies", "egressPolicies", "vpcAccessibleServices",
		"dryRunResources", "dryRunRestrictedServices", "dryRunAccessLevels", "dryRunIngressPolicies", "dryRunEgressPolicies", "dryRunVpcAccessibleServices"}
	accessLevelFields = []string{"title", "type", "usedBy", "combining", "conditions"}
	ruleFields        = []string{"mode", "identities", "sources", "sourceRestriction", "resources", "externalResources", "roles"}
	prodRestricted    = []string{"bigquery.googleapis.com", "storage.googleapis.com", "sqladmin.googleapis.com"}
)

// vpcscFixtures is VPC Service Controls: the organization's default access
// policy and a scoped policy for the platform folder, each with perimeters
// and access levels, and the two perimeters that protect demo-project as
// that project lists them. Facet children are in name order, as the live
// adapter sorts them.
func vpcscFixtures() []model.Resource {
	out := []model.Resource{
		accessPolicy(acmOrgPolicy, "example default policy", "organization"),
		accessPolicy(acmScopedPolicy, "platform team policy", "folders/platform"),
		under(dataBridge(acmOrgPolicy, "organizations/example-org", nil), acmOrgPolicy, model.FacetPerimeters),
		under(prodPerimeter(acmOrgPolicy, "organizations/example-org", nil), acmOrgPolicy, model.FacetPerimeters),
		under(perimeterFix(acmScopedPolicy, "organizations/example-org", "platform_sandbox", "Sandbox projects of the platform team", "regular", "platform team policy (771203948576)",
			map[string]string{"resources": "1 project", "restrictedServices": "1 service"}, []string{"projects/333444555666"}, []string{"storage.googleapis.com"}), acmScopedPolicy, model.FacetPerimeters),
		under(breakGlass(acmOrgPolicy, "organizations/example-org", ""), acmOrgPolicy, model.FacetAccessLevels),
		under(corpDevices(acmOrgPolicy, "organizations/example-org", ""), acmOrgPolicy, model.FacetAccessLevels),
		under(corpNetwork(acmOrgPolicy, "organizations/example-org", ""), acmOrgPolicy, model.FacetAccessLevels),
		under(levelFix(acmScopedPolicy, "organizations/example-org", "platform_vpn", "Platform VPN egress", "", map[string]string{"type": "basic", "conditions": "1"}, "ip 192.0.2.0/24"), acmScopedPolicy, model.FacetAccessLevels),
		dataBridge(acmDemoPolicy, "projects/demo-project", map[string]string{"membership": "enforced", "via": "project"}),
		prodPerimeter(acmDemoPolicy, "projects/demo-project", map[string]string{"membership": "enforced, dry-run", "via": "project"}),
	}
	// prod_perimeter has its access level and rules wherever it lists.
	for _, scope := range []struct{ policy, scope string }{{acmOrgPolicy, "organizations/example-org"}, {acmDemoPolicy, "projects/demo-project"}} {
		prod := scope.policy + "/servicePerimeters/prod_perimeter"
		out = append(out,
			under(corpNetwork(scope.policy, scope.scope, "enforced, dry-run"), prod, model.FacetAccessLevels),
			ruleFix(prod, scope.scope, model.ServiceIngressRule, "ingressRules/1", "CI deploys", "enforced, dry-run",
				map[string]string{"identities": "serviceAccount:deployer@demo-project.iam.gserviceaccount.com", "sources": "corp_network", "resources": "*"},
				"storage.googleapis.com: google.storage.objects.create, google.storage.objects.get", "run.googleapis.com: all methods"),
			ruleFix(prod, scope.scope, model.ServiceIngressRule, "dryRunIngressRules/2", "any identity from corp_network → bigquery", "dry-run only",
				map[string]string{"identities": "any identity", "sources": "corp_network", "resources": "projects/444555666777"},
				"bigquery.googleapis.com: all methods"),
			ruleFix(prod, scope.scope, model.ServiceEgressRule, "egressRules/1", "Analytics export", "enforced, dry-run",
				map[string]string{"identities": "group:data-eng@example.com", "sourceRestriction": "enabled", "sources": "projects/123456789012", "resources": "projects/444555666777", "roles": "roles/bigquery.dataEditor"},
				"bigquery.googleapis.com: google.cloud.bigquery.v2.JobService.InsertJob, permission bigquery.tables.updateData"),
		)
	}
	return out
}

// ruleFix is one of prod_perimeter's ingress or egress rules; tail is its
// id under the perimeter and mode how the dry-run spec treats it.
func ruleFix(perimeter, scope string, svc model.ServiceType, tail, name, mode string, md map[string]string, ops ...string) model.Resource {
	md["mode"] = mode
	r := res(perimeter+"/"+tail, name, svc, scope, "global", "", "", md)
	if mode == "dry-run only" {
		r.Badge = "DRY-RUN"
		r.Status = model.StatusWarning
	}
	r.Lists = []model.List{{ID: "operations", Label: "Operations", Items: ops}}
	facet := model.FacetIngressRules
	if svc == model.ServiceEgressRule {
		facet = model.FacetEgressRules
	}
	return under(ordered(r, ruleFields...), perimeter, facet)
}

// accessPolicy is an access policy with its Perimeters and Access Levels
// facets; scope is "organization" or the folder or project it is scoped to.
func accessPolicy(id, title, scope string) model.Resource {
	r := res(id, title, model.ServiceAccessPolicy, "organizations/example-org", "global", "", "", map[string]string{"policy": id[strings.LastIndexByte(id, '/')+1:], "scope": scope})
	if scope != "organization" {
		r.Badge = "SCOPED"
	}
	r.Facets = []model.Facet{{ID: model.FacetPerimeters, Label: "Perimeters"}, {ID: model.FacetAccessLevels, Label: "Access Levels"}}
	return ordered(r, "policy", "scope")
}

// prodPerimeter protects demo-project and checkout-project. Its dry-run
// spec adds analytics-project and Secret Manager, so it carries the
// DRY-RUN badge; it also adds an ingress rule. member is the project-row
// membership fields, or nil.
func prodPerimeter(policy, scope string, member map[string]string) model.Resource {
	md := map[string]string{"title": "Production perimeter", "resources": "2 projects", "restrictedServices": "3 services", "accessLevels": "corp_network",
		"ingressPolicies": "1", "egressPolicies": "1", "vpcAccessibleServices": "RESTRICTED-SERVICES",
		"dryRun": "resources +1 −0, restricted services +1 −0, ingress rules +1 −0", "dryRunResources": "3 projects", "dryRunRestrictedServices": "4 services", "dryRunIngressPolicies": "2"}
	for k, v := range member {
		md[k] = v
	}
	r := perimeterFix(policy, scope, "prod_perimeter", "Production data services", "regular", acmPolicyLabel, md,
		[]string{"projects/123456789012", "projects/210987654321"}, prodRestricted)
	r.Badge = "DRY-RUN"
	r.Status = model.StatusWarning
	r.Lists = append(r.Lists,
		model.List{ID: "dryRunResources", Label: "Dry-run resources", Items: []string{"projects/123456789012", "projects/210987654321", "projects/444555666777"}},
		model.List{ID: "dryRunRestrictedServices", Label: "Dry-run restricted services", Items: append(slices.Clone(prodRestricted), "secretmanager.googleapis.com")})
	r.Facets = []model.Facet{
		{ID: model.FacetAccessLevels, Label: "Access Levels", Inline: true},
		{ID: model.FacetIngressRules, Label: "Ingress Rules"},
		{ID: model.FacetEgressRules, Label: "Egress Rules"},
	}
	return r
}

// dataBridge is a bridge perimeter between demo-project and
// analytics-project.
func dataBridge(policy, scope string, member map[string]string) model.Resource {
	md := map[string]string{"title": "Web to analytics bridge", "resources": "2 projects"}
	for k, v := range member {
		md[k] = v
	}
	return perimeterFix(policy, scope, "data_bridge", "Lets the web app write to the warehouse", "bridge", acmPolicyLabel, md,
		[]string{"projects/123456789012", "projects/444555666777"}, nil)
}

func perimeterFix(policy, scope, name, desc, typ, policyLabel string, md map[string]string, resources, restricted []string) model.Resource {
	md["type"] = typ
	md["policy"] = policyLabel
	r := res(policy+"/servicePerimeters/"+name, name, model.ServicePerimeter, scope, "global", "", desc, md)
	if len(resources) > 0 {
		r.Lists = append(r.Lists, model.List{ID: "resources", Label: "Resources", Items: resources})
	}
	if len(restricted) > 0 {
		r.Lists = append(r.Lists, model.List{ID: "restrictedServices", Label: "Restricted services", Items: restricted})
	}
	return ordered(r, perimeterFields...)
}

// corpNetwork is the office and VPN access level prod_perimeter uses.
func corpNetwork(policy, scope, usedBy string) model.Resource {
	return levelFix(policy, scope, "corp_network", "Office and VPN egress ranges", usedBy, map[string]string{"type": "basic", "conditions": "1"},
		"ip 203.0.113.0/24, 198.51.100.0/24")
}

// corpDevices is the managed-device access level.
func corpDevices(policy, scope, usedBy string) model.Resource {
	return levelFix(policy, scope, "corp_devices", "Company-managed devices on the corp network", usedBy, map[string]string{"type": "basic", "conditions": "2", "combining": "AND"},
		"requires corp_network", "members group:engineering@example.com; device encrypted, managed COMPLETE, DESKTOP_MAC ≥ 14.0 or DESKTOP_WINDOWS ≥ 10.0.19045, screen lock")
}

// breakGlass is the custom (CEL) emergency access level, its expression
// broken into lines as vpcsc.FormatCEL does.
func breakGlass(policy, scope, usedBy string) model.Resource {
	r := levelFix(policy, scope, "break_glass", "Emergency access for the security team", usedBy, map[string]string{"type": "custom"})
	r.Lists = []model.List{{ID: "expression", Label: "Expression", Items: []string{
		"request.auth.claims.groups.exists(g, g == 'security@example.com')",
		"&& (device.is_corp_owned_device",
		"  || origin.region_code == 'US')",
	}}}
	return r
}

// levelFix is an access level; conditions are a basic level's condition
// texts.
func levelFix(policy, scope, name, desc, usedBy string, md map[string]string, conditions ...string) model.Resource {
	if usedBy != "" {
		md["usedBy"] = usedBy
	}
	r := res(policy+"/accessLevels/"+name, name, model.ServiceAccessLevel, scope, "global", "", desc, md)
	if len(conditions) > 0 {
		r.Lists = []model.List{{ID: "conditions", Label: "Conditions", Items: conditions}}
	}
	return ordered(r, accessLevelFields...)
}

// swpNode orders a proxy's or policy's fields and gives it its inline
// Rules facet.
func swpNode(r model.Resource, keys ...string) model.Resource {
	r = ordered(r, keys...)
	r.Facets = []model.Facet{{ID: model.FacetRules, Label: "Rules", Inline: true}}
	return r
}

// swpRule is one egress-policy rule listed under parent, the policy or a
// proxy that enforces it.
func swpRule(parent, name string, priority int, action string, enabled bool, md map[string]string) model.Resource {
	md["priority"] = strconv.Itoa(priority)
	md["action"] = action
	var st model.ResourceStatus
	if !enabled {
		md["disabled"] = "on"
		st = model.StatusWarning
	}
	r := res(swpPolicyID+"/rules/"+name, name, model.ServiceSWPRule, "projects/demo-project", "us-central1", st, "", md)
	r.Badge = action
	if md["urlLists"] != "" {
		r.Facets = []model.Facet{{ID: model.FacetURLLists, Label: "URL Lists", Inline: true}}
	}
	r = ordered(r, "priority", "action", "sessionMatcher", "applicationMatcher", "urlLists", "tlsInspection", "disabled")
	return under(extra(r, "", "2025-06-01T09:05:00Z", nil), parent, model.FacetRules)
}

// urlListFix is a URL list with its patterns.
func urlListFix(name, desc string, values ...string) model.Resource {
	r := res(swpPrefix+"urlLists/"+name, name, model.ServiceURLList, "projects/demo-project", "us-central1", "", desc, nil)
	r.Lists = []model.List{{ID: "values", Label: "URLs", Items: values}}
	return extra(r, "", "2025-06-01T08:45:00Z", nil)
}
