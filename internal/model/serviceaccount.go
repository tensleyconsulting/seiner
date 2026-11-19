package model

import "strings"

// MetaEmail is a service account's email address.
const MetaEmail = "email"

// GoogleServiceAccount reports whether email is a service account Google
// creates rather than one the user created: a default account
// (N-compute@developer.gserviceaccount.com, p@appspot.gserviceaccount.com)
// or a service agent (N@cloudservices.gserviceaccount.com,
// service-N@gcp-sa-x.iam.gserviceaccount.com,
// service-N@container-engine-robot.iam.gserviceaccount.com). The Console
// hides these behind a toggle.
func GoogleServiceAccount(email string) bool {
	local, domain, ok := strings.Cut(strings.ToLower(email), "@")
	if !ok || local == "" {
		return false
	}
	sub, ok := strings.CutSuffix(domain, ".gserviceaccount.com")
	if !ok {
		return false
	}
	project, ok := strings.CutSuffix(sub, ".iam")
	if !ok {
		return true // developer, appspot, cloudservices, cloudbuild, system, …
	}
	return strings.HasPrefix(project, "gcp-sa-") || serviceAgentLocal(local)
}

// serviceAgentLocal reports whether local is a service agent's local part:
// service-N, service-org-N, or service-folder-N. A user-created account id
// must start with a letter, so it can only collide by using that exact
// shape.
func serviceAgentLocal(local string) bool {
	rest, ok := strings.CutPrefix(local, "service-")
	if !ok {
		return false
	}
	if r, ok := strings.CutPrefix(rest, "org-"); ok {
		rest = r
	} else if r, ok := strings.CutPrefix(rest, "folder-"); ok {
		rest = r
	}
	return rest != "" && strings.Trim(rest, "0123456789") == ""
}

// IsGoogleServiceAccount reports whether r is a service account Google
// created (see GoogleServiceAccount).
func (r Resource) IsGoogleServiceAccount() bool {
	return r.Service == ServiceServiceAccount && GoogleServiceAccount(r.Metadata[MetaEmail])
}

// WithoutGoogleServiceAccounts returns items without the service accounts
// Google created. It returns items itself when there are none to drop.
func WithoutGoogleServiceAccounts(items []Resource) []Resource {
	for i, r := range items {
		if !r.IsGoogleServiceAccount() {
			continue
		}
		out := append([]Resource(nil), items[:i]...)
		for _, r := range items[i+1:] {
			if !r.IsGoogleServiceAccount() {
				out = append(out, r)
			}
		}
		return out
	}
	return items
}
