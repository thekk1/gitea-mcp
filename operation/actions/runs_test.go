package actions

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestJobLogErrorKeepsGiteaReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/o/r/actions/jobs/21/logs" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, `{"message":"job not started"}`, http.StatusNotFound)
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL

	for _, method := range []string{"get_job_log_preview", "download_job_log"} {
		result, err := Tool.ReadTools()[1].Handler(t.Context(), map[string]any{"method": method, "owner": "o", "repo": "r", "job_id": float64(21)})
		if err != nil {
			t.Fatal(err)
		}
		text := result.Content[0].(*mcp.TextContent).Text
		if !result.IsError || !strings.Contains(text, "o/r/job/21") || !strings.Contains(text, "job not started") {
			t.Errorf("%s: result %q, want the job and Gitea's reason", method, text)
		}
	}
}

func TestJobLogPreview(t *testing.T) {
	const jobLog = "setup\n::error::boom\nnoise\nnoise\nFAIL a.test\n##[error]exit 1\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/o/r/actions/jobs/1/logs" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(jobLog + "partial"))
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL

	for _, tc := range []struct {
		name      string
		args      map[string]any
		want      string
		truncated bool
	}{
		{"pattern from mid-line offset", map[string]any{"offset": float64(len("setup\n::err")), "pattern": `::error::|FAIL`, "context_lines": float64(0)}, "::error::boom\n--\nFAIL a.test\n", false},
		{"null arguments", map[string]any{"pattern": nil, "offset": nil, "context_lines": nil}, jobLog, false},
		{"tail_lines counts log lines, not separators", map[string]any{"pattern": "setup|error|FAIL", "context_lines": float64(0), "tail_lines": float64(3)}, "::error::boom\n--\nFAIL a.test\n##[error]exit 1\n", true},
		{"offset at the end holds back a partial row", map[string]any{"offset": float64(len(jobLog))}, "", false},
		{"offset past the end after a rerun", map[string]any{"offset": float64(len(jobLog) + 10)}, jobLog, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"method": "get_job_log_preview", "owner": "o", "repo": "r", "job_id": float64(1)}
			maps.Copy(args, tc.args)
			result, err := Tool.ReadTools()[1].Handler(t.Context(), args)
			if err != nil {
				t.Fatal(err)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			var got map[string]any
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatalf("result %q: %v", text, err)
			}
			if got["log"] != tc.want || got["truncated"] != tc.truncated || got["bytes"] != float64(len(jobLog)) {
				t.Errorf("log %q, truncated %v, bytes %v, want %q, %v and %d", got["log"], got["truncated"], got["bytes"], tc.want, tc.truncated, len(jobLog))
			}
		})
	}

	result, err := Tool.ReadTools()[1].Handler(t.Context(), map[string]any{"method": "get_job_log_preview", "owner": "o", "repo": "r", "job_id": float64(1), "pattern": "("})
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "invalid pattern") {
		t.Errorf("invalid pattern: result %v, err %v", result, err)
	}
}

func TestRunWriteJobControlMethods(t *testing.T) {
	for _, tc := range []struct {
		method string
		args   map[string]any
		want   string
	}{
		{"delete_run", map[string]any{"run_id": float64(5)}, "DELETE /api/v1/repos/o/r/actions/runs/5"},
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
					_, _ = w.Write([]byte(`{"version":"28.0.0"}`))
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
			result, err := Tool.WriteTools()[1].Handler(t.Context(), args)
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError || gotRequest != tc.want {
				t.Errorf("request %q, isError %v (%v), want %q", gotRequest, result.IsError, result.Content, tc.want)
			}
		})
	}
}
