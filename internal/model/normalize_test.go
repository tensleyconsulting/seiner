package model

import (
	"errors"
	"slices"
	"testing"
)

func TestNormalize(t *testing.T) {
	t.Parallel()
	r := Normalize(Resource{
		ID:      "id",
		Name:    "n",
		Service: ServiceCloudStorage,
	})
	if r.Status != StatusUnknown {
		t.Errorf("status=%s", r.Status)
	}
	if r.Metadata == nil {
		t.Fatal("metadata should be non-nil")
	}
	if r.Labels == nil {
		t.Fatal("labels should be non-nil")
	}
}

func TestNormalizeFieldsFromMetadata(t *testing.T) {
	t.Parallel()
	r := Normalize(Resource{
		ID: "id", Name: "n", Service: ServiceCloudStorage,
		Metadata: map[string]string{"b": "2", "a": "1", MetaKind: KindPrefix, MetaPrefix: "x/", MetaPath: "x/y"},
	})
	want := []Field{{"a", "1"}, {"b", "2"}, {MetaPath, "x/y"}}
	if !slices.Equal(r.Fields, want) {
		t.Fatalf("fields %v want %v", r.Fields, want)
	}

	kept := Normalize(Resource{
		ID: "id", Name: "n", Service: ServiceCloudStorage,
		Metadata: map[string]string{"a": "1"},
		Fields:   []Field{{"z", "9"}},
	})
	if !slices.Equal(kept.Fields, []Field{{"z", "9"}}) {
		t.Fatalf("adapter fields replaced: %v", kept.Fields)
	}
}

func TestFieldsOf(t *testing.T) {
	t.Parallel()
	md := map[string]string{"c": "3", "a": "1", "b": "2", MetaKind: KindCollection}
	cases := []struct {
		name  string
		order []string
		want  []Field
	}{
		{"sorted", nil, []Field{{"a", "1"}, {"b", "2"}, {"c", "3"}}},
		{"declared first", []string{"c", "b"}, []Field{{"c", "3"}, {"b", "2"}, {"a", "1"}}},
		{"missing and repeated keys skipped", []string{"x", "b", "b"}, []Field{{"b", "2"}, {"a", "1"}, {"c", "3"}}},
		{"internal key never shown", []string{MetaKind}, []Field{{"a", "1"}, {"b", "2"}, {"c", "3"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FieldsOf(md, tc.order...); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	ok := Resource{ID: "id", Name: "n", Service: ServiceGKE}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Resource{Name: "n", Service: ServiceGKE}).Validate(); err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestKnownServicesHaveLabels(t *testing.T) {
	t.Parallel()
	for _, svc := range KnownServices() {
		if svc.Label() == string(svc) {
			t.Errorf("%s missing Label", svc)
		}
		if svc.Abbrev() == "?" {
			t.Errorf("%s missing Abbrev", svc)
		}
	}
}

func TestProviderErrorUnwrap(t *testing.T) {
	t.Parallel()
	inner := ErrNotFound
	err := ProviderError{Service: ServiceBigQuery, Err: inner}
	if err.Error() != "BigQuery: resource not found" {
		t.Errorf("Error()=%q", err.Error())
	}
	if !errors.Is(err, inner) {
		t.Fatal("Unwrap")
	}
}
