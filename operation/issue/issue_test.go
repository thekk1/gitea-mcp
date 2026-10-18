package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Test_listRepoIssuesFn_filters(t *testing.T) {
	const (
		owner = "octo"
		repo  = "demo"
	)

	var (
		mu       sync.Mutex
		gotQuery string
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case r.URL.Path == fmt.Sprintf("/api/v1/repos/%s/%s", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"private":false}`))
		case r.URL.Path == fmt.Sprintf("/api/v1/repos/%s/%s/issues", owner, repo):
			mu.Lock()
			gotQuery = r.URL.RawQuery
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	origHost := flag.Host
	origToken := flag.Token
	origVersion := flag.Version
	flag.Host = server.URL
	flag.Token = ""
	flag.Version = "test"
	defer func() {
		flag.Host = origHost
		flag.Token = origToken
		flag.Version = origVersion
	}()

	args := map[string]any{
		"owner":        owner,
		"repo":         repo,
		"type":         "issues",
		"labels":       []any{"bug", "enhancement"},
		"milestones":   []any{"v1.0", "2"},
		"since":        "2026-01-01T00:00:00Z",
		"created_by":   "alice",
		"assigned_by":  "bob",
		"mentioned_by": "carol",
	}

	_, err := listRepoIssuesFn(context.Background(), args)
	if err != nil {
		t.Fatalf("listRepoIssuesFn() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if !strings.Contains(gotQuery, "labels=bug%2Cenhancement") {
		t.Fatalf("expected labels query param, got %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "since=2026-01-01") {
		t.Fatalf("expected since query param, got %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "milestones=v1.0%2C2") {
		t.Fatalf("expected milestones query param, got %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "type=issues") {
		t.Fatalf("expected type query param, got %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "created_by=alice") || !strings.Contains(gotQuery, "assigned_by=bob") || !strings.Contains(gotQuery, "mentioned_by=carol") {
		t.Fatalf("expected user filter query params, got %s", gotQuery)
	}
}

func Test_listRepoIssuesFn_includesMilestone(t *testing.T) {
	const (
		owner = "octo"
		repo  = "demo"
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"private":false}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s/issues", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"number": 1, "title": "with milestone", "state": "closed", "milestone": {"id": 5, "title": "v1.0"}},
				{"number": 2, "title": "without milestone", "state": "open"}
			]`))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	origHost, origToken, origVersion := flag.Host, flag.Token, flag.Version
	flag.Host, flag.Token, flag.Version = server.URL, "", "test"
	defer func() { flag.Host, flag.Token, flag.Version = origHost, origToken, origVersion }()

	args := map[string]any{
		"owner": owner, "repo": repo,
	}
	res, err := listRepoIssuesFn(context.Background(), args)
	if err != nil {
		t.Fatalf("listRepoIssuesFn() error = %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %v", res.Content)
	}
	body := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(body, `"milestone"`) || !strings.Contains(body, `"v1.0"`) {
		t.Fatalf("expected milestone in list output, got: %s", body)
	}
}

func Test_createIssueFn_labels(t *testing.T) {
	const (
		owner = "octo"
		repo  = "demo"
	)

	var (
		mu      sync.Mutex
		gotBody map[string]any
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"private":false}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s/issues", owner, repo):
			mu.Lock()
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotBody = body
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number":1,"title":"test","state":"open"}`))
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	origHost := flag.Host
	origToken := flag.Token
	origVersion := flag.Version
	flag.Host = server.URL
	flag.Token = ""
	flag.Version = "test"
	defer func() {
		flag.Host = origHost
		flag.Token = origToken
		flag.Version = origVersion
	}()

	args := map[string]any{
		"owner":    owner,
		"repo":     repo,
		"title":    "test issue",
		"labels":   []any{float64(10), float64(20)},
		"deadline": "2026-06-01T00:00:00Z",
	}

	_, err := createIssueFn(context.Background(), args)
	if err != nil {
		t.Fatalf("createIssueFn() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	labels, ok := gotBody["labels"].([]any)
	if !ok || len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %v", gotBody["labels"])
	}
	if labels[0] != float64(10) || labels[1] != float64(20) {
		t.Fatalf("expected labels [10,20], got %v", labels)
	}
	if gotBody["due_date"] == nil {
		t.Fatalf("expected due_date to be set")
	}
}

func Test_getIssueByIndexFn_includesAttachments(t *testing.T) {
	const (
		owner = "octo"
		repo  = "demo"
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.12.0"}`))
		case fmt.Sprintf("/api/v1/repos/%s/%s/issues/42", owner, repo):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"number": 42,
				"title": "bug with screenshot",
				"body": "see attached",
				"state": "open",
				"assets": [
					{"id": 1, "name": "shot.png", "size": 1024, "browser_download_url": "https://example/shot.png"}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	origHost, origToken, origVersion := flag.Host, flag.Token, flag.Version
	flag.Host, flag.Token, flag.Version = server.URL, "", "test"
	defer func() { flag.Host, flag.Token, flag.Version = origHost, origToken, origVersion }()

	args := map[string]any{
		"owner": owner, "repo": repo, "issue_number": float64(42),
	}
	res, err := getIssueByIndexFn(context.Background(), args)
	if err != nil {
		t.Fatalf("getIssueByIndexFn() error = %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %v", res.Content)
	}
	body := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(body, `[shot.png](https://example/shot.png)`) {
		t.Fatalf("expected attachment markdown inlined in body, got: %s", body)
	}
	if strings.Contains(body, `"attachments"`) {
		t.Fatalf("attachments should be inlined into body, not a separate field: %s", body)
	}
}

func Test_issueReadFn_commentsIncludeAttachments(t *testing.T) {
	const commentWithAsset = `{"id": 1, "body": "see this", "assets": [
		{"id": 9, "name": "log.txt", "size": 200, "browser_download_url": "https://example/log.txt"}
	]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/octo/demo/issues/7/comments":
			_, _ = w.Write([]byte(`[` + commentWithAsset + `, {"id": 2, "body": "no attachment", "assets": []}]`))
		case "/api/v1/repos/octo/demo/issues/comments/1":
			_, _ = w.Write([]byte(commentWithAsset))
		case "/api/v1/repos/octo/demo/issues/comments/5":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL

	for _, args := range []map[string]any{
		{"method": "get_comments", "owner": "octo", "repo": "demo", "issue_number": float64(7)},
		{"method": "get_comment", "owner": "octo", "repo": "demo", "comment_id": float64(1)},
	} {
		t.Run(args["method"].(string), func(t *testing.T) {
			res, err := issueReadFn(context.Background(), args)
			if err != nil || res.IsError {
				t.Fatalf("issueReadFn() result = %v, error = %v", res, err)
			}
			body := res.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(body, `[log.txt](https://example/log.txt)`) || strings.Contains(body, `"attachments"`) {
				t.Fatalf("expected attachment markdown inlined in body, got: %s", body)
			}
		})
	}
	res, err := issueReadFn(context.Background(), map[string]any{"method": "get_comment", "owner": "octo", "repo": "demo", "comment_id": float64(5)})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "not a discussion comment") {
		t.Fatalf("expected non-discussion comment error, got %v, %v", res, err)
	}
}

func Test_issueDependencyMethods(t *testing.T) {
	const (
		blockerRef = `{"html_url":"","number":1,"repository":"acme/infra","state":"open","title":"CI"}`
		issueRef   = `{"html_url":"","number":2,"repository":"octo/demo","state":"open","title":"Ship"}`
	)
	var gotRequest, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotRequest, gotBody = r.Method+" "+r.URL.RequestURI(), string(body)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"number": 1, "title": "CI", "state": "open", "repository": {"full_name": "acme/infra"}}]`))
		} else {
			_, _ = w.Write([]byte(`{"number": 2, "title": "Ship", "state": "open", "repository": {"full_name": "octo/demo"}}`))
		}
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL

	tests := []struct {
		toolFn      func(context.Context, map[string]any) (*mcp.CallToolResult, error)
		args        map[string]any
		wantRequest string
		wantBody    string
		wantResult  string
	}{
		{
			toolFn:      issueReadFn,
			args:        map[string]any{"method": "get_blocked_by", "page": float64(2), "per_page": float64(10)},
			wantRequest: "GET /api/v1/repos/octo/demo/issues/2/dependencies?limit=10&page=2",
			wantResult:  "[" + blockerRef + "]",
		},
		{
			toolFn:      issueReadFn,
			args:        map[string]any{"method": "get_blocking"},
			wantRequest: "GET /api/v1/repos/octo/demo/issues/2/blocks?limit=30&page=1",
			wantResult:  "[" + blockerRef + "]",
		},
		{
			toolFn:      issueWriteFn,
			args:        map[string]any{"method": "add_dependency", "dependency_type": "blocked_by", "related_owner": "acme", "related_repo": "infra", "related_issue_number": float64(1)},
			wantRequest: "POST /api/v1/repos/octo/demo/issues/2/dependencies",
			wantBody:    `{"index":1,"owner":"acme","repo":"infra"}`,
			wantResult:  issueRef,
		},
		{
			toolFn:      issueWriteFn,
			args:        map[string]any{"method": "remove_dependency", "dependency_type": "blocking", "related_owner": "", "related_issue_number": float64(3)},
			wantRequest: "DELETE /api/v1/repos/octo/demo/issues/2/blocks",
			wantBody:    `{"index":3,"owner":"octo","repo":"demo"}`,
			wantResult:  issueRef,
		},
	}
	for _, tt := range tests {
		t.Run(tt.args["method"].(string), func(t *testing.T) {
			tt.args["owner"], tt.args["repo"], tt.args["issue_number"] = "octo", "demo", float64(2)
			res, err := tt.toolFn(context.Background(), tt.args)
			if err != nil || res.IsError {
				t.Fatalf("result = %v, error = %v", res, err)
			}
			if gotRequest != tt.wantRequest || gotBody != tt.wantBody {
				t.Fatalf("request = %q body %q, want %q body %q", gotRequest, gotBody, tt.wantRequest, tt.wantBody)
			}
			if got := res.Content[0].(*mcp.TextContent).Text; got != tt.wantResult {
				t.Fatalf("result = %s, want %s", got, tt.wantResult)
			}
		})
	}
}

func Test_issueReadFn_assigneeCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			_, _ = w.Write([]byte(`{"version":"1.24.0"}`))
		case "/api/v1/repos/octo/demo/assignees":
			_, _ = w.Write([]byte(`[{"login":"alice","full_name":"Alice","email":"alice@example.com"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL

	res, err := issueReadFn(context.Background(), map[string]any{"method": "get_assignee_candidates", "owner": "octo", "repo": "demo"})
	if err != nil || res.IsError {
		t.Fatalf("issueReadFn() result = %v, error = %v", res, err)
	}
	if body := res.Content[0].(*mcp.TextContent).Text; body != `[{"full_name":"Alice","login":"alice"}]` {
		t.Fatalf("unexpected result: %s", body)
	}
}
