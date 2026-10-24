package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func serveContents(t *testing.T, files map[string]string, onChange func(body map[string]any)) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			onChange(body)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		filePath := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/octo/demo/contents/")
		if filePath == "" {
			entries := []map[string]any{}
			for _, name := range slices.Sorted(maps.Keys(files)) {
				entries = append(entries, map[string]any{"name": name, "path": name, "type": "file"})
			}
			_ = json.NewEncoder(w).Encode(entries)
			return
		}
		content, ok := files[filePath]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path":     filePath,
			"sha":      "sha-" + filePath,
			"encoding": "base64",
			"content":  base64.StdEncoding.EncodeToString([]byte(content)),
			"html_url": "/src/" + r.URL.Query().Get("ref") + "/" + filePath,
		})
	}))
	t.Cleanup(server.Close)

	origHost, origToken := flag.Host, flag.Token
	flag.Host, flag.Token = server.URL, ""
	t.Cleanup(func() { flag.Host, flag.Token = origHost, origToken })
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	return result.Content[0].(*mcp.TextContent).Text
}

func TestCreateOrUpdateFileFn(t *testing.T) {
	var (
		mu      sync.Mutex
		gotBody map[string]any
	)
	serveContents(t, map[string]string{"b.txt": "hello world\n"}, func(body map[string]any) {
		mu.Lock()
		gotBody = body
		mu.Unlock()
	})
	encoded := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

	for _, tc := range []struct {
		name      string
		args      map[string]any
		wantFiles []any
		wantErr   string
	}{
		{
			name:      "update",
			args:      map[string]any{"path": "a.txt", "content": "", "sha": "blobsha"},
			wantFiles: []any{map[string]any{"operation": "update", "path": "a.txt", "content": "", "sha": "blobsha"}},
		},
		{
			name: "edits and files in one commit",
			args: map[string]any{"path": "a.txt", "content": "hello", "files": []any{
				map[string]any{"path": "b.txt", "edits": []any{map[string]any{"old_string": "world", "new_string": "gitea"}}},
			}},
			wantFiles: []any{
				map[string]any{"operation": "create", "path": "a.txt", "content": encoded("hello")},
				map[string]any{"operation": "update", "path": "b.txt", "content": encoded("hello gitea\n"), "sha": "sha-b.txt"},
			},
		},
		{
			name: "rename and delete in one commit",
			args: map[string]any{"files": []any{
				map[string]any{"path": "c.txt", "from_path": "b.txt", "edits": []any{map[string]any{"old_string": "world", "new_string": "gitea"}}},
				map[string]any{"path": "d.txt", "from_path": "e.txt", "sha": "sha-e"},
				map[string]any{"path": "f.txt", "delete": true, "sha": "sha-f"},
			}},
			wantFiles: []any{
				map[string]any{"operation": "update", "path": "c.txt", "from_path": "b.txt", "content": encoded("hello gitea\n"), "sha": "sha-b.txt"},
				map[string]any{"operation": "rename", "path": "d.txt", "from_path": "e.txt", "content": "", "sha": "sha-e"},
				map[string]any{"operation": "delete", "path": "f.txt", "content": "", "sha": "sha-f"},
			},
		},
		{name: "no branch", args: map[string]any{"branch_name": ""}, wantErr: "branch_name is required"},
		{name: "no file", args: map[string]any{}, wantErr: "path or files is required"},
		{name: "no content", args: map[string]any{"path": "a.txt"}, wantErr: "content, edits, from_path or delete is required"},
		{name: "from_path without sha", args: map[string]any{"files": []any{map[string]any{"path": "c.txt", "from_path": "b.txt", "content": "x"}}}, wantErr: "c.txt: sha is required"},
		{name: "delete with content", args: map[string]any{"files": []any{map[string]any{"path": "a.txt", "delete": true, "content": "x"}}}, wantErr: "delete excludes"},
		{
			name: "path in two entries",
			args: map[string]any{"files": []any{
				map[string]any{"path": "b.txt", "content": "x", "sha": "sha-b.txt"},
				map[string]any{"path": "c.txt", "from_path": "b.txt", "sha": "sha-b.txt"},
			}},
			wantErr: "b.txt: appears in more than one file entry",
		},
		{
			name:    "content and edits",
			args:    map[string]any{"path": "b.txt", "content": "x", "edits": []any{map[string]any{"old_string": "a", "new_string": "b"}}},
			wantErr: "mutually exclusive",
		},
		{
			name:    "edit not found",
			args:    map[string]any{"files": []any{map[string]any{"path": "b.txt", "edits": []any{map[string]any{"old_string": "nope", "new_string": "x"}}}}},
			wantErr: "b.txt: edit 1: old_string not found",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			gotBody = nil
			mu.Unlock()
			args := map[string]any{"owner": "octo", "repo": "demo", "message": "msg", "branch_name": "main", "new_branch_name": "feature-x"}
			maps.Copy(args, tc.args)

			result, err := CreateOrUpdateFileFn(context.Background(), args)
			if err != nil {
				t.Fatalf("CreateOrUpdateFileFn() error = %v", err)
			}
			text := resultText(t, result)
			if tc.wantErr != "" {
				if !result.IsError || !strings.Contains(text, tc.wantErr) {
					t.Fatalf("result = %s, want error containing %q", text, tc.wantErr)
				}
				return
			}

			mu.Lock()
			defer mu.Unlock()
			if gotBody["branch"] != "main" || gotBody["new_branch"] != "feature-x" {
				t.Fatalf("branch = %v, new_branch = %v, want main and feature-x", gotBody["branch"], gotBody["new_branch"])
			}
			if !reflect.DeepEqual(gotBody["files"], tc.wantFiles) {
				t.Fatalf("files = %v, want %v", gotBody["files"], tc.wantFiles)
			}
			if !strings.Contains(text, "to branch feature-x") {
				t.Fatalf("result = %s, want it to name branch feature-x", text)
			}
		})
	}
}

func TestApplyEdits(t *testing.T) {
	edit := func(oldString, newString string, replaceAll bool) any {
		return map[string]any{"old_string": oldString, "new_string": newString, "replace_all": replaceAll}
	}
	for _, tc := range []struct {
		name    string
		text    string
		edits   []any
		want    string
		wantErr string
	}{
		{name: "in order", text: "a", edits: []any{edit("a", "b", false), edit("b", "c", false)}, want: "c"},
		{name: "replace_all", text: "x x x", edits: []any{edit("x", "y", true)}, want: "y y y"},
		{name: "not found", text: "a", edits: []any{edit("\ta", "b", false)}, wantErr: "edit 1: old_string not found"},
		{name: "ambiguous", text: "x x", edits: []any{edit("x", "y", false)}, wantErr: "occurs 2 times"},
		{name: "empty old_string", text: "a", edits: []any{edit("", "b", false)}, wantErr: "required"},
		{name: "missing new_string", text: "a", edits: []any{map[string]any{"old_string": "a"}}, wantErr: "required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyEdits(tc.text, tc.edits)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("applyEdits() error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("applyEdits() = %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestGetFileContentFn(t *testing.T) {
	serveContents(t, map[string]string{"a.txt": "a\r\nb\nc\n", "bin": "\x00\xff\n"}, nil)

	for _, tc := range []struct {
		name    string
		args    map[string]any
		want    map[string]any
		wantDir []string
		wantErr string
	}{
		{
			name:    "dot segment rejected",
			args:    map[string]any{"path": "a/../../../user"},
			wantErr: `invalid path segment ".."`,
		},
		{
			name: "whole file at sha",
			args: map[string]any{"path": "a.txt", "sha": "abc"},
			want: map[string]any{"content": base64.StdEncoding.EncodeToString([]byte("a\r\nb\nc\n")), "encoding": "base64", "html_url": "/src/abc/a.txt"},
		},
		{
			name:    "directory",
			args:    map[string]any{"path": "/"},
			wantDir: []string{"a.txt", "bin"},
		},
		{
			name: "range",
			args: map[string]any{"path": "a.txt", "start_line": 2.0, "end_line": 2.0},
			want: map[string]any{"content": "b", "encoding": "utf-8", "total_lines": 3.0, "start_line": 2.0, "end_line": 2.0, "truncated": true},
		},
		{
			name: "numbered lines",
			args: map[string]any{"path": "a.txt", "with_lines": true},
			want: map[string]any{"content": `[
  {
    "line": 1,
    "content": "a"
  },
  {
    "line": 2,
    "content": "b"
  },
  {
    "line": 3,
    "content": "c"
  }
]`, "encoding": "utf-8", "total_lines": 3.0},
		},
		{
			name: "binary stays base64",
			args: map[string]any{"path": "bin", "start_line": 1.0},
			want: map[string]any{"content": base64.StdEncoding.EncodeToString([]byte("\x00\xff")), "encoding": "base64", "truncated": false},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"owner": "octo", "repo": "demo", "ref": "main"}
			maps.Copy(args, tc.args)
			result, err := GetFileContentFn(context.Background(), args)
			if err != nil {
				t.Fatalf("GetFileContentFn() error = %v", err)
			}
			text := resultText(t, result)
			if result.IsError != (tc.wantErr != "") || !strings.Contains(text, tc.wantErr) {
				t.Fatalf("result = %s, want error %q", text, tc.wantErr)
			}
			if tc.wantErr != "" {
				return
			}
			if tc.wantDir != nil {
				var entries []map[string]any
				if err := json.Unmarshal([]byte(text), &entries); err != nil {
					t.Fatal(err)
				}
				var names []string
				for _, entry := range entries {
					names = append(names, entry["name"].(string))
				}
				if !slices.Equal(names, tc.wantDir) {
					t.Fatalf("entries = %v, want %v", names, tc.wantDir)
				}
				return
			}
			if structured, _ := result.StructuredContent.(json.RawMessage); string(structured) != text {
				t.Errorf("structuredContent = %s, want %s", structured, text)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatal(err)
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s = %#v, want %#v", key, got[key], want)
				}
			}
		})
	}
}

func TestSelectLines(t *testing.T) {
	const file = "a\nb\nc\nd\ne\n"
	for _, tc := range []struct {
		name                           string
		raw                            string
		start, end                     int
		wantText                       string
		wantFirst, wantLast, wantTotal int
		wantErr                        string
	}{
		{name: "whole file", raw: file, wantText: "a\nb\nc\nd\ne", wantFirst: 1, wantLast: 5, wantTotal: 5},
		{name: "start only", raw: file, start: 4, wantText: "d\ne", wantFirst: 4, wantLast: 5, wantTotal: 5},
		{name: "end only", raw: file, end: 2, wantText: "a\nb", wantFirst: 1, wantLast: 2, wantTotal: 5},
		{name: "end past eof is clamped", raw: file, start: 4, end: 999, wantText: "d\ne", wantFirst: 4, wantLast: 5, wantTotal: 5},
		{name: "empty file", raw: "", wantFirst: 1},
		{name: "no trailing newline", raw: "a\nb", start: 2, wantText: "b", wantFirst: 2, wantLast: 2, wantTotal: 2},
		{name: "trailing blank line", raw: "a\n\n", start: 2, wantFirst: 2, wantLast: 2, wantTotal: 2},
		{name: "only a newline", raw: "\n", wantFirst: 1, wantLast: 1, wantTotal: 1},
		{name: "crlf", raw: "a\r\nb\r\n", wantText: "a\r\nb", wantFirst: 1, wantLast: 2, wantTotal: 2},
		{name: "start past eof", raw: file, start: 6, wantErr: "past the end"},
		{name: "end before start", raw: file, start: 3, end: 2, wantErr: "before start_line"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectLines([]byte(tc.raw), tc.start, tc.end)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("selectLines() error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || string(got.Bytes) != tc.wantText || got.First != tc.wantFirst || got.Last != tc.wantLast || got.Total != tc.wantTotal {
				t.Fatalf("selectLines() = %q (%d-%d of %d), %v, want %q (%d-%d of %d)",
					got.Bytes, got.First, got.Last, got.Total, err, tc.wantText, tc.wantFirst, tc.wantLast, tc.wantTotal)
			}
		})
	}
}
