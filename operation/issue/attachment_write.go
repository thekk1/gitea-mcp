package issue

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const AttachmentWriteToolName = "attachment_write"

var AttachmentWriteTool = tool.NewDefinition(
	AttachmentWriteToolName,
	"Upload a file as attachment to an issue, pull request or comment. The content is passed as base64.",
	annotation.Write("Upload issue or comment attachment"),
	tool.String("method", tool.Required(), tool.Enum("create")),
	tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
	tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
	tool.Number("issue_number", tool.Description("issue or pull request number; exactly one of issue_number or comment_id")),
	tool.Number("comment_id", tool.Description("comment ID; exactly one of issue_number or comment_id")),
	tool.String("filename", tool.Required(), tool.Description("file name of the attachment, e.g. screenshot.png")),
	tool.String("content_base64", tool.Required(), tool.Description("file content, base64 encoded")),
)

func init() {
	Tool.RegisterWrite(tool.ServerTool{Tool: AttachmentWriteTool, Handler: attachmentWriteFn})
}

func attachmentWriteFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	method, err := params.GetString(args, "method")
	if err != nil {
		return to.ErrorResult(err)
	}
	if method != "create" {
		return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
	}
	owner, repo, issueNumber, commentID, err := attachmentScopeArgs(args)
	if err != nil {
		return to.ErrorResult(err)
	}
	filename, err := params.GetString(args, "filename")
	if err != nil {
		return to.ErrorResult(err)
	}
	filename = strings.TrimSpace(filename)
	if filename == "" || strings.ContainsAny(filename, `/\`) || filename == "." || filename == ".." {
		return to.ErrorResult(errors.New("filename must be a plain file name"))
	}
	encoded, err := params.GetString(args, "content_base64")
	if err != nil {
		return to.ErrorResult(err)
	}
	data, err := decodeAttachmentBase64(encoded)
	if err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	var att *gitea_sdk.Attachment
	if issueNumber > 0 {
		att, _, err = client.Issues.CreateIssueAttachment(ctx, owner, repo, issueNumber, bytes.NewReader(data), filename)
	} else {
		att, _, err = client.Issues.CreateIssueCommentAttachment(ctx, owner, repo, commentID, bytes.NewReader(data), filename)
	}
	if err != nil {
		return to.ErrorResult(fmt.Errorf("create attachment err: %v", err))
	}
	return to.TextResult(slimAttachment(att))
}

// decodeAttachmentBase64 accepts standard and URL-safe base64, with or without padding, line breaks or a data: prefix.
func decodeAttachmentBase64(encoded string) ([]byte, error) {
	text := strings.TrimSpace(encoded)
	if i := strings.Index(text, ","); strings.HasPrefix(text, "data:") && i > 0 {
		text = text[i+1:]
	}
	text = strings.Join(strings.Fields(text), "")
	text = strings.TrimRight(strings.NewReplacer("-", "+", "_", "/").Replace(text), "=")
	data, err := base64.RawStdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("content_base64 is not valid base64: %v", err)
	}
	if len(data) == 0 {
		return nil, errors.New("content_base64 is empty")
	}
	if len(data) > MaxAttachmentContentBytes {
		return nil, fmt.Errorf("content is %d bytes, larger than %d", len(data), MaxAttachmentContentBytes)
	}
	return data, nil
}
