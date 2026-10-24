package issue

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"gitea.com/gitea/gitea-mcp/internal/annotation"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/params"
	"gitea.com/gitea/gitea-mcp/internal/slim"
	"gitea.com/gitea/gitea-mcp/internal/to"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// issueWithAssets / commentWithAssets wrap the SDK types to capture the
// `assets` field that the SDK currently drops on these endpoints.
type issueWithAssets struct {
	gitea_sdk.Issue
	Assets []*gitea_sdk.Attachment `json:"assets"`
}

type commentWithAssets struct {
	gitea_sdk.Comment
	Assets []*gitea_sdk.Attachment `json:"assets"`
}

var Tool = tool.New("issue")

const (
	ListRepoIssuesToolName = "list_issues"
	IssueReadToolName      = "issue_read"
	IssueWriteToolName     = "issue_write"
)

var (
	ListRepoIssuesTool = tool.NewDefinition(
		ListRepoIssuesToolName,
		"List issues in a repository (or pull requests, via the 'type' filter), filterable by state, labels, milestones, users, and update time range.",
		annotation.ReadOnly("List repository issues"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("state", tool.Default("all")),
		tool.String("type", tool.Description("issues or pulls"), tool.Enum("issues", "pulls")),
		tool.Array("labels", tool.Description("label name filter"), tool.Items(map[string]any{"type": "string"})),
		tool.Array("milestones", tool.Description("milestone name or ID filter"), tool.Items(map[string]any{"type": "string"})),
		tool.String("since", tool.Description("updated after ISO 8601")),
		tool.String("before", tool.Description("updated before ISO 8601")),
		tool.String("created_by"),
		tool.String("assigned_by", tool.Description("assignee username")),
		tool.String("mentioned_by", tool.Description("mentioned username")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30)),
	)

	IssueReadTool = tool.NewDefinition(
		IssueReadToolName,
		"Read issue: details, comments, labels, dependencies, or the users it can be assigned to.",
		annotation.ReadOnly("Read issue details"),
		tool.String("method", tool.Required(), tool.Enum("get", "get_comments", "get_comment", "get_labels", "get_blocked_by", "get_blocking", "get_assignee_candidates")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("issue_number", tool.Description("required except for 'get_comment'/'get_assignee_candidates'")),
		tool.Number("comment_id", tool.Description("for 'get_comment'")),
		tool.Number("page", tool.Description("for 'get_blocked_by'/'get_blocking'"), tool.Default(1)),
		tool.Number("per_page", tool.Description("for 'get_blocked_by'/'get_blocking'"), tool.Default(30)),
	)

	IssueWriteTool = tool.NewDefinition(
		IssueWriteToolName,
		"Write issues: create, update, manage comments, labels and dependencies.",
		annotation.Write("Create or update issues, comments, labels, and dependencies"),
		tool.String("method", tool.Required(), tool.Enum("create", "update", "add_comment", "edit_comment", "add_labels", "remove_label", "replace_labels", "clear_labels", "add_dependency", "remove_dependency")),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.Number("issue_number", tool.Description("required except for 'create'")),
		tool.String("title", tool.Description("required for 'create'")),
		tool.String("body", tool.Description("required for 'add_comment'/'edit_comment'")),
		tool.Array("assignees", tool.Items(map[string]any{"type": "string"})),
		tool.Number("milestone"),
		tool.String("state", tool.Enum("open", "closed", "all")),
		tool.Number("comment_id", tool.Description("for 'edit_comment'")),
		tool.Number("commentID", tool.Description("deprecated alias of comment_id")),
		tool.Array("labels", tool.Description("label IDs"), tool.Items(map[string]any{"type": "number"})),
		tool.Number("label_id", tool.Description("for 'remove_label'")),
		tool.String("ref", tool.Description("branch to associate")),
		tool.String("deadline", tool.Description("ISO 8601")),
		tool.Boolean("remove_deadline"),
		tool.String("dependency_type", tool.Description("issue_number is blocked_by or blocking the related issue"), tool.Enum("blocked_by", "blocking")),
		tool.String("related_owner", tool.Description("defaults to owner")),
		tool.String("related_repo", tool.Description("defaults to repo")),
		tool.Number("related_issue_number"),
	)
)

var dependencyEndpoints = map[string]string{"blocked_by": "dependencies", "blocking": "blocks"}

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    ListRepoIssuesTool,
		Handler: listRepoIssuesFn,
	})
	Tool.RegisterRead(tool.ServerTool{
		Tool:    IssueReadTool,
		Handler: issueReadFn,
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool:    IssueWriteTool,
		Handler: issueWriteFn,
	})
}

func issueReadFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "get":
		return getIssueByIndexFn(ctx, args)
	case "get_comments":
		return ListComments(ctx, args, "issue_number")
	case "get_comment":
		return getIssueCommentFn(ctx, args)
	case "get_labels":
		return getIssueLabelsFn(ctx, args)
	case "get_blocked_by":
		return listIssueDependenciesFn(ctx, args, dependencyEndpoints["blocked_by"])
	case "get_blocking":
		return listIssueDependenciesFn(ctx, args, dependencyEndpoints["blocking"])
	case "get_assignee_candidates":
		return listAssigneeCandidatesFn(ctx, args)
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func issueWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	switch method {
	case "create":
		return createIssueFn(ctx, args)
	case "update":
		return editIssueFn(ctx, args)
	case "add_comment":
		return createIssueCommentFn(ctx, args)
	case "edit_comment":
		return editIssueCommentFn(ctx, args)
	case "add_labels":
		return addIssueLabelsFn(ctx, args)
	case "remove_label":
		return removeIssueLabelFn(ctx, args)
	case "replace_labels":
		return replaceIssueLabelsFn(ctx, args)
	case "clear_labels":
		return clearIssueLabelsFn(ctx, args)
	case "add_dependency":
		return writeIssueDependencyFn(ctx, args, "POST")
	case "remove_dependency":
		return writeIssueDependencyFn(ctx, args, "DELETE")
	default:
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
}

func getIssueByIndexFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	var issue issueWithAssets
	path := fmt.Sprintf("repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), index)
	if _, err := gitea.DoJSON(ctx, "GET", path, nil, nil, &issue); err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/issue/%v err: %v", owner, repo, index, err))
	}
	m := slimIssue(&issue.Issue)
	m["body"] = slim.BodyWithAttachments(issue.Body, issue.Assets)
	return to.TextResult(m)
}

func listRepoIssuesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	state, ok := args["state"].(string)
	if !ok {
		state = "all"
	}
	labels := params.GetStringSlice(args, "labels")
	milestones := params.GetStringSlice(args, "milestones")
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.ListIssueOption{
		State:       gitea_sdk.StateType(state),
		Labels:      labels,
		Milestones:  milestones,
		CreatedBy:   params.GetOptionalString(args, "created_by", ""),
		AssignedBy:  params.GetOptionalString(args, "assigned_by", ""),
		MentionedBy: params.GetOptionalString(args, "mentioned_by", ""),
		Page:        page,
		PageSize:    pageSize,
	}
	switch args["type"] {
	case "issues":
		opt.Type = gitea_sdk.IssueTypeIssue
	case "pulls":
		opt.Type = gitea_sdk.IssueTypePull
	}
	if t := params.GetOptionalTime(args, "since"); t != nil {
		opt.Since = *t
	}
	if t := params.GetOptionalTime(args, "before"); t != nil {
		opt.Before = *t
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issues, _, err := client.Issues.ListRepoIssues(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/issues err: %v", owner, repo, err))
	}
	return to.TextResult(slimIssues(issues))
}

func createIssueFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	opt := gitea_sdk.CreateIssueOption{
		Title: title,
		Body:  params.GetOptionalString(args, "body", ""),
	}
	opt.Assignees = params.GetStringSlice(args, "assignees")
	if val, exists := args["milestone"]; exists {
		if milestone, ok := params.ToInt64(val); ok {
			opt.Milestone = milestone
		}
	}
	if labelIDs, err := params.GetInt64Slice(args, "labels"); err == nil {
		opt.Labels = labelIDs
	}
	if ref, ok := args["ref"].(string); ok {
		opt.Ref = ref
	}
	opt.Deadline = params.GetOptionalTime(args, "deadline")
	issue, _, err := client.Issues.CreateIssue(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create %v/%v/issue err: %v", owner, repo, err))
	}

	return to.TextResult(slimIssue(issue))
}

func createIssueCommentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	body, err := params.GetString(args, "body")
	if err != nil {
		return to.ErrorResult(err)
	}
	opt := gitea_sdk.CreateIssueCommentOption{
		Body: body,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issueComment, _, err := client.Issues.CreateIssueComment(ctx, owner, repo, index, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create %v/%v/issue/%v/comment err: %v", owner, repo, index, err))
	}

	return to.TextResult(slimComment(issueComment))
}

func editIssueFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	opt := gitea_sdk.EditIssueOption{
		Body:           params.GetPresentStringPtr(args, "body"),
		Ref:            params.GetPresentStringPtr(args, "ref"),
		Assignees:      params.GetStringSlice(args, "assignees"),
		Deadline:       params.GetOptionalTime(args, "deadline"),
		RemoveDeadline: params.GetOptionalBoolPtr(args, "remove_deadline"),
	}
	if title, ok := args["title"].(string); ok {
		opt.Title = title
	}
	if val, exists := args["milestone"]; exists {
		if milestone, ok := params.ToInt64(val); ok {
			opt.Milestone = &milestone
		}
	}
	if state, ok := args["state"].(string); ok {
		s := gitea_sdk.StateType(state)
		opt.State = &s
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issue, _, err := client.Issues.EditIssue(ctx, owner, repo, index, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("edit %v/%v/issue/%v err: %v", owner, repo, index, err))
	}

	return to.TextResult(slimIssue(issue))
}

func editIssueCommentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	commentKey := "comment_id"
	if _, ok := args[commentKey]; !ok {
		commentKey = "commentID" // deprecated name, kept for existing callers
	}
	commentID, err := params.GetIndex(args, commentKey)
	if err != nil {
		return to.ErrorResult(err)
	}
	body, err := params.GetString(args, "body")
	if err != nil {
		return to.ErrorResult(err)
	}
	opt := gitea_sdk.EditIssueCommentOption{
		Body: body,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issueComment, _, err := client.Issues.EditIssueComment(ctx, owner, repo, commentID, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("edit %v/%v/issues/comments/%v err: %v", owner, repo, commentID, err))
	}

	return to.TextResult(slimComment(issueComment))
}

func ListComments(ctx context.Context, args map[string]any, indexKey string) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, indexKey)
	if err != nil {
		return to.ErrorResult(err)
	}
	var comments []commentWithAssets
	path := fmt.Sprintf("repos/%s/%s/issues/%d/comments", url.PathEscape(owner), url.PathEscape(repo), index)
	if _, err := gitea.DoJSON(ctx, "GET", path, nil, nil, &comments); err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/issues/%v/comments err: %v", owner, repo, index, err))
	}
	out := make([]map[string]any, 0, len(comments))
	for i := range comments {
		out = append(out, slimCommentWithAssets(&comments[i]))
	}
	return to.TextResult(out)
}

func getIssueCommentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	commentID, err := params.GetIndex(args, "comment_id")
	if err != nil {
		return to.ErrorResult(err)
	}
	var comment commentWithAssets
	path := fmt.Sprintf("repos/%s/%s/issues/comments/%d", url.PathEscape(owner), url.PathEscape(repo), commentID)
	status, err := gitea.DoJSON(ctx, "GET", path, nil, nil, &comment)
	if status == http.StatusNoContent {
		return to.ErrorResult(fmt.Errorf("comment %v is not a discussion comment", commentID))
	}
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/issues/comments/%v err: %v", owner, repo, commentID, err))
	}
	return to.TextResult(slimCommentWithAssets(&comment))
}

func listIssueDependenciesFn(ctx context.Context, args map[string]any, endpoint string) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	var issues []gitea_sdk.Issue
	path := fmt.Sprintf("repos/%s/%s/issues/%d/%s", url.PathEscape(owner), url.PathEscape(repo), index, endpoint)
	if _, err := gitea.DoJSON(ctx, "GET", path, url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(pageSize)}}, nil, &issues); err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/issues/%v/%v err: %v", owner, repo, index, endpoint, err))
	}
	out := make([]map[string]any, 0, len(issues))
	for i := range issues {
		out = append(out, slimIssueRef(&issues[i]))
	}
	return to.TextResult(out)
}

func writeIssueDependencyFn(ctx context.Context, args map[string]any, httpMethod string) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	endpoint, ok := dependencyEndpoints[params.GetOptionalString(args, "dependency_type", "")]
	if !ok {
		return to.ErrorResult(errors.New("dependency_type must be 'blocked_by' or 'blocking'"))
	}
	relatedIssueNumber, err := params.GetIndex(args, "related_issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	var issue gitea_sdk.Issue
	path := fmt.Sprintf("repos/%s/%s/issues/%d/%s", url.PathEscape(owner), url.PathEscape(repo), index, endpoint)
	if _, err := gitea.DoJSON(ctx, httpMethod, path, nil, map[string]any{
		"owner": cmp.Or(params.GetOptionalString(args, "related_owner", ""), owner),
		"repo":  cmp.Or(params.GetOptionalString(args, "related_repo", ""), repo),
		"index": relatedIssueNumber,
	}, &issue); err != nil {
		return to.ErrorResult(fmt.Errorf("%v %v/%v/issues/%v/%v err: %v", httpMethod, owner, repo, index, endpoint, err))
	}
	return to.TextResult(slimIssueRef(&issue))
}

func getIssueLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	labels, _, err := client.Issues.GetIssueLabels(ctx, owner, repo, index, gitea_sdk.ListLabelsOptions{})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/issues/%v/labels err: %v", owner, repo, index, err))
	}
	return to.TextResult(slim.Labels(labels))
}

func listAssigneeCandidatesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	users, _, err := client.Repositories.GetAssignees(ctx, owner, repo)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v assignees err: %v", owner, repo, err))
	}
	return to.TextResult(slim.UserSummaries(users))
}

func addIssueLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	labels, err := params.GetInt64Slice(args, "labels")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issueLabels, _, err := client.Issues.AddIssueLabels(ctx, owner, repo, index, gitea_sdk.IssueLabelsOption{Labels: labels})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("add labels to %v/%v/issue/%v err: %v", owner, repo, index, err))
	}
	return to.TextResult(slim.Labels(issueLabels))
}

func replaceIssueLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	labels, err := params.GetInt64Slice(args, "labels")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issueLabels, _, err := client.Issues.ReplaceIssueLabels(ctx, owner, repo, index, gitea_sdk.IssueLabelsOption{Labels: labels})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("replace labels on %v/%v/issue/%v err: %v", owner, repo, index, err))
	}
	return to.TextResult(slim.Labels(issueLabels))
}

func clearIssueLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Issues.ClearIssueLabels(ctx, owner, repo, index)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("clear labels on %v/%v/issue/%v err: %v", owner, repo, index, err))
	}
	return to.TextResult("Labels cleared successfully")
}

func removeIssueLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	index, err := params.GetIndex(args, "issue_number")
	if err != nil {
		return to.ErrorResult(err)
	}
	labelID, err := params.GetIndex(args, "label_id")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Issues.DeleteIssueLabel(ctx, owner, repo, index, labelID)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("remove label %v from %v/%v/issue/%v err: %v", labelID, owner, repo, index, err))
	}
	return to.TextResult("Label removed successfully")
}
