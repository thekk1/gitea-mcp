package repo

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCommitTools(t *testing.T) {
	const base = "/api/v1/repos/octo/demo"

	for _, tc := range []struct {
		name       string
		fn         tool.Handler
		args       map[string]any
		routes     map[string]string
		wantQuery  map[string]string
		wantBody   map[string]any
		wantResult []string
		wantAbsent []string
	}{
		{
			name:       "list commits",
			fn:         CommitTool.ReadTools()[0].Handler,
			args:       map[string]any{"method": "list", "sha": "main", "path": "x.go", "page": float64(2), "per_page": float64(5)},
			routes:     map[string]string{"GET " + base + "/commits": `[{"sha":"abc"}]`},
			wantQuery:  map[string]string{"sha": "main", "path": "x.go", "page": "2", "limit": "5"},
			wantResult: []string{`"sha":"abc"`},
		},
		{
			name:       "get_commit without diff",
			fn:         CommitTool.ReadTools()[0].Handler,
			args:       map[string]any{"method": "get", "sha": "abc"},
			routes:     map[string]string{"GET " + base + "/git/commits/abc": `{"sha":"abc"}`},
			wantResult: []string{`"sha":"abc"`},
			wantAbsent: []string{`"diff"`},
		},
		{
			name: "get_commit with diff",
			fn:   CommitTool.ReadTools()[0].Handler,
			args: map[string]any{"method": "get", "sha": "abc", "include_diff": true},
			routes: map[string]string{
				"GET " + base + "/git/commits/abc":      `{"sha":"abc"}`,
				"GET " + base + "/git/commits/abc.diff": "diff --git a/x.go b/x.go",
			},
			wantResult: []string{`"sha":"abc"`, `"diff":"diff --git a/x.go b/x.go"`},
		},
		{
			name: "compare_commits",
			fn:   CommitTool.ReadTools()[0].Handler,
			args: map[string]any{"method": "compare", "base": "main", "head": "feature"},
			routes: map[string]string{"GET " + base + "/compare/main...feature": `{"total_commits":2,"commits":[
				{"sha":"a","files":[{"filename":"x.go"},{"filename":"y.go"}]},
				{"sha":"b","files":[{"filename":"x.go"}]}]}`},
			wantResult: []string{`"total_commits":2`, `"files":["x.go","y.go"]`, `"sha":"b"`},
		},
		{
			name: "get_commit_status",
			fn:   CommitTool.ReadTools()[0].Handler,
			args: map[string]any{"method": "get_status", "ref": "main"},
			routes: map[string]string{"GET " + base + "/commits/main/status": `{"state":"success","sha":"abc","total_count":1,
				"statuses":[{"context":"ci/build","status":"success","creator":{"login":"bot"}}],"repository":{"id":1}}`},
			wantResult: []string{`"state":"success"`, `"context":"ci/build"`, `"creator":"bot"`},
			wantAbsent: []string{`"repository"`},
		},
		{
			name: "create_commit_status",
			fn:   CommitTool.WriteTools()[0].Handler,
			args: map[string]any{"method": "create_status", "sha": "abc", "state": "failure", "context": "ci/build", "target_url": "https://ci/1"},
			routes: map[string]string{
				"POST " + base + "/statuses/abc": `{"id":1,"status":"failure","context":"ci/build","target_url":"https://ci/1"}`,
			},
			wantBody:   map[string]any{"state": "failure", "context": "ci/build", "target_url": "https://ci/1"},
			wantResult: []string{`"state":"failure"`, `"context":"ci/build"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serveStub(t, func(w http.ResponseWriter, r *http.Request) {
				for key, want := range tc.wantQuery {
					if got := r.URL.Query().Get(key); got != want {
						t.Errorf("query %s = %q, want %q", key, got, want)
					}
				}
				resp, ok := tc.routes[r.Method+" "+r.URL.Path]
				if !ok {
					http.NotFound(w, r)
					return
				}
				if tc.wantBody != nil {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					for key, want := range tc.wantBody {
						if body[key] != want {
							t.Errorf("request body %s = %v, want %v", key, body[key], want)
						}
					}
				}
				_, _ = w.Write([]byte(resp))
			})

			tc.args["owner"], tc.args["repo"] = "octo", "demo"
			result, err := tc.fn(t.Context(), tc.args)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if result.IsError {
				t.Fatalf("unexpected error: %s", text)
			}
			for _, want := range tc.wantResult {
				if !strings.Contains(text, want) {
					t.Errorf("result = %s, want it to contain %s", text, want)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(text, absent) {
					t.Errorf("result = %s, want it to omit %s", text, absent)
				}
			}
		})
	}
}
