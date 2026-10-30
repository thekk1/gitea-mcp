package issue

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/flag"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestIssueAttachmentMethods(t *testing.T) {
	const (
		metadata      = `{"id":1,"name":"shot.png","uuid":"uuid-1"}`
		issueAssets   = "/api/v1/repos/octo/demo/issues/42/assets"
		commentAssets = "/api/v1/repos/octo/demo/issues/comments/7/assets"
		scopeError    = "exactly one of issue_number or comment_id is required"
	)
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost:
			file, header, err := r.FormFile("attachment")
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			defer file.Close()
			content, err := io.ReadAll(file)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"name": header.Filename, "uuid": string(content)})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/attachments/uuid-1":
			_, _ = w.Write([]byte("payload"))
		case strings.HasSuffix(r.URL.Path, "/assets"):
			_, _ = w.Write([]byte("[" + metadata + "]"))
		default:
			_, _ = w.Write([]byte(metadata))
		}
	}))
	defer server.Close()
	origHost, origToken := flag.Host, flag.Token
	flag.Host, flag.Token = server.URL, ""
	defer func() { flag.Host, flag.Token = origHost, origToken }()

	read, write := issueToolHandler(t, IssueReadToolName), issueToolHandler(t, IssueWriteToolName)
	for _, tt := range []struct {
		name     string
		handler  tool.Handler
		args     map[string]any
		requests []string
		result   []string
	}{
		{"list issue", read, map[string]any{"method": "get_attachments", "issue_number": float64(42)}, []string{"GET " + issueAssets}, []string{`"uuid":"uuid-1"`, `"mime_type":"image/png"`}},
		{"list comment", read, map[string]any{"method": "get_attachments", "comment_id": float64(7)}, []string{"GET " + commentAssets}, []string{`"uuid":"uuid-1"`, `"mime_type":"image/png"`}},
		{"get issue", read, map[string]any{"method": "get_attachment", "issue_number": float64(42), "attachment_id": float64(1)}, []string{"GET " + issueAssets + "/1"}, []string{`"uuid":"uuid-1"`, `"mime_type":"image/png"`}},
		{"get comment", read, map[string]any{"method": "get_attachment", "comment_id": float64(7), "attachment_id": float64(1)}, []string{"GET " + commentAssets + "/1"}, []string{`"uuid":"uuid-1"`, `"mime_type":"image/png"`}},
		{"download issue", read, map[string]any{"method": "download_attachment", "issue_number": float64(42), "attachment_id": float64(1)}, []string{"GET " + issueAssets + "/1", "GET /attachments/uuid-1"}, []string{`"uuid":"uuid-1"`}},
		{"download comment", read, map[string]any{"method": "download_attachment", "comment_id": float64(7), "attachment_id": float64(1)}, []string{"GET " + commentAssets + "/1", "GET /attachments/uuid-1"}, []string{`"uuid":"uuid-1"`}},
		{"download UUID", read, map[string]any{"method": "download_attachment", "attachment_uuid": "uuid-1"}, []string{"GET /attachments/uuid-1"}, []string{`"uuid":"uuid-1"`}},
		{"add issue", write, map[string]any{"method": "add_attachment", "issue_number": float64(42), "name": "app.tar.gz", "content": "cGF5bG9hZA=="}, []string{"POST " + issueAssets}, []string{`"name":"app.tar.gz"`, `"uuid":"payload"`}},
		{"add comment", write, map[string]any{"method": "add_attachment", "comment_id": float64(7), "name": "app.tar.gz", "content": "cGF5bG9hZA=="}, []string{"POST " + commentAssets}, []string{`"name":"app.tar.gz"`, `"uuid":"payload"`}},
		{"delete issue", write, map[string]any{"method": "delete_attachment", "issue_number": float64(42), "attachment_id": float64(3)}, []string{"DELETE " + issueAssets + "/3"}, []string{"attachment deleted"}},
		{"delete comment", write, map[string]any{"method": "delete_attachment", "comment_id": float64(7), "attachment_id": float64(3)}, []string{"DELETE " + commentAssets + "/3"}, []string{"attachment deleted"}},
		{"two scopes", read, map[string]any{"method": "get_attachments", "issue_number": float64(42), "comment_id": float64(7)}, nil, []string{scopeError}},
		{"no scope", write, map[string]any{"method": "add_attachment"}, nil, []string{scopeError}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests = nil
			tt.args["owner"], tt.args["repo"] = "octo", "demo"
			if tt.args["method"] == "download_attachment" {
				tt.args["output_path"] = filepath.Join(t.TempDir(), "download")
			}
			res, err := tt.handler(context.Background(), tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError != (tt.requests == nil) {
				t.Fatalf("unexpected result: %v", res)
			}
			if !slices.Equal(requests, tt.requests) {
				t.Fatalf("requests = %q, want %q", requests, tt.requests)
			}
			for _, want := range tt.result {
				if body := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(body, want) {
					t.Fatalf("result = %s, want %s", body, want)
				}
			}
			if outputPath, ok := tt.args["output_path"].(string); ok {
				if content, err := os.ReadFile(outputPath); err != nil || string(content) != "payload" {
					t.Fatalf("download = %q, error = %v", content, err)
				}
			}
		})
	}
}
