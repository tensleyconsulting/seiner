package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		ldflags string
		info    *debug.BuildInfo
		want    string
	}{
		{
			name:    "ldflags wins over build info",
			ldflags: "v0.3.0",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}},
			want:    "v0.3.0",
		},
		{
			name: "go install module version",
			info: &debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}},
			want: "v0.2.0",
		},
		{
			name: "local vcs build clean",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "fa4262c987654321"},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			want: "dev (fa4262c)",
		},
		{
			name: "local vcs build dirty",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "fa4262c987654321"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			want: "dev (fa4262c-dirty)",
		},
		{
			name: "no build info fallback",
			info: nil,
			want: "dev",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveVersion(tc.ldflags, tc.info); got != tc.want {
				t.Errorf("resolveVersion(%q) = %q, want %q", tc.ldflags, got, tc.want)
			}
		})
	}
}
