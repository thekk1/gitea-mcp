package version

import (
	"context"
	"fmt"

	"gitea.com/gitea/gitea-mcp/internal/annotation"
	"gitea.com/gitea/gitea-mcp/internal/flag"
	"gitea.com/gitea/gitea-mcp/internal/to"
	"gitea.com/gitea/gitea-mcp/internal/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Tool = tool.New("version")

const (
	GetGiteaMCPServerVersion = "get_gitea_mcp_server_version"
)

var GetGiteaMCPServerVersionTool = tool.NewDefinition(
	GetGiteaMCPServerVersion,
	"Get this MCP server's version, not Gitea's.",
	annotation.ReadOnly("Get server version"),
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    GetGiteaMCPServerVersionTool,
		Handler: GetGiteaMCPServerVersionFn,
	})
}

func GetGiteaMCPServerVersionFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	version := flag.Version
	if version == "" {
		version = "dev"
	}
	return to.TextResult(fmt.Sprintf("Gitea MCP Server version: %v", version))
}
