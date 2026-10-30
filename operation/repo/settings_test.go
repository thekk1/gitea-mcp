package repo

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRepoSettingsMethods(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []string
	)
	serveStub(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, strings.TrimSpace(r.Method+" "+r.URL.RequestURI()+" "+string(body)))
		mu.Unlock()
		switch {
		case r.Method == http.MethodPatch && string(body) == `{"description":"reject"}`:
			http.Error(w, `{"message":"rejected"}`, http.StatusBadRequest)
		case r.Method == http.MethodDelete || strings.HasSuffix(r.URL.Path, "/topics"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && (r.URL.Path == "/api/v1/user/repos" || strings.HasPrefix(r.URL.Path, "/api/v1/orgs/")):
			_, _ = w.Write([]byte(`[{"full_name":"octo/demo"}]`))
		case strings.Contains(r.URL.Path, "/branch_protections"):
			_, _ = w.Write([]byte(`{"rule_name":"release/*","enable_push":false,"required_approvals":0,"status_check_contexts":[],"protected_file_patterns":"","dismiss_stale_approvals":null}`))
		default:
			_, _ = w.Write([]byte(`{"full_name":"octo/demo","topics":["old"]}`))
		}
	})

	for _, tc := range []struct {
		name         string
		toolName     string
		args         map[string]any
		wantRequests []string
		wantContains string
		wantError    bool
	}{
		{
			"create", RepoWriteToolName,
			map[string]any{"method": "create", "name": "demo"},
			[]string{`POST /api/v1/user/repos {"name":"demo"}`},
			`"full_name":"octo/demo"`,
			false,
		},
		{
			"create_org", RepoWriteToolName,
			map[string]any{"method": "create", "name": "demo", "org": "octo", "description": "example", "private": true, "auto_init": true, "template": true, "gitignores": "Go", "license": "MIT", "readme": "Default", "default_branch": "main", "issue_labels": "Default", "trust_model": "committer", "object_format_name": "sha256"},
			[]string{`POST /api/v1/orgs/octo/repos {"auto_init":true,"default_branch":"main","description":"example","gitignores":"Go","issue_labels":"Default","license":"MIT","name":"demo","object_format_name":"sha256","private":true,"readme":"Default","template":true,"trust_model":"committer"}`},
			`"full_name":"octo/demo"`,
			false,
		},
		{
			"fork", RepoWriteToolName,
			map[string]any{"method": "fork", "owner": "octo", "repo": "demo", "org": "target", "name": "forked"},
			[]string{`POST /api/v1/repos/octo/demo/forks {"name":"forked","organization":"target"}`},
			`"full_name":"octo/demo"`,
			false,
		},
		{
			"list_my_repos", RepoReadToolName,
			map[string]any{"method": "list_my_repos", "page": float64(2), "per_page": float64(5)},
			[]string{`GET /api/v1/user/repos?limit=5&page=2`},
			`"full_name":"octo/demo"`,
			false,
		},
		{
			"list_org_repos", RepoReadToolName,
			map[string]any{"method": "list_org_repos", "org": "octo", "owner": "other"},
			[]string{`GET /api/v1/orgs/octo/repos?limit=30&page=1`},
			`"full_name":"octo/demo"`,
			false,
		},
		{
			"update", RepoWriteToolName,
			map[string]any{"method": "update", "owner": "octo", "repo": "demo", "topics": []any{"go"}, "options": map[string]any{"archived": true}},
			[]string{`PATCH /api/v1/repos/octo/demo {"archived":true}`, `PUT /api/v1/repos/octo/demo/topics {"topics":["go"]}`},
			`"topics":["go"]`,
			false,
		},
		{
			"topics_only", RepoWriteToolName,
			map[string]any{"method": "update", "owner": "octo", "repo": "demo", "topics": []any{"go"}},
			[]string{`PUT /api/v1/repos/octo/demo/topics {"topics":["go"]}`},
			"topics updated",
			false,
		},
		{
			"update_patch_failure", RepoWriteToolName,
			map[string]any{"method": "update", "owner": "octo", "repo": "demo", "topics": []any{"go"}, "options": map[string]any{"description": "reject"}},
			[]string{`PATCH /api/v1/repos/octo/demo {"description":"reject"}`},
			"update repo err:",
			true,
		},
		{
			"update_rejects_non_string_topics", RepoWriteToolName,
			map[string]any{"method": "update", "owner": "octo", "repo": "demo", "topics": []any{1}},
			nil,
			"topics must be strings",
			true,
		},
		{
			"list_org_repos_requires_org", RepoReadToolName,
			map[string]any{"method": "list_org_repos", "owner": "octo"},
			nil,
			"org is required",
			true,
		},
		{
			"get_branch_protection", RepoReadToolName,
			map[string]any{"method": "get_branch_protection", "owner": "octo", "repo": "demo", "rule_name": "release/*"},
			[]string{`GET /api/v1/repos/octo/demo/branch_protections/release%2F%2A`},
			`{"enable_push":false,"required_approvals":0,"rule_name":"release/*"}`,
			false,
		},
		{
			"create_branch_protection", RepoWriteToolName,
			map[string]any{"method": "create_branch_protection", "owner": "octo", "repo": "demo", "rule_name": "main", "options": map[string]any{"enable_push": true}},
			[]string{`POST /api/v1/repos/octo/demo/branch_protections {"enable_push":true,"rule_name":"main"}`},
			`"rule_name":"release/*"`,
			false,
		},
		{
			"delete_branch_protection", RepoWriteToolName,
			map[string]any{"method": "delete_branch_protection", "owner": "octo", "repo": "demo", "rule_name": "main"},
			[]string{`DELETE /api/v1/repos/octo/demo/branch_protections/main`},
			"deleted",
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			requests = nil
			mu.Unlock()
			var result *mcp.CallToolResult
			var err error
			for _, registered := range append(Tool.ReadTools(), Tool.WriteTools()...) {
				if registered.Tool.Name == tc.toolName {
					result, err = registered.Handler(context.Background(), tc.args)
					break
				}
			}
			if err != nil || result == nil || result.IsError != tc.wantError {
				t.Fatalf("result = %v, error = %v", result, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(requests, tc.wantRequests) {
				t.Errorf("requests = %q, want %q", requests, tc.wantRequests)
			}
			if text := resultText(t, result); !strings.Contains(text, tc.wantContains) {
				t.Errorf("result = %s, want it to contain %s", text, tc.wantContains)
			}
		})
	}
}
