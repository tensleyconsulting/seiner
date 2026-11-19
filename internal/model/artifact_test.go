package model

import (
	"slices"
	"testing"
)

func TestGroupBySeverity(t *testing.T) {
	t.Parallel()
	parent := Resource{ID: "projects/p/locations/us/repositories/r/packages/a/versions/sha256:x", ScopeID: "projects/p", Location: "us"}
	vuln := func(id, sev, fix string) Resource {
		return Resource{ID: id, Name: id, Service: ServiceVulnerability, State: sev,
			Metadata: map[string]string{"fixAvailable": fix, MetaArtifactImage: "us-docker.pkg.dev/p/r/a@sha256:x"}}
	}
	items := []Resource{
		vuln("low-1", "LOW", "no"),
		vuln("crit-1", "CRITICAL", "yes"),
		vuln("odd-1", "SEVERITY_UNSPECIFIED", "no"),
		vuln("crit-2", "CRITICAL", "no"),
		vuln("high-1", "HIGH", "yes"),
		{ID: "other", Service: ServiceArtifactVersion},
	}
	groups, rest := GroupBySeverity(parent, items)
	if len(rest) != 1 || rest[0].ID != "other" {
		t.Fatalf("rest %+v", rest)
	}
	var names []string
	for _, g := range groups {
		names = append(names, g.Node.Name)
		if !g.Node.IsSeverityGroup() || g.Node.ParentID != parent.ID || g.Node.FacetID != FacetVulnerabilities {
			t.Errorf("%s node %+v", g.Node.Name, g.Node)
		}
	}
	if want := []string{"Critical", "High", "Low", "Other"}; !slices.Equal(names, want) {
		t.Fatalf("groups %q, want %q", names, want)
	}
	crit := groups[0]
	if len(crit.Vulnerabilities) != 2 || crit.Vulnerabilities[0].ID != "crit-1" || crit.Vulnerabilities[1].ID != "crit-2" {
		t.Fatalf("critical %+v", crit.Vulnerabilities)
	}
	n := crit.Node
	if n.Badge != "2" || n.Status != StatusError || n.Metadata["fixAvailable"] != "1 of 2" || n.ID != SeverityGroupID(parent.ID, "CRITICAL") {
		t.Fatalf("critical node %+v", n)
	}
	if n.Metadata[MetaArtifactImage] == "" {
		t.Error("group node should keep the image for its console link")
	}
	if other := groups[3].Node; other.ID != SeverityGroupID(parent.ID, "BOGUS") || other.Status != StatusUnknown {
		t.Fatalf("other node %+v", other)
	}
}

func TestSeverityStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		severity string
		want     ResourceStatus
	}{
		{"CRITICAL", StatusError},
		{"HIGH", StatusError},
		{"MEDIUM", StatusWarning},
		{"LOW", StatusWarning},
		{"MINIMAL", StatusHealthy},
		{"SEVERITY_UNSPECIFIED", StatusUnknown},
		{"", StatusUnknown},
	}
	for _, tc := range cases {
		if got := SeverityStatus(tc.severity); got != tc.want {
			t.Errorf("SeverityStatus(%q) = %q, want %q", tc.severity, got, tc.want)
		}
	}
}
