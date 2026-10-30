package repo

import (
	"context"
	"errors"
	"fmt"

	"gitea.com/gitea/gitea-mcp/internal/annotation"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/params"
	"gitea.com/gitea/gitea-mcp/internal/to"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// BranchTool holds the branch-related tools (scope "branch").
var BranchTool = tool.New("branch")

const (
	BranchReadToolName  = "branch_read"
	BranchWriteToolName = "branch_write"
)

var (
	BranchReadTool = tool.NewDefinition(
		BranchReadToolName,
		"Read branches.",
		annotation.ReadOnly("Read branches"),
		tool.String("method", tool.Required(), tool.Enum("list", "get")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.String("branch", tool.Description("required for 'get'")),
		tool.Number("page", tool.Default(1)),
		tool.Number("per_page", tool.Default(30)),
	)

	BranchWriteTool = tool.NewDefinition(
		BranchWriteToolName,
		"Create, delete or rename branches.",
		annotation.Destructive("Create, delete, or rename branches"),
		tool.String("method", tool.Required(), tool.Enum("create", "delete", "rename")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.String("branch", tool.Description("required for 'create', 'delete', 'rename'")),
		tool.String("old_branch", tool.Description("source for 'create' (default: repo default)")),
		tool.String("new_name", tool.Description("required for 'rename'")),
	)
)

func init() {
	BranchTool.RegisterRead(tool.ServerTool{
		Tool: BranchReadTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"list": listBranchesFn,
			"get":  branchGetFn,
		}),
	})
	BranchTool.RegisterWrite(tool.ServerTool{
		Tool: BranchWriteTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"create": createBranchFn,
			"delete": deleteBranchFn,
			"rename": renameBranchFn,
		}),
	})
}

func branchGetFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in branchArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	branch, _, err := client.Repositories.GetRepoBranch(ctx, in.Owner, in.Repo, in.Branch)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get branch error: %v", err))
	}
	return to.TextResult(slimBranch(branch))
}

type createBranchArgs struct {
	Owner     string `json:"owner" required:"true"`
	Repo      string `json:"repo" required:"true"`
	Branch    string `json:"branch" required:"true"`
	OldBranch string `json:"old_branch"`
}

func createBranchFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in createBranchArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, _, err = client.Repositories.CreateBranch(ctx, in.Owner, in.Repo, gitea_sdk.CreateBranchOption{
		BranchName:    in.Branch,
		OldBranchName: in.OldBranch,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create branch error: %v", err))
	}

	return to.TextResult("Branch Created")
}

type branchArgs struct {
	Owner  string `json:"owner" required:"true"`
	Repo   string `json:"repo" required:"true"`
	Branch string `json:"branch" required:"true"`
}

func deleteBranchFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in branchArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, _, err = client.Repositories.DeleteRepoBranch(ctx, in.Owner, in.Repo, in.Branch)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete branch error: %v", err))
	}

	return to.TextResult("Branch Deleted")
}

type listBranchesArgs struct {
	Owner string `json:"owner" required:"true"`
	Repo  string `json:"repo" required:"true"`
}

func listBranchesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in listBranchesArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.ListRepoBranchesOptions{
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	branches, _, err := client.Repositories.ListRepoBranches(ctx, in.Owner, in.Repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list branches error: %v", err))
	}

	return to.TextResult(slimBranches(branches))
}

func renameBranchFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	branch, err := params.GetString(args, "branch")
	if err != nil {
		return to.ErrorResult(err)
	}
	newName, err := params.GetString(args, "new_name")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	successful, _, err := client.Repositories.RenameRepoBranch(ctx, owner, repo, branch, gitea_sdk.RenameRepoBranchOption{Name: newName})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("rename branch error: %v", err))
	}
	if !successful {
		return to.ErrorResult(errors.New("rename branch error: unsuccessful"))
	}

	return to.TextResult("Branch renamed")
}
