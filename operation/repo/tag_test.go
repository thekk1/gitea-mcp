package repo

import (
	"net/http"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/tool"
)

func TestTagMethods(t *testing.T) {
	requests := make(chan string, 1)
	serveStub(t, func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.RequestURI()
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/tags"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"name":"v1"}`))
		}
	})

	for _, tc := range []struct {
		method      string
		handler     tool.Handler
		args        map[string]any
		wantRequest string
	}{
		{"list", TagTool.ReadTools()[0].Handler, map[string]any{"page": float64(2), "per_page": float64(5)}, "GET /api/v1/repos/octo/demo/tags?limit=5&page=2"},
		{"get", TagTool.ReadTools()[0].Handler, map[string]any{"tag_name": "v1"}, "GET /api/v1/repos/octo/demo/tags/v1"},
		{"create", TagTool.WriteTools()[0].Handler, map[string]any{"tag_name": "v1"}, "POST /api/v1/repos/octo/demo/tags"},
		{"delete", TagTool.WriteTools()[0].Handler, map[string]any{"tag_name": "v1"}, "DELETE /api/v1/repos/octo/demo/tags/v1"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			tc.args["method"], tc.args["owner"], tc.args["repo"] = tc.method, "octo", "demo"
			result, err := tc.handler(t.Context(), tc.args)
			if err != nil || result.IsError {
				t.Fatalf("result = %v, error = %v", result, err)
			}
			if request := <-requests; request != tc.wantRequest {
				t.Errorf("request = %q, want %q", request, tc.wantRequest)
			}
		})
	}
}
