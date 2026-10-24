package gitea

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/gitea/gitea-mcp/internal/flag"
	"gitea.com/gitea/gitea-mcp/internal/log"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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

	transport, err := newHTTPTransport(false, listener.Addr().String(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
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

func TestNewHTTPTransportTLS(t *testing.T) {
	if transport, err := newHTTPTransport(true, "", "", "", ""); err != nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Errorf("insecure: err = %v, want InsecureSkipVerify", err)
	}

	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key")
	writePair := func() []byte {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(1), NotAfter: time.Now().Add(time.Hour)}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
			t.Fatal(err)
		}
		return der
	}
	caDER := writePair()

	for name, files := range map[string][3]string{
		"cert without key":  {certPath, "", ""},
		"unloadable pair":   {keyPath, keyPath, ""},
		"missing CA file":   {"", "", filepath.Join(dir, "missing.crt")},
		"CA without a cert": {"", "", keyPath},
	} {
		if _, err := newHTTPTransport(false, "", files[0], files[1], files[2]); err == nil {
			t.Errorf("%s: err = nil, want an error", name)
		}
	}

	transport, err := newHTTPTransport(false, "", certPath, keyPath, certPath)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ca.Verify(x509.VerifyOptions{Roots: transport.TLSClientConfig.RootCAs}); err != nil {
		t.Errorf("CA not trusted: %v", err)
	}
	renewed := writePair()
	info := &tls.CertificateRequestInfo{
		Version:          tls.VersionTLS13,
		SignatureSchemes: []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256},
		AcceptableCAs:    [][]byte{ca.RawIssuer},
	}
	if pair, err := transport.TLSClientConfig.GetClientCertificate(info); err != nil || len(pair.Certificate) == 0 || !bytes.Equal(pair.Certificate[0], renewed) {
		t.Errorf("err = %v, want the renewed certificate", err)
	}
	info.AcceptableCAs = [][]byte{[]byte("another CA")}
	if pair, err := transport.TLSClientConfig.GetClientCertificate(info); err != nil || len(pair.Certificate) != 0 {
		t.Errorf("err = %v, want no certificate for a server accepting another CA", err)
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

func TestDebugLogsGiteaRequestsInsteadOfPrintingToStdout(t *testing.T) {
	giteaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer giteaSrv.Close()
	origHost, origDebug := flag.Host, flag.Debug
	defer func() { flag.Host, flag.Debug = origHost, origDebug }()
	flag.Host, flag.Debug = giteaSrv.URL, true
	core, logs := observer.New(zapcore.DebugLevel)
	defer log.SetDefault(log.Default())
	log.SetDefault(zap.New(core))

	client, err := NewClient("the-token")
	if err != nil {
		t.Fatal(err)
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdout := os.Stdout
	os.Stdout = stdoutWriter
	_, _, _ = client.Users.GetMyUserInfo(t.Context())
	os.Stdout = origStdout
	_ = stdoutWriter.Close()
	stdout, _ := io.ReadAll(stdoutReader)

	want := "GET " + giteaSrv.URL + "/api/v1/user: 401 Unauthorized"
	if len(stdout) != 0 || logs.FilterMessage(want).Len() != 1 {
		t.Errorf("stdout %q, logs %v, want only the log %q", stdout, logs.All(), want)
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
