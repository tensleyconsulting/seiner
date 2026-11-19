package model

import "strings"

// Resource.ScopeID and HierarchyNode.ID are always full CRM resource names
// (organizations/1, folders/2, projects/p). Bare project ids exist only at
// the CLI boundary; convert them with ProjectScope.

// ProjectScope returns projects/{id} for a bare id, or the input when it
// already is a CRM name.
func ProjectScope(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") {
		return id
	}
	return "projects/" + id
}

// ScopeNode is a CRM node for scope (a CRM name, or a bare project id)
// before the tree is read, named by its id.
func ScopeNode(scope string) HierarchyNode {
	scope = ProjectScope(scope)
	_, id, _ := strings.Cut(scope, "/")
	return HierarchyNode{ID: scope, Name: id, Kind: ScopeKind(scope)}
}

// ScopeKind reports org, folder, or project for a CRM name, or "" for
// anything else.
func ScopeKind(scope string) NodeKind {
	coll, id, ok := strings.Cut(strings.TrimSpace(scope), "/")
	if !ok || id == "" || strings.Contains(id, "/") {
		return ""
	}
	switch coll {
	case "organizations":
		return KindOrg
	case "folders":
		return KindFolder
	case "projects":
		return KindProject
	default:
		return ""
	}
}

// ScopeService is the explorer service of a CRM name, or "" for anything
// else.
func ScopeService(scope string) ServiceType {
	switch ScopeKind(scope) {
	case KindOrg:
		return ServiceOrganization
	case KindFolder:
		return ServiceFolder
	case KindProject:
		return ServiceProject
	}
	return ""
}

// ProjectID extracts a project id from a scope. It accepts "projects/my-proj"
// and, for CLI input, a bare "my-proj". Org/folder names are not projects.
func ProjectID(scope string) (string, bool) {
	scope = strings.TrimSpace(scope)
	switch ScopeKind(scope) {
	case KindProject:
		return strings.TrimPrefix(scope, "projects/"), true
	case KindOrg, KindFolder:
		return "", false
	}
	if scope != "" && !strings.Contains(scope, "/") {
		return scope, true
	}
	return "", false
}

// IAMResourceName is the Resource Manager resource name for GetIamPolicy:
// scope itself when it is a CRM name, else "".
func IAMResourceName(scope string) string {
	scope = strings.TrimSpace(scope)
	if ScopeKind(scope) == "" {
		return ""
	}
	return scope
}
