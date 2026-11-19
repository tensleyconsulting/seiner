// Package workspace adapts Google Workspace (or Cloud Identity) users,
// groups, and group members to model.Resource. They belong to the
// customer that owns an organization, so only organizations list them.
// Users come from the Admin SDK Directory API and groups from Cloud
// Identity, both read with the cloud-platform scope. Access is granted by
// a Workspace admin role, not Cloud IAM; a caller without one is told
// nothing, since most callers have none.
package workspace
