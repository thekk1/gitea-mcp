package repo

import (
	"context"
	"fmt"

	"gitea.com/gitea/gitea-mcp/internal/annotation"
	"gitea.com/gitea/gitea-mcp/internal/attachment"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/params"
	"gitea.com/gitea/gitea-mcp/internal/to"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReleaseTool holds the release-related tools (scope "release").
var ReleaseTool = tool.New("release")

const (
	ReleaseReadToolName  = "release_read"
	ReleaseWriteToolName = "release_write"
)

var (
	ReleaseReadTool = tool.NewDefinition(
		ReleaseReadToolName,
		"Read releases by ID, tag or latest, list or download their assets.",
		annotation.ReadOnly("Read releases"),
		tool.String("method", tool.Required(), tool.Enum("list", "get", "get_by_tag", "get_latest", "list_assets", "get_asset", "download_asset")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.Number("id", tool.Description("required for 'get' and asset methods")),
		tool.String("tag", tool.Description("required for 'get_by_tag'")),
		tool.Boolean("is_draft", tool.Description("for 'list'")),
		tool.Boolean("is_pre_release", tool.Description("for 'list'")),
		tool.Number("attachment_id", tool.Description("required for 'get_asset'/'download_asset'")),
		tool.String("output_path", tool.Description("for 'download_asset', optional local file path")),
		tool.Number("page", tool.Default(1), tool.Minimum(1)),
		tool.Number("per_page", tool.Default(20), tool.Minimum(1)),
	)
	ReleaseWriteTool = tool.NewDefinition(
		ReleaseWriteToolName,
		"Write releases, upload or delete their assets.",
		annotation.Destructive("Write releases"),
		tool.String("method", tool.Required(), tool.Enum("create", "update", "delete", "upload_asset", "delete_asset")),
		tool.String("owner", tool.Required()),
		tool.String("repo", tool.Required()),
		tool.Number("id", tool.Description("required except for 'create'")),
		tool.String("tag_name", tool.Description("required for 'create', optional for 'update'")),
		tool.String("target", tool.Description("commitish, required for 'create', optional for 'update'")),
		tool.String("title", tool.Description("required for 'create', optional for 'update'")),
		tool.String("body", tool.Description("for 'create'/'update', omitted or empty fields stay unchanged on update")),
		tool.Boolean("is_draft", tool.Description("for 'create'/'update'")),
		tool.Boolean("is_pre_release", tool.Description("for 'create'/'update'")),
		tool.Number("attachment_id", tool.Description("required for 'delete_asset'")),
		tool.String("name", tool.Description("filename, required for 'upload_asset'")),
		tool.String("content", tool.Description("base64, required for 'upload_asset'")),
	)
)

func init() {
	ReleaseTool.RegisterRead(tool.ServerTool{Tool: ReleaseReadTool, Handler: tool.Dispatch(map[string]tool.Handler{
		"list":           listReleasesFn,
		"get":            getReleaseFn,
		"get_by_tag":     getReleaseByTagFn,
		"get_latest":     getLatestReleaseFn,
		"list_assets":    listReleaseAssetsFn,
		"get_asset":      getReleaseAssetFn,
		"download_asset": downloadReleaseAssetFn,
	})})
	ReleaseTool.RegisterWrite(tool.ServerTool{Tool: ReleaseWriteTool, Handler: tool.Dispatch(map[string]tool.Handler{
		"create":       createReleaseFn,
		"update":       updateReleaseFn,
		"delete":       deleteReleaseFn,
		"upload_asset": uploadReleaseAssetFn,
		"delete_asset": deleteReleaseAssetFn,
	})})
}

func releaseArgs(ctx context.Context, args map[string]any) (string, string, *gitea_sdk.Client, error) {
	owner, repo, err := params.GetRepo(args)
	if err != nil {
		return "", "", nil, err
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return "", "", nil, fmt.Errorf("get gitea client err: %v", err)
	}
	return owner, repo, client, nil
}

func listReleaseAssetsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 20)
	assets, _, err := client.Releases.ListReleaseAttachments(ctx, owner, repo, id, gitea_sdk.ListReleaseAttachmentsOptions{
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list release assets error: %v", err))
	}
	return to.TextResult(attachment.SlimList(assets))
}

func uploadReleaseAssetFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}
	name, content, err := attachment.Upload(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	asset, _, err := client.Releases.CreateReleaseAttachment(ctx, owner, repo, id, content, name)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("upload release asset error: %v", err))
	}
	return to.TextResult(attachment.Slim(asset))
}

func deleteReleaseAssetFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}
	attachmentID, err := params.GetIndex(args, "attachment_id")
	if err != nil {
		return to.ErrorResult(err)
	}
	if _, err := client.Releases.DeleteReleaseAttachment(ctx, owner, repo, id, attachmentID); err != nil {
		return to.ErrorResult(fmt.Errorf("delete release asset error: %v", err))
	}
	return to.TextResult("Release asset deleted successfully")
}

func releaseAsset(ctx context.Context, args map[string]any, owner, repo string, client *gitea_sdk.Client) (*gitea_sdk.Attachment, error) {
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return nil, err
	}
	attachmentID, err := params.GetIndex(args, "attachment_id")
	if err != nil {
		return nil, err
	}
	asset, _, err := client.Releases.GetReleaseAttachment(ctx, owner, repo, id, attachmentID)
	if err != nil {
		return nil, fmt.Errorf("get release asset error: %v", err)
	}
	return asset, nil
}

func getReleaseAssetFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	asset, err := releaseAsset(ctx, args, owner, repo, client)
	if err != nil {
		return to.ErrorResult(err)
	}
	return to.TextResult(attachment.Slim(asset))
}

func downloadReleaseAssetFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	asset, err := releaseAsset(ctx, args, owner, repo, client)
	if err != nil {
		return to.ErrorResult(err)
	}
	return attachment.Download(ctx, owner, repo, asset, "", params.GetOptionalString(args, "output_path", ""))
}

func createReleaseFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	tagName, err := params.GetString(args, "tag_name")
	if err != nil {
		return to.ErrorResult(err)
	}
	target, err := params.GetString(args, "target")
	if err != nil {
		return to.ErrorResult(err)
	}
	title, err := params.GetString(args, "title")
	if err != nil {
		return to.ErrorResult(err)
	}
	isDraft, _ := args["is_draft"].(bool)
	isPreRelease, _ := args["is_pre_release"].(bool)
	body, _ := args["body"].(string)

	_, _, err = client.Releases.CreateRelease(ctx, owner, repo, gitea_sdk.CreateReleaseOption{
		TagName:      tagName,
		Target:       target,
		Title:        title,
		Note:         body,
		IsDraft:      isDraft,
		IsPrerelease: isPreRelease,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create release error: %v", err))
	}

	return to.TextResult("Release Created")
}

func updateReleaseFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}

	release, _, err := client.Releases.EditRelease(ctx, owner, repo, id, gitea_sdk.EditReleaseOption{
		TagName:      params.GetOptionalString(args, "tag_name", ""),
		Target:       params.GetOptionalString(args, "target", ""),
		Title:        params.GetOptionalString(args, "title", ""),
		Note:         params.GetOptionalString(args, "body", ""),
		IsDraft:      params.GetOptionalBoolPtr(args, "is_draft"),
		IsPrerelease: params.GetOptionalBoolPtr(args, "is_pre_release"),
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("update release error: %v", err))
	}

	return to.TextResult(slimRelease(release))
}

func deleteReleaseFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}

	_, err = client.Releases.DeleteRelease(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete release error: %v", err))
	}

	return to.TextResult("Release deleted successfully")
}

func getReleaseFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	id, err := params.GetIndex(args, "id")
	if err != nil {
		return to.ErrorResult(err)
	}

	release, _, err := client.Releases.GetRelease(ctx, owner, repo, id)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get release error: %v", err))
	}

	return to.TextResult(slimRelease(release))
}

func getReleaseByTagFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	tag, err := params.GetString(args, "tag")
	if err != nil {
		return to.ErrorResult(err)
	}

	release, _, err := client.Releases.GetReleaseByTag(ctx, owner, repo, tag)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get release by tag error: %v", err))
	}

	return to.TextResult(slimRelease(release))
}

func getLatestReleaseFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}

	release, _, err := client.Releases.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get latest release error: %v", err))
	}

	return to.TextResult(slimRelease(release))
}

func listReleasesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, client, err := releaseArgs(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	page, pageSize := params.GetPagination(args, 20)

	releases, _, err := client.Releases.ListReleases(ctx, owner, repo, gitea_sdk.ListReleasesOptions{
		Page:         page,
		PageSize:     pageSize,
		IsDraft:      params.GetOptionalBoolPtr(args, "is_draft"),
		IsPreRelease: params.GetOptionalBoolPtr(args, "is_pre_release"),
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list releases error: %v", err))
	}

	return to.TextResult(slimReleases(releases))
}
