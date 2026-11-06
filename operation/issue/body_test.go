package issue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func useTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			_, _ = w.Write([]byte(`{"version":"1.25.0"}`))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	origHost, origToken := flag.Host, flag.Token
	flag.Host, flag.Token = server.URL, ""
	t.Cleanup(func() { flag.Host, flag.Token = origHost, origToken })
}

func TestAddAttachmentLinksInIssueBody(t *testing.T) {
	body := "Before\n![a b.png](<a b.png>)\nand [a b.png](a%20b.png \"Title\") and ![x](other.png)"
	var patched string
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/octo/demo/issues/7/assets":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":9,"name":"a b.png","size":1,"uuid":"uuid-9"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/octo/demo/issues/7":
			b, _ := json.Marshal(map[string]any{"number": 7, "body": body})
			_, _ = w.Write(b)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/octo/demo/issues/7":
			var opt map[string]any
			_ = json.NewDecoder(r.Body).Decode(&opt)
			patched, _ = opt["body"].(string)
			_, _ = w.Write([]byte(`{"number":7}`))
		default:
			http.NotFound(w, r)
		}
	})

	result, err := issueToolHandler(t, IssueWriteToolName)(context.Background(), map[string]any{
		"method": "add_attachment", "owner": "octo", "repo": "demo", "issue_number": float64(7),
		"name": "a b.png", "content": "AA==", "link_in_body": true,
	})
	if err != nil || result.IsError {
		t.Fatalf("add_attachment = %v, %v", result, err)
	}
	want := "Before\n![a b.png](/attachments/uuid-9)\nand [a b.png](/attachments/uuid-9 \"Title\") and ![x](other.png)"
	if patched != want {
		t.Errorf("body = %q, want %q", patched, want)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got)
	if got["linked_in_body"] != float64(2) {
		t.Errorf("linked_in_body = %v, want 2", got["linked_in_body"])
	}
}

func TestAddAttachmentLinkInBodyNeedsIssue(t *testing.T) {
	result, err := issueToolHandler(t, IssueWriteToolName)(context.Background(), map[string]any{
		"method": "add_attachment", "owner": "o", "repo": "r", "comment_id": float64(3),
		"name": "x.png", "content": "AA==", "link_in_body": true,
	})
	if err != nil || !result.IsError {
		t.Errorf("want error result, got %v, %v", result, err)
	}
}

func TestBodyArg(t *testing.T) {
	text := "Ä *x* `y`\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	if body, present, err := bodyArg(map[string]any{"body_base64": encoded}); err != nil || !present || body != text {
		t.Errorf("bodyArg(base64) = %q, %v, %v", body, present, err)
	}
	if body, present, err := bodyArg(map[string]any{"body_base64": base64.RawURLEncoding.EncodeToString([]byte(text))}); err != nil || !present || body != text {
		t.Errorf("bodyArg(url-safe, unpadded) = %q, %v, %v", body, present, err)
	}
	if body, present, err := bodyArg(map[string]any{"body": "plain"}); err != nil || !present || body != "plain" {
		t.Errorf("bodyArg(body) = %q, %v, %v", body, present, err)
	}
	if _, present, err := bodyArg(map[string]any{}); err != nil || present {
		t.Errorf("bodyArg(none) = %v, %v", present, err)
	}
	if _, _, err := bodyArg(map[string]any{"body": "a", "body_base64": encoded}); err == nil {
		t.Error("bodyArg(both): want error")
	}
	if _, _, err := bodyArg(map[string]any{"body_base64": base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe})}); err == nil {
		t.Error("bodyArg(binary): want error")
	}
	if _, err := requiredBodyArg(map[string]any{}); err == nil {
		t.Error("requiredBodyArg(none): want error")
	}
}

func TestIssueWriteBodyBase64(t *testing.T) {
	text := "![a.png](a.png)\n**bold** _x_"
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	got := map[string]string{}
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var opt map[string]any
		_ = json.NewDecoder(r.Body).Decode(&opt)
		body, _ := opt["body"].(string)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/octo/demo/issues":
			got["create"] = body
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":8,"title":"t"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/octo/demo/issues/8":
			got["update"] = body
			_, _ = w.Write([]byte(`{"number":8,"title":"t"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/octo/demo/issues/8/comments":
			got["add_comment"] = body
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":5}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/octo/demo/issues/comments/5":
			got["update_comment"] = body
			_, _ = w.Write([]byte(`{"id":5}`))
		default:
			http.NotFound(w, r)
		}
	})

	write := issueToolHandler(t, IssueWriteToolName)
	for _, args := range []map[string]any{
		{"method": "create", "title": "t"},
		{"method": "update", "issue_number": float64(8)},
		{"method": "add_comment", "issue_number": float64(8)},
		{"method": "update_comment", "comment_id": float64(5)},
	} {
		args["owner"], args["repo"], args["body_base64"] = "octo", "demo", encoded
		result, err := write(context.Background(), args)
		if err != nil || result.IsError {
			t.Fatalf("%v = %v, %v", args["method"], result, err)
		}
		if method := args["method"].(string); got[method] != text {
			t.Errorf("%s body = %q, want %q", method, got[method], text)
		}
	}

	result, err := write(context.Background(), map[string]any{
		"method": "add_comment", "owner": "octo", "repo": "demo", "issue_number": float64(8),
	})
	if err != nil || !result.IsError {
		t.Errorf("add_comment without body: want error result, got %v, %v", result, err)
	}
}
