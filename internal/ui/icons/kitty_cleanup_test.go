package icons

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestDeleteImageBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		id   int
		tmux bool
		want string
	}{
		{"direct", 7, false, "\x1b_Ga=d,d=I,i=7,q=2\x1b\\"},
		{"tmux", 7, true, "\x1bPtmux;\x1b\x1b_Ga=d,d=I,i=7,q=2\x1b\x1b\\\x1b\\"},
		{"large id", 70000, false, "\x1b_Ga=d,d=I,i=70000,q=2\x1b\\"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := deleteImage(tc.id, tc.tmux); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestKittyDeleteSentImages(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		upload bool
		logo   bool
		tmux   bool
		want   int // delete sequences
	}{
		{"nothing uploaded", false, false, false, 0},
		{"icons", true, false, false, 4},
		{"icons and logo", true, true, false, 6},
		{"icons and logo under tmux", true, true, true, 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			png := tinyPNG(t)
			k := newKitty(map[model.ServiceType][]byte{
				model.ServiceGKE:          png,
				model.ServiceCloudStorage: png,
			})
			k.tmux = tc.tmux
			if tc.logo {
				k.logo = png
			}
			if tc.upload {
				_ = k.Upload()
			}
			del := k.Delete()
			if n := strings.Count(del, "a=d,d=I,"); n != tc.want {
				t.Fatalf("deletes=%d want %d: %q", n, tc.want, del)
			}
			wrapped := 0
			if tc.tmux {
				wrapped = tc.want
			}
			if n := strings.Count(del, "\x1bPtmux;"); n != wrapped {
				t.Fatalf("tmux wrappers=%d want %d", n, wrapped)
			}
			if tc.upload {
				for _, id := range []int{k.listIDs[model.ServiceGKE], k.heroIDs[model.ServiceGKE]} {
					if !strings.Contains(del, fmt.Sprintf("i=%d,", id)) {
						t.Fatalf("missing delete for id %d", id)
					}
				}
			}
			if k.Delete() != "" {
				t.Fatal("second delete must be empty")
			}
			if tc.upload && k.Upload() == "" {
				t.Fatal("upload after delete must send again")
			}
		})
	}
}

func TestKittyResetResendsAll(t *testing.T) {
	t.Parallel()
	png := tinyPNG(t)
	k := newKitty(map[model.ServiceType][]byte{
		model.ServiceGKE:          png,
		model.ServiceCloudStorage: png,
	})
	k.logo = png
	first := k.Upload()
	if k.Upload() != "" {
		t.Fatal("second upload must not resend")
	}
	k.Reset()
	again := k.Upload()
	if a, b := strings.Count(first, "a=t,"), strings.Count(again, "a=t,"); a != b || a != 6 {
		t.Fatalf("transmits first=%d after reset=%d, want 6", a, b)
	}
	if k.sentCount() != 2 {
		t.Fatalf("sent=%d", k.sentCount())
	}
}

func TestOtherRenderersHaveNothingToDelete(t *testing.T) {
	t.Parallel()
	for _, r := range []Renderer{TextRenderer{}, NoopRenderer{}, (*KittyRenderer)(nil)} {
		r.Reset()
		if got := r.Delete(); got != "" {
			t.Errorf("%T: delete %q", r, got)
		}
	}
}
