package operation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	mcpContext "gitea.com/gitea/gitea-mcp/internal/context"
	"gitea.com/gitea/gitea-mcp/internal/flag"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/log"
	"gitea.com/gitea/gitea-mcp/internal/oauth"
	"gitea.com/gitea/gitea-mcp/internal/tool"
	"gitea.com/gitea/gitea-mcp/operation/actions"
	"gitea.com/gitea/gitea-mcp/operation/issue"
	"gitea.com/gitea/gitea-mcp/operation/label"
	"gitea.com/gitea/gitea-mcp/operation/milestone"
	"gitea.com/gitea/gitea-mcp/operation/notification"
	"gitea.com/gitea/gitea-mcp/operation/packages"
	"gitea.com/gitea/gitea-mcp/operation/project"
	"gitea.com/gitea/gitea-mcp/operation/pull"
	"gitea.com/gitea/gitea-mcp/operation/repo"
	"gitea.com/gitea/gitea-mcp/operation/search"
	"gitea.com/gitea/gitea-mcp/operation/timetracking"
	"gitea.com/gitea/gitea-mcp/operation/user"
	"gitea.com/gitea/gitea-mcp/operation/version"
	"gitea.com/gitea/gitea-mcp/operation/wiki"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxRequestBodyBytes raises the SDK's 4 MiB default, which is too tight for the
// base64 file content create_or_update_file accepts.
const maxRequestBodyBytes = 32 << 20

// httpReadHeaderTimeout bounds slow header reads without limiting SSE writes.
const httpReadHeaderTimeout = 10 * time.Second

const mcpPath = "/mcp"

var (
	mcpServer *mcp.Server

	domainTools = []*tool.Tool{
		user.Tool, actions.Tool, repo.Tool, notification.Tool, issue.Tool,
		label.Tool, milestone.Tool, packages.Tool, pull.Tool, search.Tool,
		project.Tool, version.Tool, wiki.Tool, timetracking.Tool,
		repo.FileTool, repo.BranchTool, repo.TagTool, repo.CommitTool, repo.ReleaseTool,
	}
)

func RegisterTool(s *mcp.Server) {
	for _, t := range domainTools {
		for _, registeredTool := range t.Tools() {
			s.AddTool(registeredTool.Tool, registeredTool.MCPHandler())
		}
	}
	tool.WarnUnmatchedAllowedTools(domainTools...)
	tool.WarnUnmatchedAllowedScopes(domainTools...)
}

func authTokenMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if extra := req.GetExtra(); extra != nil {
			if token, ok := oauth.ParseToken(extra.Header.Get("Authorization")); ok {
				ctx = context.WithValue(ctx, mcpContext.TokenContextKey, token)
			}
		}
		return next(ctx, method, req)
	}
}

func protectMCPOrigin(next http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check exempts safe methods, but MCP requires Origin validation on every request.
		checkRequest := r
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			checkRequest = r.Clone(r.Context())
			checkRequest.Method = http.MethodPost
		}
		if err := protection.Check(checkRequest); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func newHTTPServer(s *mcp.Server) *http.Server {
	mux := http.NewServeMux()
	var handler http.Handler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return s },
		&mcp.StreamableHTTPOptions{
			Logger:                       log.Slog(),
			MaxRequestBodyBytes:          maxRequestBodyBytes,
			Stateless:                    true,
			PropagateRequestCancellation: true,
			// The SDK 403s a proxied non-loopback Host, and Protect validates it instead.
			DisableLocalhostProtection: flag.OAuth,
		},
	)
	var provider *oauth.Provider
	if flag.OAuth {
		provider = oauth.New(oauth.Config{
			BaseURL:  flag.OAuthPublicURL,
			MCPPath:  mcpPath,
			GiteaURL: flag.Host,
			ReadOnly: flag.ReadOnly,
		})
		provider.RegisterRoutes(mux)
	}
	mux.Handle(mcpPath, protectMCPOrigin(provider.Protect(handler, gitea.TokenRejected)))
	mux.HandleFunc("/healthz", handleHealthz)
	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: httpReadHeaderTimeout,
	}
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func Run() error {
	mcpServer = newMCPServer(flag.Version)
	RegisterTool(mcpServer)
	switch flag.Mode {
	case "stdio":
		writeHealthcheckFile("stdio")
		if err := mcpServer.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			return err
		}
	case "http":
		addr := net.JoinHostPort(flag.Bind, strconv.Itoa(flag.Port))
		httpServer := newHTTPServer(mcpServer)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		writeHealthcheckFile(listener.Addr().String()) // Go dials a wildcard address as local
		log.Infof("Gitea MCP HTTP server listening on %s (stateless, protocol up to 2026-07-28)", addr)
		if flag.OAuth {
			log.Infof("OAuth enabled, clients authorize against %s and connect to %s%s", flag.Host, flag.OAuthPublicURL, mcpPath)
		}

		// Graceful shutdown setup
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		shutdownDone := make(chan struct{})

		go func() {
			<-sigCh
			log.Infof("Shutdown signal received, gracefully stopping HTTP server...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				log.Errorf("HTTP server shutdown error: %v", err)
			}
			close(shutdownDone)
		}()

		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		<-shutdownDone // Wait for shutdown to finish
	default:
		return fmt.Errorf("invalid transport type: %s. Must be 'stdio' or 'http'", flag.Mode)
	}
	return nil
}

func writeHealthcheckFile(content string) {
	if flag.HealthcheckFile == "" {
		return
	}
	if err := os.WriteFile(flag.HealthcheckFile, []byte(content), 0o600); err != nil {
		_ = os.Remove(flag.HealthcheckFile) // a stale or truncated file would mislead -healthcheck
		log.Warnf("Failed to write healthcheck file, -healthcheck falls back to its own -port: %v", err)
	}
}

func newMCPServer(version string) *mcp.Server {
	// SDK keepalives send MCP ping requests and disconnect clients without a
	// server-to-client channel, so KeepAlive stays disabled.
	s := mcp.NewServer(
		&mcp.Implementation{
			Name:    "Gitea MCP Server",
			Version: version,
		},
		&mcp.ServerOptions{
			Instructions: "Work with a Gitea instance: repositories, files, branches, tags, commits, releases, issues, pull requests, " +
				"reviews, labels, milestones, projects, wiki, Actions, packages, notifications and time tracking. " +
				"Use these tools for any Gitea repository or URL. <scope>_read and <scope>_write tools select their operation with the `method` argument.",
			Logger: log.Slog(),
			Capabilities: &mcp.ServerCapabilities{
				Tools: &mcp.ToolCapabilities{},
			},
		},
	)
	s.AddReceivingMiddleware(authTokenMiddleware)
	return s
}
