package model

import (
	"encoding/json"
	"time"
)

// JSON is a stable, copy-friendly encoding using an explicit field allowlist
// so provider payloads never leak into the clipboard.
func (r Resource) JSON() (string, error) {
	type dto struct {
		ID          string            `json:"id"`
		Name        string            `json:"name"`
		Service     ServiceType       `json:"service"`
		ScopeID     string            `json:"scopeId"`
		Location    string            `json:"location"`
		Status      ResourceStatus    `json:"status"`
		Description string            `json:"description,omitempty"`
		Metadata    map[string]string `json:"metadata,omitempty"`
		ParentID    string            `json:"parentId,omitempty"`
		Facets      []Facet           `json:"facets,omitempty"`
		FacetID     string            `json:"facetId,omitempty"`
		State       string            `json:"state,omitempty"`
		Created     string            `json:"created,omitempty"`
		Labels      map[string]string `json:"labels,omitempty"`
		Fields      []fieldDTO        `json:"fields,omitempty"`
		Lists       []listDTO         `json:"lists,omitempty"`
	}
	var created string
	if !r.Created.IsZero() {
		created = r.Created.UTC().Format(time.RFC3339)
	}
	var fields []fieldDTO
	for _, f := range r.Fields {
		fields = append(fields, fieldDTO(f))
	}
	var lists []listDTO
	for _, l := range r.Lists {
		lists = append(lists, listDTO(l))
	}
	b, err := json.MarshalIndent(dto{
		ID:          r.ID,
		Name:        r.Name,
		Service:     r.Service,
		ScopeID:     r.ScopeID,
		Location:    r.Location,
		Status:      r.Status,
		Description: r.Description,
		Metadata:    r.Metadata,
		ParentID:    r.ParentID,
		Facets:      r.Facets,
		FacetID:     r.FacetID,
		State:       r.State,
		Created:     created,
		Labels:      r.Labels,
		Fields:      fields,
		Lists:       lists,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type fieldDTO struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type listDTO struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Items []string `json:"items"`
}
