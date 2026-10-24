package cmd

import (
	"bytes"
	"flag"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	flagPkg "gitea.com/gitea/gitea-mcp/internal/flag"
)

func TestInitFlagSetBind(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "default is empty, meaning all interfaces", args: []string{}},
		{name: "-b sets the address", args: []string{"-b", "127.0.0.1"}, want: "127.0.0.1"},
		{name: "-bind sets an IPv6 literal", args: []string{"-bind", "::1"}, want: "::1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Cleanup(func() { flagPkg.Bind = "" })
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			initFlagSet(fs, test.args, func(string) string { return "" }, func(string) ([]byte, error) { return nil, nil }, &bytes.Buffer{})
			if flagPkg.Bind != test.want {
				t.Errorf("Bind = %q, want %q", flagPkg.Bind, test.want)
			}
		})
	}
}

func TestInitFlagSetOAuth(t *testing.T) {
	for _, test := range []struct {
		name     string
		args     []string
		env      map[string]string
		want     bool
		wantURL  string
		wantExit bool
	}{
		{name: "off by default"},
		{
			name:    "flags enable it and drop the static token",
			args:    []string{"-t", "http", "-oauth", "-oauth-public-url", "https://mcp.example.com", "-T", "statictoken"},
			want:    true,
			wantURL: "https://mcp.example.com",
		},
		{
			name:    "env enables it and trims the trailing slash",
			args:    []string{"-t", "http"},
			env:     map[string]string{"GITEA_MCP_OAUTH": "true", "GITEA_MCP_PUBLIC_URL": "https://mcp.example.com/"},
			want:    true,
			wantURL: "https://mcp.example.com",
		},
		{name: "public URL is required", args: []string{"-t", "http", "-oauth"}, wantExit: true},
		{name: "public URL must not have a path", args: []string{"-t", "http", "-oauth", "-oauth-public-url", "https://mcp.example.com/base"}, wantExit: true},
		{name: "stdio transport is rejected", args: []string{"-oauth", "-oauth-public-url", "https://mcp.example.com"}, wantExit: true},
		{
			name:     "client certificate is rejected",
			args:     []string{"-t", "http", "-oauth", "-oauth-public-url", "https://mcp.example.com"},
			env:      map[string]string{"GITEA_CLIENT_CERT": "client.crt"},
			wantExit: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			origExit := osExit
			t.Cleanup(func() {
				osExit = origExit
				flagPkg.OAuth, flagPkg.OAuthPublicURL, flagPkg.Token, flagPkg.ClientCertFile = false, "", "", ""
			})
			var exits int
			osExit = func(int) { exits++ }

			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			initFlagSet(fs, test.args, func(key string) string { return test.env[key] }, func(string) ([]byte, error) { return nil, nil }, &bytes.Buffer{})

			if (exits > 0) != test.wantExit {
				t.Fatalf("exits = %d, want exit %v", exits, test.wantExit)
			}
			if test.wantExit {
				return // state after a rejected flag set is not reached in the real binary
			}
			if flagPkg.OAuth != test.want {
				t.Errorf("OAuth = %v, want %v", flagPkg.OAuth, test.want)
			}
			if flagPkg.OAuthPublicURL != test.wantURL {
				t.Errorf("OAuthPublicURL = %q, want %q", flagPkg.OAuthPublicURL, test.wantURL)
			}
			if test.want && flagPkg.Token != "" {
				t.Errorf("Token = %q, want it dropped in OAuth mode", flagPkg.Token)
			}
		})
	}
}

func TestInitFlagSetScopes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want []string
	}{
		{
			name: "no scope flag or env leaves AllowedScopes unset",
			args: []string{},
			want: nil,
		},
		{
			name: "-S sets a single scope",
			args: []string{"-S", "repository"},
			want: []string{"repository"},
		},
		{
			name: "-scope sets a comma-separated list",
			args: []string{"-scope", "repository,file"},
			want: []string{"file", "repository"},
		},
		{
			name: "GITEA_SCOPES env sets the default",
			args: []string{},
			env:  map[string]string{"GITEA_SCOPES": "issue,pull_request"},
			want: []string{"issue", "pull_request"},
		},
		{
			name: "-S flag takes precedence over GITEA_SCOPES env",
			args: []string{"-S", "file"},
			env:  map[string]string{"GITEA_SCOPES": "issue"},
			want: []string{"file"},
		},
		{
			name: "normalizes case, whitespace, and hyphens/spaces to underscores",
			args: []string{"-S", " Pull Request , pull-request , PULL_REQUEST "},
			want: []string{"pull_request"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origScopes := flagPkg.AllowedScopes
			t.Cleanup(func() {
				flagPkg.AllowedScopes = origScopes
			})
			flagPkg.AllowedScopes = nil

			getenv := func(key string) string { return tt.env[key] }
			readFile := func(string) ([]byte, error) { return nil, nil }
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			var stderr bytes.Buffer

			initFlagSet(fs, tt.args, getenv, readFile, &stderr)

			got := slices.Sorted(maps.Keys(flagPkg.AllowedScopes))
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("AllowedScopes = %v, want %v", got, want)
			}
		})
	}
}

func TestInitFlagSetHealthcheckSkipsServerValidation(t *testing.T) {
	origExit := osExit
	t.Cleanup(func() {
		osExit = origExit
		healthcheck, flagPkg.Port, flagPkg.HealthcheckFile = false, 0, ""
	})
	osExit = func(int) { t.Error("osExit called, want the probe to skip server validation") }
	env := map[string]string{
		"GITEA_MCP_HEALTHCHECK_FILE": "/tmp/state",
		"GITEA_MCP_OAUTH":            "true",
		"GITEA_ACCESS_TOKEN_FILE":    "/nonexistent",
	}
	initFlagSet(flag.NewFlagSet("test", flag.ContinueOnError), []string{"-healthcheck", "-p", "9000"}, func(key string) string { return env[key] }, os.ReadFile, &bytes.Buffer{})
	if !healthcheck || flagPkg.Port != 9000 || flagPkg.HealthcheckFile != "/tmp/state" {
		t.Errorf("healthcheck, Port, HealthcheckFile = %v, %d, %q, want true, 9000, /tmp/state", healthcheck, flagPkg.Port, flagPkg.HealthcheckFile)
	}
}

func TestInitFlagSetGiteaUnixSocket(t *testing.T) {
	for _, test := range []struct {
		name       string
		args       []string
		env        map[string]string
		wantHost   string
		wantSocket string
		wantExit   bool
	}{
		{name: "no socket keeps the default host", wantHost: "https://gitea.com"},
		{
			name:       "env sets the socket and defaults the host",
			env:        map[string]string{"GITEA_UNIX_SOCKET": "/run/gitea/gitea.sock"},
			wantHost:   "http://unix/",
			wantSocket: "/run/gitea/gitea.sock",
		},
		{
			name:       "flag keeps an explicit http host with a sub-path",
			args:       []string{"-gitea-unix-socket", "/run/gitea/gitea.sock", "-host", "http://gitea.local/git"},
			wantHost:   "http://gitea.local/git",
			wantSocket: "/run/gitea/gitea.sock",
		},
		{name: "https host is rejected", args: []string{"-gitea-unix-socket", "/run/gitea/gitea.sock", "-host", "https://gitea.com"}, wantExit: true},
		{name: "socket path as host is rejected", args: []string{"-gitea-unix-socket", "/run/gitea/gitea.sock", "-host", "http:///run/gitea/gitea.sock"}, wantExit: true},
		{
			name:     "oauth is rejected",
			args:     []string{"-gitea-unix-socket", "/run/gitea/gitea.sock", "-t", "http", "-oauth", "-oauth-public-url", "https://mcp.example.com"},
			wantExit: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			origExit := osExit
			t.Cleanup(func() {
				osExit = origExit
				flagPkg.Host, flagPkg.GiteaUnixSocket, flagPkg.OAuth, flagPkg.OAuthPublicURL = "", "", false, ""
			})
			var exits int
			osExit = func(int) { exits++ }

			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			initFlagSet(fs, test.args, func(key string) string { return test.env[key] }, func(string) ([]byte, error) { return nil, nil }, &bytes.Buffer{})

			if (exits > 0) != test.wantExit {
				t.Fatalf("exits = %d, want exit %v", exits, test.wantExit)
			}
			if test.wantExit {
				return
			}
			if flagPkg.Host != test.wantHost || flagPkg.GiteaUnixSocket != test.wantSocket {
				t.Errorf("Host, GiteaUnixSocket = %q, %q, want %q, %q", flagPkg.Host, flagPkg.GiteaUnixSocket, test.wantHost, test.wantSocket)
			}
		})
	}
}

func TestInitFlagSetTLSFlagsOverrideEnvAndWarnWhenInsecure(t *testing.T) {
	t.Cleanup(func() {
		flagPkg.ClientCertFile, flagPkg.ClientKeyFile, flagPkg.CACertFile, flagPkg.Insecure = "", "", "", false
	})
	env := map[string]string{"GITEA_CLIENT_CERT": "env.crt", "GITEA_CLIENT_KEY": "env.key", "GITEA_CA_CERT": "env-ca.crt"}
	var stderr bytes.Buffer
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	initFlagSet(fs, []string{"-client-cert", "flag.crt", "-insecure"}, func(key string) string { return env[key] }, func(string) ([]byte, error) { return nil, nil }, &stderr)
	if flagPkg.ClientCertFile != "flag.crt" || flagPkg.ClientKeyFile != "env.key" || flagPkg.CACertFile != "env-ca.crt" {
		t.Errorf("cert, key, CA = %q, %q, %q, want flag.crt, env.key, env-ca.crt", flagPkg.ClientCertFile, flagPkg.ClientKeyFile, flagPkg.CACertFile)
	}
	if !strings.Contains(stderr.String(), "-ca-cert has no effect with -insecure") {
		t.Errorf("stderr = %q, want the -insecure warning", stderr.String())
	}
}

func TestInitFlagSetExtraHeaders(t *testing.T) {
	origExit := osExit
	t.Cleanup(func() { osExit, flagPkg.ExtraHeaders = origExit, nil })
	var exitCode int
	osExit = func(code int) { exitCode = code }
	run := func(env string) string {
		var stderr bytes.Buffer
		getenv := func(key string) string { return map[string]string{"GITEA_EXTRA_HEADERS": env}[key] }
		initFlagSet(flag.NewFlagSet("test", flag.ContinueOnError), nil, getenv, func(string) ([]byte, error) { return nil, nil }, &stderr)
		return stderr.String()
	}

	run(`{"CF-Access-Client-Id":"id"}`)
	if exitCode != 0 || flagPkg.ExtraHeaders["CF-Access-Client-Id"] != "id" {
		t.Errorf("exitCode, ExtraHeaders = %d, %v, want 0 and the parsed header", exitCode, flagPkg.ExtraHeaders)
	}
	if stderr := run("not-json"); exitCode != 1 || !strings.Contains(stderr, "GITEA_EXTRA_HEADERS") {
		t.Errorf("exitCode, stderr = %d, %q, want 1 and a GITEA_EXTRA_HEADERS diagnostic", exitCode, stderr)
	}
}
