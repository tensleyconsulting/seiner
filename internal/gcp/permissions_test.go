package gcp

import (
	"regexp"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/crm"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// permRE is the shape of an IAM permission: service.resource.verb.
var permRE = regexp.MustCompile(`^[a-z]+\.[a-zA-Z]+\.[a-zA-Z]+$`)

// Every live provider that lists at a scope declares its permissions, so a
// new adapter cannot silently skip the check.
func TestLiveProvidersDeclarePermissions(t *testing.T) {
	t.Parallel()
	// These list nothing at a scope: Service Usage feeds API gating, and
	// vulnerabilities, role permissions, and managed instances are facets. Workstations falls back to the caller's
	// usable workstations when a list is denied, so a project-level check
	// would hide what a workstation user may see. BigQuery lists the
	// datasets the caller may read, often granted on the dataset alone.
	// Workspace users and groups are read with a Workspace admin role,
	// not Cloud IAM, and list nothing when it is missing.
	undeclared := map[model.ServiceType]bool{
		model.ServiceWorkspaceUser:      true,
		model.ServiceWorkspaceGroup:     true,
		model.ServiceUsage:              true,
		model.ServiceVulnerability:      true,
		model.ServiceIAMPermission:      true,
		model.ServiceManagedInstance:    true,
		model.ServiceWorkstationCluster: true,
		model.ServiceBigQueryDataset:    true,
	}
	// Providers that also list at org and folder scopes, by the kinds they
	// serve there.
	crmWide := map[model.ServiceType][]string{
		model.ServiceIAMBinding:    {"organizations/1", "folders/2"},
		model.ServiceLogSink:       {"organizations/1", "folders/2"},
		model.ServiceOrgPolicy:     {"organizations/1", "folders/2"},
		model.ServiceCustomRole:    {"organizations/1"},
		model.ServiceAccessPolicy:  {"organizations/1"},
		model.ServiceAccessBinding: {"organizations/1"},
	}
	// Providers that list nothing on a project: access policies and access
	// bindings belong to an organization.
	notOnProjects := map[model.ServiceType]bool{
		model.ServiceAccessPolicy:  true,
		model.ServiceAccessBinding: true,
	}
	providers, _ := liveProviders(nil, crm.NewLazyClients(nil), nil)
	for _, p := range providers {
		svc := p.Service()
		d, ok := p.(model.PermissionDeclarer)
		if undeclared[svc] {
			if ok {
				t.Errorf("%s declares permissions; drop it from undeclared", svc)
			}
			continue
		}
		if !ok {
			t.Errorf("%s does not implement model.PermissionDeclarer", svc)
			continue
		}
		perms := d.ListPermissions("projects/p")
		switch {
		case notOnProjects[svc] && perms != nil:
			t.Errorf("%s declares %v on a project, which it does not list", svc, perms)
		case !notOnProjects[svc] && len(perms) == 0:
			t.Errorf("%s declares nothing on a project", svc)
		}
		for _, scope := range crmWide[svc] {
			perms = append(perms, d.ListPermissions(scope)...)
			if len(d.ListPermissions(scope)) == 0 {
				t.Errorf("%s declares nothing on %s", svc, scope)
			}
		}
		for _, perm := range perms {
			if !permRE.MatchString(perm) {
				t.Errorf("%s: %q is not a permission name", svc, perm)
			}
		}
		for _, scope := range []string{"organizations/1", "folders/2", ""} {
			if len(crmWide[svc]) > 0 {
				break
			}
			if got := d.ListPermissions(scope); got != nil {
				t.Errorf("%s declares %v on %q, which it does not list", svc, got, scope)
			}
		}
	}
}
