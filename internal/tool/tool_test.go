package tool

import (
	"slices"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func makeTool(name string) ServerTool {
	return ServerTool{Tool: &mcp.Tool{Name: name}}
}

func names(sts []ServerTool) []string {
	out := make([]string, len(sts))
	for i, st := range sts {
		out[i] = st.Tool.Name
	}
	return out
}

func TestTools(t *testing.T) {
	tests := []struct {
		name     string
		readOnly bool
		allowed  map[string]struct{}
		read     []string
		write    []string
		want     []string
	}{
		{
			name:  "no filters returns write then read",
			read:  []string{"r1", "r2"},
			write: []string{"w1", "w2"},
			want:  []string{"w1", "w2", "r1", "r2"},
		},
		{
			name:     "read-only excludes write",
			readOnly: true,
			read:     []string{"r1", "r2"},
			write:    []string{"w1"},
			want:     []string{"r1", "r2"},
		},
		{
			name:    "allowlist keeps only listed",
			allowed: map[string]struct{}{"r1": {}, "w1": {}},
			read:    []string{"r1", "r2"},
			write:   []string{"w1", "w2"},
			want:    []string{"w1", "r1"},
		},
		{
			name:     "allowlist intersected with read-only drops write entries",
			readOnly: true,
			allowed:  map[string]struct{}{"r1": {}, "w1": {}},
			read:     []string{"r1", "r2"},
			write:    []string{"w1", "w2"},
			want:     []string{"r1"},
		},
		{
			name:    "allowlist with only unknown names returns empty",
			allowed: map[string]struct{}{"unknown": {}},
			read:    []string{"r1"},
			write:   []string{"w1"},
			want:    []string{},
		},
		{
			name:    "empty allowlist map passes through",
			allowed: map[string]struct{}{},
			read:    []string{"r1"},
			write:   []string{"w1"},
			want:    []string{"w1", "r1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origRO, origAllow := flag.ReadOnly, flag.AllowedTools
			t.Cleanup(func() {
				flag.ReadOnly, flag.AllowedTools = origRO, origAllow
			})
			flag.ReadOnly = tt.readOnly
			flag.AllowedTools = tt.allowed

			tr := New("scope1")
			for _, n := range tt.read {
				tr.RegisterRead(makeTool(n))
			}
			for _, n := range tt.write {
				tr.RegisterWrite(makeTool(n))
			}

			got := names(tr.Tools())
			if !slices.Equal(got, tt.want) {
				t.Errorf("Tools() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolsScopeFiltering(t *testing.T) {
	tests := []struct {
		name          string
		toolScope     string
		readOnly      bool
		allowedScopes map[string]struct{}
		allowedTools  map[string]struct{}
		read          []string
		write         []string
		want          []string
	}{
		{
			name:      "no scope or tool filters returns all",
			toolScope: "repository",
			read:      []string{"r1"},
			write:     []string{"w1"},
			want:      []string{"w1", "r1"},
		},
		{
			name:          "matching scope keeps everything",
			toolScope:     "repository",
			allowedScopes: map[string]struct{}{"repository": {}},
			read:          []string{"r1"},
			write:         []string{"w1"},
			want:          []string{"w1", "r1"},
		},
		{
			name:          "non-matching scope drops everything not in allowed tools",
			toolScope:     "repository",
			allowedScopes: map[string]struct{}{"file": {}},
			read:          []string{"r1"},
			write:         []string{"w1"},
			want:          []string{},
		},
		{
			name:         "tools-only allowlist behaves as before scopes existed",
			toolScope:    "repository",
			allowedTools: map[string]struct{}{"r1": {}},
			read:         []string{"r1", "r2"},
			write:        []string{"w1"},
			want:         []string{"r1"},
		},
		{
			name:          "scope and tools allowlists are unioned",
			toolScope:     "repository",
			allowedScopes: map[string]struct{}{"file": {}},
			allowedTools:  map[string]struct{}{"r1": {}},
			read:          []string{"r1", "r2"},
			write:         []string{"w1"},
			want:          []string{"r1"},
		},
		{
			name:          "matching scope combined with read-only drops write entries",
			toolScope:     "repository",
			readOnly:      true,
			allowedScopes: map[string]struct{}{"repository": {}},
			read:          []string{"r1"},
			write:         []string{"w1"},
			want:          []string{"r1"},
		},
		{
			name:          "unknown scope name matches nothing",
			toolScope:     "repository",
			allowedScopes: map[string]struct{}{"unknown_scope": {}},
			read:          []string{"r1"},
			write:         []string{"w1"},
			want:          []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origRO, origAllowScopes, origAllowTools := flag.ReadOnly, flag.AllowedScopes, flag.AllowedTools
			t.Cleanup(func() {
				flag.ReadOnly, flag.AllowedScopes, flag.AllowedTools = origRO, origAllowScopes, origAllowTools
			})
			flag.ReadOnly = tt.readOnly
			flag.AllowedScopes = tt.allowedScopes
			flag.AllowedTools = tt.allowedTools

			tr := New(tt.toolScope)
			for _, n := range tt.read {
				tr.RegisterRead(makeTool(n))
			}
			for _, n := range tt.write {
				tr.RegisterWrite(makeTool(n))
			}

			got := names(tr.Tools())
			if !slices.Equal(got, tt.want) {
				t.Errorf("Tools() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScope(t *testing.T) {
	tr := New("repository")
	if got := tr.Scope(); got != "repository" {
		t.Errorf("Scope() = %q, want %q", got, "repository")
	}
}

func TestWarnUnmatchedAllowedScopes(t *testing.T) {
	origAllowScopes := flag.AllowedScopes
	t.Cleanup(func() {
		flag.AllowedScopes = origAllowScopes
	})

	repoTool := New("repository")
	fileTool := New("file")

	t.Run("empty allowlist is a no-op", func(t *testing.T) {
		flag.AllowedScopes = nil
		WarnUnmatchedAllowedScopes(repoTool, fileTool)
	})

	t.Run("known scopes produce no warning", func(t *testing.T) {
		flag.AllowedScopes = map[string]struct{}{"repository": {}, "file": {}}
		WarnUnmatchedAllowedScopes(repoTool, fileTool)
	})

	t.Run("unknown scope is tolerated", func(t *testing.T) {
		flag.AllowedScopes = map[string]struct{}{"not_a_real_scope": {}}
		WarnUnmatchedAllowedScopes(repoTool, fileTool)
	})
}
