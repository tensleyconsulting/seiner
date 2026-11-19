package model

import (
	"fmt"
	"slices"
)

// Normalize sanitizes untrusted provider strings and fills derived fields.
func Normalize(r Resource) Resource {
	r = sanitizeResource(r)
	if r.Status == "" {
		r.Status = StatusUnknown
	}
	if r.Metadata == nil {
		r.Metadata = map[string]string{}
	}
	if r.Labels == nil {
		r.Labels = map[string]string{}
	}
	if len(r.Fields) == 0 {
		r.Fields = FieldsOf(r.Metadata)
	}
	return r
}

// FieldsOf lists md's entries as display fields: keys named in order first,
// in that order, then the rest sorted. Internal keys (MetaKind, MetaPrefix)
// and keys missing from md are skipped.
func FieldsOf(md map[string]string, order ...string) []Field {
	var out []Field
	seen := make(map[string]bool, len(order))
	for _, k := range order {
		if v, ok := md[k]; ok && !seen[k] && !internalKey(k) {
			out = append(out, Field{Key: k, Value: v})
		}
		seen[k] = true
	}
	var rest []string
	for k := range md {
		if !seen[k] && !internalKey(k) {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	for _, k := range rest {
		out = append(out, Field{Key: k, Value: md[k]})
	}
	return out
}

func internalKey(k string) bool { return k == MetaKind || k == MetaPrefix }

// Validate reports missing identity fields.
func (r Resource) Validate() error {
	if r.ID == "" || r.Name == "" || r.Service == "" {
		return fmt.Errorf("resource requires id, name, and service")
	}
	return nil
}
