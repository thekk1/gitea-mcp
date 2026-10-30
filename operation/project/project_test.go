package project

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/flag"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProjectRequestsAndSlimResults(t *testing.T) {
	var gotRequest, response string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotRequest = strings.TrimSpace(r.Method + " " + r.URL.RequestURI() + " " + string(body))
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL

	for _, tt := range []struct {
		handler                           tool.Handler
		args                              map[string]any
		response, wantRequest, wantResult string
	}{
		{
			handler:     Tool.ReadTools()[0].Handler,
			args:        map[string]any{"method": "list", "owner": "o", "repo": "r"},
			response:    `[{"id":1,"title":"Roadmap","state":"open","card_type":"text_only","num_open_issues":2,"html_url":"u"}]`,
			wantRequest: "GET /api/v1/repos/o/r/projects?limit=30&page=1&state=all",
			wantResult:  `[{"id":1,"title":"Roadmap","state":"open","num_open_issues":2,"num_closed_issues":0,"html_url":"u"}]`,
		},
		{
			handler:     Tool.ReadTools()[0].Handler,
			args:        map[string]any{"method": "list_column_issues", "project_id": 3.0, "column_id": "4"},
			response:    `[{"id":9,"number":5,"title":"Bug","body":"long","state":"open","html_url":"u"}]`,
			wantRequest: "GET /api/v1/user/projects/3/columns/4/issues?limit=30&page=1",
			wantResult:  `[{"id":9,"number":5,"title":"Bug","state":"open","html_url":"u"}]`,
		},
		{
			handler:     Tool.WriteTools()[0].Handler,
			args:        map[string]any{"method": "update", "org": "acme", "project_id": 3.0, "state": "closed"},
			response:    `{"id":3,"title":"Roadmap","state":"closed","html_url":"u"}`,
			wantRequest: `PATCH /api/v1/orgs/acme/projects/3 {"state":"closed"}`,
			wantResult:  `{"id":3,"title":"Roadmap","state":"closed","num_open_issues":0,"num_closed_issues":0,"html_url":"u"}`,
		},
		{
			handler:     Tool.WriteTools()[0].Handler,
			args:        map[string]any{"method": "add_issue", "owner": "o", "repo": "r", "project_id": 3.0, "column_id": 4.0, "issue_id": 5.0},
			wantRequest: "POST /api/v1/repos/o/r/projects/3/columns/4/issues/5",
			wantResult:  `"ok"`,
		},
		{
			handler:     Tool.WriteTools()[0].Handler,
			args:        map[string]any{"method": "update_column", "project_id": 3.0, "column_id": 4.0, "color": "#FF0000", "sorting": 2.0},
			response:    `{"id":4,"title":"Doing","sorting":2,"color":"#FF0000","project_id":3}`,
			wantRequest: `PATCH /api/v1/user/projects/3/columns/4 {"color":"#FF0000","sorting":2}`,
			wantResult:  `{"id":4,"title":"Doing","sorting":2,"color":"#FF0000"}`,
		},
		{
			handler:     Tool.WriteTools()[0].Handler,
			args:        map[string]any{"method": "move_columns", "project_id": 3.0, "column_ids": []any{5.0, "4"}},
			wantRequest: `POST /api/v1/user/projects/3/columns/move {"column_ids":[5,4]}`,
			wantResult:  `"ok"`,
		},
		{
			handler:     Tool.WriteTools()[0].Handler,
			args:        map[string]any{"method": "set_default_column", "project_id": 3.0, "column_id": 4.0},
			wantRequest: "POST /api/v1/user/projects/3/columns/4/default",
			wantResult:  `"ok"`,
		},
		{
			handler:     Tool.WriteTools()[0].Handler,
			args:        map[string]any{"method": "move_issue", "project_id": 3.0, "column_id": 4.0, "issue_id": 5.0, "sorting": 1.0},
			wantRequest: `POST /api/v1/user/projects/3/issues/5/move {"column_id":4,"sorting":1}`,
			wantResult:  `"ok"`,
		},
		{
			handler:     Tool.WriteTools()[0].Handler,
			args:        map[string]any{"method": "move_issue", "project_id": 3.0, "column_id": 4.0, "issue_id": 5.0, "sorting": nil},
			wantRequest: `POST /api/v1/user/projects/3/issues/5/move {"column_id":4}`,
			wantResult:  `"ok"`,
		},
		{
			handler:    Tool.WriteTools()[0].Handler,
			args:       map[string]any{"method": "move_columns", "project_id": 3.0},
			wantResult: "column_ids (array of IDs) is required",
		},
		{
			handler:    Tool.WriteTools()[0].Handler,
			args:       map[string]any{"method": "update_column", "project_id": 3.0, "column_id": 4.0, "sorting": "left"},
			wantResult: `sorting must be a valid integer (got "left")`,
		},
		{
			handler:    Tool.ReadTools()[0].Handler,
			args:       map[string]any{"method": "list", "owner": "o"},
			wantResult: "owner and repo must be set together",
		},
		{
			handler:    Tool.WriteTools()[0].Handler,
			args:       map[string]any{"method": "delete_column", "project_id": 3.0},
			wantResult: "column_id is required",
		},
	} {
		gotRequest, response = "", tt.response
		result, err := tt.handler(t.Context(), tt.args)
		if err != nil {
			t.Fatal(err)
		}
		if got := result.Content[0].(*mcp.TextContent).Text; gotRequest != tt.wantRequest || got != tt.wantResult {
			t.Errorf("%v: request %q, result %s, want %q, %s", tt.args, gotRequest, got, tt.wantRequest, tt.wantResult)
		}
	}
}
