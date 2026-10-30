package repo

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

// TagTool holds the tag-related tools (scope "tag").
var TagTool = tool.New("tag")

const (
	TagReadToolName  = "tag_read"
	TagWriteToolName = "tag_write"
)

var (
	TagReadTool = tool.NewDefinition(
		TagReadToolName,
		"Read tags.",
		annotation.ReadOnly("Read tags"),
		tool.String("method", tool.Required(), tool.Enum("list", "get")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.String("tag_name", tool.Description("for 'get'")),
		tool.Number("page", tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Default(20), tool.Minimum(1)),
	)

	TagWriteTool = tool.NewDefinition(
		TagWriteToolName,
		"Create or delete tags.",
		annotation.Destructive("Write tags"),
		tool.String("method", tool.Required(), tool.Enum("create", "delete")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.String("tag_name", tool.Description("for 'create', 'delete'")),
		tool.String("target", tool.Description("commitish for 'create'")),
		tool.String("message", tool.Description("annotation for 'create'")),
	)
)

func init() {
	TagTool.RegisterRead(tool.ServerTool{
		Tool: TagReadTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"list": listTagsFn,
			"get":  getTagFn,
		}),
	})
	TagTool.RegisterWrite(tool.ServerTool{
		Tool: TagWriteTool,
		Handler: tool.Dispatch(map[string]tool.Handler{
			"create": createTagFn,
			"delete": deleteTagFn,
		}),
	})
}

func createTagFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	tagName, err := params.GetString(args, "tag_name")
	if err != nil {
		return to.ErrorResult(err)
	}
	target, _ := args["target"].(string)
	message, _ := args["message"].(string)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, _, err = client.Repositories.CreateTag(ctx, owner, repo, gitea_sdk.CreateTagOption{
		TagName: tagName,
		Target:  target,
		Message: message,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create tag error: %v", err))
	}

	return to.TextResult("Tag Created")
}

func deleteTagFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	tagName, err := params.GetString(args, "tag_name")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Repositories.DeleteTag(ctx, owner, repo, tagName)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete tag error: %v", err))
	}

	return to.TextResult("Tag deleted")
}

func getTagFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	tagName, err := params.GetString(args, "tag_name")
	if err != nil {
		return to.ErrorResult(err)
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	tag, _, err := client.Repositories.GetTag(ctx, owner, repo, tagName)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get tag error: %v", err))
	}

	return to.TextResult(slimTag(tag))
}

func listTagsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	page := params.GetOptionalInt(args, "page", 1)
	pageSize := params.GetOptionalInt(args, "per_page", 20)

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	tags, _, err := client.Repositories.ListRepoTags(ctx, owner, repo, gitea_sdk.ListRepoTagsOptions{
		Page:     int(page),
		PageSize: int(pageSize),
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list tags error: %v", err))
	}

	return to.TextResult(slimTags(tags))
}
