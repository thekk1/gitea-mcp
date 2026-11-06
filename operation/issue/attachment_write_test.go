package issue

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAttachmentReadContentReturnsBase64(t *testing.T) {
	payload := []byte("%PDF-1.4 \x00\x01 binary")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/octo/demo/issues/7/assets/3":
			_, _ = w.Write([]byte(`{"id":3,"name":"report.pdf","size":` + itoa(len(payload)) + `,"uuid":"uuid-3"}`))
		case "/attachments/uuid-3":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	originalHost := flag.Host
	flag.Host = server.URL
	defer func() { flag.Host = originalHost }()

	result, err := attachmentReadFn(context.Background(), map[string]any{
		"method": "content", "owner": "octo", "repo": "demo", "issue_number": float64(7), "attachment_id": float64(3),
	})
	if err != nil || result.IsError {
		t.Fatalf("attachmentReadFn() = %v, %v", result, err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	if got["filename"] != "report.pdf" || got["mime_type"] != "application/pdf" {
		t.Errorf("metadata = %v", got)
	}
	data, _ := base64.StdEncoding.DecodeString(got["content_base64"].(string))
	if !bytes.Equal(data, payload) {
		t.Errorf("content = %q, want %q", data, payload)
	}
}

func TestAttachmentWriteUploadsToIssue(t *testing.T) {
	payload := []byte{0x89, 'P', 'N', 'G', 0, 1, 2}
	var gotName string
	var gotData []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/repos/octo/demo/issues/7/assets" {
			http.NotFound(w, r)
			return
		}
		file, header, err := r.FormFile("attachment")
		if err != nil {
			t.Errorf("FormFile: %v", err)
			return
		}
		gotName = header.Filename
		gotData, _ = io.ReadAll(file)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":9,"name":"shot.png","size":7,"uuid":"uuid-9"}`))
	}))
	defer server.Close()
	originalHost := flag.Host
	flag.Host = server.URL
	defer func() { flag.Host = originalHost }()

	encoded := base64.URLEncoding.EncodeToString(payload)
	result, err := attachmentWriteFn(context.Background(), map[string]any{
		"method": "create", "owner": "octo", "repo": "demo", "issue_number": float64(7),
		"filename": "shot.png", "content_base64": strings.TrimRight(encoded, "="),
	})
	if err != nil || result.IsError {
		t.Fatalf("attachmentWriteFn() = %v, %v", result, err)
	}
	if gotName != "shot.png" || !bytes.Equal(gotData, payload) {
		t.Errorf("uploaded %q %v, want shot.png %v", gotName, gotData, payload)
	}
}

func TestAttachmentWriteRejectsBadInput(t *testing.T) {
	for _, args := range []map[string]any{
		{"method": "create", "owner": "o", "repo": "r", "issue_number": float64(1), "filename": "../x", "content_base64": "AA=="},
		{"method": "create", "owner": "o", "repo": "r", "issue_number": float64(1), "filename": "x", "content_base64": "not base64!"},
		{"method": "create", "owner": "o", "repo": "r", "filename": "x", "content_base64": "AA=="},
	} {
		result, err := attachmentWriteFn(context.Background(), args)
		if err != nil || !result.IsError {
			t.Errorf("args %v: want error result, got %v, %v", args, result, err)
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestAttachmentWriteLinksInIssueBody(t *testing.T) {
	body := "Vorher\n![a b.png](<a b.png>)\nund [a b.png](a%20b.png \"Titel\") und ![x](andere.png)"
	var patched string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/version":
			_, _ = w.Write([]byte(`{"version":"1.25.0"}`))
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
	}))
	defer server.Close()
	originalHost := flag.Host
	flag.Host = server.URL
	defer func() { flag.Host = originalHost }()

	result, err := attachmentWriteFn(context.Background(), map[string]any{
		"method": "create", "owner": "octo", "repo": "demo", "issue_number": float64(7),
		"filename": "a b.png", "content_base64": "AA==", "link_in_body": true,
	})
	if err != nil || result.IsError {
		t.Fatalf("attachmentWriteFn() = %v, %v", result, err)
	}
	want := "Vorher\n![a b.png](/attachments/uuid-9)\nund [a b.png](/attachments/uuid-9 \"Titel\") und ![x](andere.png)"
	if patched != want {
		t.Errorf("body = %q, want %q", patched, want)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got)
	if got["linked_in_body"] != float64(2) {
		t.Errorf("linked_in_body = %v, want 2", got["linked_in_body"])
	}
}

func TestAttachmentWriteLinkInBodyNeedsIssue(t *testing.T) {
	result, err := attachmentWriteFn(context.Background(), map[string]any{
		"method": "create", "owner": "o", "repo": "r", "comment_id": float64(3),
		"filename": "x.png", "content_base64": "AA==", "link_in_body": true,
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
}

func TestIssueWriteCreateWithBodyBase64(t *testing.T) {
	text := "![a.png](a.png)\n**fett**"
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			_, _ = w.Write([]byte(`{"version":"1.25.0"}`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/repos/octo/demo/issues" {
			http.NotFound(w, r)
			return
		}
		var opt map[string]any
		_ = json.NewDecoder(r.Body).Decode(&opt)
		gotBody, _ = opt["body"].(string)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":8,"title":"t"}`))
	}))
	defer server.Close()
	originalHost := flag.Host
	flag.Host = server.URL
	defer func() { flag.Host = originalHost }()

	result, err := issueWriteFn(context.Background(), map[string]any{
		"method": "create", "owner": "octo", "repo": "demo", "title": "t",
		"body_base64": base64.StdEncoding.EncodeToString([]byte(text)),
	})
	if err != nil || result.IsError {
		t.Fatalf("issueWriteFn() = %v, %v", result, err)
	}
	if gotBody != text {
		t.Errorf("body = %q, want %q", gotBody, text)
	}
}
