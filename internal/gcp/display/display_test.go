package display

import (
	"testing"
	"time"
)

func TestStamp(t *testing.T) {
	t.Parallel()
	if got := Stamp(time.Time{}); got != "" {
		t.Errorf("Stamp(zero) = %q", got)
	}
	est := time.FixedZone("EST", -5*3600)
	if got := Stamp(time.Date(2026, 11, 7, 22, 0, 30, 0, est)); got != "2026-11-08 03:00 UTC" {
		t.Errorf("Stamp = %q", got)
	}
}

func TestRelLink(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://www.googleapis.com/compute/v1/projects/p/zones/z/instanceGroupManagers/g": "projects/p/zones/z/instanceGroupManagers/g",
		"projects/p/zones/z/instanceGroupManagers/g":                                       "projects/p/zones/z/instanceGroupManagers/g",
		"zones/z": "zones/z",
		"":        "",
	}
	for in, want := range cases {
		if got := RelLink(in); got != want {
			t.Errorf("RelLink(%q) = %q, want %q", in, got, want)
		}
	}
}
