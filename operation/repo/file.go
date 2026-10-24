package repo

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"gitea.com/gitea/gitea-mcp/internal/annotation"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/params"
	"gitea.com/gitea/gitea-mcp/internal/to"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FileTool holds the file-related tools (scope "file").
var FileTool = tool.New("file")

const (
	GetFileToolName            = "get_file_contents"
	GetDirToolName             = "get_dir_contents"
	CreateOrUpdateFileToolName = "create_or_update_file"
	DeleteFileToolName         = "delete_file"
)

var (
	fileEditSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"old_string":  map[string]any{"type": "string"},
			"new_string":  map[string]any{"type": "string"},
			"replace_all": map[string]any{"type": "boolean"},
		},
		"required":             []string{"old_string", "new_string"},
		"additionalProperties": false,
	}

	GetFileContentTool = tool.NewDefinition(
		GetFileToolName,
		"Get file content and metadata, or list a directory",
		annotation.ReadOnly("Get file or directory contents"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("path", tool.Default("/"), tool.Description(params.PathDesc)),
		tool.String("ref", tool.Description("branch, tag, or commit SHA, defaults to the default branch")),
		tool.String("sha", tool.Description("commit SHA, overrides ref")),
		tool.Boolean("with_lines", tool.Description("return numbered lines")),
		tool.Number("start_line", tool.Minimum(1)),
		tool.Number("end_line", tool.Minimum(1)),
	)

	GetDirContentTool = tool.NewDefinition(
		GetDirToolName,
		"List the entries (files and subdirectories) in a repository directory at a given ref (branch, tag, or commit SHA).",
		annotation.ReadOnly("Get directory contents"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("ref", tool.Required(), tool.Description("branch, tag, or commit SHA")),
		tool.String("path", tool.Required(), tool.Description(params.PathDesc)),
	)

	CreateOrUpdateFileTool = tool.NewDefinition(
		CreateOrUpdateFileToolName,
		"Write files in one commit: create, update, rename, delete.",
		annotation.Write("Create, update, rename, or delete files"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("path", tool.Description(params.PathDesc)),
		tool.String("content", tool.Description(params.FileContentDesc)),
		tool.String("message", tool.Required(), tool.Description("commit message")),
		tool.String("branch_name", tool.Required(), tool.Description("branch to commit to")),
		tool.String("sha", tool.Description("existing file SHA (omit to create)")),
		tool.String("new_branch_name", tool.Description("branch to create from branch_name and commit to")),
		tool.Array("edits", tool.Items(fileEditSchema)),
		tool.Array("files", tool.Items(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":      map[string]any{"type": "string", "description": params.PathDesc},
				"content":   map[string]any{"type": "string", "description": params.FileContentDesc},
				"edits":     map[string]any{"type": "array", "items": fileEditSchema},
				"sha":       map[string]any{"type": "string", "description": "existing file SHA, of from_path when renaming (omit to create)"},
				"from_path": map[string]any{"type": "string", "description": "rename source"},
				"delete":    map[string]any{"type": "boolean"},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		})),
	)

	DeleteFileTool = tool.NewDefinition(
		DeleteFileToolName,
		"Delete a file from a repository by committing the removal to a branch. Requires the file's current SHA and a commit message.",
		annotation.Destructive("Delete a file"),
		tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
		tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
		tool.String("path", tool.Required(), tool.Description(params.PathDesc)),
		tool.String("message", tool.Required(), tool.Description("commit message")),
		tool.String("branch_name", tool.Required(), tool.Description("branch to commit to")),
		tool.String("sha", tool.Required(), tool.Description("existing file SHA")),
	)
)

func init() {
	FileTool.RegisterRead(tool.ServerTool{
		Tool:    GetFileContentTool,
		Handler: GetFileContentFn,
	})
	FileTool.RegisterRead(tool.ServerTool{
		Tool:    GetDirContentTool,
		Handler: GetDirContentFn,
	})
	FileTool.RegisterWrite(tool.ServerTool{
		Tool:    CreateOrUpdateFileTool,
		Handler: CreateOrUpdateFileFn,
	})
	FileTool.RegisterWrite(tool.ServerTool{
		Tool:    DeleteFileTool,
		Handler: DeleteFileFn,
	})
}

type ContentLine struct {
	LineNumber int    `json:"line"`
	Content    string `json:"content"`
}

type getFileContentArgs struct {
	Owner     string `json:"owner" required:"true"`
	Repo      string `json:"repo" required:"true"`
	Ref       string `json:"ref"`
	SHA       string `json:"sha"`
	Path      string `json:"path"`
	WithLines bool   `json:"with_lines"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type lineSelection struct {
	Bytes              []byte
	First, Last, Total int
}

// selectLines returns lines start..end (1-based, 0 means unbounded) as a sub-slice of raw, counting lines like git.
func selectLines(raw []byte, start, end int) (lineSelection, error) {
	total := bytes.Count(raw, []byte{'\n'})
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		total++
	}
	if total == 0 {
		return lineSelection{First: 1}, nil
	}
	first, last := max(start, 1), total
	if end > 0 {
		last = min(end, total)
	}
	if first > total {
		return lineSelection{}, fmt.Errorf("start_line %d is past the end of the file (%d lines)", first, total)
	}
	if last < first {
		return lineSelection{}, fmt.Errorf("end_line %d is before start_line %d", last, first)
	}

	begin := 0
	for range first - 1 {
		begin += bytes.IndexByte(raw[begin:], '\n') + 1
	}
	stop := begin
	for range last - first + 1 {
		newline := bytes.IndexByte(raw[stop:], '\n')
		if newline < 0 {
			stop = len(raw)
			break
		}
		stop += newline + 1
	}
	selected := raw[begin:stop]
	if trimmed, ok := bytes.CutSuffix(selected, []byte("\n")); ok {
		selected = bytes.TrimSuffix(trimmed, []byte("\r"))
	}
	return lineSelection{Bytes: selected, First: first, Last: last, Total: total}, nil
}

func GetFileContentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in getFileContentArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	segments := strings.Split(strings.Trim(in.Path, "/"), "/")
	for i, segment := range segments {
		if segment == "." || segment == ".." { // PathEscape keeps them, so they would traverse out of the contents endpoint
			return to.ErrorResult(fmt.Errorf("invalid path segment %q", segment))
		}
		segments[i] = url.PathEscape(segment)
	}
	var body json.RawMessage
	if _, err := gitea.DoJSON(ctx, "GET", fmt.Sprintf("repos/%s/%s/contents/%s", url.PathEscape(in.Owner), url.PathEscape(in.Repo), strings.Join(segments, "/")), url.Values{"ref": {cmp.Or(in.SHA, in.Ref)}}, nil, &body); err != nil {
		return to.ErrorResult(fmt.Errorf("get file err: %v", err))
	}
	if body[0] == '[' {
		var entries []*gitea_sdk.ContentsResponse
		if err := json.Unmarshal(body, &entries); err != nil {
			return to.ErrorResult(fmt.Errorf("decode directory err: %v", err))
		}
		return to.TextResult(slimDirEntries(entries))
	}
	content := new(gitea_sdk.ContentsResponse)
	if err := json.Unmarshal(body, content); err != nil {
		return to.ErrorResult(fmt.Errorf("decode file err: %v", err))
	}

	rangeRequested := in.StartLine > 0 || in.EndLine > 0
	if !in.WithLines && !rangeRequested {
		return to.StructuredResult(slimContents(content))
	}

	raw, err := decodeContent(content)
	if err != nil {
		return to.ErrorResult(err)
	}
	selection, err := selectLines(raw, in.StartLine, in.EndLine)
	if err != nil {
		return to.ErrorResult(err)
	}

	text, encoding := string(selection.Bytes), "utf-8"
	if !utf8.Valid(selection.Bytes) {
		text, encoding = base64.StdEncoding.EncodeToString(selection.Bytes), "base64"
	}
	if in.WithLines && encoding == "utf-8" {
		contentLines := make([]ContentLine, 0, selection.Last-selection.First+1)
		if selection.Total > 0 {
			for line := range strings.SplitSeq(text, "\n") {
				contentLines = append(contentLines, ContentLine{LineNumber: selection.First + len(contentLines), Content: strings.TrimSuffix(line, "\r")})
			}
		}
		contentBytes, err := json.MarshalIndent(contentLines, "", "  ")
		if err != nil {
			return to.ErrorResult(fmt.Errorf("marshal content lines err: %v", err))
		}
		text = string(contentBytes)
	}
	content.Content = &text
	content.Encoding = &encoding

	result := slimContents(content)
	result["total_lines"] = selection.Total
	if rangeRequested {
		result["start_line"] = selection.First
		result["end_line"] = selection.Last
		result["truncated"] = selection.First > 1 || selection.Last < selection.Total
	}
	return to.StructuredResult(result)
}

func decodeContent(content *gitea_sdk.ContentsResponse) ([]byte, error) {
	if content.Content == nil {
		return nil, fmt.Errorf("%s has no inline content, it may be a directory, submodule or too large", content.Path)
	}
	raw, err := base64.StdEncoding.DecodeString(*content.Content)
	if err != nil {
		return nil, fmt.Errorf("decode base64 content err: %v", err)
	}
	return raw, nil
}

type getDirContentArgs struct {
	Owner string `json:"owner" required:"true"`
	Repo  string `json:"repo" required:"true"`
	Ref   string `json:"ref"`
	Path  string `json:"path" required:"true"`
}

func GetDirContentFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in getDirContentArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	content, _, err := client.Repositories.ListContents(ctx, in.Owner, in.Repo, in.Ref, in.Path)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get dir content err: %v", err))
	}
	return to.TextResult(slimDirEntries(content))
}

type createOrUpdateFileArgs struct {
	Owner         string `json:"owner" required:"true"`
	Repo          string `json:"repo" required:"true"`
	Path          string `json:"path"`
	Message       string `json:"message" required:"true"`
	BranchName    string `json:"branch_name" required:"true"`
	NewBranchName string `json:"new_branch_name"`
	Files         []any  `json:"files"`
}

func CreateOrUpdateFileFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in createOrUpdateFileArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	files := in.Files
	if in.Path != "" {
		files = append([]any{args}, files...)
	}
	if len(files) == 0 {
		return to.ErrorResult(errors.New("path or files is required"))
	}

	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	operations := make([]*gitea_sdk.ChangeFileOperation, 0, len(files))
	touched := make(map[string]bool, len(files))
	for _, file := range files {
		fileArgs, _ := file.(map[string]any)
		operation, err := changeFileOperation(ctx, client, in.Owner, in.Repo, in.BranchName, fileArgs)
		if err != nil {
			return to.ErrorResult(err)
		}
		for _, touchedPath := range []string{operation.Path, operation.FromPath} {
			if touched[touchedPath] {
				return to.ErrorResult(fmt.Errorf("%s: appears in more than one file entry", touchedPath))
			}
			touched[touchedPath] = touchedPath != ""
		}
		operations = append(operations, operation)
	}

	_, _, err = client.Repositories.ChangeFiles(ctx, in.Owner, in.Repo, gitea_sdk.ChangeFilesOptions{
		Files:     operations,
		Message:   in.Message,
		Branch:    in.BranchName,
		NewBranch: in.NewBranchName,
	})
	if err != nil {
		return to.ErrorResult(fmt.Errorf("change files err: %v", err))
	}
	return to.TextResult(fmt.Sprintf("Committed %d file(s) to branch %s", len(operations), cmp.Or(in.NewBranchName, in.BranchName)))
}

func changeFileOperation(ctx context.Context, client *gitea_sdk.Client, owner, repo, ref string, file map[string]any) (*gitea_sdk.ChangeFileOperation, error) {
	filePath, err := params.GetString(file, "path")
	if err != nil {
		return nil, err
	}
	content, hasContent := file["content"].(string)
	sha, _ := file["sha"].(string)
	fromPath, _ := file["from_path"].(string)
	edits, _ := file["edits"].([]any)
	operation := "update"
	switch deleteFile, _ := file["delete"].(bool); {
	case deleteFile:
		if hasContent || len(edits) > 0 || fromPath != "" {
			return nil, fmt.Errorf("%s: delete excludes content, edits and from_path", filePath)
		}
		operation = "delete"
	case len(edits) > 0:
		if hasContent {
			return nil, fmt.Errorf("%s: content and edits are mutually exclusive", filePath)
		}
		sourcePath := cmp.Or(fromPath, filePath)
		current, _, err := client.Repositories.GetContents(ctx, owner, repo, ref, sourcePath)
		if err != nil {
			return nil, fmt.Errorf("get %s err: %v", sourcePath, err)
		}
		raw, err := decodeContent(current)
		if err != nil {
			return nil, err
		}
		if content, err = applyEdits(string(raw), edits); err != nil {
			return nil, fmt.Errorf("%s: %w", filePath, err)
		}
		sha = cmp.Or(sha, current.SHA)
	case hasContent:
		if sha == "" && fromPath == "" {
			operation = "create"
		}
	case fromPath != "":
		operation = "rename"
	default:
		return nil, fmt.Errorf("%s: content, edits, from_path or delete is required", filePath)
	}
	if sha == "" && operation != "create" { // Gitea skips its SHA check when none is given
		return nil, fmt.Errorf("%s: sha is required", filePath)
	}

	return &gitea_sdk.ChangeFileOperation{
		Operation: operation,
		Path:      filePath,
		Content:   base64.StdEncoding.EncodeToString([]byte(content)),
		SHA:       sha,
		FromPath:  fromPath,
	}, nil
}

func applyEdits(text string, edits []any) (string, error) {
	for i, raw := range edits {
		edit, _ := raw.(map[string]any)
		oldString, _ := edit["old_string"].(string)
		newString, hasNewString := edit["new_string"].(string)
		replaceAll, _ := edit["replace_all"].(bool)
		if oldString == "" || !hasNewString {
			return "", fmt.Errorf("edit %d: old_string and new_string are required", i+1)
		}
		switch count := strings.Count(text, oldString); {
		case count == 0:
			return "", fmt.Errorf("edit %d: old_string not found, it must match exactly", i+1)
		case count > 1 && !replaceAll:
			return "", fmt.Errorf("edit %d: old_string occurs %d times, add context or set replace_all", i+1, count)
		}
		text = strings.ReplaceAll(text, oldString, newString)
	}
	return text, nil
}

type deleteFileArgs struct {
	Owner      string `json:"owner" required:"true"`
	Repo       string `json:"repo" required:"true"`
	Path       string `json:"path" required:"true"`
	Message    string `json:"message" required:"true"`
	BranchName string `json:"branch_name" required:"true"`
	SHA        string `json:"sha" required:"true"`
}

func DeleteFileFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	var in deleteFileArgs
	if err := params.Bind(args, &in); err != nil {
		return to.ErrorResult(err)
	}
	opt := gitea_sdk.DeleteFileOptions{
		Message:    in.Message,
		BranchName: in.BranchName,
		SHA:        in.SHA,
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}
	_, err = client.Repositories.DeleteFile(ctx, in.Owner, in.Repo, in.Path, opt)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("delete file err: %v", err))
	}
	return to.TextResult("Delete file success")
}
