package attachment

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/flag"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAttachmentFilename(t *testing.T) {
	tests := []struct {
		name string
		file string
		uuid string
		want string
	}{
		{"uses attachment name", "screenshot.png", "abc", "screenshot.png"},
		{"falls back for traversal", "../etc/passwd", "abc", "abc.bin"},
		{"falls back for empty name", "", "abc", "abc.bin"},
		{"uses generic fallback", "", "", "attachment.bin"},
		{"falls back for windows stream name", "a.png:b", "abc", "abc.bin"},
		{"falls back keeping extension for windows reserved character", "a?.png", "abc", "abc.png"},
		{"falls back for trailing dot", "a.", "abc", "abc.bin"},
		{"falls back for windows device name on every os", "nul.txt", "abc", "abc.txt"},
		{"falls back for control character", "a\x01b.png", "abc", "abc.png"},
		{"falls back for name without room for a suffix", strings.Repeat("a", 201), "abc", "abc.bin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := attachmentFilename(tt.file, tt.uuid); got != tt.want {
				t.Fatalf("attachmentFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInferAttachmentMimeType(t *testing.T) {
	if got := inferAttachmentMimeType("shot.png"); got != "image/png" {
		t.Fatalf("inferAttachmentMimeType() = %q, want image/png", got)
	}
	if got := inferAttachmentMimeType("archive.unknownext"); got != "application/octet-stream" {
		t.Fatalf("inferAttachmentMimeType() = %q, want application/octet-stream", got)
	}
}

func TestDefaultAttachmentPath(t *testing.T) {
	for _, tt := range []struct{ repo, name, want string }{
		{"demo", "shot.png", filepath.Join("demo", "shot-uuid-1.png")},
		{"demo", "a?.png", filepath.Join("demo", "uuid-1.png")},
		{"aux", "uuid-1", filepath.Join("unknown", "uuid-1")},
	} {
		want := filepath.Join(".gitea-mcp", "attachments", "octo", tt.want)
		if got := defaultAttachmentPath("octo", tt.repo, tt.name, "uuid-1"); !strings.HasSuffix(got, want) {
			t.Errorf("defaultAttachmentPath(%q, %q) = %q, want suffix %q", tt.repo, tt.name, got, want)
		}
	}
}

func TestDownloadSavesLargeAttachmentToDefaultFile(t *testing.T) {
	const owner = "octo"
	const repo = "demo"
	const uuid = "uuid-1"
	const name = "large.bin"
	payload := strings.Repeat("a", 32)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/attachments/" + uuid:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte(payload))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	origHost, origToken, origVersion, origInline := flag.Host, flag.Token, flag.Version, flag.MaxInlineAttachmentBytes
	flag.Host, flag.Token, flag.Version = server.URL, "", "test"
	flag.MaxInlineAttachmentBytes = 8
	defer func() {
		flag.Host, flag.Token, flag.Version, flag.MaxInlineAttachmentBytes = origHost, origToken, origVersion, origInline
	}()

	res, err := Download(context.Background(), owner, repo, &gitea_sdk.Attachment{ID: 1, Name: name, UUID: uuid, Size: int64(len(payload))}, "", "")
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	var got struct {
		Path  string `json:"path"`
		Bytes int    `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(home, ".gitea-mcp", "attachments", owner, repo, "large-uuid-1.bin")
	if got.Path != wantPath || got.Bytes != len(payload) {
		t.Fatalf("result = %+v, want path %q and %d bytes", got, wantPath, len(payload))
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", wantPath, err)
	}
	if string(data) != payload {
		t.Fatalf("saved payload mismatch")
	}
}

func TestDownloadReturnsRawImageContent(t *testing.T) {
	const uuid = "uuid-1"
	payload := []byte{0, 1, 2, 250}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/attachments/"+uuid {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	originalHost := flag.Host
	originalLimit := flag.MaxInlineAttachmentBytes
	flag.Host = server.URL
	flag.MaxInlineAttachmentBytes = len(payload)
	defer func() {
		flag.Host = originalHost
		flag.MaxInlineAttachmentBytes = originalLimit
	}()

	result, err := Download(context.Background(), "octo", "demo", nil, uuid, "")
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if len(result.Content) != 2 {
		t.Fatalf("content count = %d, want 2", len(result.Content))
	}
	if _, ok := result.Content[0].(*mcp.TextContent); !ok {
		t.Fatalf("first content type = %T, want *mcp.TextContent", result.Content[0])
	}
	image, ok := result.Content[1].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("second content type = %T, want *mcp.ImageContent", result.Content[1])
	}
	if image.MIMEType != "image/png" {
		t.Errorf("image MIME type = %q, want image/png", image.MIMEType)
	}
	if !bytes.Equal(image.Data, payload) {
		t.Errorf("image data = %v, want raw payload %v", image.Data, payload)
	}
	wire, err := json.Marshal(image)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	wantBase64 := base64.StdEncoding.EncodeToString(payload)
	if !strings.Contains(string(wire), `"data":"`+wantBase64+`"`) {
		t.Errorf("wire image = %s, want base64 data %q", wire, wantBase64)
	}
	if result, err := Download(context.Background(), "octo", "demo", nil, "../../x", ""); err != nil || !result.IsError {
		t.Errorf("traversing uuid: result = %v, error = %v, want error result", result, err)
	}
}
