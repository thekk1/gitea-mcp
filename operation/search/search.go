package search

import (
	"context"
	"fmt"
	"strings"

	"gitea.com/gitea/gitea-mcp/internal/annotation"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/params"
	"gitea.com/gitea/gitea-mcp/internal/slim"
	"gitea.com/gitea/gitea-mcp/internal/to"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("search")

const (
	SearchUsersToolName    = "search_users"
	SearchOrgTeamsToolName = "search_org_teams"
	SearchReposToolName    = "search_repos"
	SearchIssuesToolName   = "search_issues"
)

var (
	SearchUsersTool = tool.NewDefinition(
		SearchUsersToolName,
		"Search users by username or full name.",
		annotation.ReadOnly("Search users"),
		tool.String("query", tool.Required()),
		tool.Number("page", tool.Default(1)),
		tool.Number("per_page", tool.Default(30)),
	)

	SearchOrgTeamsTool = tool.NewDefinition(
		SearchOrgTeamsToolName,
		"Search an organization's teams by name.",
		annotation.ReadOnly("Search organization teams"),
		tool.String("org", tool.Required()),
		tool.String("query", tool.Required()),
		tool.Boolean("include_description"),
		tool.Number("page", tool.Default(1)),
		tool.Number("per_page", tool.Default(30)),
	)

	SearchReposTool = tool.NewDefinition(
		SearchReposToolName,
		"Search repositories by keyword.",
		annotation.ReadOnly("Search repositories"),
		tool.String("query", tool.Required()),
		tool.Boolean("keyword_is_topic"),
		tool.Boolean("keyword_in_description"),
		tool.Number("owner_id"),
		tool.Boolean("is_private"),
		tool.Boolean("is_archived"),
		tool.String("sort"),
		tool.String("order"),
		tool.Number("page", tool.Default(1)),
		tool.Number("per_page", tool.Default(30)),
	)

	SearchIssuesTool = tool.NewDefinition(
		SearchIssuesToolName,
		"Full-text search of issues and pull requests across repositories.",
		annotation.ReadOnly("Search issues"),
		tool.String("query", tool.Required()),
		tool.String("state", tool.Enum("open", "closed", "all")),
		tool.String("type", tool.Enum("issues", "pulls")),
		tool.String("labels", tool.Description("comma-separated")),
		tool.String("owner", tool.Description("filter by owner")),
		tool.Number("page", tool.Default(1)),
		tool.Number("per_page", tool.Default(30)),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    SearchUsersTool,
		Handler: UsersFn,
	})
	Tool.RegisterRead(tool.ServerTool{
		Tool:    SearchOrgTeamsTool,
		Handler: OrgTeamsFn,
	})
	Tool.RegisterRead(tool.ServerTool{
		Tool:    SearchReposTool,
		Handler: ReposFn,
	})
	Tool.RegisterRead(tool.ServerTool{
		Tool:    SearchIssuesTool,
		Handler: IssuesFn,
	})
}

func UsersFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	keyword, err := params.GetString(args, "query")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.SearchUsersOption{
		KeyWord:  keyword,
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	users, _, err := client.Users.SearchUsers(ctx, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("search users err: %v", err))
	}
	return to.TextResult(slimUserDetails(users))
}

func OrgTeamsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	query, err := params.GetString(args, "query")
	if err != nil {
		return to.ErrorResult(err)
	}
	includeDescription, _ := args["include_description"].(bool)
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.SearchTeamsOptions{
		Query:              query,
		IncludeDescription: includeDescription,
		Page:               page,
		PageSize:           pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	teams, _, err := client.Organizations.SearchOrgTeams(ctx, org, &opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("search organization teams error: %v", err))
	}
	return to.TextResult(slimTeams(teams))
}

func ReposFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	keyword, err := params.GetString(args, "query")
	if err != nil {
		return to.ErrorResult(err)
	}
	keywordIsTopic, _ := args["keyword_is_topic"].(bool)
	keywordInDescription, _ := args["keyword_in_description"].(bool)
	sort, _ := args["sort"].(string)
	order, _ := args["order"].(string)
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.SearchRepoOptions{
		Keyword:              keyword,
		KeywordIsTopic:       keywordIsTopic,
		KeywordInDescription: keywordInDescription,
		OwnerID:              params.GetOptionalInt(args, "owner_id", 0),
		IsPrivate:            params.GetOptionalBoolPtr(args, "is_private"),
		IsArchived:           params.GetOptionalBoolPtr(args, "is_archived"),
		Sort:                 sort,
		Order:                order,
		Page:                 page,
		PageSize:             pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	repos, _, err := client.Repositories.SearchRepos(ctx, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("search repos error: %v", err))
	}
	return to.TextResult(slim.Repos(repos))
}

func IssuesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	query, err := params.GetString(args, "query")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)

	opt := gitea_sdk.ListIssueOption{
		KeyWord:  query,
		Page:     page,
		PageSize: pageSize,
	}
	if state, ok := args["state"].(string); ok {
		opt.State = gitea_sdk.StateType(state)
	}
	if issueType, ok := args["type"].(string); ok {
		opt.Type = gitea_sdk.IssueType(issueType)
	}
	if labels, ok := args["labels"].(string); ok && labels != "" {
		opt.Labels = strings.Split(labels, ",")
	}
	if owner, ok := args["owner"].(string); ok {
		opt.Owner = owner
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	issues, _, err := client.Issues.ListIssues(ctx, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("search issues err: %v", err))
	}
	return to.TextResult(slimIssues(issues))
}
