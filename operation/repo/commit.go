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
	CommitReadToolName  = "commit_read"
	CommitWriteToolName = "commit_write"
)

var (
	CommitReadTool = tool.NewDefinition(
		CommitReadToolName,
		"Read commits with optional diff, combined statuses, and compare refs.",
		annotation.ReadOnly("Read commits"),
		tool.String("method", tool.Required(), tool.Enum("list", "get", "get_status", "compare")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.String("sha", tool.Description("required for 'get', optional starting SHA or branch for 'list'")),
		tool.String("path", tool.Description("only commits touching this path for 'list'")),
		tool.Number("page", tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Default(30), tool.Minimum(1)),
		tool.Boolean("include_diff", tool.Description("include unified diff for 'get'")),
		tool.String("ref", tool.Description("SHA, branch or tag, required for 'get_status'")),
		tool.Number("wait_seconds", tool.Description(fmt.Sprintf("for 'get_status': seconds to wait while pending, max %d", gitea.MaxStatusWaitSeconds))),
		tool.String("base", tool.Description("branch, tag or SHA, required for 'compare'")),
		tool.String("head", tool.Description("branch, tag or SHA, required for 'compare'")),
	)

	CommitWriteTool = tool.NewDefinition(
		CommitWriteToolName,
		"Create a commit status.",
		annotation.Write("Create a commit status"),
		tool.String("method", tool.Required(), tool.Enum("create_status")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.String("sha", tool.Description("required for 'create_status'")),
		tool.String("state", tool.Description("required for 'create_status'"), tool.Enum("pending", "success", "error", "failure", "warning")),
		tool.String("context", tool.Description("check name, e.g. 'ci/build'")),
		tool.String("description"),
		tool.String("target_url", tool.Description("link to details")),
	)
)

func init() {
	CommitTool.RegisterRead(tool.ServerTool{
		Tool: CommitReadTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"list":       listRepoCommitsFn,
			"get":        getCommitFn,
			"get_status": getCommitStatusFn,
			"compare":    compareCommitsFn,
		}),
	})
	CommitTool.RegisterWrite(tool.ServerTool{
		Tool: CommitWriteTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"create_status": createCommitStatusFn,
		}),
	})
}

func listRepoCommitsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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

func getCommitFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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

func compareCommitsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	Owner       string `json:"owner" required:"true"`
	Repo        string `json:"repo" required:"true"`
	Ref         string `json:"ref" required:"true"`
	WaitSeconds int64  `json:"wait_seconds"`
}

func getCommitStatusFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in getCommitStatusArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	status, err := gitea.WaitStatus(ctx, in.WaitSeconds, func() (*gitea_sdk.CombinedStatus, error) {
		status, _, err := client.Repositories.GetCombinedStatus(ctx, in.Owner, in.Repo, in.Ref)
		return status, err
	})
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

func createCommitStatusFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
