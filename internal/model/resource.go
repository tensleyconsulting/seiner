package model

import (
	"strings"
	"time"
)

// ResourceStatus is a coarse UI status, independent of provider enums.
type ResourceStatus string

const (
	StatusHealthy ResourceStatus = "Healthy"
	StatusWarning ResourceStatus = "Warning"
	StatusError   ResourceStatus = "Error"
	StatusUnknown ResourceStatus = "Unknown"
)

// Field is one ordered display field in the details pane.
type Field struct{ Key, Value string }

// Resource is the normalized view of a cloud object.
// Provider-specific payloads belong in Metadata, not in the UI.
type Resource struct {
	ID          string
	Name        string
	Service     ServiceType
	ScopeID     string
	Location    string
	Status      ResourceStatus
	Description string
	Metadata    map[string]string
	// ParentID is a product-containment link (cluster → node pool). Empty
	// means top-level in this CRM scope. CRM parent is ScopeID, not this field.
	ParentID string
	// Facets are named drills shown in the details pane.
	Facets []Facet
	// FacetID is set on child rows produced by a drill (permissions, objects).
	FacetID string
	// State is the provider's own state word (RUNNING, TERMINATED); "" if
	// none. Status is the coarse UI bucket it maps to.
	State string
	// Created is the creation time; zero if unknown.
	Created time.Time
	// Labels are GCP labels, kept apart from Metadata.
	Labels map[string]string
	// Fields are the ordered display fields. Metadata stays the lookup map
	// for internal keys and search.
	Fields []Field
	// Lists are value lists the details pane shows in full, one item per
	// line (an IAM binding's members).
	Lists []List
	// Badge is a short tag the explorer draws at the end of the row,
	// colored by Status, so a row's weight shows before it is selected (a
	// vulnerability's severity). "" draws none.
	Badge string
	// Origin is the kind of node a row is inherited from (the organization
	// or folder whose IAM binding a project inherits). The explorer draws
	// its icon at the end of the row. "" draws none.
	Origin ServiceType
	// Copies are extra copy targets the adapter offers after name, ID, and
	// JSON (an HSM attestation, a PEM chain). They are kept out of JSON
	// and search, since they can be large.
	Copies []Copy
}

// Copy is one adapter-supplied copy target.
type Copy struct {
	Title string // menu row, "Attestation"
	Label string // flash text after "copied ", "attestation"
	Text  string
}

// List is an ordered set of values shown under Label.
type List struct {
	ID    string
	Label string
	Items []string
}

// List returns the items of r's list with the given id, or nil.
func (r Resource) List(id string) []string {
	for _, l := range r.Lists {
		if l.ID == id {
			return l.Items
		}
	}
	return nil
}

// Matches reports whether query (already lower-cased) hits useful fields.
func (r Resource) Matches(query string) bool {
	if query == "" {
		return true
	}
	if containsFold(r.Name, query) ||
		containsFold(r.ID, query) ||
		containsFold(r.Location, query) ||
		containsFold(string(r.Status), query) ||
		containsFold(r.State, query) ||
		containsFold(r.Service.Label(), query) ||
		containsFold(r.Service.Abbrev(), query) ||
		containsFold(r.Description, query) {
		return true
	}
	for k, v := range r.Metadata {
		if internalKey(k) {
			continue
		}
		if containsFold(k, query) || containsFold(v, query) {
			return true
		}
	}
	for k, v := range r.Labels {
		if containsFold(k, query) || containsFold(v, query) {
			return true
		}
	}
	for _, f := range r.Fields {
		if containsFold(f.Key, query) || containsFold(f.Value, query) {
			return true
		}
	}
	for _, l := range r.Lists {
		for _, item := range l.Items {
			if containsFold(item, query) {
				return true
			}
		}
	}
	return false
}

// ContainsFold reports whether haystack contains needle under case-insensitive
// matching, using a zero-allocation byte scan when both strings are ASCII.
func ContainsFold(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	if isASCII(haystack) && isASCII(needle) {
		if len(needle) > len(haystack) {
			return false
		}
		n0 := asciiLower(needle[0])
		limit := len(haystack) - len(needle)
		for i := 0; i <= limit; i++ {
			if asciiLower(haystack[i]) != n0 {
				continue
			}
			match := true
			for j := 1; j < len(needle); j++ {
				if asciiLower(haystack[i+j]) != asciiLower(needle[j]) {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
		return false
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func containsFold(haystack, needle string) bool {
	return ContainsFold(haystack, needle)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func asciiLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// Roots returns resources with no product parent. Nested objects stay hidden
// until a drill stack exists; search may still pass the unfiltered catalog.
func Roots(resources []Resource) []Resource {
	out := make([]Resource, 0, len(resources))
	for _, r := range resources {
		if r.ParentID == "" {
			out = append(out, r)
		}
	}
	return out
}
