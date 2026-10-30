package operation

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/flag"
	"gitea.com/gitea/gitea-mcp/internal/tool"
)

const metadataPath = "/.well-known/oauth-protected-resource" + mcpPath

func TestNewHTTPServerOAuth(t *testing.T) {
	giteaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "token expired" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(giteaServer.Close)
	origOAuth, origURL, origHost := flag.OAuth, flag.OAuthPublicURL, flag.Host
	t.Cleanup(func() { flag.OAuth, flag.OAuthPublicURL, flag.Host = origOAuth, origURL, origHost })
	flag.OAuth, flag.OAuthPublicURL, flag.Host = true, "https://mcp.example.com", giteaServer.URL
	handler := newHTTPServer(newMCPServer("test")).Handler

	// Only the flag wiring is checked here, pkg/oauth pins the document itself.
	t.Run("serves discovery metadata", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, metadataPath, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		want := `"resource":"https://mcp.example.com/mcp","authorization_servers":["` + giteaServer.URL + `"]`
		if got := recorder.Body.String(); !strings.Contains(got, want) {
			t.Errorf("body = %s, want it to contain %s", got, want)
		}
	})

	// The SDK 403s a public Host on a loopback connection unless localhost
	// protection is off, and only a real connection triggers it.
	t.Run("accepts a proxied non-loopback Host", func(t *testing.T) {
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)

		request, err := http.NewRequest(http.MethodPost, server.URL+mcpPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Host = "mcp.example.com"
		request.Header.Set("Authorization", "Bearer sometoken")

		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode == http.StatusForbidden {
			t.Error("proxied Host rejected as DNS rebinding")
		}
	})

	t.Run("challenges unauthenticated MCP requests", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, mcpPath, nil)
		request.Host = "mcp.example.com"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
		if recorder.Header().Get("WWW-Authenticate") == "" {
			t.Error("WWW-Authenticate header missing")
		}
	})

	t.Run("stays off without the flag", func(t *testing.T) {
		flag.OAuth = false
		handler := newHTTPServer(newMCPServer("test")).Handler

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, metadataPath, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("metadata status = %d, want %d", recorder.Code, http.StatusNotFound)
		}

		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, mcpPath, nil))
		if recorder.Code == http.StatusUnauthorized {
			t.Error("unauthenticated request challenged with OAuth disabled")
		}

		request := httptest.NewRequest(http.MethodPost, mcpPath, nil)
		request.Header.Set("Authorization", "token expired")
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if got := recorder.Header().Get("WWW-Authenticate"); recorder.Code != http.StatusUnauthorized || got != `Bearer error="invalid_token"` {
			t.Errorf("rejected token: status = %d, WWW-Authenticate = %q", recorder.Code, got)
		}
	})
}

func TestNewHTTPServerConfig(t *testing.T) {
	server := newHTTPServer(newMCPServer("test"))
	if server.Handler == nil {
		t.Error("Handler is nil")
	}
	if server.ReadHeaderTimeout != httpReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", server.ReadHeaderTimeout, httpReadHeaderTimeout)
	}
	if server.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want zero for SSE", server.WriteTimeout)
	}
}

func TestHealthzEndpoint(t *testing.T) {
	server := newHTTPServer(newMCPServer("test"))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body == "" {
		t.Error("body is empty, want a non-empty health message")
	}
}

func TestDeprecatedAliasesPointToRegisteredMethods(t *testing.T) {
	methods := map[string][]string{}
	for _, d := range domainTools {
		for _, st := range slices.Concat(d.ReadTools(), d.WriteTools()) {
			methods[st.Tool.Name] = nil
			if method, ok := st.Tool.InputSchema.(map[string]any)["properties"].(map[string]any)["method"].(map[string]any); ok {
				methods[st.Tool.Name], _ = method["enum"].([]string)
			}
		}
	}
	for name, alias := range tool.DeprecatedAliases {
		_, aliasRegistered := methods[name]
		if aliasRegistered || len(alias.Methods) == 0 {
			t.Errorf("alias %q: registered %v, methods %v", name, aliasRegistered, alias.Methods)
		}
		for _, method := range alias.Methods {
			if !slices.Contains(methods[alias.Tool], method) {
				t.Errorf("alias %q: %s has no method %q", name, alias.Tool, method)
			}
		}
	}
}
