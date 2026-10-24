package repo

import (
	"context"
	"fmt"

	"gitea.com/gitea/gitea-mcp/internal/annotation"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/params"
	"gitea.com/gitea/gitea-mcp/internal/slim"
	"gitea.com/gitea/gitea-mcp/internal/to"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CommitTool holds the commit-related tools (scope "commit").
var CommitTool = tool.New("commit")

const (
	ListRepoCommitsToolName    = "list_commits"
	GetCommitToolName          = "get_commit"
	CompareCommitsToolName     = "compare_commits"
	GetCommitStatusToolName    = "get_commit_status"
	CreateCommitStatusToolName = "create_commit_status"
)

var (
	ListRepoCommitsTool = tool.NewDefinition(
		ListRepoCommitsToolName,
		"List commits in a repository, optionally starting from a specific branch or SHA and filtered to commits touching a given file path.",
		annotation.ReadOnly("List repository commits"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("sha", tool.Description("starting SHA or branch")),
		tool.String("path", tool.Description("only commits touching this path")),
		tool.Number("page", tool.Description(params.PageDesc), tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Description(params.PaginationDesc), tool.Default(30), tool.Minimum(1)),
	)

	GetCommitTool = tool.NewDefinition(
		GetCommitToolName,
		"Get details for a single commit in a repository by its SHA, optionally with its unified diff.",
		annotation.ReadOnly("Get commit details"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("sha", tool.Required()),
		tool.Boolean("include_diff", tool.Description("include the unified diff")),
	)

	CompareCommitsTool = tool.NewDefinition(
		CompareCommitsToolName,
		"Compare two refs in a repository: the commits in head that are not in base, and the files they touch.",
		annotation.ReadOnly("Compare commits"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("base", tool.Required(), tool.Description("branch, tag or SHA")),
		tool.String("head", tool.Required(), tool.Description("branch, tag or SHA")),
	)

	GetCommitStatusTool = tool.NewDefinition(
		GetCommitStatusToolName,
		"Get the combined status of a commit, branch or tag: the overall state and the latest status per context, e.g. CI checks.",
		annotation.ReadOnly("Get commit status"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("ref", tool.Required(), tool.Description("SHA, branch or tag")),
	)

	CreateCommitStatusTool = tool.NewDefinition(
		CreateCommitStatusToolName,
		"Create a status on a commit, e.g. the result of a CI check or review bot.",
		annotation.Write("Create a commit status"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("sha", tool.Required()),
		tool.String("state", tool.Required(), tool.Enum("pending", "success", "error", "failure", "warning")),
		tool.String("context", tool.Description("check name, e.g. 'ci/build'")),
		tool.String("description"),
		tool.String("target_url", tool.Description("link to details")),
	)
)

func init() {
	CommitTool.RegisterRead(tool.ServerTool{
		Tool:    ListRepoCommitsTool,
		Handler: ListRepoCommitsFn,
	})
	CommitTool.RegisterRead(tool.ServerTool{
		Tool:    GetCommitTool,
		Handler: GetCommitFn,
	})
	CommitTool.RegisterRead(tool.ServerTool{
		Tool:    CompareCommitsTool,
		Handler: CompareCommitsFn,
	})
	CommitTool.RegisterRead(tool.ServerTool{
		Tool:    GetCommitStatusTool,
		Handler: GetCommitStatusFn,
	})
	CommitTool.RegisterWrite(tool.ServerTool{
		Tool:    CreateCommitStatusTool,
		Handler: CreateCommitStatusFn,
	})
}

func ListRepoCommitsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	sha, _ := args["sha"].(string)
	path, _ := args["path"].(string)
	opt := gitea_sdk.ListCommitOptions{
		Page:     page,
		PageSize: pageSize,
		SHA:      sha,
		Path:     path,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	commits, _, err := client.Repositories.ListRepoCommits(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list repo commits err: %v", err))
	}
	return to.TextResult(slim.Commits(commits))
}

type getCommitArgs struct {
	Owner       string `json:"owner" required:"true"`
	Repo        string `json:"repo" required:"true"`
	SHA         string `json:"sha" required:"true"`
	IncludeDiff bool   `json:"include_diff"`
}

func GetCommitFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in getCommitArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	commit, _, err := client.Repositories.GetSingleCommit(ctx, in.Owner, in.Repo, in.SHA)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get commit %v err: %v", in.SHA, err))
	}
	result := slim.Commit(commit)
	if in.IncludeDiff {
		diff, _, err := client.Repositories.GetCommitDiff(ctx, in.Owner, in.Repo, in.SHA)
		if err != nil {
			return to.ErrorResult(fmt.Errorf("get commit %v diff err: %v", in.SHA, err))
		}
		result["diff"] = string(diff)
	}
	return to.TextResult(result)
}

type compareCommitsArgs struct {
	Owner string `json:"owner" required:"true"`
	Repo  string `json:"repo" required:"true"`
	Base  string `json:"base" required:"true"`
	Head  string `json:"head" required:"true"`
}

func CompareCommitsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in compareCommitsArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	compare, _, err := client.Repositories.CompareCommits(ctx, in.Owner, in.Repo, in.Base, in.Head)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("compare %v...%v err: %v", in.Base, in.Head, err))
	}
	return to.TextResult(map[string]any{
		"total_commits": compare.TotalCommits,
		"commits":       slim.Commits(compare.Commits),
		"files":         changedFiles(compare.Commits),
	})
}

type getCommitStatusArgs struct {
	Owner string `json:"owner" required:"true"`
	Repo  string `json:"repo" required:"true"`
	Ref   string `json:"ref" required:"true"`
}

func GetCommitStatusFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in getCommitStatusArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	status, _, err := client.Repositories.GetCombinedStatus(ctx, in.Owner, in.Repo, in.Ref)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v status err: %v", in.Ref, err))
	}
	return to.TextResult(slimCombinedStatus(status))
}

type createCommitStatusArgs struct {
	Owner       string                `json:"owner" required:"true"`
	Repo        string                `json:"repo" required:"true"`
	SHA         string                `json:"sha" required:"true"`
	State       gitea_sdk.StatusState `json:"state" required:"true"`
	Context     string                `json:"context"`
	Description string                `json:"description"`
	TargetURL   string                `json:"target_url"`
}

func CreateCommitStatusFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in createCommitStatusArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	status, _, err := client.Repositories.CreateStatus(ctx, in.Owner, in.Repo, in.SHA, gitea_sdk.CreateStatusOption{
		State:       in.State,
		TargetURL:   in.TargetURL,
		Description: in.Description,
		Context:     in.Context,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create commit %v status err: %v", in.SHA, err))
	}
	return to.TextResult(slimStatus(status))
}
