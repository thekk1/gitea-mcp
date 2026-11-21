package batch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/to"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestEach(t *testing.T) {
	handler := func(_ context.Context, args map[string]any) (*mcp.CallToolResult, error) {
		if _, ok := args["numbers"]; ok {
			return nil, errors.New("list key passed on")
		}
		switch n := args["number"].(float64); n {
		case 2:
			return to.ErrorResult(errors.New("not found"))
		case 3:
			return nil, errors.New("broken")
		default:
			return to.TextResult(map[string]any{"n": n, "owner": args["owner"]})
		}
	}
	result, err := Each(t.Context(), map[string]any{"owner": "acme", "numbers": []any{1.0, 2.0, 3.0, 4.0}}, "numbers", "number", handler)
	if err != nil || result.IsError {
		t.Fatalf("err %v, result %+v", err, result)
	}
	var got struct {
		Count   int              `json:"count"`
		Failed  int              `json:"failed"`
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	if got.Count != 4 || got.Failed != 2 {
		t.Errorf("count %d failed %d", got.Count, got.Failed)
	}
	if fmt.Sprint(got.Results) != "[map[number:1 result:map[n:1 owner:acme]] map[error:not found number:2] map[error:broken number:3] map[number:4 result:map[n:4 owner:acme]]]" {
		t.Errorf("results = %v", got.Results)
	}

	many := make([]any, MaxItems+1)
	for i := range many {
		many[i] = float64(i)
	}
	for _, args := range []map[string]any{{"numbers": []any{}}, {"numbers": "1"}, {"numbers": many}} {
		if result, _ := Each(t.Context(), args, "numbers", "number", handler); !result.IsError {
			t.Errorf("%v: want an error result", args["numbers"])
		}
	}
}
