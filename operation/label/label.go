package label

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

var Tool = tool.New("label")

const (
	LabelReadToolName  = "label_read"
	LabelWriteToolName = "label_write"
)

var (
	LabelReadTool = tool.NewDefinition(
		LabelReadToolName,
		"Read repo or org labels.",
		annotation.ReadOnly("Read labels"),
		tool.String("method", tool.Required(), tool.Enum("list_repo_labels", "get_repo_label", "list_org_labels")),
		tool.String("owner", tool.Description("for repo methods")),
		tool.String("repo", tool.Description("for repo methods")),
		tool.String("org", tool.Description("for org methods")),
		tool.Number("id", tool.Description("label ID (for 'get_repo_label')")),
		tool.Number("page", tool.Default(1)),
		tool.Number("per_page", tool.Default(30)),
	)

	LabelWriteTool = tool.NewDefinition(
		LabelWriteToolName,
		"Write repo or org labels.",
		annotation.Destructive("Create, update, or delete labels"),
		tool.String("method", tool.Required(), tool.Enum("create_repo_label", "update_repo_label", "delete_repo_label", "create_org_label", "update_org_label", "delete_org_label")),
		tool.String("owner", tool.Description("for repo methods")),
		tool.String("repo", tool.Description("for repo methods")),
		tool.String("org", tool.Description("for org methods")),
		tool.Number("id", tool.Description("for edit/delete")),
		tool.String("name", tool.Description("required for create")),
		tool.String("color", tool.Description("hex (#RRGGBB); required for create")),
		tool.String("description"),
		tool.Boolean("exclusive", tool.Description("mutually exclusive with labels of the same scope, e.g. 'priority/'")),
		tool.Boolean("is_archived", tool.Description("archived (repo only)")),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool: LabelReadTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"list_repo_labels": listRepoLabelsFn,
			"get_repo_label":   getRepoLabelFn,
			"list_org_labels":  listOrgLabelsFn,
		}),
	})
	Tool.RegisterWrite(tool.ServerTool{
		Tool: LabelWriteTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"create_repo_label": createRepoLabelFn,
			"update_repo_label": editRepoLabelFn,
			"delete_repo_label": deleteRepoLabelFn,
			"create_org_label":  createOrgLabelFn,
			"update_org_label":  editOrgLabelFn,
			"delete_org_label":  deleteOrgLabelFn,
		}),
	})
}

func listRepoLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)

	opt := gitea_sdk.ListLabelsOptions{
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	labels, _, err := client.Repositories.ListRepoLabels(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list %v/%v/labels err: %v", owner, repo, err))
	}
	return to.TextResult(slim.Labels(labels))
}

func getRepoLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	label, _, err := client.Repositories.GetRepoLabel(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get %v/%v/label/%v err: %v", owner, repo, id, err))
	}
	return to.TextResult(slim.Label(label))
}

func createRepoLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}
	color, err := params.GetString(args, "color")
	if err != nil {
		return to.ErrorResult(err)
	}
	description, _ := args["description"].(string) // Optional
	exclusive, _ := args["exclusive"].(bool)
	isArchived, _ := args["is_archived"].(bool)

	opt := gitea_sdk.CreateLabelOption{
		Name:        name,
		Color:       color,
		Description: description,
		Exclusive:   exclusive,
		IsArchived:  isArchived,
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	label, _, err := client.Repositories.CreateLabel(ctx, owner, repo, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create %v/%v/label err: %v", owner, repo, err))
	}
	return to.TextResult(slim.Label(label))
}

func editRepoLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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

	opt := gitea_sdk.EditLabelOption{
		Name:        params.GetOptionalStringPtr(args, "name"),
		Color:       params.GetOptionalStringPtr(args, "color"),
		Description: params.GetPresentStringPtr(args, "description"),
		Exclusive:   params.GetOptionalBoolPtr(args, "exclusive"),
		IsArchived:  params.GetOptionalBoolPtr(args, "is_archived"),
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	label, _, err := client.Repositories.EditLabel(ctx, owner, repo, id, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("edit %v/%v/label/%v err: %v", owner, repo, id, err))
	}
	return to.TextResult(slim.Label(label))
}

func deleteRepoLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	_, err = client.Repositories.DeleteLabel(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete %v/%v/label/%v err: %v", owner, repo, id, err))
	}
	return to.TextResult("Label deleted successfully")
}

func listOrgLabelsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 30)

	opt := gitea_sdk.ListOrgLabelsOptions{
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	labels, _, err := client.Organizations.ListOrgLabels(ctx, org, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list %v/labels err: %v", org, err))
	}
	return to.TextResult(slim.Labels(labels))
}

func createOrgLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, err := params.GetString(args, "name")
	if err != nil {
		return to.ErrorResult(err)
	}
	color, err := params.GetString(args, "color")
	if err != nil {
		return to.ErrorResult(err)
	}
	description, _ := args["description"].(string)
	exclusive, _ := args["exclusive"].(bool)

	opt := gitea_sdk.CreateOrgLabelOption{
		Name:        name,
		Color:       color,
		Description: description,
		Exclusive:   exclusive,
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	label, _, err := client.Organizations.CreateOrgLabel(ctx, org, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create %v/labels err: %v", org, err))
	}
	return to.TextResult(slim.Label(label))
}

func editOrgLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}

	opt := gitea_sdk.EditOrgLabelOption{
		Name:        params.GetOptionalStringPtr(args, "name"),
		Color:       params.GetOptionalStringPtr(args, "color"),
		Description: params.GetPresentStringPtr(args, "description"),
		Exclusive:   params.GetOptionalBoolPtr(args, "exclusive"),
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	label, _, err := client.Organizations.EditOrgLabel(ctx, org, id, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("edit %v/labels/%v err: %v", org, id, err))
	}
	return to.TextResult(slim.Label(label))
}

func deleteOrgLabelFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	org, err := params.GetString(args, "org")
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
	_, err = client.Organizations.DeleteOrgLabel(ctx, org, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete %v/labels/%v err: %v", org, id, err))
	}
	return to.TextResult("Label deleted successfully")
}
