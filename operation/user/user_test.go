package user

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCreateOrganization(t *testing.T) {
	var gotRequest string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotRequest = strings.TrimSpace(r.Method + " " + r.URL.RequestURI() + " " + string(body))
		_, _ = w.Write([]byte(`{"id":7,"name":"acme","full_name":"Acme","description":"A demo organization","website":"https://example.com"}`))
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL

	result, err := CreateOrganizationFn(t.Context(), map[string]any{
		"username":    "acme",
		"full_name":   "Acme",
		"description": "A demo organization",
		"visibility":  "private",
		"website":     "https://example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := gotRequest, `POST /api/v1/orgs {"username":"acme","full_name":"Acme","email":"","description":"A demo organization","website":"https://example.com","location":"","visibility":"private","repo_admin_change_team_access":false}`; got != want {
		t.Errorf("request = %q, want %q", got, want)
	}
	if got, want := result.Content[0].(*mcp.TextContent).Text, `{"avatar_url":"","description":"A demo organization","full_name":"Acme","id":7,"name":"acme","website":"https://example.com"}`; got != want {
		t.Errorf("result = %s, want %s", got, want)
	}

	result, err = CreateOrganizationFn(t.Context(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Content[0].(*mcp.TextContent).Text, "username is required"; got != want {
		t.Errorf("missing username result = %q, want %q", got, want)
	}
}
