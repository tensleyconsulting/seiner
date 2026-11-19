package resources

import (
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestRowKeys(t *testing.T) {
	t.Parallel()
	owner := model.Resource{ID: "projects/p"}
	bucket := model.Resource{ID: "projects/p/buckets/b"}
	objects := model.Facet{ID: model.FacetObjects}
	tests := []struct {
		name string
		row  Row
		want string
	}{
		{"resource", Row{Kind: RowResource, Resource: owner}, "projects/p"},
		{"facet", Row{Kind: RowFacet, Resource: bucket, Facet: objects}, "projects/p/buckets/b\x1fobjects"},
		{"facet note", Row{Kind: RowNote, Resource: bucket, Facet: objects}, "projects/p/buckets/b\x1fobjects\x1f~"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.row.Key(); got != tc.want {
				t.Errorf("Key()=%q, want %q", got, tc.want)
			}
		})
	}
}
