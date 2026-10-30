package milestone

import (
	"context"
	"fmt"

	"gitea.com/gitea/gitea-mcp/internal/annotation"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/params"
	"gitea.com/gitea/gitea-mcp/internal/to"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("milestone")

const (
	MilestoneReadToolName  = "milestone_read"
	MilestoneWriteToolName = "milestone_write"
)

var (
	MilestoneReadTool = tool.NewDefinition(
		MilestoneReadToolName,
		"Read milestones.",
		annotation.ReadOnly("Read milestones"),
		tool.String("method", tool.Required(), tool.Enum("get", "list")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.Number("id", tool.Description("for 'get'")),
		tool.String("state", tool.Default("all")),
		tool.String("name", tool.Description("name filter (for 'list')")),
		tool.Number("page", tool.Default(1)),
		tool.Number("per_page", tool.Default(30)),
	)

	MilestoneWriteTool = tool.NewDefinition(
		MilestoneWriteToolName,
		"Write milestones.",
		annotation.Destructive("Create, update, or delete milestones"),
		tool.String("method", tool.Required(), tool.Enum("create", "update", "delete")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.Number("id", tool.Description("for 'update'/'delete'")),
		tool.String("title", tool.Description("for 'create'")),
		tool.String("description"),
		tool.String("due_on", tool.Description("due date")),
		tool.String("state", tool.Enum("open", "closed")),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool: MilestoneReadTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"get":  getMilestoneFn,
			"list": listMilestonesFn,
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool: MilestoneWriteTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"create": createMilestoneFn,
			"update": editMilestoneFn,
			"delete": deleteMilestoneFn,
		}),
	})
}

func getMilestoneFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	milestone, _, err := client.Repositories.GetMilestone(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/milestone/%v err: %v", owner, repo, id, err))
	}

	return to.TextResult(slimMilestone(milestone))
}

func listMilestonesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	state := params.GetOptionalString(args, "state", "all")
	name := params.GetOptionalString(args, "name", "")
	page, pageSize := params.GetPagination(args, 30)
	opt := gitea_sdk.ListMilestoneOption{
		State:    gitea_sdk.StateType(state),
		Name:     name,
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	milestones, _, err := client.Repositories.ListMilestones(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/milestones err: %v", owner, repo, err))
	}
	return to.TextResult(slimMilestones(milestones))
}

func createMilestoneFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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

	opt := gitea_sdk.CreateMilestoneOption{
		Title: title,
	}

	description, ok := args["description"].(string)
	if ok {
		opt.Description = description
	}
	opt.Deadline = params.GetOptionalTime(args, "due_on")

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	milestone, _, err := client.Repositories.CreateMilestone(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create %v/%v/milestone err: %v", owner, repo, err))
	}

	return to.TextResult(slimMilestone(milestone))
}

func editMilestoneFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}

	opt := gitea_sdk.EditMilestoneOption{
		Description: params.GetPresentStringPtr(args, "description"),
		Deadline:    params.GetOptionalTime(args, "due_on"),
	}
	if title, ok := args["title"].(string); ok {
		opt.Title = title
	}
	if state, ok := args["state"].(string); ok {
		s := gitea_sdk.StateType(state)
		opt.State = &s
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	milestone, _, err := client.Repositories.EditMilestone(ctx, owner, repo, id, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("edit %v/%v/milestone/%v err: %v", owner, repo, id, err))
	}

	return to.TextResult(slimMilestone(milestone))
}

func deleteMilestoneFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Repositories.DeleteMilestone(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete %v/%v/milestone/%v err: %v", owner, repo, id, err))
	}

	return to.TextResult("Milestone deleted successfully")
}
