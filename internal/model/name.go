package model

import (
	"net/url"
	"strings"
)

// ParsedName is a GCP relative resource name split into segments.
type ParsedName struct {
	Scope    string   // organizations/1, folders/2, or projects/p
	Project  string   // p when Scope is a project
	Location string   // from locations|regions|zones/{l}; "" for global
	Kind     string   // collection segment before the leaf (buckets, instances)
	Name     string   // the leaf, path-unescaped
	Rest     []string // segments after the leaf (objects, foo), unescaped
}

// ScopeCollection and ScopeID split Scope into organizations|folders|projects
// and the id after it.
func (p ParsedName) ScopeCollection() string {
	coll, _, _ := strings.Cut(p.Scope, "/")
	return coll
}

func (p ParsedName) ScopeID() string {
	_, id, _ := strings.Cut(p.Scope, "/")
	return id
}

// ParseName splits a relative resource name such as
// projects/p/zones/z/instances/vm. A bare scope (projects/p) parses with the
// scope collection as Kind and its id as Name. "global" and the "iam"
// namespace before bindings are skipped, so organizations/o/iam/bindings/r
// has Kind "bindings". It reports false for names not rooted at a CRM scope.
func ParseName(id string) (ParsedName, bool) {
	segs := strings.Split(strings.TrimSpace(id), "/")
	if len(segs) < 2 || segs[1] == "" {
		return ParsedName{}, false
	}
	var out ParsedName
	switch segs[0] {
	case "organizations", "folders", "projects":
	default:
		return ParsedName{}, false
	}
	out.Scope = segs[0] + "/" + segs[1]
	if segs[0] == "projects" {
		out.Project = segs[1]
	}
	rest := segs[2:]
	if len(rest) == 0 {
		out.Kind = segs[0]
		out.Name = segs[1]
		return out, true
	}
	switch rest[0] {
	case "locations", "regions", "zones":
		if len(rest) < 2 || rest[1] == "" {
			return ParsedName{}, false
		}
		out.Location = rest[1]
		rest = rest[2:]
	case "global":
		rest = rest[1:]
	}
	if len(rest) > 0 && rest[0] == "iam" {
		rest = rest[1:]
	}
	if len(rest) < 2 || rest[0] == "" || rest[1] == "" {
		return ParsedName{}, false
	}
	out.Kind = rest[0]
	name, err := url.PathUnescape(rest[1])
	if err != nil {
		return ParsedName{}, false
	}
	out.Name = name
	for _, s := range rest[2:] {
		u, err := url.PathUnescape(s)
		if err != nil {
			return ParsedName{}, false
		}
		out.Rest = append(out.Rest, u)
	}
	return out, true
}
