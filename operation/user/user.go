package user

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

const (
	GetMyUserInfoToolName      = "get_me"
	GetUserOrgsToolName        = "get_user_orgs"
	CreateOrganizationToolName = "create_organization"
)

var Tool = tool.New("user")

var (
	GetMyUserInfoTool = tool.NewDefinition(
		GetMyUserInfoToolName,
		"Get the authenticated user.",
		annotation.ReadOnly("Get current user information"),
	)

	GetUserOrgsTool = tool.NewDefinition(
		GetUserOrgsToolName,
		"List your organizations.",
		annotation.ReadOnly("Get user organizations"),
		tool.Number("page", tool.Default(1)),
		tool.Number("per_page", tool.Default(30)),
	)

	CreateOrganizationTool = tool.NewDefinition(
		CreateOrganizationToolName,
		"Create an organization.",
		annotation.Write("Create an organization"),
		tool.String("username", tool.Required(), tool.Description("organization name")),
		tool.String("full_name"),
		tool.String("description"),
		tool.String("visibility", tool.Default("public"), tool.Enum("public", "limited", "private")),
		tool.String("website"),
	)
)

func init() {
	Tool.RegisterRead(tool.ServerTool{Tool: GetMyUserInfoTool, Handler: GetUserInfoFn})
	Tool.RegisterRead(tool.ServerTool{Tool: GetUserOrgsTool, Handler: GetUserOrgsFn})
	Tool.RegisterWrite(tool.ServerTool{Tool: CreateOrganizationTool, Handler: CreateOrganizationFn})
}

func GetUserInfoFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	user, _, err := client.Users.GetMyUserInfo(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get user info err: %v", err))
	}
	return to.TextResult(slim.UserDetail(user))
}

func GetUserOrgsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	page, pageSize := params.GetPagination(args, 30)

	opt := gitea_sdk.ListOrgsOptions{
		Page:     page,
		PageSize: pageSize,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	orgs, _, err := client.Organizations.ListMyOrgs(ctx, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get user orgs err: %v", err))
	}
	return to.TextResult(slimOrgs(orgs))
}

func CreateOrganizationFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	username, err := params.GetString(args, "username")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	organization, _, err := client.Organizations.CreateOrg(ctx, gitea_sdk.CreateOrgOption{
		Name:        username,
		FullName:    params.GetOptionalString(args, "full_name", ""),
		Description: params.GetOptionalString(args, "description", ""),
		Visibility:  gitea_sdk.VisibleType(params.GetOptionalString(args, "visibility", "public")),
		Website:     params.GetOptionalString(args, "website", ""),
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create organization %q err: %v", username, err))
	}
	return to.TextResult(slimOrg(organization))
}
