package project

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gitea.com/gitea/gitea-mcp/internal/annotation"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/params"
	"gitea.com/gitea/gitea-mcp/internal/to"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("project")

const (
	ProjectReadToolName  = "project_read"
	ProjectWriteToolName = "project_write"
)

var (
	ProjectReadTool = tool.NewDefinition(
		ProjectReadToolName,
		"Read projects of owner+repo, org, or your own if neither.",
		annotation.ReadOnly("Read projects"),
		tool.String("method", tool.Required(), tool.Enum("list", "get", "list_columns", "list_column_issues")),
		tool.String("owner", tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Description(params.RepoDesc)),
		tool.String("org"),
		tool.String("state", tool.Default("all"), tool.Enum("open", "closed", "all")),
		tool.Number("project_id"),
		tool.Number("column_id"),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30), tool.Minimum(1)),
	)

	ProjectWriteTool = tool.NewDefinition(
		ProjectWriteToolName,
		"Write projects of owner+repo, org, or your own if neither. add_issue also moves between columns, move_issue also sets the position within one.",
		annotation.Destructive("Manage projects"),
		tool.String("method", tool.Required(), tool.Enum("create", "update", "delete", "create_column", "update_column", "delete_column", "move_columns", "set_default_column", "add_issue", "remove_issue", "move_issue")),
		tool.String("owner", tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Description(params.RepoDesc)),
		tool.String("org"),
		tool.Number("project_id"),
		tool.Number("column_id"),
		tool.Array("column_ids", tool.Description("for 'move_columns': every column ID of the project, in the new order"), tool.Items(map[string]any{"type": "number"})),
		tool.Number("issue_id", tool.Description("issue id, not number")),
		tool.String("title"),
		tool.String("description"),
		tool.String("color", tool.Description("column color as 6-digit hex, e.g. #FF0000")),
		tool.Number("sorting", tool.Description("'update_column': column position, -128 to 127; 'move_issue': position in the column, omit to append")),
		tool.String("template_type", tool.Enum("none", "basic_kanban", "bug_triage")),
		tool.String("state", tool.Enum("open", "closed")),
	)
)

type project struct {
	ID              int64  `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description,omitempty"`
	State           string `json:"state"`
	NumOpenIssues   int64  `json:"num_open_issues"`
	NumClosedIssues int64  `json:"num_closed_issues"`
	HTMLURL         string `json:"html_url"`
}

type column struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Default bool   `json:"default,omitempty"`
	Sorting int    `json:"sorting"`
	Color   string `json:"color,omitempty"`
}

type columnIssue struct {
	ID      int64  `json:"id"`
	Number  int64  `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
}

func init() {
	Tool.RegisterRead(tool.ServerTool{Tool: ProjectReadTool, Handler: projectReadFn})
	Tool.RegisterWrite(tool.ServerTool{Tool: ProjectWriteTool, Handler: projectWriteFn})
}

func projectReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	query := url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(pageSize)}}
	switch method {
	case "list":
		query.Set("state", params.GetOptionalString(args, "state", "all"))
		return request(ctx, args, "GET", "", query, nil, &[]project{})
	case "get":
		return request(ctx, args, "GET", "/{project_id}", nil, nil, &project{})
	case "list_columns":
		return request(ctx, args, "GET", "/{project_id}/columns", query, nil, &[]column{})
	case "list_column_issues":
		return request(ctx, args, "GET", "/{project_id}/columns/{column_id}/issues", query, nil, &[]columnIssue{})
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func projectWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "create":
		return request(ctx, args, "POST", "", nil, pick(args, "title", "description", "template_type"), &project{})
	case "update":
		return request(ctx, args, "PATCH", "/{project_id}", nil, pick(args, "title", "description", "state"), &project{})
	case "delete":
		return request(ctx, args, "DELETE", "/{project_id}", nil, nil, nil)
	case "create_column":
		return request(ctx, args, "POST", "/{project_id}/columns", nil, pick(args, "title", "color"), &column{})
	case "update_column":
		body := pick(args, "title", "color")
		if err := setInt(args, body, "sorting"); err != nil {
			return to.ErrorResult(err)
		}
		return request(ctx, args, "PATCH", "/{project_id}/columns/{column_id}", nil, body, &column{})
	case "delete_column":
		return request(ctx, args, "DELETE", "/{project_id}/columns/{column_id}", nil, nil, nil)
	case "move_columns":
		columnIDs, err := params.GetInt64Slice(args, "column_ids")
		if err != nil {
			return to.ErrorResult(err)
		}
		return request(ctx, args, "POST", "/{project_id}/columns/move", nil, map[string]any{"column_ids": columnIDs}, nil)
	case "set_default_column":
		return request(ctx, args, "POST", "/{project_id}/columns/{column_id}/default", nil, nil, nil)
	case "add_issue":
		return request(ctx, args, "POST", "/{project_id}/columns/{column_id}/issues/{issue_id}", nil, nil, nil)
	case "remove_issue":
		return request(ctx, args, "DELETE", "/{project_id}/columns/{column_id}/issues/{issue_id}", nil, nil, nil)
	case "move_issue":
		columnID, err := params.GetIndex(args, "column_id")
		if err != nil {
			return to.ErrorResult(err)
		}
		body := map[string]any{"column_id": columnID}
		if err := setInt(args, body, "sorting"); err != nil {
			return to.ErrorResult(err)
		}
		return request(ctx, args, "POST", "/{project_id}/issues/{issue_id}/move", nil, body, nil)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func request(ctx context.Context, args map[string]any, verb, path string, query url.Values, body, out any) (*mcp.CallToolResult, error) {
	owner := params.GetOptionalString(args, "owner", "")
	repo := params.GetOptionalString(args, "repo", "")
	org := params.GetOptionalString(args, "org", "")
	var endpoint string
	switch {
	case org != "":
		endpoint = "orgs/" + url.PathEscape(org) + "/projects"
	case owner != "" && repo != "":
		endpoint = fmt.Sprintf("repos/%s/%s/projects", url.PathEscape(owner), url.PathEscape(repo))
	case owner == "" && repo == "":
		endpoint = "user/projects"
	default:
		return to.ErrorResult(errors.New("owner and repo must be set together"))
	}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if key, ok := strings.CutPrefix(segment, "{"); ok {
			id, err := params.GetIndex(args, strings.TrimSuffix(key, "}"))
			if err != nil {
				return to.ErrorResult(err)
			}
			segments[i] = strconv.FormatInt(id, 10)
		}
	}
	endpoint += strings.Join(segments, "/")
	if _, err := gitea.DoJSON(ctx, verb, endpoint, query, body, out); err != nil {
		return to.ErrorResult(fmt.Errorf("%s %s err: %v", verb, endpoint, err))
	}
	if out == nil {
		return to.TextResult("ok")
	}
	return to.TextResult(out)
}

func pick(args map[string]any, keys ...string) map[string]any {
	body := map[string]any{}
	for _, key := range keys {
		if value, ok := args[key].(string); ok {
			body[key] = value
		}
	}
	return body
}

// setInt adds an optional integer so an unset value is omitted rather than sent as 0.
func setInt(args, body map[string]any, key string) error {
	if args[key] == nil {
		return nil
	}
	value, err := params.GetIndex(args, key)
	if err != nil {
		return err
	}
	body[key] = value
	return nil
}
