package regions

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestStaticIgnoresProject(t *testing.T) {
	t.Parallel()
	s := Static{"us-central1", "europe-west1"}
	got, err := s.Regions(context.Background(), "any-project")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"us-central1", "europe-west1"}) {
		t.Fatalf("got %v", got)
	}

	other, err := s.Regions(context.Background(), "other-project")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(other, got) {
		t.Fatalf("other = %v, want %v", other, got)
	}
}

func TestStaticRespectsCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DefaultRegions.Regions(ctx, "p"); err == nil {
		t.Fatal("want error from canceled context")
	}
}

func TestFakeReturnsErr(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	f := Fake{Err: boom}
	if _, err := f.Regions(context.Background(), "p"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestFakeReturnsNames(t *testing.T) {
	t.Parallel()
	f := Fake{Names: []string{"us-central1"}}
	got, err := f.Regions(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"us-central1"}) {
		t.Fatalf("got %v", got)
	}
}
