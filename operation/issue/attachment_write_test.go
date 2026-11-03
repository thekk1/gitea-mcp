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
