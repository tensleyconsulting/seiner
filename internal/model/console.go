package model

import (
	"net/url"
	"strings"
)

const consoleHost = "https://console.cloud.google.com"

// ConsoleURL is the Cloud Console deep link for this resource.
// Over SSH the TUI copies this URL rather than exec'ing a remote browser.
func (r Resource) ConsoleURL() string {
	kind, crmID := crmScope(r)
	scope := ConsoleScope{Kind: kind, ID: crmID}
	if kind == "projects" {
		scope.Project = crmID
	}

	if api := r.Metadata["api"]; r.FacetID == FacetEnabledServices && api != "" {
		return consoleURL("/apis/api/"+url.PathEscape(api)+"/overview", projectQuery(scope.Project))
	}
	if spec, ok := Spec(r.Service); ok && spec.Console != nil {
		return spec.Console(r, scope)
	}
	if scope.Project != "" {
		return consoleURL("/home/dashboard", projectQuery(scope.Project))
	}
	return consoleURL("/home/dashboard", crmQuery(kind, crmID))
}

func consoleURL(path string, q url.Values) string {
	u := consoleHost + path
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	return u
}

func projectQuery(project string) url.Values {
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	return q
}

func crmQuery(kind, id string) url.Values {
	q := url.Values{}
	switch kind {
	case "organizations":
		q.Set("organizationId", id)
	case "folders":
		q.Set("folderId", id)
	case "projects":
		q.Set("project", id)
	}
	return q
}

// crmScope returns the CRM collection and id from the resource ID, falling
// back to ScopeID.
func crmScope(r Resource) (kind, id string) {
	if p, ok := ParseName(r.ID); ok {
		return p.ScopeCollection(), p.ScopeID()
	}
	return scopeParts(r.ScopeID)
}

// scopeParts splits a CRM scope name. A bare id is read as a project.
func scopeParts(scope string) (kind, id string) {
	if p, ok := ParseName(scope); ok {
		return p.ScopeCollection(), p.ScopeID()
	}
	if proj, ok := ProjectID(scope); ok {
		return "projects", proj
	}
	return "", ""
}

// bucketName is the bucket in a bucket, object, or bucket binding id.
func bucketName(id string) string {
	p, ok := ParseName(id)
	if !ok || p.Kind != "buckets" {
		return ""
	}
	return p.Name
}

func crmIDFrom(r Resource) string {
	if _, id := crmScope(r); id != "" {
		return id
	}
	return r.Name
}

// namedConsole links to prefix + name + suffix.
func namedConsole(prefix, suffix string) func(Resource, ConsoleScope) string {
	return func(r Resource, s ConsoleScope) string {
		return consoleURL(prefix+url.PathEscape(r.Name)+suffix, projectQuery(s.Project))
	}
}

// locatedConsole links to prefix + location/name + suffix.
func locatedConsole(prefix, suffix string) func(Resource, ConsoleScope) string {
	return func(r Resource, s ConsoleScope) string {
		return consoleURL(prefix+url.PathEscape(r.Location)+"/"+url.PathEscape(r.Name)+suffix, projectQuery(s.Project))
	}
}

func storageConsole(r Resource, s ConsoleScope) string {
	if r.FacetID == FacetObjects || strings.Contains(r.ID, "/objects/") {
		bucket := bucketName(r.ID)
		if bucket == "" {
			bucket = bucketName(r.ParentID)
		}
		return consoleURL("/storage/browser/_details/"+url.PathEscape(bucket)+"/"+r.Name, projectQuery(s.Project))
	}
	return consoleURL("/storage/browser/"+url.PathEscape(r.Name), projectQuery(s.Project))
}

// bigQueryConsole links the BigQuery node and connections to BigQuery
// Studio, a table to its page there, and a dataset, routine, or model to
// its dataset's page. The dataset and table come from the id
// (projects/p/datasets/d/tables/t).
func bigQueryConsole(r Resource, s ConsoleScope) string {
	q := projectQuery(s.Project)
	n, ok := ParseName(r.ID)
	if r.IsProductGroup() || !ok || n.Kind != "datasets" {
		return consoleURL("/bigquery", q)
	}
	q.Set("p", n.Project)
	q.Set("d", n.Name)
	q.Set("page", "dataset")
	if len(n.Rest) == 2 && n.Rest[0] == "tables" {
		q.Set("t", r.Name)
		q.Set("page", "table")
	}
	return consoleURL("/bigquery", q)
}

// adminHost is the Google Admin console, where Workspace users and groups
// are managed.
const adminHost = "https://admin.google.com"

// adminConsole links to an Admin console page. Google documents no
// per-user or per-group link, so users and groups open their list.
func adminConsole(path string) func(Resource, ConsoleScope) string {
	return func(Resource, ConsoleScope) string { return adminHost + path }
}

// listConsole links to a product list page that has no per-resource deep
// link the Console addresses by name.
func listConsole(path string) func(Resource, ConsoleScope) string {
	return func(_ Resource, s ConsoleScope) string {
		return consoleURL(path, projectQuery(s.Project))
	}
}

// vpcConsole links a network to its details page and the VPC Network
// product node to the network list.
func vpcConsole(r Resource, s ConsoleScope) string {
	if r.IsProductGroup() {
		return consoleURL("/networking/networks/list", projectQuery(s.Project))
	}
	return namedConsole("/networking/networks/details/", "")(r, s)
}

func computeConsole(r Resource, s ConsoleScope) string {
	if r.IsProductGroup() {
		return consoleURL("/compute/instances", projectQuery(s.Project))
	}
	return consoleURL("/compute/instancesDetail/zones/"+url.PathEscape(r.Location)+"/instances/"+url.PathEscape(r.Name), projectQuery(s.Project))
}

// sqlConsole links an instance to its overview and a database to its
// instance's Databases page. The instance comes from the database's id
// (projects/p/instances/i/databases/d).
func sqlConsole(r Resource, s ConsoleScope) string {
	n, ok := ParseName(r.ID)
	if ok && n.Kind == "instances" && len(n.Rest) == 2 {
		// The Console's tab for each child collection.
		if tab, ok := map[string]string{"databases": "databases", "users": "users", "backupRuns": "backups"}[n.Rest[0]]; ok {
			return consoleURL("/sql/instances/"+url.PathEscape(n.Name)+"/"+tab, projectQuery(s.Project))
		}
	}
	return namedConsole("/sql/instances/", "/overview")(r, s)
}

// nodePoolConsole links to a pool under its cluster. The cluster comes from
// the pool's id (projects/p/locations/l/clusters/c/nodePools/np).
func nodePoolConsole(r Resource, s ConsoleScope) string {
	n, ok := ParseName(r.ID)
	if !ok || n.Kind != "clusters" || len(n.Rest) != 2 || n.Rest[0] != "nodePools" {
		return consoleURL("/kubernetes/list/overview", projectQuery(s.Project))
	}
	return consoleURL("/kubernetes/nodepool/"+url.PathEscape(r.Location)+"/"+url.PathEscape(n.Name)+"/"+url.PathEscape(r.Name), projectQuery(s.Project))
}

// artifactConsole links a repository, package, or version. Artifact Registry
// opens an artifact in the Console when its full name is used as a URL
// (https://LOCATION-docker.pkg.dev/PROJECT/REPO/IMAGE@DIGEST), so Docker
// images and digests link there. Other formats link to the repository the
// same way; without a format the Artifact Registry landing page is used.
func artifactConsole(r Resource, s ConsoleScope) string {
	n, ok := ParseName(r.ID)
	format := strings.ToLower(r.Metadata[MetaArtifactFormat])
	if !ok || n.Kind != "repositories" || n.Project == "" || n.Location == "" || format == "" {
		return consoleURL("/artifacts", projectQuery(s.Project))
	}
	u := ArtifactRepositoryURI(n.Project, n.Location, format, n.Name)
	if format == "docker" && len(n.Rest) >= 2 && n.Rest[0] == "packages" {
		u += "/" + n.Rest[1]
		if len(n.Rest) == 4 && n.Rest[2] == "versions" && strings.HasPrefix(n.Rest[3], "sha256:") {
			u += "@" + n.Rest[3]
		}
	}
	return "https://" + u
}

// vulnerabilityConsole links to the scanned or signed image, whose Console
// page has the vulnerability list and signature artifacts.
func vulnerabilityConsole(r Resource, s ConsoleScope) string {
	if img := r.Metadata[MetaArtifactImage]; img != "" {
		return "https://" + img
	}
	return consoleURL("/artifacts", projectQuery(s.Project))
}

// loggingConsole links a log bucket to the Logs Storage page and a sink (or
// the Logging product node) to the Log Router for its organization, folder,
// or project.
func loggingConsole(r Resource, s ConsoleScope) string {
	switch r.Service {
	case ServiceLogBucket, ServiceLogView, ServiceLogLink:
		return consoleURL("/logs/storage", projectQuery(s.Project))
	case ServiceLogMetric:
		return consoleURL("/logs/metrics", projectQuery(s.Project))
	}
	return consoleURL("/logs/router", crmQuery(s.Kind, s.ID))
}

// acmConsole links to the Access Context Manager page, on the organization
// whose policy it shows.
func acmConsole(_ Resource, s ConsoleScope) string {
	return consoleURL("/security/access-level", crmQuery(s.Kind, s.ID))
}

// vpcscConsole links access levels to the Access Context Manager page and
// everything else in VPC Service Controls to the perimeter page. Access
// policies belong to an organization, so the org's id picks the policy
// there; a project's perimeter rows open on the project.
func vpcscConsole(r Resource, s ConsoleScope) string {
	if r.Service == ServiceAccessLevel {
		return acmConsole(r, s)
	}
	return consoleURL("/security/service-perimeter", crmQuery(s.Kind, s.ID))
}

func iamConsole(r Resource, s ConsoleScope) string {
	if strings.Contains(r.ID, "/iap_web/") {
		return consoleURL("/security/iap", projectQuery(s.Project))
	}
	if strings.Contains(r.ID, "/caPools/") || strings.Contains(r.ID, "/certificateTemplates/") {
		return consoleURL("/security/cas", projectQuery(s.Project))
	}
	if r.FacetID == FacetPermissions || strings.Contains(r.ID, "/buckets/") {
		bucket := bucketName(r.ParentID)
		if bucket == "" {
			bucket = bucketName(r.ID)
		}
		if bucket != "" {
			return consoleURL("/storage/browser/"+url.PathEscape(bucket), projectQuery(s.Project))
		}
	}
	return consoleURL("/iam-admin/iam", crmQuery(s.Kind, s.ID))
}

// serviceAccountConsole links to an account's details page, which the
// Console addresses by unique id. Without one it opens the account list.
func serviceAccountConsole(r Resource, s ConsoleScope) string {
	if id := r.Metadata[MetaUniqueID]; id != "" {
		return consoleURL("/iam-admin/serviceaccounts/details/"+url.PathEscape(id), projectQuery(s.Project))
	}
	return consoleURL("/iam-admin/serviceaccounts", projectQuery(s.Project))
}

func projectConsole(r Resource, _ ConsoleScope) string {
	proj, ok := ProjectID(r.ID)
	if !ok {
		proj, ok = ProjectID(r.ScopeID)
	}
	if !ok {
		proj = r.Name
	}
	return consoleURL("/home/dashboard", projectQuery(proj))
}
