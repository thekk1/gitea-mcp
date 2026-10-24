package milestone

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

func TestMilestoneRequestsAndSlimResults(t *testing.T) {
	const (
		due           = "2026-05-18T23:59:59Z"
		milestone     = `{"id":42,"title":"v1","description":"d","state":"open","open_issues":1,"closed_issues":2,"due_on":"` + due + `","url":"u"}`
		slimMilestone = `{"closed_issues":2,"description":"d","due_on":"` + due + `","id":42,"open_issues":1,"state":"open","title":"v1"}`
	)
	var gotRequest, response string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotRequest = strings.TrimSpace(r.Method + " " + r.URL.RequestURI() + " " + string(body))
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL

	tested := map[string]bool{}
	for _, tt := range []struct {
		handler                           tool.Handler
		args                              map[string]any
		response, wantRequest, wantResult string
	}{
		{
			handler:     milestoneReadFn,
			args:        map[string]any{"method": "get", "owner": "o", "repo": "r", "id": 42.0},
			response:    milestone,
			wantRequest: "GET /api/v1/repos/o/r/milestones/42",
			wantResult:  slimMilestone,
		},
		{
			handler:     milestoneReadFn,
			args:        map[string]any{"method": "list", "owner": "o", "repo": "r", "name": "v1"},
			response:    "[" + milestone + "]",
			wantRequest: "GET /api/v1/repos/o/r/milestones?limit=30&name=v1&page=1&state=all",
			wantResult:  "[" + slimMilestone + "]",
		},
		{
			handler:     milestoneWriteFn,
			args:        map[string]any{"method": "create", "owner": "o", "repo": "r", "title": "v1", "description": "d", "due_on": due},
			response:    milestone,
			wantRequest: `POST /api/v1/repos/o/r/milestones {"title":"v1","description":"d","state":"","due_on":"` + due + `"}`,
			wantResult:  slimMilestone,
		},
		{
			handler:     milestoneWriteFn,
			args:        map[string]any{"method": "edit", "owner": "o", "repo": "r", "id": 42.0, "state": "closed", "due_on": due},
			response:    milestone,
			wantRequest: `PATCH /api/v1/repos/o/r/milestones/42 {"title":"","description":null,"state":"closed","due_on":"` + due + `"}`,
			wantResult:  slimMilestone,
		},
		{
			handler:     milestoneWriteFn,
			args:        map[string]any{"method": "update", "owner": "o", "repo": "r", "id": 42.0, "title": "v2", "description": ""},
			response:    milestone,
			wantRequest: `PATCH /api/v1/repos/o/r/milestones/42 {"title":"v2","description":"","state":null,"due_on":null}`,
			wantResult:  slimMilestone,
		},
		{
			handler:     milestoneWriteFn,
			args:        map[string]any{"method": "delete", "owner": "o", "repo": "r", "id": 42.0},
			wantRequest: "DELETE /api/v1/repos/o/r/milestones/42",
			wantResult:  `"Milestone deleted successfully"`,
		},
		{
			handler:    milestoneWriteFn,
			args:       map[string]any{"method": "create", "owner": "o", "repo": "r"},
			wantResult: "title is required",
		},
	} {
		tested[tt.args["method"].(string)] = true
		gotRequest, response = "", tt.response
		result, err := tt.handler(t.Context(), tt.args)
		if err != nil {
			t.Fatal(err)
		}
		if got := result.Content[0].(*mcp.TextContent).Text; gotRequest != tt.wantRequest || got != tt.wantResult {
			t.Errorf("%v: request %q, result %s, want %q, %s", tt.args, gotRequest, got, tt.wantRequest, tt.wantResult)
		}
	}

	for _, definition := range []*mcp.Tool{MilestoneReadTool, MilestoneWriteTool} {
		for _, method := range definition.InputSchema.(map[string]any)["properties"].(map[string]any)["method"].(map[string]any)["enum"].([]string) {
			if !tested[method] {
				t.Errorf("%s method %q has no case", definition.Name, method)
			}
		}
	}
}
