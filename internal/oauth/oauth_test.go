package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testConfig = Config{BaseURL: "https://mcp.example.com", MCPPath: "/mcp", GiteaURL: "https://gitea.example.com"}

func metadataBody(t *testing.T, cfg Config, path string) string {
	t.Helper()
	mux := http.NewServeMux()
	New(cfg).RegisterRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusOK)
	}
	return recorder.Body.String()
}

func TestParseToken(t *testing.T) {
	for _, tt := range []struct {
		name      string
		header    string
		wantToken string
		wantOK    bool
	}{
		{name: "bearer token", header: "Bearer validtoken", wantToken: "validtoken", wantOK: true},
		{name: "case-insensitive prefix", header: "bEaReR mixed", wantToken: "mixed", wantOK: true},
		{name: "surrounding spaces trimmed", header: "Bearer   spacedToken ", wantToken: "spacedToken", wantOK: true},
		{name: "internal spaces kept", header: "Bearer token with spaces", wantToken: "token with spaces", wantOK: true},
		{name: "gitea token format", header: "Token giteaapitoken", wantToken: "giteaapitoken", wantOK: true},
		{name: "no token after prefix", header: "Bearer     "},
		{name: "no space after prefix", header: "Bearertoken"},
		{name: "other scheme", header: "Basic dXNlcjpwYXNz"},
		{name: "empty header", header: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gotToken, gotOK := ParseToken(tt.header)
			if gotToken != tt.wantToken || gotOK != tt.wantOK {
				t.Errorf("ParseToken(%q) = %q, %v, want %q, %v", tt.header, gotToken, gotOK, tt.wantToken, tt.wantOK)
			}
		})
	}
}

func TestProviderMetadata(t *testing.T) {
	// Pinned as a wire format: resource has to match the URL entered in the client.
	want := `{"resource":"https://mcp.example.com/mcp","authorization_servers":["https://gitea.example.com"],` +
		`"scopes_supported":["write:repository","write:issue","write:user","write:organization","write:notification","write:package","write:misc"],` +
		`"bearer_methods_supported":["header"]}` + "\n"
	if got := metadataBody(t, testConfig, wellKnownPath+"/mcp"); got != want {
		t.Errorf("body =\n%s\nwant\n%s", got, want)
	}
	if got := metadataBody(t, testConfig, wellKnownPath); got != want {
		t.Errorf("bare probe body =\n%s\nwant\n%s", got, want)
	}

	readOnly := testConfig
	readOnly.ReadOnly = true
	wantScopes := `"scopes_supported":["read:repository","read:issue","read:user","read:organization","read:notification","read:package","read:misc"]`
	if got := metadataBody(t, readOnly, wellKnownPath+"/mcp"); !strings.Contains(got, wantScopes) {
		t.Errorf("read-only body = %s, want it to contain %s", got, wantScopes)
	}
}

func TestProviderProtect(t *testing.T) {
	var reached bool
	handler := New(testConfig).Protect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}), func(_ context.Context, token string) bool { return token == "expired" })

	for _, tt := range []struct {
		name        string
		host        string
		header      string
		wantCode    int
		wantReach   bool
		wantInvalid bool
	}{
		{name: "no credentials", wantCode: http.StatusUnauthorized},
		{name: "unusable credentials", header: "Basic dXNlcjpwYXNz", wantCode: http.StatusUnauthorized},
		{name: "bearer token", header: "Bearer sometoken", wantCode: http.StatusOK, wantReach: true},
		{name: "rejected token", header: "Bearer expired", wantCode: http.StatusUnauthorized, wantInvalid: true},
		// A proxy may forward its own Host rather than the public one.
		{name: "loopback host", host: "127.0.0.1:8080", header: "Bearer sometoken", wantCode: http.StatusOK, wantReach: true},
		{name: "localhost host", host: "localhost:8080", header: "Bearer sometoken", wantCode: http.StatusOK, wantReach: true},
		{name: "ipv6 loopback host", host: "[::1]:8080", header: "Bearer sometoken", wantCode: http.StatusOK, wantReach: true},
		{name: "rebinding host", host: "evil.com", header: "Bearer sometoken", wantCode: http.StatusMisdirectedRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reached = false
			request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			request.Host = "mcp.example.com"
			if tt.host != "" {
				request.Host = tt.host
			}
			if tt.header != "" {
				request.Header.Set("Authorization", tt.header)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantCode)
			}
			if reached != tt.wantReach {
				t.Errorf("handler reached = %v, want %v", reached, tt.wantReach)
			}
			if tt.wantCode != http.StatusUnauthorized {
				return
			}
			want := `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource/mcp", ` +
				`scope="write:repository write:issue write:user write:organization write:notification write:package write:misc"`
			if tt.wantInvalid {
				want += `, error="invalid_token"`
			}
			if got := recorder.Header().Get("WWW-Authenticate"); got != want {
				t.Errorf("WWW-Authenticate = %q, want %q", got, want)
			}
		})
	}
}
