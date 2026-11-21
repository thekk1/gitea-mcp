package repo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetFilesContent(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		text := base64.StdEncoding.EncodeToString([]byte("hello\n"))
		bin := base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe})
		_, _ = fmt.Fprintf(w, `[{"name":"a.txt","path":"a.txt","sha":"1","type":"file","size":6,"encoding":"base64","content":%q},null,
			{"name":"b.bin","path":"dir/b.bin","sha":"2","type":"file","size":2,"encoding":"base64","content":%q}]`, text, bin)
	}))
	defer server.Close()
	origHost, origToken := flag.Host, flag.Token
	flag.Host, flag.Token = server.URL, ""
	t.Cleanup(func() { flag.Host, flag.Token = origHost, origToken })

	result, err := GetFileContentFn(t.Context(), map[string]any{"owner": "acme", "repo": "docs", "ref": "main",
		"paths": []any{"/a.txt", "missing.md", "dir/b.bin"}})
	if err != nil || result.IsError {
		t.Fatalf("err %v, result %+v", err, result)
	}
	if want := `[POST /api/v1/repos/acme/docs/file-contents?ref=main {"files":["a.txt","missing.md","dir/b.bin"]}]`; fmt.Sprint(requests) != want {
		t.Errorf("requests = %v, want %v", requests, want)
	}
	var got struct {
		Count   int              `json:"count"`
		Missing int              `json:"missing"`
		Files   []map[string]any `json:"files"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	if got.Count != 3 || got.Missing != 1 {
		t.Errorf("count %d missing %d", got.Count, got.Missing)
	}
	if got.Files[0]["content"] != "hello\n" || got.Files[0]["encoding"] != "utf-8" {
		t.Errorf("text file = %v", got.Files[0])
	}
	if got.Files[1]["path"] != "missing.md" || got.Files[1]["error"] == nil {
		t.Errorf("missing file = %v", got.Files[1])
	}
	if got.Files[2]["encoding"] != "base64" {
		t.Errorf("binary file = %v", got.Files[2])
	}

	for _, args := range []map[string]any{
		{"owner": "acme", "repo": "docs", "paths": []any{"../etc/passwd"}},
		{"owner": "acme", "repo": "docs", "paths": []any{"a.txt"}, "withLines": true},
		{"owner": "acme", "repo": "docs", "paths": []any{}},
	} {
		if result, _ := GetFileContentFn(t.Context(), args); !result.IsError {
			t.Errorf("%v: want an error result", args)
		}
	}
}
