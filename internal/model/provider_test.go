package model

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestProviderErrorMessage(t *testing.T) {
	t.Parallel()
	inner := errors.New("rpc error: code = PermissionDenied desc = long text")
	tests := []struct {
		name string
		err  ProviderError
		want string
	}{
		{"message", ProviderError{Service: ServiceGKE, Kind: ErrKindPermissionDenied, Message: "missing container.clusters.list", Err: inner}, ServiceGKE.Label() + ": missing container.clusters.list"},
		{"no message", ProviderError{Service: ServiceGKE, Err: inner}, ServiceGKE.Label() + ": " + inner.Error()},
		{"kind only", ProviderError{Service: ServiceGKE, Kind: ErrKindTimeout}, ServiceGKE.Label() + ": timed out"},
		{"partial", ProviderError{Service: ServiceGKE, Kind: ErrKindPermissionDenied, Message: "missing x.y.list", Partial: true}, ServiceGKE.Label() + ": partial list: missing x.y.list"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error()=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestErrorKindLabel(t *testing.T) {
	t.Parallel()
	kinds := []ErrorKind{ErrKindUnknown, ErrKindPermissionDenied, ErrKindAPIDisabled, ErrKindNotFound, ErrKindTimeout, ErrKindQuota, ErrKindCanceled}
	seen := map[string]ErrorKind{}
	for _, k := range kinds {
		l := k.Label()
		if prev, dup := seen[l]; dup {
			t.Errorf("%s and %s share label %q", prev, k, l)
		}
		seen[l] = k
	}
}

func TestAsProviderError(t *testing.T) {
	t.Parallel()
	plain := errors.New("boom")
	classified := ProviderError{Service: ServiceGKE, Kind: ErrKindQuota, Message: "quota exceeded", Err: plain}
	tests := []struct {
		name string
		err  error
		want ProviderError
	}{
		{"plain", plain, ProviderError{Service: ServiceCloudStorage, Kind: ErrKindUnknown, Err: plain}},
		{"classified", classified, classified},
		{"wrapped", fmt.Errorf("ctx: %w", classified), classified},
		{"no service", ProviderError{Err: plain}, ProviderError{Service: ServiceCloudStorage, Kind: ErrKindUnknown, Err: plain}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := AsProviderError(ServiceCloudStorage, tc.err); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestProjectPermissions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope string
		want  []string
	}{
		{"projects/p", []string{"a.b.list"}},
		{"folders/1", nil},
		{"organizations/1", nil},
		{"", nil},
	}
	for _, tc := range cases {
		if got := ProjectPermissions(tc.scope, "a.b.list"); !slices.Equal(got, tc.want) {
			t.Errorf("%q: %v, want %v", tc.scope, got, tc.want)
		}
	}
}
