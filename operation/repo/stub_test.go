package repo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/flag"
)

func serveStub(t *testing.T, handler http.HandlerFunc) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			_, _ = w.Write([]byte(`{"version":"1.27.0"}`))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	origHost, origToken := flag.Host, flag.Token
	flag.Host, flag.Token = server.URL, ""
	t.Cleanup(func() { flag.Host, flag.Token = origHost, origToken })
}
