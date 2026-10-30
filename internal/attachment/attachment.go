package attachment

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"gitea.com/gitea/gitea-mcp/internal/flag"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/params"
	"gitea.com/gitea/gitea-mcp/internal/to"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Upload(args map[string]any) (name string, content io.Reader, err error) {
	if name, err = params.GetString(args, "name"); err != nil {
		return "", nil, err
	}
	encoded, ok := args["content"].(string)
	if !ok {
		return "", nil, errors.New("content is required")
	}
	return name, base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded)), nil
}

func Download(ctx context.Context, owner, repo string, att *gitea_sdk.Attachment, attachmentUUID, explicitOutputPath string) (*mcp.CallToolResult, error) {
	attachmentUUID = strings.TrimSpace(attachmentUUID)
	if attachmentUUID == "" && att != nil {
		attachmentUUID = strings.TrimSpace(att.UUID)
	}
	if attachmentUUID == "" {
		return to.ErrorResult(errors.New("attachment_uuid or attachment metadata with uuid is required"))
	}
	if strings.ContainsAny(attachmentUUID, `/\.`) {
		return to.ErrorResult(fmt.Errorf("invalid attachment_uuid %q", attachmentUUID))
	}

	name := attachmentUUID
	if att != nil && strings.TrimSpace(att.Name) != "" {
		name = strings.TrimSpace(att.Name)
	}

	resp, err := gitea.OpenAttachment(ctx, "/attachments/"+attachmentUUID, "*/*")
	if err != nil {
		return to.ErrorResult(fmt.Errorf("download attachment err: %v", err))
	}
	defer resp.Body.Close()

	mimeType := normalizeAttachmentContentType(resp.ContentType, name)
	var body io.Reader = resp.Body
	if explicitOutputPath == "" && shouldInlineAttachment(att, mimeType) {
		limited, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(flag.MaxInlineAttachmentBytes)+1))
		if readErr != nil {
			return to.ErrorResult(fmt.Errorf("read attachment err: %v", readErr))
		}
		if len(limited) <= flag.MaxInlineAttachmentBytes {
			text := fmt.Sprintf("attachment %s (%s, %d bytes, %s)", name, attachmentUUID, len(limited), mimeType)
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.TextContent{Text: text},
				&mcp.ImageContent{Data: limited, MIMEType: mimeType},
			}}, nil
		}
		body = io.MultiReader(bytes.NewReader(limited), resp.Body)
	}

	outputPath := explicitOutputPath
	if outputPath == "" {
		outputPath = defaultAttachmentPath(owner, repo, name, attachmentUUID)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return to.ErrorResult(fmt.Errorf("create output dir err: %v", err))
	}
	written, err := gitea.WriteAttachment(body, outputPath)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("write attachment file err: %v", err))
	}
	return attachmentFileResult(att, outputPath, written, name, attachmentUUID, mimeType)
}

func shouldInlineAttachment(att *gitea_sdk.Attachment, mimeType string) bool {
	if !strings.HasPrefix(mimeType, "image/") || flag.MaxInlineAttachmentBytes <= 0 {
		return false
	}
	if att == nil || att.Size <= 0 {
		return true
	}
	return att.Size <= int64(flag.MaxInlineAttachmentBytes)
}

func attachmentFileResult(att *gitea_sdk.Attachment, outputPath string, written int64, name, attachmentUUID, mimeType string) (*mcp.CallToolResult, error) {
	res := map[string]any{
		"path":         outputPath,
		"bytes":        written,
		"name":         name,
		"uuid":         attachmentUUID,
		"mime_type":    mimeType,
		"content_type": mimeType,
	}
	if att != nil {
		res["attachment_id"] = att.ID
	}
	return to.TextResult(res)
}

func SlimList(atts []*gitea_sdk.Attachment) []map[string]any {
	out := make([]map[string]any, 0, len(atts))
	for _, att := range atts {
		out = append(out, Slim(att))
	}
	return out
}

func Slim(att *gitea_sdk.Attachment) map[string]any {
	if att == nil {
		return nil
	}
	m := map[string]any{
		"id":             att.ID,
		"name":           att.Name,
		"uuid":           att.UUID,
		"size":           att.Size,
		"download_count": att.DownloadCount,
		"created_at":     att.Created,
		"mime_type":      inferAttachmentMimeType(att.Name),
	}
	if att.DownloadURL != "" {
		m["browser_download_url"] = att.DownloadURL
	}
	return m
}

func inferAttachmentMimeType(name string) string {
	if ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name))); ext != "" {
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return strings.Split(mimeType, ";")[0]
		}
	}
	return "application/octet-stream"
}

func normalizeAttachmentContentType(contentType, name string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && mediaType != "" && mediaType != "application/octet-stream" {
		return mediaType
	}
	return inferAttachmentMimeType(name)
}

func defaultAttachmentPath(owner, repo, name, uuid string) string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.TempDir()
	}
	filename := attachmentFilename(name, uuid)
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	if uuid != "" {
		filename = uuid
		if base != "" && base != "attachment" {
			filename = base + "-" + uuid
		}
		filename += ext
	}
	return filepath.Join(home, ".gitea-mcp", "attachments", safePathPart(owner), safePathPart(repo), filename)
}

func attachmentFilename(name, uuid string) string {
	name = strings.TrimSpace(name)
	if name != "" && !strings.ContainsAny(name, `/\\`) && name != "." && name != ".." {
		return name
	}
	if uuid != "" {
		return uuid + ".bin"
	}
	return "attachment.bin"
}

func safePathPart(name string) string {
	name = strings.TrimSpace(name)
	if name != "" && !strings.ContainsAny(name, `/\\`) && name != "." && name != ".." {
		return name
	}
	return "unknown"
}
