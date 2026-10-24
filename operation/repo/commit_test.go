package repo

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCommitTools(t *testing.T) {
	const base = "/api/v1/repos/octo/demo"
	repoArgs := func(extra map[string]any) map[string]any {
		args := map[string]any{"owner": "octo", "repo": "demo"}
		maps.Copy(args, extra)
		return args
	}

	for _, tc := range []struct {
		name       string
		fn         func(context.Context, map[string]any) (*mcp.CallToolResult, error)
		args       map[string]any
		routes     map[string]string // "METHOD path" -> response body; any other request 404s
		wantBody   map[string]any
		wantResult []string
		wantAbsent []string
	}{
		{
			name:       "get_commit without diff",
			fn:         GetCommitFn,
			args:       repoArgs(map[string]any{"sha": "abc"}),
			routes:     map[string]string{"GET " + base + "/git/commits/abc": `{"sha":"abc"}`},
			wantResult: []string{`"sha":"abc"`},
			wantAbsent: []string{`"diff"`},
		},
		{
			name: "get_commit with diff",
			fn:   GetCommitFn,
			args: repoArgs(map[string]any{"sha": "abc", "include_diff": true}),
			routes: map[string]string{
				"GET " + base + "/git/commits/abc":      `{"sha":"abc"}`,
				"GET " + base + "/git/commits/abc.diff": "diff --git a/x.go b/x.go",
			},
			wantResult: []string{`"sha":"abc"`, `"diff":"diff --git a/x.go b/x.go"`},
		},
		{
			name: "compare_commits",
			fn:   CompareCommitsFn,
			args: repoArgs(map[string]any{"base": "main", "head": "feature"}),
			routes: map[string]string{"GET " + base + "/compare/main...feature": `{"total_commits":2,"commits":[
				{"sha":"a","files":[{"filename":"x.go"},{"filename":"y.go"}]},
				{"sha":"b","files":[{"filename":"x.go"}]}]}`},
			wantResult: []string{`"total_commits":2`, `"files":["x.go","y.go"]`, `"sha":"b"`},
		},
		{
			name: "get_commit_status",
			fn:   GetCommitStatusFn,
			args: repoArgs(map[string]any{"ref": "main"}),
			routes: map[string]string{"GET " + base + "/commits/main/status": `{"state":"success","sha":"abc","total_count":1,
				"statuses":[{"context":"ci/build","status":"success","creator":{"login":"bot"}}],"repository":{"id":1}}`},
			wantResult: []string{`"state":"success"`, `"context":"ci/build"`, `"creator":"bot"`},
			wantAbsent: []string{`"repository"`},
		},
		{
			name: "create_commit_status",
			fn:   CreateCommitStatusFn,
			args: repoArgs(map[string]any{"sha": "abc", "state": "failure", "context": "ci/build", "target_url": "https://ci/1"}),
			routes: map[string]string{
				"POST " + base + "/statuses/abc": `{"id":1,"status":"failure","context":"ci/build","target_url":"https://ci/1"}`,
			},
			wantBody:   map[string]any{"state": "failure", "context": "ci/build", "target_url": "https://ci/1"},
			wantResult: []string{`"state":"failure"`, `"context":"ci/build"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mu      sync.Mutex
				gotBody map[string]any
			)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/version" {
					_, _ = w.Write([]byte(`{"version":"1.24.0"}`))
					return
				}
				resp, ok := tc.routes[r.Method+" "+r.URL.Path]
				if !ok {
					http.NotFound(w, r)
					return
				}
				if r.Method == http.MethodPost {
					mu.Lock()
					_ = json.NewDecoder(r.Body).Decode(&gotBody)
					mu.Unlock()
				}
				_, _ = w.Write([]byte(resp))
			}))
			defer server.Close()

			origHost, origToken := flag.Host, flag.Token
			flag.Host, flag.Token = server.URL, ""
			defer func() { flag.Host, flag.Token = origHost, origToken }()

			result, err := tc.fn(context.Background(), tc.args)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if result.IsError {
				t.Fatalf("unexpected error result: %s", text)
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
			mu.Lock()
			defer mu.Unlock()
			for k, want := range tc.wantBody {
				if gotBody[k] != want {
					t.Errorf("request body %s = %v, want %v", k, gotBody[k], want)
				}
			}
		})
	}
}
