package gitea

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"
)

func TestNewHTTPTransportPinsRequestsAndRedirectsToUnixSocket(t *testing.T) {
	t.Chdir(t.TempDir())
	listener, err := net.Listen("unix", "gitea.sock")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "unix" {
			http.Redirect(w, r, "http://example.com/api/v1/version", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, r.Host+r.URL.Path)
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	transport := newHTTPTransport(false, listener.Addr().String())
	if transport.Proxy != nil {
		t.Error("Proxy is set, want nil so proxy settings are ignored")
	}
	resp, err := (&http.Client{Transport: transport}).Get("http://unix/api/v1/version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "example.com/api/v1/version" {
		t.Errorf("body = %q, want the redirect served over the socket", body)
	}
}

func TestExtraHeadersReachOnlyGiteaOriginWithoutOverridingRESTOrSDKHeaders(t *testing.T) {
	var otherSecret string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherSecret = r.Header.Get("CF-Access-Client-Secret")
		_, _ = io.WriteString(w, "[]")
	}))
	defer other.Close()
	var giteaHeader http.Header
	giteaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		giteaHeader = r.Header
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer giteaSrv.Close()

	origHost, origToken, origHeaders := flag.Host, flag.Token, flag.ExtraHeaders
	defer func() { flag.Host, flag.Token, flag.ExtraHeaders = origHost, origToken, origHeaders }()
	flag.Host, flag.Token = giteaSrv.URL, "the-token"
	flag.ExtraHeaders = map[string]string{"CF-Access-Client-Secret": "secret", "Authorization": "override", "Content-Type": "text/plain"}

	client, err := NewClient("the-token")
	if err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"rest": func() error { _, err := DoJSON(t.Context(), http.MethodGet, "user", nil, nil, nil); return err },
		"sdk":  func() error { _, _, err := client.Repositories.GetMyStarredRepos(t.Context()); return err },
	} {
		otherSecret = ""
		if err := call(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if giteaHeader.Get("CF-Access-Client-Secret") != "secret" || giteaHeader.Get("Authorization") != "token the-token" ||
			len(giteaHeader.Values("Content-Type")) != 1 || otherSecret != "" {
			t.Errorf("%s: gitea headers %v, redirect target secret %q", name, giteaHeader, otherSecret)
		}
	}
}

func TestIsGiteaOriginNormalizesOnlyTheDefaultPort(t *testing.T) {
	origHost := flag.Host
	defer func() { flag.Host = origHost }()
	flag.Host = "https://gitea.test"
	for raw, want := range map[string]bool{
		"https://gitea.test:443/x": true,
		"http://gitea.test:443/x":  false,
		"https://Gitea.test/x":     false,
	} {
		reqURL, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isGiteaOrigin(reqURL); got != want {
			t.Errorf("isGiteaOrigin(%q) = %v, want %v", raw, got, want)
		}
	}
}
