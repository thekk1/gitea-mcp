package repo

import (
	"encoding/json"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/tool"
)

func TestBranchMethods(t *testing.T) {
	for _, tc := range []struct {
		name         string
		handler      tool.Handler
		args         map[string]any
		required     []string
		request      string
		status       int
		response     string
		wantContains string
		wantBranch   map[string]any
	}{
		{
			name:     "list",
			handler:  BranchTool.ReadTools()[0].Handler,
			required: []string{"owner", "repo"},
		},
		{
			name:     "create",
			handler:  BranchTool.WriteTools()[0].Handler,
			args:     map[string]any{"branch": "feature/topic"},
			required: []string{"owner", "repo", "branch"},
		},
		{
			name:     "delete",
			handler:  BranchTool.WriteTools()[0].Handler,
			args:     map[string]any{"branch": "feature/topic"},
			required: []string{"owner", "repo", "branch"},
		},
		{
			name:       "get",
			handler:    BranchTool.ReadTools()[0].Handler,
			args:       map[string]any{"branch": "feature/topic"},
			request:    "GET /api/v1/repos/octo/demo/branches/feature/topic",
			status:     http.StatusOK,
			response:   `{"name":"feature/topic","protected":true,"commit":{"id":"abc123","message":"omitted"},"user_can_push":true}`,
			wantBranch: map[string]any{"name": "feature/topic", "protected": true, "commit_sha": "abc123"},
		},
		{
			name:         "get error",
			handler:      BranchTool.ReadTools()[0].Handler,
			args:         map[string]any{"method": "get", "branch": "feature/topic"},
			request:      "GET /api/v1/repos/octo/demo/branches/feature/topic",
			status:       http.StatusNotFound,
			wantContains: "get branch error",
		},
		{
			name:         "rename",
			handler:      BranchTool.WriteTools()[0].Handler,
			args:         map[string]any{"branch": "old-name", "new_name": "new-name"},
			required:     []string{"owner", "repo", "branch", "new_name"},
			request:      "PATCH /api/v1/repos/octo/demo/branches/old-name",
			status:       http.StatusNoContent,
			wantContains: "Branch renamed",
		},
		{
			name:         "rename error",
			handler:      BranchTool.WriteTools()[0].Handler,
			args:         map[string]any{"method": "rename", "branch": "old-name", "new_name": "new-name"},
			request:      "PATCH /api/v1/repos/octo/demo/branches/old-name",
			status:       http.StatusInternalServerError,
			wantContains: "rename branch error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"method": tc.name, "owner": "octo", "repo": "demo"}
			maps.Copy(args, tc.args)
			for _, missing := range tc.required {
				t.Run("missing "+missing, func(t *testing.T) {
					incomplete := maps.Clone(args)
					delete(incomplete, missing)
					result, err := tc.handler(t.Context(), incomplete)
					if err != nil || !result.IsError || !strings.Contains(resultText(t, result), missing+" is required") {
						t.Fatalf("missing %q: result = %v, error = %v", missing, result, err)
					}
				})
			}
			if tc.request == "" {
				return
			}
			serveStub(t, func(w http.ResponseWriter, r *http.Request) {
				if request := r.Method + " " + r.URL.Path; request != tc.request {
					t.Errorf("request = %q, want %q", request, tc.request)
					http.NotFound(w, r)
					return
				}
				if r.Method == http.MethodPatch {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["name"] != "new-name" {
						t.Errorf("request body name = %v, want new-name", body["name"])
					}
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			})
			result, err := tc.handler(t.Context(), args)
			if err != nil || result.IsError != (tc.status >= http.StatusBadRequest) {
				t.Fatalf("result = %v, error = %v", result, err)
			}
			text := resultText(t, result)
			if !strings.Contains(text, tc.wantContains) {
				t.Fatalf("result = %s, want it to contain %q", text, tc.wantContains)
			}
			if tc.wantBranch != nil {
				var branch map[string]any
				if err := json.Unmarshal([]byte(text), &branch); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(branch, tc.wantBranch) {
					t.Fatalf("branch = %v, want %v", branch, tc.wantBranch)
				}
			}
		})
	}
}
