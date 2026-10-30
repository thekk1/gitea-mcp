package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gitea.com/gitea/gitea-mcp/internal/flag"
	"gitea.com/gitea/gitea-mcp/internal/log"
	"gitea.com/gitea/gitea-mcp/internal/to"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Handler func(context.Context, map[string]any) (*mcp.CallToolResult, error)

type ServerTool struct {
	Tool    *mcp.Tool
	Handler Handler
}

type Tool struct {
	scope string
	write []ServerTool
	read  []ServerTool
}

func New(scope string) *Tool {
	return &Tool{
		scope: scope,
		write: make([]ServerTool, 0, 100),
		read:  make([]ServerTool, 0, 100),
	}
}

// Scope returns the canonical scope name this domain of tools was registered under.
func (t *Tool) Scope() string {
	return t.scope
}

func (t *Tool) RegisterWrite(s ServerTool) {
	t.write = append(t.write, s)
}

func (t *Tool) RegisterRead(s ServerTool) {
	t.read = append(t.read, s)
}

// ReadTools returns the read-only tools registered on this domain, ignoring
// the read-only and allowlist flags that Tools applies.
func (t *Tool) ReadTools() []ServerTool {
	return t.read
}

// WriteTools returns the write tools registered on this domain, ignoring the
// read-only and allowlist flags that Tools applies.
func (t *Tool) WriteTools() []ServerTool {
	return t.write
}

// Tools returns the tools registered on this domain after applying the
// read-only filter and the scope/tool allowlists (union semantics: a tool is
// kept if its domain's scope is in AllowedScopes OR its name is in
// AllowedTools). With no allowlists set, all tools pass through unchanged.
func (t *Tool) Tools() []ServerTool {
	all := make([]ServerTool, 0, len(t.write)+len(t.read))
	if !flag.ReadOnly {
		all = append(all, t.write...)
	}
	all = append(all, t.read...)
	if len(flag.AllowedScopes) == 0 && len(flag.AllowedTools) == 0 {
		return all
	}
	_, scopeAllowed := flag.AllowedScopes[t.scope]
	filtered := make([]ServerTool, 0, len(all))
	for _, st := range all {
		if _, toolAllowed := flag.AllowedTools[st.Tool.Name]; scopeAllowed || toolAllowed {
			filtered = append(filtered, st)
		} else if methods := aliasMethods(st.Tool.Name); len(methods) > 0 {
			filtered = append(filtered, restrictMethods(st, methods))
		}
	}
	return filtered
}

func aliasMethods(name string) []string {
	var methods []string
	for allowed := range flag.AllowedTools {
		if alias, ok := DeprecatedAliases[allowed]; ok && alias.Tool == name {
			methods = append(methods, alias.Methods...)
		}
	}
	slices.Sort(methods)
	return methods
}

// restrictMethods narrows the advertised method enum and rejects other methods, so an alias never grants more than the tool it replaced.
func restrictMethods(st ServerTool, methods []string) ServerTool {
	restricted := *st.Tool
	schema := maps.Clone(restricted.InputSchema.(map[string]any))
	properties := maps.Clone(schema["properties"].(map[string]any))
	method := maps.Clone(properties["method"].(map[string]any))
	method["enum"] = methods
	properties["method"], schema["properties"], restricted.InputSchema = method, properties, schema
	return ServerTool{Tool: &restricted, Handler: func(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
		if method, _ := args["method"].(string); !slices.Contains(methods, method) {
			return to.ErrorResult(fmt.Errorf("method %q is not enabled by the --tools allowlist", method))
		}
		return st.Handler(ctx, args)
	}}
}

func Dispatch(handlers map[string]Handler) Handler {
	return func(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
		method, _ := args["method"].(string)
		if method == "" {
			return to.ErrorResult(errors.New("method is required"))
		}
		handler, ok := handlers[method]
		if !ok {
			return to.ErrorResult(fmt.Errorf("unknown method: %s", method))
		}
		return handler(ctx, args)
	}
}

// MCPHandler adapts a project handler to the official SDK's low-level handler.
func (s ServerTool) MCPHandler() mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (result *mcp.CallToolResult, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				result, err = to.ErrorResult(fmt.Errorf("panic recovered in %s tool handler: %v", s.Tool.Name, recovered))
			}
		}()

		arguments, err := decodeArguments(req.Params.Arguments)
		if err != nil {
			return nil, err
		}

		if outputPath, _ := arguments["output_path"].(string); outputPath != "" && flag.Mode == "http" {
			return to.ErrorResult(errors.New("output_path is only supported with the stdio transport")) // the caller is remote but the path is on the server
		}

		result, err = s.Handler(ctx, arguments)
		if err != nil {
			if _, ok := errors.AsType[*jsonrpc.Error](err); ok {
				return nil, err
			}
			return to.ErrorResult(err)
		}
		return result, nil
	}
}

func decodeArguments(raw json.RawMessage) (map[string]any, error) {
	// An omitted and a null "arguments" both mean the tool was called without any.
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}

	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidParams,
			Message: fmt.Sprintf("invalid tool arguments: %v", err),
		}
	}
	return arguments, nil
}

// warnUnmatched logs the names present in allowlist but absent from known,
// via logUnmatched, so WarnUnmatchedAllowedTools and WarnUnmatchedAllowedScopes
// share the same "collect, sort, no-op when empty" logic and can't drift.
// No-op if allowlist is empty or every name in it is known.
func warnUnmatched(allowlist, known map[string]struct{}, logUnmatched func(unmatched []string)) {
	if len(allowlist) == 0 {
		return
	}
	var unmatched []string
	for name := range allowlist {
		if _, ok := known[name]; !ok {
			unmatched = append(unmatched, name)
		}
	}
	if len(unmatched) == 0 {
		return
	}
	slices.Sort(unmatched)
	logUnmatched(unmatched)
}

// WarnUnmatchedAllowedTools logs any names in flag.AllowedTools that don't
// match a tool registered on any of the given domains. No-op if the allowlist
// is empty.
func WarnUnmatchedAllowedTools(domains ...*Tool) {
	known := map[string]struct{}{}
	for _, d := range domains {
		for _, st := range d.read {
			known[st.Tool.Name] = struct{}{}
		}
		for _, st := range d.write {
			known[st.Tool.Name] = struct{}{}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(flag.AllowedTools)) {
		if alias, ok := DeprecatedAliases[name]; ok {
			log.Warnf("Tool %q in --tools allowlist is deprecated, enabling %q methods %s instead", name, alias.Tool, strings.Join(alias.Methods, ", "))
			known[name] = struct{}{}
		}
	}
	warnUnmatched(flag.AllowedTools, known, func(unmatched []string) {
		log.Warnf("Unknown tools in --tools allowlist (ignored): %s", strings.Join(unmatched, ", "))
	})
}

// WarnUnmatchedAllowedScopes logs any names in flag.AllowedScopes that don't
// match the scope of any of the given domains. No-op if the allowlist is
// empty.
func WarnUnmatchedAllowedScopes(domains ...*Tool) {
	knownSet := map[string]struct{}{}
	known := make([]string, 0, len(domains))
	for _, d := range domains {
		if _, ok := knownSet[d.scope]; !ok {
			knownSet[d.scope] = struct{}{}
			known = append(known, d.scope)
		}
	}
	warnUnmatched(flag.AllowedScopes, knownSet, func(unmatched []string) {
		slices.Sort(known)
		log.Warnf("Unknown scopes in --scope allowlist (ignored): %s. Valid scopes: %s", strings.Join(unmatched, ", "), strings.Join(known, ", "))
	})
}
