package repo

import (
	"cmp"
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"gitea.com/gitea/gitea-mcp/pkg/batch"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/to"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// getFilesContentFn reads several files with one request to Gitea's batch endpoint POST
// /repos/{owner}/{repo}/file-contents. Gitea returns the entries in the order of the paths and null for a path
// that does not exist at the ref.
func getFilesContentFn(ctx context.Context, args map[string]any, in getFileContentArgs) (*mcp.CallToolResult, error) {
	raw, ok := args["paths"].([]any)
	if !ok || len(raw) == 0 {
		return to.ErrorResult(fmt.Errorf("paths must be a non-empty list"))
	}
	if len(raw) > batch.MaxItems {
		return to.ErrorResult(fmt.Errorf("paths has %d values, at most %d per call", len(raw), batch.MaxItems))
	}
	if in.WithLines || in.StartLine > 0 || in.EndLine > 0 {
		return to.ErrorResult(fmt.Errorf("withLines, start_line and end_line work with path only, not with paths"))
	}
	paths := make([]string, 0, len(raw))
	for _, v := range raw {
		p, ok := v.(string)
		if !ok {
			return to.ErrorResult(fmt.Errorf("paths must hold strings, got %v", v))
		}
		p = strings.Trim(p, "/")
		for _, segment := range strings.Split(p, "/") {
			if segment == "." || segment == ".." {
				return to.ErrorResult(fmt.Errorf("invalid path segment %q in %q", segment, p))
			}
		}
		paths = append(paths, p)
	}
	var entries []*gitea_sdk.ContentsResponse
	query := url.Values{}
	if ref := cmp.Or(in.SHA, in.Ref); ref != "" {
		query.Set("ref", ref)
	}
	endpoint := fmt.Sprintf("repos/%s/%s/file-contents", url.PathEscape(in.Owner), url.PathEscape(in.Repo))
	if _, err := gitea.DoJSON(ctx, "POST", endpoint, query, map[string]any{"files": paths}, &entries); err != nil {
		return to.ErrorResult(fmt.Errorf("get files err: %v", err))
	}
	files := make([]map[string]any, 0, len(paths))
	missing := 0
	for i, p := range paths {
		var c *gitea_sdk.ContentsResponse
		if i < len(entries) {
			c = entries[i]
		}
		if c == nil {
			missing++
			files = append(files, map[string]any{"path": p, "error": "not found at this ref"})
			continue
		}
		m := slimContents(c)
		if c.Content != nil {
			if data, err := base64.StdEncoding.DecodeString(*c.Content); err == nil && utf8.Valid(data) {
				m["content"] = string(data)
				m["encoding"] = "utf-8"
			}
		}
		files = append(files, m)
	}
	return to.TextResult(map[string]any{"count": len(files), "missing": missing, "files": files})
}
