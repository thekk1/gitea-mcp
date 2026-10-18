package actions

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"
)

func TestListRepoActionRunsForwardsFilters(t *testing.T) {
	var gotRequest string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest = r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"total_count":0,"workflow_runs":[]}`))
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL

	result, err := listRepoActionRunsFn(t.Context(), map[string]any{"owner": "o", "repo": "r", "head_sha": "abc123", "branch": "main"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "/api/v1/repos/o/r/actions/runs?branch=main&head_sha=abc123&limit=30&page=1"; result.IsError || gotRequest != want {
		t.Errorf("request %q, isError %v, want %q", gotRequest, result.IsError, want)
	}
}

func TestRunWriteJobControlMethods(t *testing.T) {
	for _, tc := range []struct {
		method string
		args   map[string]any
		want   string
	}{
		{"rerun_job", map[string]any{"run_id": float64(5), "job_id": float64(9)}, "POST /api/v1/repos/o/r/actions/runs/5/jobs/9/rerun"},
		{"rerun_failed_jobs", map[string]any{"run_id": float64(5)}, "POST /api/v1/repos/o/r/actions/runs/5/rerun-failed-jobs"},
		{"approve_run", map[string]any{"run_id": float64(5)}, "POST /api/v1/repos/o/r/actions/runs/5/approve"},
		{"enable_workflow", map[string]any{"workflow_id": "ci.yml"}, "PUT /api/v1/repos/o/r/actions/workflows/ci.yml/enable"},
		{"disable_workflow", map[string]any{"workflow_id": "ci.yml"}, "PUT /api/v1/repos/o/r/actions/workflows/ci.yml/disable"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			var gotRequest string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/version" {
					_, _ = w.Write([]byte(`{"version":"1.28.0"}`))
					return
				}
				gotRequest = r.Method + " " + r.URL.Path
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			defer func(host string) { flag.Host = host }(flag.Host)
			flag.Host = server.URL

			args := map[string]any{"method": tc.method, "owner": "o", "repo": "r"}
			maps.Copy(args, tc.args)
			result, err := runWriteFn(t.Context(), args)
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError || gotRequest != tc.want {
				t.Errorf("request %q, isError %v (%v), want %q", gotRequest, result.IsError, result.Content, tc.want)
			}
		})
	}
}
