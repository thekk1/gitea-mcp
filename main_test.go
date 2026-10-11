package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	cases := []struct {
		name string
		dev  string
		info *debug.BuildInfo
		want string
	}{
		{
			name: "nil build info falls back to dev version",
			dev:  "dev",
			info: nil,
			want: "dev",
		},
		{
			name: "devel version falls back to dev version",
			dev:  "dev",
			info: &debug.BuildInfo{Main: debug.Module{Path: "gitea.com/gitea/gitea-mcp", Version: "(devel)"}},
			want: "dev",
		},
		{
			name: "empty version falls back to dev version",
			dev:  "dev",
			info: &debug.BuildInfo{Main: debug.Module{Path: "gitea.com/gitea/gitea-mcp", Version: ""}},
			want: "dev",
		},
		{
			name: "v1 version accepted for module path without major suffix",
			dev:  "dev",
			info: &debug.BuildInfo{Main: debug.Module{Path: "gitea.com/gitea/gitea-mcp", Version: "v1.2.3"}},
			want: "v1.2.3",
		},
		{
			name: "v0 pseudo-version accepted for module path without major suffix",
			dev:  "dev",
			info: &debug.BuildInfo{Main: debug.Module{Path: "gitea.com/gitea/gitea-mcp", Version: "v0.0.0-20240102150405-abcdef123456"}},
			want: "v0.0.0-20240102150405-abcdef123456",
		},
		{
			name: "unparsable version rejected",
			dev:  "dev",
			info: &debug.BuildInfo{Main: debug.Module{Path: "gitea.com/gitea/gitea-mcp", Version: "garbage"}},
			want: "dev",
		},
		{
			name: "v2 version rejected when module path has no /v2 suffix",
			dev:  "dev",
			info: &debug.BuildInfo{Main: debug.Module{Path: "gitea.com/gitea/gitea-mcp", Version: "v2.0.0"}},
			want: "dev",
		},
		{
			name: "v2 version accepted when module path has /v2 suffix",
			dev:  "dev",
			info: &debug.BuildInfo{Main: debug.Module{Path: "gitea.com/gitea/gitea-mcp/v2", Version: "v2.0.0"}},
			want: "v2.0.0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveVersion(tc.dev, tc.info)
			if got != tc.want {
				t.Errorf("resolveVersion(%q, %+v) = %q, want %q", tc.dev, tc.info, got, tc.want)
			}
		})
	}
}
