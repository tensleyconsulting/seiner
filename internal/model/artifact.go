package model

import (
	"slices"
	"strconv"
	"strings"
)

const (
	// MetaArtifactFormat is the repository format (DOCKER, PYTHON, …) on
	// Artifact Registry repositories, packages, and versions.
	MetaArtifactFormat = "format"
	// MetaArtifactImage is the scanned image (host/project/repo/image@digest)
	// on a vulnerability.
	MetaArtifactImage = "image"
)

// ArtifactRepositoryURI is a repository's registry address without a scheme:
// us-central1-docker.pkg.dev/p/repo. format is the lower-case repository
// format. A domain-scoped project (example.com:p) becomes example.com/p.
func ArtifactRepositoryURI(project, location, format, repo string) string {
	return location + "-" + format + ".pkg.dev/" + strings.ReplaceAll(project, ":", "/") + "/" + repo
}

// KindSeverity is a synthetic node holding one severity's vulnerabilities
// under an image version.
const KindSeverity = "severity"

// MetaSeverity is the effective severity on a severity group node.
const MetaSeverity = "severity"

// severities are the Container Analysis severities, most severe first.
var severities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "MINIMAL"}

// SeverityRank is 0 for CRITICAL through 4 for MINIMAL; unknown severities
// rank last.
func SeverityRank(s string) int {
	if i := slices.Index(severities, s); i >= 0 {
		return i
	}
	return len(severities)
}

// SeverityStatus maps a severity onto the coarse UI bucket. Every real
// finding is at least a warning; only MINIMAL reads as healthy.
func SeverityStatus(s string) ResourceStatus {
	switch s {
	case "CRITICAL", "HIGH":
		return StatusError
	case "MEDIUM", "LOW":
		return StatusWarning
	case "MINIMAL":
		return StatusHealthy
	default:
		return StatusUnknown
	}
}

// IsSeverityGroup reports whether r is a synthetic node holding one
// severity's vulnerabilities.
func (r Resource) IsSeverityGroup() bool {
	return r.Metadata[MetaKind] == KindSeverity
}

// SeverityGroup is the vulnerabilities of one severity, under its synthetic
// node.
type SeverityGroup struct {
	Node            Resource
	Vulnerabilities []Resource
}

// GroupBySeverity groups the vulnerabilities in items by their severity
// (State), most severe first; unknown severities share a last group. Items
// keep their order within a group. Anything that is not a vulnerability
// is returned in rest. Group nodes sit under parent, the image version.
func GroupBySeverity(parent Resource, items []Resource) (groups []SeverityGroup, rest []Resource) {
	at := map[string]int{}
	for _, r := range items {
		if r.Service != ServiceVulnerability {
			rest = append(rest, r)
			continue
		}
		sev := r.State
		if SeverityRank(sev) == len(severities) {
			sev = ""
		}
		i, ok := at[sev]
		if !ok {
			i = len(groups)
			at[sev] = i
			groups = append(groups, SeverityGroup{})
		}
		groups[i].Vulnerabilities = append(groups[i].Vulnerabilities, r)
	}
	for i := range groups {
		groups[i].Node = severityGroupResource(parent, groups[i].Vulnerabilities)
	}
	slices.SortStableFunc(groups, func(a, b SeverityGroup) int {
		return SeverityRank(a.Node.Metadata[MetaSeverity]) - SeverityRank(b.Node.Metadata[MetaSeverity])
	})
	return groups, rest
}

// SeverityGroupID is the id of parent's group node for severity, or for
// unknown severities when severity is not one of the known ones.
func SeverityGroupID(parentID, severity string) string {
	if SeverityRank(severity) == len(severities) {
		severity = "other"
	}
	return parentID + "/severity/" + strings.ToLower(severity)
}

// severityGroupResource is the node for vulns, all of one severity, under
// parent. Its badge is the count, colored by the severity.
func severityGroupResource(parent Resource, vulns []Resource) Resource {
	sev := vulns[0].State
	label := "Other"
	if SeverityRank(sev) < len(severities) {
		label = sev[:1] + strings.ToLower(sev[1:])
	} else {
		sev = ""
	}
	fixable := 0
	for _, v := range vulns {
		if v.Metadata["fixAvailable"] == "yes" {
			fixable++
		}
	}
	n := strconv.Itoa(len(vulns))
	fix := strconv.Itoa(fixable) + " of " + n
	md := map[string]string{MetaKind: KindSeverity, "vulnerabilities": n, "fixAvailable": fix}
	if sev != "" {
		md[MetaSeverity] = sev
	}
	if img := vulns[0].Metadata[MetaArtifactImage]; img != "" {
		md[MetaArtifactImage] = img
	}
	return Resource{
		ID:       SeverityGroupID(parent.ID, sev),
		Name:     label,
		Service:  ServiceVulnerability,
		ScopeID:  parent.ScopeID,
		Location: parent.Location,
		Status:   SeverityStatus(sev),
		Badge:    n,
		ParentID: parent.ID,
		FacetID:  FacetVulnerabilities,
		Metadata: md,
		Fields:   FieldsOf(md, MetaSeverity, "vulnerabilities", "fixAvailable", MetaArtifactImage),
	}
}
