package fake

import (
	"context"
	"slices"
	"strconv"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/iam"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// roleFixtures is the custom roles, each with its permissions as facet
// children.
func roleFixtures() []model.Resource {
	return slices.Concat(
		customRole("projects/demo-project", "deployer", "Release Deployer", "Ships Cloud Run and GKE releases", "GA",
			"container.deployments.get", "container.deployments.update", "run.services.get", "run.services.update", "iam.serviceAccounts.actAs"),
		customRole("projects/demo-project", "bucketAuditor", "Bucket Auditor", "Superseded by roles/storage.objectViewer", "DEPRECATED",
			"storage.buckets.get", "storage.buckets.getIamPolicy", "storage.objects.list"),
		customRole("organizations/example-org", "securityReviewer", "Security Reviewer", "Read-only access to IAM and org policy for quarterly reviews", "GA",
			"iam.roles.list", "iam.serviceAccounts.list", "orgpolicy.policies.list", "resourcemanager.organizations.getIamPolicy", "resourcemanager.folders.getIamPolicy", "resourcemanager.projects.getIamPolicy"),
	)
}

// customRole is a custom role defined on scope, built as the live adapter
// builds it, followed by its permissions.
func customRole(scope, id, title, desc, stage string, perms ...string) []model.Resource {
	full := scope + "/roles/" + id
	md := map[string]string{"title": title, model.MetaRole: full, model.ListPermissions: strconv.Itoa(len(perms))}
	r := res(full, id, model.ServiceCustomRole, scope, "global", "", desc, md)
	r.State = stage
	if stage == "DEPRECATED" {
		r.Status = model.StatusWarning
	}
	r.Facets = []model.Facet{model.RolePermissionsFacet()}
	out := []model.Resource{ordered(r, "title", model.MetaRole, model.ListPermissions)}
	for _, p := range perms {
		out = append(out, model.PermissionResource(r, model.RoleInfo{Name: full, Title: title, Description: desc}, p))
	}
	return out
}

// roles is the fixture iam.AdminAPI behind role permissions: custom roles
// from the fixtures, predefined ones from predefinedRoles.
type roles struct{}

func (roles) ListServiceAccounts(ctx context.Context, _ string) ([]iam.ServiceAccount, error) {
	return nil, ctx.Err()
}

func (roles) ListRoles(ctx context.Context, _ string) ([]iam.Role, error) {
	return nil, ctx.Err()
}

func (roles) GetRole(ctx context.Context, name string) (iam.Role, error) {
	if err := ctx.Err(); err != nil {
		return iam.Role{}, err
	}
	if r, ok := predefinedRoles[name]; ok {
		return iam.Role{Name: name, Title: r.title, Description: r.desc, Stage: "GA", Permissions: r.perms}, nil
	}
	role := iam.Role{Name: name}
	found := false
	for _, r := range Resources() {
		switch {
		case r.Service == model.ServiceCustomRole && r.ID == name:
			found = true
			role.Title, role.Description, role.Stage = r.Metadata["title"], r.Description, r.State
		case r.IsPermission() && r.ParentID == name:
			role.Permissions = append(role.Permissions, r.Name)
		}
	}
	if !found {
		return iam.Role{}, model.ErrNotFound
	}
	return role, nil
}

// ListServiceAccountKeys is not used: fixture keys are Static facet
// children of their accounts.
func (roles) ListServiceAccountKeys(ctx context.Context, _, _ string) ([]iam.ServiceAccountKey, error) {
	return nil, ctx.Err()
}

// GetServiceAccountPolicy is not used: fixture bindings are Static facet
// children of their accounts.
func (roles) GetServiceAccountPolicy(ctx context.Context, _, _ string) (iam.Policy, error) {
	return iam.Policy{}, ctx.Err()
}

var _ iam.AdminAPI = roles{}

// perms is service.resource.verb for each verb.
func perms(service, resource string, verbs ...string) []string {
	out := make([]string, len(verbs))
	for i, v := range verbs {
		out[i] = service + "." + resource + "." + v
	}
	return out
}

var (
	viewerPerms = slices.Concat(
		perms("compute", "instances", "get", "list"),
		perms("compute", "disks", "get", "list"),
		perms("compute", "networks", "get", "list"),
		perms("compute", "subnetworks", "get", "list"),
		perms("compute", "firewalls", "get", "list"),
		perms("compute", "addresses", "get", "list"),
		perms("compute", "routes", "get", "list"),
		perms("compute", "images", "get", "list"),
		perms("compute", "snapshots", "get", "list"),
		perms("compute", "instanceTemplates", "get", "list"),
		perms("compute", "regions", "get", "list"),
		perms("compute", "zones", "get", "list"),
		perms("compute", "projects", "get"),
		perms("container", "clusters", "get", "list"),
		perms("container", "pods", "get", "list"),
		perms("storage", "buckets", "list"),
		perms("iam", "roles", "get", "list"),
		perms("iam", "serviceAccounts", "get", "list"),
		perms("resourcemanager", "projects", "get", "getIamPolicy"),
		perms("logging", "logEntries", "list"),
		perms("logging", "sinks", "get", "list"),
		perms("pubsub", "topics", "get", "list"),
		perms("pubsub", "subscriptions", "get", "list"),
		perms("run", "services", "get", "list"),
		perms("cloudsql", "instances", "get", "list"),
		perms("secretmanager", "secrets", "get", "list"),
		perms("dns", "managedZones", "get", "list"),
		perms("dns", "resourceRecordSets", "list"),
		perms("artifactregistry", "repositories", "get", "list"),
	)
	editorPerms = slices.Concat(viewerPerms,
		perms("compute", "instances", "create", "delete", "start", "stop", "setMetadata", "setLabels"),
		perms("compute", "disks", "create", "delete", "resize"),
		perms("compute", "firewalls", "create", "delete", "update"),
		perms("container", "clusters", "create", "delete", "update"),
		perms("storage", "buckets", "create", "delete", "get", "update"),
		perms("storage", "objects", "create", "delete", "get", "list", "update"),
		perms("pubsub", "topics", "create", "delete", "publish"),
		perms("run", "services", "create", "delete", "update"),
		perms("cloudsql", "instances", "create", "delete", "update"),
		perms("logging", "sinks", "create", "delete", "update"),
		perms("secretmanager", "versions", "access", "add"),
	)
	ownerPerms = slices.Concat(editorPerms,
		perms("resourcemanager", "projects", "setIamPolicy", "delete"),
		perms("iam", "roles", "create", "delete", "update"),
		perms("iam", "serviceAccounts", "getIamPolicy", "setIamPolicy"),
		perms("iam", "serviceAccountKeys", "create", "delete", "list"),
	)
	storageObjectViewerPerms = slices.Concat(
		perms("resourcemanager", "projects", "get", "list"),
		perms("storage", "folders", "get", "list"),
		perms("storage", "managedFolders", "get", "list"),
		perms("storage", "objects", "get", "list"),
	)
	storageObjectAdminPerms = slices.Concat(
		perms("resourcemanager", "projects", "get", "list"),
		perms("storage", "folders", "create", "delete", "get", "list", "rename"),
		perms("storage", "managedFolders", "create", "delete", "get", "list"),
		perms("storage", "multipartUploads", "abort", "create", "list", "listParts"),
		perms("storage", "objects", "create", "delete", "get", "getIamPolicy", "list", "restore", "setIamPolicy", "update"),
	)
	artifactReaderPerms = slices.Concat(
		perms("artifactregistry", "dockerimages", "get", "list"),
		perms("artifactregistry", "files", "download", "get", "list"),
		perms("artifactregistry", "packages", "get", "list"),
		perms("artifactregistry", "repositories", "downloadArtifacts", "get", "list"),
		perms("artifactregistry", "tags", "get", "list"),
		perms("artifactregistry", "versions", "get", "list"),
	)
)

// predefinedRoles is a sample of each predefined role a fixture binds:
// enough permissions to show the grouping, not the role's full set. The
// descriptions are short paraphrases, not Google's text.
var predefinedRoles = map[string]struct {
	title, desc string
	perms       []string
}{
	"roles/owner":                             {"Owner", "Full access to most resources, including managing roles and permissions", ownerPerms},
	"roles/editor":                            {"Editor", "View, create, update, and delete most resources", editorPerms},
	"roles/viewer":                            {"Viewer", "View most resources", viewerPerms},
	"roles/storage.admin":                     {"Storage Admin", "Full control of buckets and objects", slices.Concat(perms("storage", "buckets", "create", "delete", "get", "getIamPolicy", "list", "setIamPolicy", "update"), storageObjectAdminPerms)},
	"roles/storage.objectAdmin":               {"Storage Object Admin", "Full control of objects", storageObjectAdminPerms},
	"roles/storage.objectViewer":              {"Storage Object Viewer", "Read objects and their metadata", storageObjectViewerPerms},
	"roles/artifactregistry.reader":           {"Artifact Registry Reader", "Read and download artifacts", artifactReaderPerms},
	"roles/artifactregistry.writer":           {"Artifact Registry Writer", "Read and write artifacts", slices.Concat(artifactReaderPerms, perms("artifactregistry", "repositories", "uploadArtifacts"), perms("artifactregistry", "tags", "create", "update"))},
	"roles/bigquery.admin":                    {"BigQuery Admin", "Administer all BigQuery resources and data", slices.Concat(perms("bigquery", "datasets", "create", "delete", "get", "update"), perms("bigquery", "tables", "create", "delete", "get", "getData", "list", "update", "updateData"), perms("bigquery", "jobs", "create", "get", "list", "listAll"))},
	"roles/bigquery.dataEditor":               {"BigQuery Data Editor", "Edit datasets and tables and their data", slices.Concat(perms("bigquery", "datasets", "create", "get"), perms("bigquery", "tables", "create", "delete", "get", "getData", "list", "update", "updateData"))},
	"roles/billing.admin":                     {"Billing Account Administrator", "Manage billing accounts and budgets", slices.Concat(perms("billing", "accounts", "close", "get", "getIamPolicy", "list", "setIamPolicy", "update"), perms("billing", "budgets", "create", "delete", "get", "list", "update"))},
	"roles/container.developer":               {"Kubernetes Engine Developer", "Access Kubernetes API objects in clusters", slices.Concat(perms("container", "clusters", "get", "list"), perms("container", "deployments", "create", "delete", "get", "list", "update"), perms("container", "pods", "create", "delete", "exec", "get", "getLogs", "list"), perms("container", "services", "create", "delete", "get", "list", "update"))},
	"roles/iam.securityAdmin":                 {"Security Admin", "Get and set IAM policies", slices.Concat(perms("iam", "roles", "get", "list"), perms("iam", "serviceAccounts", "getIamPolicy", "setIamPolicy"), perms("resourcemanager", "projects", "getIamPolicy", "setIamPolicy"))},
	"roles/iam.workloadIdentityUser":          {"Workload Identity User", "Impersonate service accounts from workloads", perms("iam", "serviceAccounts", "get", "getAccessToken", "getOpenIdToken", "list")},
	"roles/iap.httpsResourceAccessor":         {"IAP-secured Web App User", "Access web apps secured by IAP", perms("iap", "webServiceVersions", "accessViaIAP")},
	"roles/iap.settingsAdmin":                 {"IAP Settings Admin", "Manage IAP settings", slices.Concat(perms("iap", "web", "getSettings", "updateSettings"), perms("iap", "webServices", "getSettings", "updateSettings"))},
	"roles/resourcemanager.folderAdmin":       {"Folder Admin", "Full control of folders", slices.Concat(perms("resourcemanager", "folders", "create", "delete", "get", "getIamPolicy", "list", "move", "setIamPolicy", "update"), perms("resourcemanager", "projects", "get", "list"))},
	"roles/resourcemanager.organizationAdmin": {"Organization Administrator", "Manage the organization's IAM policies and view its hierarchy", slices.Concat(perms("resourcemanager", "organizations", "get", "getIamPolicy", "setIamPolicy"), perms("resourcemanager", "folders", "get", "list"), perms("resourcemanager", "projects", "get", "list"))},
}
