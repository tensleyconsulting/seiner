package resources

import (
	"github.com/tensleyconsulting/seiner/internal/model"
)

// RowKind is a list row: a real resource, a facet header, or a muted note
// such as a truncation marker.
type RowKind int

const (
	RowResource RowKind = iota
	RowFacet
	// RowNote is informational and never selectable.
	RowNote
)

// Row is one line in the explorer tree or the contents list.
type Row struct {
	Kind       RowKind
	Resource   model.Resource
	Facet      model.Facet
	Depth      int
	Expanded   bool
	Expandable bool
	// Pinned marks the top organization row, which is always expanded. It
	// draws no expand triangle, and the rows under it are indented one
	// level less.
	Pinned bool
	// Loading marks a scope, group, or facet whose load is in flight.
	Loading bool
	// Problems marks a scope, group, or facet whose load reported errors.
	Problems bool
	// Note is the text of a RowNote row. Resource is the owner the load
	// was for, and Facet is set when the note is about that facet's load.
	Note string
	// Via is the Key of the row this one is listed under when Resource
	// appears in more than one place (a binding under each of its
	// principals). It keeps the row's Key unique.
	Via string
	// Choices are mutually exclusive options drawn after the name, with
	// Chosen marking the selected one.
	Choices []string
	Chosen  int
}

func (r Row) Key() string {
	switch r.Kind {
	case RowFacet:
		return r.Resource.ID + "\x1f" + r.Facet.ID
	case RowNote:
		owner := r.Resource.ID
		if r.Via != "" {
			owner = r.Via + "\x1f" + owner
		}
		if r.Facet.ID != "" {
			owner += "\x1f" + r.Facet.ID
		}
		return owner + "\x1f~"
	}
	if r.Via != "" {
		return r.Via + "\x1f" + r.Resource.ID
	}
	return r.Resource.ID
}
