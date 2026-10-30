package wiki

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"

	"gitea.com/gitea/gitea-mcp/internal/annotation"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/params"
	"gitea.com/gitea/gitea-mcp/internal/to"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("wiki")

const (
	WikiReadToolName  = "wiki_read"
	WikiWriteToolName = "wiki_write"
)

var (
	WikiReadTool = tool.NewDefinition(
		WikiReadToolName,
		"Read wiki pages and revisions.",
		annotation.ReadOnly("Read wiki pages"),
		tool.String("method", tool.Required(), tool.Enum("list", "get", "get_revisions")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.String("page_name", tool.Description("for 'get'/'get_revisions'")),
	)

	WikiWriteTool = tool.NewDefinition(
		WikiWriteToolName,
		"Write wiki pages.",
		annotation.Destructive("Create, update, or delete wiki pages"),
		tool.String("method", tool.Required(), tool.Enum("create", "update", "delete")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.String("page_name", tool.Description("for 'update'/'delete'")),
		tool.String("title", tool.Description("for 'create'")),
		tool.String("content", tool.Description("for 'create'/'update'")),
		tool.String("message", tool.Description("commit message")),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool: WikiReadTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"list":          listWikiPagesFn,
			"get":           getWikiPageFn,
			"get_revisions": getWikiRevisionsFn,
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool: WikiWriteTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"create": createWikiPageFn,
			"update": updateWikiPageFn,
			"delete": deleteWikiPageFn,
		}),
	})
}

func listWikiPagesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}

	var result any
	_, err = gitea.DoJSON(ctx, "GET", fmt.Sprintf("repos/%s/%s/wiki/pages", url.PathEscape(owner), url.PathEscape(repo)), nil, nil, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list wiki pages err: %v", err))
	}

	return to.TextResult(result)
}

func getWikiPageFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	pageName, err := params.GetString(args, "page_name")
	if err != nil {
		return to.ErrorResult(err)
	}

	var result any
	_, err = gitea.DoJSON(ctx, "GET", fmt.Sprintf("repos/%s/%s/wiki/page/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(pageName)), nil, nil, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get wiki page err: %v", err))
	}

	return to.TextResult(result)
}

func getWikiRevisionsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	pageName, err := params.GetString(args, "page_name")
	if err != nil {
		return to.ErrorResult(err)
	}

	var result any
	_, err = gitea.DoJSON(ctx, "GET", fmt.Sprintf("repos/%s/%s/wiki/revisions/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(pageName)), nil, nil, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get wiki revisions err: %v", err))
	}

	return to.TextResult(result)
}

func createWikiPageFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	title, err := params.GetString(args, "title")
	if err != nil {
		return to.ErrorResult(err)
	}
	content, err := params.GetString(args, "content")
	if err != nil {
		return to.ErrorResult(err)
	}

	message, _ := args["message"].(string)
	if message == "" {
		message = fmt.Sprintf("Create wiki page '%s'", title)
	}

	requestBody := map[string]string{
		"title":          title,
		"content_base64": base64.StdEncoding.EncodeToString([]byte(content)),
		"message":        message,
	}

	var result any
	_, err = gitea.DoJSON(ctx, "POST", fmt.Sprintf("repos/%s/%s/wiki/new", url.PathEscape(owner), url.PathEscape(repo)), nil, requestBody, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create wiki page err: %v", err))
	}

	return to.TextResult(result)
}

func updateWikiPageFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	pageName, err := params.GetString(args, "page_name")
	if err != nil {
		return to.ErrorResult(err)
	}
	content, err := params.GetString(args, "content")
	if err != nil {
		return to.ErrorResult(err)
	}

	requestBody := map[string]string{
		"content_base64": base64.StdEncoding.EncodeToString([]byte(content)),
	}

	// If title is given, use it. Otherwise, keep current page name
	if title, ok := args["title"].(string); ok && title != "" {
		requestBody["title"] = title
	} else {
		requestBody["title"] = pageName
	}

	if message, ok := args["message"].(string); ok && message != "" {
		requestBody["message"] = message
	} else {
		requestBody["message"] = fmt.Sprintf("Update wiki page '%s'", pageName)
	}

	var result any
	_, err = gitea.DoJSON(ctx, "PATCH", fmt.Sprintf("repos/%s/%s/wiki/page/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(pageName)), nil, requestBody, &result)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("update wiki page err: %v", err))
	}

	return to.TextResult(result)
}

func deleteWikiPageFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	pageName, err := params.GetString(args, "page_name")
	if err != nil {
		return to.ErrorResult(err)
	}

	_, err = gitea.DoJSON(ctx, "DELETE", fmt.Sprintf("repos/%s/%s/wiki/page/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(pageName)), nil, nil, nil)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete wiki page err: %v", err))
	}

	return to.TextResult(map[string]string{"message": "Wiki page deleted successfully"})
}
