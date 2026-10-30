package repo

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/tool"
)

func TestReleaseMethods(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "release.txt")
	var (
		mu       sync.Mutex
		requests []string
	)
	serveStub(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())
		mu.Unlock()
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/assets") {
			file, header, err := r.FormFile("attachment")
			if err != nil {
				t.Errorf("multipart attachment: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer file.Close()
			content, err := io.ReadAll(file)
			if err != nil || string(content) != "release content" || header.Filename != "release.txt" {
				t.Errorf("upload = %q, filename = %q, error = %v", content, header.Filename, err)
			}
		}
		switch {
		case r.URL.Path == "/attachments/release-uuid":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "release content")
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/assets/7") || strings.HasSuffix(r.URL.Path, "/assets"):
			asset := `{"id":7,"name":"release.txt","uuid":"release-uuid","size":15}`
			if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/assets") {
				_, _ = io.WriteString(w, "["+asset+"]")
			} else {
				_, _ = io.WriteString(w, asset)
			}
		case r.URL.Path == "/api/v1/repos/octo/demo/releases" && r.Method == http.MethodGet:
			if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "20" {
				t.Errorf("pagination = %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `[{"id":3,"tag_name":"v1"}]`)
		default:
			_, _ = io.WriteString(w, `{"id":3,"tag_name":"v1"}`)
		}
	})

	for _, tc := range []struct {
		method       string
		handler      tool.Handler
		args         map[string]any
		wantRequests []string
		wantContains string
	}{
		{"list", ReleaseTool.ReadTools()[0].Handler, nil, []string{"GET /api/v1/repos/octo/demo/releases"}, `"tag_name":"v1"`},
		{"get", ReleaseTool.ReadTools()[0].Handler, map[string]any{"id": 3.0}, []string{"GET /api/v1/repos/octo/demo/releases/3"}, `"tag_name":"v1"`},
		{"get_by_tag", ReleaseTool.ReadTools()[0].Handler, map[string]any{"tag": "v1"}, []string{"GET /api/v1/repos/octo/demo/releases/tags/v1"}, `"tag_name":"v1"`},
		{"get_latest", ReleaseTool.ReadTools()[0].Handler, nil, []string{"GET /api/v1/repos/octo/demo/releases/latest"}, `"tag_name":"v1"`},
		{"list_assets", ReleaseTool.ReadTools()[0].Handler, map[string]any{"id": 3.0}, []string{"GET /api/v1/repos/octo/demo/releases/3/assets"}, `"uuid":"release-uuid"`},
		{"get_asset", ReleaseTool.ReadTools()[0].Handler, map[string]any{"id": 3.0, "attachment_id": 7.0}, []string{"GET /api/v1/repos/octo/demo/releases/3/assets/7"}, `"uuid":"release-uuid"`},
		{"download_asset", ReleaseTool.ReadTools()[0].Handler, map[string]any{"id": 3.0, "attachment_id": 7.0, "output_path": outputPath}, []string{"GET /api/v1/repos/octo/demo/releases/3/assets/7", "GET /attachments/release-uuid"}, `"bytes":15`},
		{"create", ReleaseTool.WriteTools()[0].Handler, map[string]any{"tag_name": "v1", "target": "main", "title": "v1"}, []string{"POST /api/v1/repos/octo/demo/releases"}, "Release Created"},
		{"update", ReleaseTool.WriteTools()[0].Handler, map[string]any{"id": 3.0}, []string{"PATCH /api/v1/repos/octo/demo/releases/3"}, `"tag_name":"v1"`},
		{"delete", ReleaseTool.WriteTools()[0].Handler, map[string]any{"id": 3.0}, []string{"DELETE /api/v1/repos/octo/demo/releases/3"}, "deleted"},
		{"upload_asset", ReleaseTool.WriteTools()[0].Handler, map[string]any{"id": 3.0, "name": "release.txt", "content": "cmVsZWFzZSBjb250ZW50"}, []string{"POST /api/v1/repos/octo/demo/releases/3/assets"}, `"uuid":"release-uuid"`},
		{"delete_asset", ReleaseTool.WriteTools()[0].Handler, map[string]any{"id": 3.0, "attachment_id": 7.0}, []string{"DELETE /api/v1/repos/octo/demo/releases/3/assets/7"}, "deleted"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			mu.Lock()
			requests = nil
			mu.Unlock()
			if tc.args == nil {
				tc.args = map[string]any{}
			}
			tc.args["method"], tc.args["owner"], tc.args["repo"] = tc.method, "octo", "demo"
			result, err := tc.handler(t.Context(), tc.args)
			if err != nil || result.IsError {
				t.Fatalf("result = %v, error = %v", result, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(requests, tc.wantRequests) {
				t.Errorf("requests = %q, want %q", requests, tc.wantRequests)
			}
			if text := resultText(t, result); !strings.Contains(text, tc.wantContains) {
				t.Errorf("result = %s, want it to contain %s", text, tc.wantContains)
			}
			if tc.method == "download_asset" {
				content, err := os.ReadFile(outputPath)
				if err != nil || string(content) != "release content" {
					t.Errorf("download = %q, error = %v", content, err)
				}
			}
		})
	}
}
