// Package batch runs a read handler for each value of a list argument, so one tool call answers what would
// otherwise take one call per item.
package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync"

	"gitea.com/gitea/gitea-mcp/pkg/to"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MaxItems caps the values of one call; workers caps the requests that run at the same time.
const (
	MaxItems = 50
	workers  = 2
)

// Handler is the signature of a tool handler.
type Handler func(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error)

// Each runs handler once per value of args[listKey], with args[singleKey] set to that value and listKey removed,
// at most two at a time. The result is {"results": [...]} in the order of the list; each entry names its value
// under singleKey and holds either "result" or "error", so one failing item does not fail the others.
func Each(ctx context.Context, args map[string]any, listKey, singleKey string, handler Handler) (*mcp.CallToolResult, error) {
	values, ok := args[listKey].([]any)
	if !ok || len(values) == 0 {
		return to.ErrorResult(fmt.Errorf("%s must be a non-empty list", listKey))
	}
	if len(values) > MaxItems {
		return to.ErrorResult(fmt.Errorf("%s has %d values, at most %d per call", listKey, len(values), MaxItems))
	}
	results := make([]map[string]any, len(values))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, value := range values {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			one := maps.Clone(args)
			delete(one, listKey)
			one[singleKey] = value
			entry := map[string]any{singleKey: value}
			res, err := handler(ctx, one)
			switch {
			case err != nil:
				entry["error"] = err.Error()
			case res == nil:
				entry["error"] = "no result"
			case res.IsError:
				entry["error"] = text(res)
			default:
				var parsed any
				if t := text(res); json.Unmarshal([]byte(t), &parsed) == nil {
					entry["result"] = parsed
				} else {
					entry["result"] = t
				}
			}
			results[i] = entry
		}()
	}
	wg.Wait()
	failed := 0
	for _, r := range results {
		if _, bad := r["error"]; bad {
			failed++
		}
	}
	return to.TextResult(map[string]any{"count": len(results), "failed": failed, "results": results})
}

func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}
