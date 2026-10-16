package gitea

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	mcpContext "gitea.com/gitea/gitea-mcp/pkg/context"
	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"gitea.dev/sdk"
)

var (
	clientCache     sync.Map // token -> *gitea.Client
	sharedTransOnce sync.Once
	sharedTrans     http.RoundTripper
)

func sharedTransport() http.RoundTripper {
	sharedTransOnce.Do(func() {
		sharedTrans = &extraHeaderTransport{base: newHTTPTransport(flag.Insecure, flag.GiteaUnixSocket)}
	})
	return sharedTrans
}

type extraHeaderTransport struct { // the SDK has no per-request header hook
	base http.RoundTripper
}

func (t *extraHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if len(flag.ExtraHeaders) == 0 || !isGiteaOrigin(req.URL) { // redirects re-enter here and must not carry them elsewhere
		return t.base.RoundTrip(req)
	}
	req = req.Clone(req.Context())
	for name, value := range flag.ExtraHeaders {
		if !hasHeader(req.Header, name) {
			req.Header.Set(name, value)
		}
	}
	return t.base.RoundTrip(req)
}

func hasHeader(header http.Header, name string) bool { // the SDK sets non-canonical keys like "content-type"
	for key := range header {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func isGiteaOrigin(reqURL *url.URL) bool {
	giteaURL, err := url.Parse(flag.Host)
	return err == nil && reqURL.Scheme == giteaURL.Scheme && reqURL.Hostname() == giteaURL.Hostname() &&
		portOrDefault(reqURL) == portOrDefault(giteaURL)
}

func portOrDefault(target *url.URL) string {
	return cmp.Or(target.Port(), map[string]string{"http": "80", "https": "443"}[target.Scheme])
}

func newHTTPTransport(insecure bool, unixSocket string) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // user-requested insecure mode
	}
	if unixSocket != "" {
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) { // ignores addr so redirects can't leave the socket
			return (&net.Dialer{}).DialContext(ctx, "unix", unixSocket)
		}
	}
	return transport
}

// NewClient returns a cached *gitea.Client keyed by host+token. The SDK's per-client
// version cache and the shared transport let us reuse keep-alive connections
// and avoid the SDK's /api/v1/version preflight on every tool call.
func NewClient(token string) (*gitea.Client, error) {
	key := flag.Host + "\x00" + token
	if v, ok := clientCache.Load(key); ok {
		return v.(*gitea.Client), nil
	}

	httpClient := &http.Client{
		Transport:     sharedTransport(),
		CheckRedirect: checkRedirect,
	}
	opts := []gitea.ClientOption{
		gitea.SetToken(token),
		gitea.SetHTTPClient(httpClient),
	}
	if flag.Debug {
		opts = append(opts, gitea.SetDebugMode())
	}
	client, err := gitea.NewClient(flag.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("create gitea client err: %w", err)
	}
	client.SetUserAgent("gitea-mcp-server/" + flag.Version)

	actual, _ := clientCache.LoadOrStore(key, client)
	return actual.(*gitea.Client), nil
}

// checkRedirect prevents Go from silently changing mutating requests (POST, PATCH, etc.)
// to GET when following 301/302/303 redirects, which would drop the request body and
// make writes appear to succeed when they didn't.
func checkRedirect(_ *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if via[0].Method != http.MethodGet && via[0].Method != http.MethodHead {
		return http.ErrUseLastResponse
	}
	return nil
}

func ClientFromContext(ctx context.Context) (*gitea.Client, error) {
	token, ok := ctx.Value(mcpContext.TokenContextKey).(string)
	if !ok {
		token = flag.Token
	}
	return NewClient(token)
}
