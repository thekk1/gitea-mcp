package repo

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strconv"
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

var Tool = tool.New("repository")

const (
	RepoReadToolName  = "repo_read"
	RepoWriteToolName = "repo_write"
)

var (
	RepoReadTool = tool.NewDefinition(
		RepoReadToolName,
		"Read a repository, list yours or an org's, and read branch protections.",
		annotation.ReadOnly("Read repositories"),
		tool.String("method", tool.Required(), tool.Enum("get", "list_my_repos", "list_org_repos", "list_branch_protections", "get_branch_protection")),
		tool.String("owner", tool.Description("required except list_my_repos/list_org_repos")),
		tool.String("repo", tool.Description("required for get and branch protections")),
		tool.String("org", tool.Description(`for "list_org_repos"`)),
		tool.Number("page", tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Default(30), tool.Minimum(1)),
		tool.String("rule_name", tool.Description("required for get_branch_protection")),
	)

	RepoWriteTool = tool.NewDefinition(
		RepoWriteToolName,
		"Write repositories (create, fork, settings, topics) and branch protections.",
		annotation.Destructive("Write repositories"),
		tool.String("method", tool.Required(), tool.Enum("create", "fork", "update", "create_branch_protection", "update_branch_protection", "delete_branch_protection")),
		tool.String("owner", tool.Description("required except create, source owner for fork")),
		tool.String("repo", tool.Description("required except create, source repo for fork")),
		tool.String("name", tool.Description("required for create, optional fork name")),
		tool.String("org", tool.Description("create/fork target, defaults to personal account")),
		tool.String("description"),
		tool.Boolean("private"),
		tool.Boolean("auto_init"),
		tool.Boolean("template"),
		tool.String("gitignores"),
		tool.String("license"),
		tool.String("readme"),
		tool.String("default_branch"),
		tool.String("issue_labels"),
		tool.String("trust_model", tool.Enum("default", "collaborator", "committer", "collaboratorcommitter")),
		tool.String("object_format_name", tool.Enum("sha1", "sha256")),
		tool.Object("options", tool.Description("update/branch protection API fields, required for update_branch_protection or update without topics, omitted fields unchanged")),
		tool.Array("topics", tool.Description("for 'update': replaces all topics"), tool.Items(map[string]any{"type": "string"})),
		tool.String("rule_name", tool.Description("required for branch protections, branch name or glob")),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{Tool: RepoReadTool, Handler: tool.Dispatch(map[string]tool.Handler{
		"get": repoGetFn,
		"list_my_repos": func(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
			return repoListFn(ctx, args, "user/repos")
		},
		"list_org_repos": func(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
			org, err := params.GetString(args, "org")
			if err != nil {
				return to.ErrorResult(err)
			}
			return repoListFn(ctx, args, "orgs/"+url.PathEscape(org)+"/repos")
		},
		"list_branch_protections": repoListBranchProtectionsFn,
		"get_branch_protection":   repoGetBranchProtectionFn,
	})})
	Tool.RegisterWrite(tool.ServerTool{Tool: RepoWriteTool, Handler: tool.Dispatch(map[string]tool.Handler{
		"create":                   repoCreateFn,
		"fork":                     repoForkFn,
		"update":                   repoUpdateFn,
		"create_branch_protection": repoCreateBranchProtectionFn,
		"update_branch_protection": repoUpdateBranchProtectionFn,
		"delete_branch_protection": repoDeleteBranchProtectionFn,
	})})
}

func repoListFn(ctx context.Context, args map[string]any, endpoint string) (*mcp.CallToolResult, error) {
	page, pageSize := params.GetPagination(args, 30)
	var repositories []*gitea_sdk.Repository
	if _, err := gitea.DoJSON(ctx, "GET", endpoint, url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(pageSize)}}, nil, &repositories); err != nil {
		return to.ErrorResult(fmt.Errorf("list repositories err: %v", err))
	}
	return to.TextResult(slim.Repos(repositories))
}

func repoGetFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	repoPath, err := repoAPIPath(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	var repository gitea_sdk.Repository
	if _, err := gitea.DoJSON(ctx, "GET", repoPath, nil, nil, &repository); err != nil {
		return to.ErrorResult(fmt.Errorf("get repo err: %v", err))
	}
	return to.TextResult(slimRepoSettings(&repository))
}

func repoListBranchProtectionsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	repoPath, err := repoAPIPath(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	var rules []map[string]any
	if _, err := gitea.DoJSON(ctx, "GET", repoPath+"/branch_protections", nil, nil, &rules); err != nil {
		return to.ErrorResult(fmt.Errorf("list branch protections err: %v", err))
	}
	for _, rule := range rules {
		omitZeroValues(rule)
	}
	return to.TextResult(rules)
}

func repoGetBranchProtectionFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	rulePath, err := branchProtectionPath(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	var rule map[string]any
	if _, err := gitea.DoJSON(ctx, "GET", rulePath, nil, nil, &rule); err != nil {
		return to.ErrorResult(fmt.Errorf("get branch protection err: %v", err))
	}
	return to.TextResult(omitZeroValues(rule))
}

func repoForkFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	repoPath, err := repoAPIPath(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	var repository gitea_sdk.Repository
	if _, err := gitea.DoJSON(ctx, "POST", repoPath+"/forks", nil, map[string]any{
		"organization": params.GetOptionalStringPtr(args, "org"),
		"name":         params.GetOptionalStringPtr(args, "name"),
	}, &repository); err != nil {
		return to.ErrorResult(fmt.Errorf("fork repository err: %v", err))
	}
	return to.TextResult(slim.Repo(&repository))
}

func repoUpdateFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	repoPath, err := repoAPIPath(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	options, err := optionsArg(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	rawTopics, hasTopics := args["topics"].([]any)
	if !hasTopics && len(options) == 0 {
		return to.ErrorResult(errors.New("options or topics is required"))
	}
	topics := params.GetStringSlice(args, "topics")
	if len(topics) != len(rawTopics) {
		return to.ErrorResult(errors.New("topics must be strings"))
	}
	var repository gitea_sdk.Repository
	if len(options) > 0 {
		if _, err := gitea.DoJSON(ctx, "PATCH", repoPath, nil, options, &repository); err != nil {
			return to.ErrorResult(fmt.Errorf("update repo err: %v", err))
		}
		if owner, name, ok := strings.Cut(repository.FullName, "/"); ok {
			repoPath = fmt.Sprintf("repos/%s/%s", url.PathEscape(owner), url.PathEscape(name)) // follows a rename in options
		}
	}
	if hasTopics {
		repository.Topics = topics
		if _, err := gitea.DoJSON(ctx, "PUT", repoPath+"/topics", nil, map[string]any{"topics": repository.Topics}, nil); err != nil {
			return to.ErrorResult(fmt.Errorf("set topics err: %v", err))
		}
		if len(options) == 0 {
			return to.TextResult("topics updated")
		}
	}
	return to.TextResult(slimRepoSettings(&repository))
}

func repoCreateBranchProtectionFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	repoPath, err := repoAPIPath(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	options, err := optionsArg(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	ruleName, err := params.GetString(args, "rule_name")
	if err != nil {
		return to.ErrorResult(err)
	}
	body := map[string]any{}
	maps.Copy(body, options)
	body["rule_name"] = ruleName
	var rule map[string]any
	if _, err := gitea.DoJSON(ctx, "POST", repoPath+"/branch_protections", nil, body, &rule); err != nil {
		return to.ErrorResult(fmt.Errorf("create branch protection err: %v", err))
	}
	return to.TextResult(omitZeroValues(rule))
}

func repoUpdateBranchProtectionFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	options, err := optionsArg(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	rulePath, err := branchProtectionPath(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	if len(options) == 0 {
		return to.ErrorResult(errors.New("options is required"))
	}
	var rule map[string]any
	if _, err := gitea.DoJSON(ctx, "PATCH", rulePath, nil, options, &rule); err != nil {
		return to.ErrorResult(fmt.Errorf("update branch protection err: %v", err))
	}
	return to.TextResult(omitZeroValues(rule))
}

func repoDeleteBranchProtectionFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	rulePath, err := branchProtectionPath(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	if _, err := gitea.DoJSON(ctx, "DELETE", rulePath, nil, nil, nil); err != nil {
		return to.ErrorResult(fmt.Errorf("delete branch protection err: %v", err))
	}
	return to.TextResult("branch protection deleted")
}

func repoCreateFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}
	body := map[string]any{"name": name}
	for _, key := range []string{"description", "private", "auto_init", "template", "gitignores", "license", "readme", "default_branch", "issue_labels", "trust_model", "object_format_name"} {
		if value, ok := args[key]; ok {
			body[key] = value
		}
	}
	endpoint := "user/repos"
	if organization := params.GetOptionalString(args, "org", ""); organization != "" {
		endpoint = "orgs/" + url.PathEscape(organization) + "/repos"
	}
	var repository gitea_sdk.Repository
	if _, err := gitea.DoJSON(ctx, "POST", endpoint, nil, body, &repository); err != nil {
		return to.ErrorResult(fmt.Errorf("create repository err: %v", err))
	}
	return to.TextResult(slim.Repo(&repository))
}

func optionsArg(args map[string]any) (map[string]any, error) {
	options, ok := args["options"].(map[string]any)
	if _, present := args["options"]; present && !ok {
		return nil, errors.New("options must be an object")
	}
	return options, nil
}

func repoAPIPath(args map[string]any) (string, error) {
	owner, repo, err := params.GetRepo(args)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo)), nil
}

func branchProtectionPath(args map[string]any) (string, error) {
	repoPath, err := repoAPIPath(args)
	if err != nil {
		return "", err
	}
	ruleName, err := params.GetString(args, "rule_name")
	if err != nil {
		return "", err
	}
	return repoPath + "/branch_protections/" + url.PathEscape(ruleName), nil
}

func slimRepoSettings(repository *gitea_sdk.Repository) map[string]any {
	settings := slim.Repo(repository)
	maps.Copy(settings, map[string]any{
		"website":                           repository.Website,
		"template":                          repository.Template,
		"mirror":                            repository.Mirror,
		"empty":                             repository.Empty,
		"has_issues":                        repository.HasIssues,
		"has_wiki":                          repository.HasWiki,
		"has_pull_requests":                 repository.HasPullRequests,
		"has_projects":                      repository.HasProjects,
		"has_releases":                      repository.HasReleases,
		"has_packages":                      repository.HasPackages,
		"has_actions":                       repository.HasActions,
		"allow_merge_commits":               repository.AllowMerge,
		"allow_rebase":                      repository.AllowRebase,
		"allow_rebase_explicit":             repository.AllowRebaseMerge,
		"allow_squash_merge":                repository.AllowSquash,
		"allow_fast_forward_only_merge":     repository.AllowFastForwardOnlyMerge,
		"default_merge_style":               repository.DefaultMergeStyle,
		"default_delete_branch_after_merge": repository.DefaultDeleteBranchAfterMerge,
		"permissions":                       repository.Permissions,
	})
	if repository.Parent != nil {
		settings["parent"] = repository.Parent.FullName
	}
	return settings
}

func omitZeroValues(rule map[string]any) map[string]any {
	maps.DeleteFunc(rule, func(_key string, value any) bool {
		switch typed := value.(type) {
		case nil:
			return true
		case string:
			return typed == ""
		case []any:
			return len(typed) == 0
		}
		return false
	})
	return rule
}
