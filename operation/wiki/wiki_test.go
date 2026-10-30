package wiki

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpContext "gitea.com/gitea/gitea-mcp/internal/context"
	"gitea.com/gitea/gitea-mcp/internal/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWikiWriteBase64Encoding(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		content string
	}{
		{"create ascii", "create", "Hello, World!"},
		{"create unicode", "create", "日本語テスト 🎉"},
		{"create multiline", "create", "line1\nline2\nline3"},
		{"update ascii", "update", "Updated content"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBody map[string]string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &gotBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"title":"test"}`))
			}))
			defer srv.Close()

			origHost := flag.Host
			flag.Host = srv.URL
			defer func() { flag.Host = origHost }()

			ctx := context.WithValue(context.Background(), mcpContext.TokenContextKey, "test-token")

			args := map[string]any{
				"method":    tt.method,
				"owner":     "org",
				"repo":      "repo",
				"content":   tt.content,
				"page_name": "TestPage",
				"title":     "TestPage",
			}

			result, err := Tool.WriteTools()[0].Handler(ctx, args)
			if err != nil {
				t.Fatalf("wikiWriteFn() error: %v", err)
			}
			if result.IsError {
				t.Fatalf("wikiWriteFn() returned error result")
			}

			got := gotBody["content_base64"]
			want := base64.StdEncoding.EncodeToString([]byte(tt.content))
			if got != want {
				t.Errorf("content_base64 = %q, want %q", got, want)
			}
		})
	}
}

func TestWikiReadGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/org/repo/wiki/page/Home":
			_, _ = w.Write([]byte(`{"title":"Home","content_base64":"` + base64.StdEncoding.EncodeToString([]byte("# Привет")) +
				`","sidebar":"` + base64.StdEncoding.EncodeToString([]byte("[[Home]]")) + `"}`))
		default:
			http.Error(w, `{"message":"not found","url":"https://gitea.example.com/api/swagger"}`, http.StatusNotFound)
		}
	}))
	defer srv.Close()
	origHost := flag.Host
	flag.Host = srv.URL
	defer func() { flag.Host = origHost }()
	ctx := context.WithValue(context.Background(), mcpContext.TokenContextKey, "test-token")

	result, err := Tool.ReadTools()[0].Handler(ctx, map[string]any{"method": "get", "owner": "org", "repo": "repo", "page_name": "Home"})
	if err != nil || result.IsError {
		t.Fatalf("wiki_read get = %v, %v", result, err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"content":"# Привет"`) || !strings.Contains(text, `"sidebar":"[[Home]]"`) || strings.Contains(text, "content_base64") {
		t.Errorf("result = %s, want decoded content and sidebar without content_base64", text)
	}

	result, _ = Tool.ReadTools()[0].Handler(ctx, map[string]any{"method": "list", "owner": "org", "repo": "nowiki"})
	if text := result.Content[0].(*mcp.TextContent).Text; !result.IsError || !strings.Contains(text, "status 404") || !strings.Contains(text, "wiki may be disabled or empty") {
		t.Errorf("list on missing wiki = %s, want the 404 with a cause hint", text)
	}
}
