// Package vpcsc adapts VPC Service Controls (the Access Context Manager API)
// to model.Resource. Access policies belong to an organization: the
// organization lists its policies, org-level and scoped, and each expands
// to its service perimeters and access levels. The API cannot say which
// perimeters protect a project, so a project's perimeters come from listing
// every perimeter in its organization and matching the project's number.
package vpcsc
