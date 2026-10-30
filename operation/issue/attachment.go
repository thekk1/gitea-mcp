package issue

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gitea.com/gitea/gitea-mcp/internal/attachment"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/params"
	"gitea.com/gitea/gitea-mcp/internal/to"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func createAttachmentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	scope, err := attachmentScopeArgs(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	name, file, err := attachment.Upload(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	var att *gitea_sdk.Attachment
	if scope.issueNumber > 0 {
		att, _, err = client.Issues.CreateIssueAttachment(ctx, scope.owner, scope.repo, scope.issueNumber, file, name)
	} else {
		att, _, err = client.Issues.CreateIssueCommentAttachment(ctx, scope.owner, scope.repo, scope.commentID, file, name)
	}
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create attachment err: %v", err))
	}
	return to.TextResult(attachment.Slim(att))
}

func deleteAttachmentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	scope, err := attachmentScopeArgs(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	attachmentID, err := params.GetIndex(args, "attachment_id")
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	if scope.issueNumber > 0 {
		_, err = client.Issues.DeleteIssueAttachment(ctx, scope.owner, scope.repo, scope.issueNumber, attachmentID)
	} else {
		_, err = client.Issues.DeleteIssueCommentAttachment(ctx, scope.owner, scope.repo, scope.commentID, attachmentID)
	}
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete attachment err: %v", err))
	}
	return to.TextResult("attachment deleted")
}

func listAttachmentsFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	scope, err := attachmentScopeArgs(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	var attachments []*gitea_sdk.Attachment
	if scope.issueNumber > 0 {
		attachments, _, err = client.Issues.ListIssueAttachments(ctx, scope.owner, scope.repo, scope.issueNumber)
	} else {
		attachments, _, err = client.Issues.ListIssueCommentAttachments(ctx, scope.owner, scope.repo, scope.commentID)
	}
	if err != nil {
		return to.ErrorResult(fmt.Errorf("list attachments err: %v", err))
	}
	return to.TextResult(attachment.SlimList(attachments))
}

func getAttachmentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	att, err := lookupAttachment(ctx, args)
	if err != nil {
		return to.ErrorResult(err)
	}
	return to.TextResult(attachment.Slim(att))
}

func downloadAttachmentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := params.GetRepo(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	attachmentUUID := params.GetOptionalString(args, "attachment_uuid", "")
	var att *gitea_sdk.Attachment
	if strings.TrimSpace(attachmentUUID) == "" {
		if att, err = lookupAttachment(ctx, args); err != nil {
			return to.ErrorResult(err)
		}
	}
	return attachment.Download(ctx, owner, repo, att, attachmentUUID, params.GetOptionalString(args, "output_path", ""))
}

type attachmentScope struct {
	owner, repo            string
	issueNumber, commentID int64
}

func attachmentScopeArgs(args map[string]any) (attachmentScope, error) {
	owner, repo, err := params.GetRepo(args)
	if err != nil {
		return attachmentScope{}, err
	}
	scope := attachmentScope{
		owner:       owner,
		repo:        repo,
		issueNumber: params.GetOptionalInt(args, "issue_number", 0),
		commentID:   params.GetOptionalInt(args, "comment_id", 0),
	}
	if (scope.issueNumber > 0) == (scope.commentID > 0) {
		return attachmentScope{}, errors.New("exactly one of issue_number or comment_id is required")
	}
	return scope, nil
}

func lookupAttachment(ctx context.Context, args map[string]any) (*gitea_sdk.Attachment, error) {
	scope, err := attachmentScopeArgs(args)
	if err != nil {
		return nil, err
	}
	attachmentID := params.GetOptionalInt(args, "attachment_id", 0)
	if attachmentID <= 0 {
		return nil, errors.New("attachment_id is required")
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("get gitea client err: %v", err)
	}
	var att *gitea_sdk.Attachment
	if scope.issueNumber > 0 {
		att, _, err = client.Issues.GetIssueAttachment(ctx, scope.owner, scope.repo, scope.issueNumber, attachmentID)
	} else {
		att, _, err = client.Issues.GetIssueCommentAttachment(ctx, scope.owner, scope.repo, scope.commentID, attachmentID)
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment err: %v", err)
	}
	return att, nil
}
