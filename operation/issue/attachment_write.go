package issue

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

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
	tool.Boolean("link_in_body", tool.Description("issue_number only: then replace each Markdown link target equal to filename in the issue body, as in ![x](filename) or [x](<file name>), with the URL of the attachment")),
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
	linkInBody, _ := args["link_in_body"].(bool)
	if linkInBody && issueNumber <= 0 {
		return to.ErrorResult(errors.New("link_in_body works only with issue_number"))
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
	result := slimAttachment(att)
	if linkInBody {
		linked, err := linkAttachmentInIssueBody(ctx, client, owner, repo, issueNumber, filename, "/attachments/"+att.UUID)
		if err != nil {
			return to.ErrorResult(fmt.Errorf("attachment %d uploaded, but linking it in the issue body failed: %v", att.ID, err))
		}
		result["linked_in_body"] = linked
	}
	return to.TextResult(result)
}

// issueBodyLocks serialises the read-modify-write of an issue body, so attachments uploaded in
// parallel do not overwrite each other's links.
var issueBodyLocks sync.Map

func linkAttachmentInIssueBody(ctx context.Context, client *gitea_sdk.Client, owner, repo string, index int64, filename, target string) (int, error) {
	key := strings.ToLower(owner + "/" + repo + "#" + fmt.Sprint(index))
	lock, _ := issueBodyLocks.LoadOrStore(key, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	issue, _, err := client.Issues.GetIssue(ctx, owner, repo, index)
	if err != nil {
		return 0, err
	}
	body, linked := replaceLinkTarget(issue.Body, filename, target)
	if linked == 0 {
		return 0, nil
	}
	if _, _, err := client.Issues.EditIssue(ctx, owner, repo, index, gitea_sdk.EditIssueOption{Body: &body}); err != nil {
		return 0, err
	}
	return linked, nil
}

// replaceLinkTarget replaces the Markdown link targets filename, <filename> and filename with
// %20 for spaces by target, with or without a link title; it returns the new text and the count.
func replaceLinkTarget(text, filename, target string) (string, int) {
	count := 0
	seen := map[string]bool{}
	for _, form := range []string{filename, "<" + filename + ">", strings.ReplaceAll(filename, " ", "%20")} {
		if seen[form] {
			continue
		}
		seen[form] = true
		for _, end := range []string{")", " \""} {
			old := "](" + form + end
			if n := strings.Count(text, old); n > 0 {
				text = strings.ReplaceAll(text, old, "]("+target+end)
				count += n
			}
		}
	}
	return text, count
}

// bodyArg returns the body from body or body_base64; present is false when neither is given.
func bodyArg(args map[string]any) (body string, present bool, err error) {
	plain, hasPlain := args["body"].(string)
	encoded, _ := args["body_base64"].(string)
	if strings.TrimSpace(encoded) == "" {
		return plain, hasPlain, nil
	}
	if hasPlain && plain != "" {
		return "", false, errors.New("give body or body_base64, not both")
	}
	data, err := decodeBase64Field(encoded, "body_base64")
	if err != nil {
		return "", false, err
	}
	if !utf8.Valid(data) {
		return "", false, errors.New("body_base64 is not UTF-8 text")
	}
	return string(data), true, nil
}

func decodeAttachmentBase64(encoded string) ([]byte, error) {
	return decodeBase64Field(encoded, "content_base64")
}

// decodeBase64Field accepts standard and URL-safe base64, with or without padding, line breaks or a data: prefix.
func decodeBase64Field(encoded, field string) ([]byte, error) {
	text := strings.TrimSpace(encoded)
	if i := strings.Index(text, ","); strings.HasPrefix(text, "data:") && i > 0 {
		text = text[i+1:]
	}
	text = strings.Join(strings.Fields(text), "")
	text = strings.TrimRight(strings.NewReplacer("-", "+", "_", "/").Replace(text), "=")
	data, err := base64.RawStdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid base64: %v", field, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%s is empty", field)
	}
	if len(data) > MaxAttachmentContentBytes {
		return nil, fmt.Errorf("content is %d bytes, larger than %d", len(data), MaxAttachmentContentBytes)
	}
	return data, nil
}
