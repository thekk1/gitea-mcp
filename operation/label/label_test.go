package label

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

func TestLabelRequestsAndSlimResults(t *testing.T) {
	const (
		label     = `{"id":7,"name":"bug","color":"ff0000","description":"a bug","exclusive":true,"url":"u"}`
		slimLabel = `{"color":"ff0000","description":"a bug","exclusive":true,"id":7,"name":"bug"}`
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
			handler:     labelReadFn,
			args:        map[string]any{"method": "list_repo_labels", "owner": "o", "repo": "r", "page": 2.0, "per_page": 5.0},
			response:    "[" + label + "]",
			wantRequest: "GET /api/v1/repos/o/r/labels?limit=5&page=2",
			wantResult:  "[" + slimLabel + "]",
		},
		{
			handler:     labelReadFn,
			args:        map[string]any{"method": "get_repo_label", "owner": "o", "repo": "r", "id": 7.0},
			response:    label,
			wantRequest: "GET /api/v1/repos/o/r/labels/7",
			wantResult:  slimLabel,
		},
		{
			handler:     labelReadFn,
			args:        map[string]any{"method": "list_org_labels", "org": "acme"},
			response:    "[" + label + "]",
			wantRequest: "GET /api/v1/orgs/acme/labels?limit=30&page=1",
			wantResult:  "[" + slimLabel + "]",
		},
		{
			handler:     labelWriteFn,
			args:        map[string]any{"method": "create_repo_label", "owner": "o", "repo": "r", "name": "bug", "color": "#ff0000", "description": "a bug", "exclusive": true, "is_archived": true},
			response:    label,
			wantRequest: `POST /api/v1/repos/o/r/labels {"name":"bug","color":"#ff0000","description":"a bug","exclusive":true,"is_archived":true}`,
			wantResult:  slimLabel,
		},
		{
			handler:     labelWriteFn,
			args:        map[string]any{"method": "edit_repo_label", "owner": "o", "repo": "r", "id": 7.0, "name": "defect", "description": "", "exclusive": false, "is_archived": false},
			response:    label,
			wantRequest: `PATCH /api/v1/repos/o/r/labels/7 {"name":"defect","color":null,"description":"","exclusive":false,"is_archived":false}`,
			wantResult:  slimLabel,
		},
		{
			handler:     labelWriteFn,
			args:        map[string]any{"method": "delete_repo_label", "owner": "o", "repo": "r", "id": 7.0},
			wantRequest: "DELETE /api/v1/repos/o/r/labels/7",
			wantResult:  `"Label deleted successfully"`,
		},
		{
			handler:     labelWriteFn,
			args:        map[string]any{"method": "create_org_label", "org": "acme", "name": "bug", "color": "#ff0000", "exclusive": true},
			response:    label,
			wantRequest: `POST /api/v1/orgs/acme/labels {"name":"bug","color":"#ff0000","description":"","exclusive":true}`,
			wantResult:  slimLabel,
		},
		{
			handler:     labelWriteFn,
			args:        map[string]any{"method": "edit_org_label", "org": "acme", "id": 7.0, "color": "#00ff00", "exclusive": false},
			response:    label,
			wantRequest: `PATCH /api/v1/orgs/acme/labels/7 {"name":null,"color":"#00ff00","description":null,"exclusive":false}`,
			wantResult:  slimLabel,
		},
		{
			handler:     labelWriteFn,
			args:        map[string]any{"method": "delete_org_label", "org": "acme", "id": 7.0},
			wantRequest: "DELETE /api/v1/orgs/acme/labels/7",
			wantResult:  `"Label deleted successfully"`,
		},
		{
			handler:    labelWriteFn,
			args:       map[string]any{"method": "create_org_label", "org": "acme", "name": "bug"},
			wantResult: "color is required",
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

	for _, definition := range []*mcp.Tool{LabelReadTool, LabelWriteTool} {
		for _, method := range definition.InputSchema.(map[string]any)["properties"].(map[string]any)["method"].(map[string]any)["enum"].([]string) {
			if !tested[method] {
				t.Errorf("%s method %q has no case", definition.Name, method)
			}
		}
	}
}
