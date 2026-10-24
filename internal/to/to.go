package to

import (
	"encoding/json"
	"fmt"

	"gitea.com/gitea/gitea-mcp/internal/flag"
	"gitea.com/gitea/gitea-mcp/internal/log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TextResult(v any) (*mcp.CallToolResult, error) {
	resultBytes, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal result err: %v", err)
	}
	if flag.Debug {
		log.Debugf("Text Result: %s", string(resultBytes))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(resultBytes)}},
	}, nil
}

const maxStructuredBytes = 8 << 20 // text plus structuredContent stays under go-sdk's 16 MiB stdio line limit

func StructuredResult(value map[string]any) (*mcp.CallToolResult, error) { // ChatGPT misreads JSON text with a top-level "content" key as a nested result
	result, err := TextResult(value)
	if err != nil {
		return nil, err
	}
	if text := result.Content[0].(*mcp.TextContent).Text; len(text) <= maxStructuredBytes {
		result.StructuredContent = json.RawMessage(text)
	}
	return result, nil
}

func ErrorResult(err error) (*mcp.CallToolResult, error) {
	log.Errorf("%s", err.Error())
	var result mcp.CallToolResult
	result.SetError(err)
	return &result, nil
}
