// Package oauth implements the OAuth 2.1 resource server role of the MCP
// authorization spec: RFC 9728 discovery plus the bearer challenge.
package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const wellKnownPath = "/.well-known/oauth-protected-resource"

// scopeCategories have to match Gitea's models/auth/access_token_scope.go
// exactly, since it widens an unrecognized scope to full API access.
var scopeCategories = [...]string{"repository", "issue", "user", "organization", "notification", "package", "misc"}

type Config struct {
	BaseURL  string // public origin of this server, no trailing slash
	MCPPath  string
	GiteaURL string // the authorization server
	ReadOnly bool
}

type Provider struct {
	metadata     http.Handler
	metadataPath string
	challenge    string
	host         string
}

// ParseToken extracts the token from an Authorization header, accepting
// "Bearer <token>" (case-insensitive per RFC 7235) and Gitea's "token <token>".
func ParseToken(authHeader string) (string, bool) {
	for _, prefix := range [...]string{"Bearer ", "token "} {
		if len(authHeader) > len(prefix) && strings.EqualFold(authHeader[:len(prefix)], prefix) {
			if token := strings.TrimSpace(authHeader[len(prefix):]); token != "" {
				return token, true
			}
		}
	}
	return "", false
}

func New(cfg Config) *Provider {
	prefix := "write:"
	if cfg.ReadOnly {
		prefix = "read:"
	}
	scopes := make([]string, len(scopeCategories))
	for i, category := range scopeCategories {
		scopes[i] = prefix + category
	}

	parsed, _ := url.Parse(cfg.BaseURL)
	metadataPath := wellKnownPath + cfg.MCPPath
	return &Provider{
		metadata: auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
			Resource:               cfg.BaseURL + cfg.MCPPath,
			AuthorizationServers:   []string{cfg.GiteaURL},
			ScopesSupported:        scopes,
			BearerMethodsSupported: []string{"header"},
		}),
		metadataPath: metadataPath,
		// Claude honours this scope parameter in preference to the metadata.
		challenge: fmt.Sprintf("Bearer resource_metadata=%q, scope=%q", cfg.BaseURL+metadataPath, strings.Join(scopes, " ")),
		host:      parsed.Host,
	}
}

// allowedHost also accepts loopback, which a proxy may forward instead of the
// public host. Anything else is a rebinding attempt.
func (p *Provider) allowedHost(host string) bool {
	name, _, err := net.SplitHostPort(host)
	if err != nil {
		name = host
	}
	if strings.EqualFold(host, p.host) || strings.EqualFold(name, "localhost") {
		return true
	}
	ip, _ := netip.ParseAddr(strings.Trim(name, "[]"))
	return ip.IsLoopback()
}

func (p *Provider) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle(p.metadataPath, p.metadata)
	mux.Handle(wellKnownPath, p.metadata) // clients probe the bare path too
}

// Protect challenges missing tokens to start the OAuth flow and rejected ones so clients refresh them, a nil Provider only the latter.
func (p *Provider) Protect(next http.Handler, rejected func(ctx context.Context, token string) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p != nil && !p.allowedHost(r.Host) {
			http.Error(w, "Host must be "+p.host+" or loopback", http.StatusMisdirectedRequest)
			return
		}
		token, ok := ParseToken(r.Header.Get("Authorization"))
		if p != nil && !ok {
			w.Header().Set("WWW-Authenticate", p.challenge)
			http.Error(w, "authorization required", http.StatusUnauthorized)
			return
		}
		if ok && rejected(r.Context(), token) {
			challenge := `Bearer error="invalid_token"`
			if p != nil {
				challenge = p.challenge + `, error="invalid_token"`
			}
			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
