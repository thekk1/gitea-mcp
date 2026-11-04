package cmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"
	"unicode/utf16"

	flagPkg "gitea.com/gitea/gitea-mcp/internal/flag"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
	"gitea.com/gitea/gitea-mcp/internal/log"
	"gitea.com/gitea/gitea-mcp/operation"
)

var (
	host                            string
	bind                            string
	token                           string
	tools                           string
	scopes                          string
	oauthPublicURL                  string
	version                         bool
	healthcheck                     bool
	maxInlineAttachmentBytes        int
	maxInlineAttachmentBytesFlagSet bool
	osExit                          = os.Exit
)

func initFlagSet(fs *flag.FlagSet, args []string, getenv func(string) string, readFile func(string) ([]byte, error), stderr io.Writer) {
	fs.StringVar(&flagPkg.Mode, "t", "stdio", "")
	fs.StringVar(&flagPkg.Mode, "transport", "stdio", "")
	fs.StringVar(&host, "H", getenv("GITEA_HOST"), "")
	fs.StringVar(&host, "host", getenv("GITEA_HOST"), "")
	fs.StringVar(&flagPkg.GiteaUnixSocket, "gitea-unix-socket", getenv("GITEA_UNIX_SOCKET"), "")
	fs.StringVar(&bind, "b", "", "")
	fs.StringVar(&bind, "bind", "", "")
	fs.IntVar(&flagPkg.Port, "p", 8080, "")
	fs.IntVar(&flagPkg.Port, "port", 8080, "")
	fs.StringVar(&token, "T", "", "")
	fs.StringVar(&token, "token", "", "")
	fs.BoolVar(&flagPkg.ReadOnly, "r", false, "")
	fs.BoolVar(&flagPkg.ReadOnly, "read-only", false, "")
	defaultTools := getenv("GITEA_TOOLS")
	fs.StringVar(&tools, "O", defaultTools, "")
	fs.StringVar(&tools, "tools", defaultTools, "")
	defaultScopes := getenv("GITEA_SCOPES")
	fs.StringVar(&scopes, "S", defaultScopes, "")
	fs.StringVar(&scopes, "scope", defaultScopes, "")
	fs.BoolVar(&flagPkg.Debug, "d", false, "")
	fs.BoolVar(&flagPkg.Debug, "debug", false, "")
	fs.BoolVar(&flagPkg.Insecure, "k", false, "")
	fs.BoolVar(&flagPkg.Insecure, "insecure", false, "")
	fs.StringVar(&flagPkg.ClientCertFile, "client-cert", getenv("GITEA_CLIENT_CERT"), "")
	fs.StringVar(&flagPkg.ClientKeyFile, "client-key", getenv("GITEA_CLIENT_KEY"), "")
	fs.StringVar(&flagPkg.CACertFile, "ca-cert", getenv("GITEA_CA_CERT"), "")
	fs.BoolVar(&flagPkg.OAuth, "oauth", false, "")
	fs.StringVar(&oauthPublicURL, "oauth-public-url", "", "")
	fs.BoolVar(&version, "v", false, "")
	fs.BoolVar(&version, "version", false, "")
	fs.BoolVar(&healthcheck, "healthcheck", false, "")
	maxInlineAttachmentBytes = 5 * 1024 * 1024
	fs.Func("max-inline-attachment-bytes", "", func(val string) error {
		parsed, err := strconv.Atoi(val)
		if err != nil || parsed < 0 {
			return fmt.Errorf("invalid value %q", val)
		}
		maxInlineAttachmentBytes = parsed
		maxInlineAttachmentBytesFlagSet = true
		return nil
	})

	fs.Usage = func() {
		w := tabwriter.NewWriter(stderr, 0, 0, 3, ' ', 0)
		fmt.Fprintln(stderr, "Usage: gitea-mcp [options]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Options:")
		fmt.Fprintf(w, "  -t, -transport <type>\tTransport type: stdio or http (default: stdio)\n")
		fmt.Fprintf(w, "  -H, -host <url>\tGitea host URL (default: https://gitea.com)\n")
		fmt.Fprintf(w, "  -gitea-unix-socket <path>\tConnect to Gitea through a Unix socket\n")
		fmt.Fprintf(w, "  -b, -bind <address>\tHTTP listen address, e.g. 127.0.0.1 (default: all interfaces)\n")
		fmt.Fprintf(w, "  -p, -port <number>\tHTTP server port (default: 8080)\n")
		fmt.Fprintf(w, "  -T, -token <token>\tPersonal access token\n")
		fmt.Fprintf(w, "  -r, -read-only\tExpose only read-only tools\n")
		fmt.Fprintf(w, "  -O, -tools <names>\tComma-separated list of tool names to expose\n")
		fmt.Fprintf(w, "  -S, -scope <names>\tComma-separated list of tool scopes to expose\n")
		fmt.Fprintf(w, "  -d, -debug\tEnable debug mode\n")
		fmt.Fprintf(w, "  -k, -insecure\tIgnore TLS certificate errors\n")
		fmt.Fprintf(w, "  -client-cert <path>\tPEM client certificate to present to the Gitea host (mTLS)\n")
		fmt.Fprintf(w, "  -client-key <path>\tPEM private key matching the client certificate\n")
		fmt.Fprintf(w, "  -ca-cert <path>\tPEM CA bundle used to verify the Gitea host\n")
		fmt.Fprintf(w, "  -oauth\tLet clients authorize against Gitea instead of using a static token (http only)\n")
		fmt.Fprintf(w, "  -oauth-public-url <url>\tPublic origin of this server, required with -oauth\n")
		fmt.Fprintf(w, "  -max-inline-attachment-bytes <bytes>\tInline image attachments up to this size (default: 5242880)\n")
		fmt.Fprintf(w, "  -v, -version\tPrint version and exit\n")
		fmt.Fprintf(w, "  -healthcheck\tCheck server health and exit\n")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Environment variables:")
		fmt.Fprintf(w, "  GITEA_ACCESS_TOKEN\tProvide access token\n")
		fmt.Fprintf(w, "  GITEA_ACCESS_TOKEN_FILE\tPath to a file containing the access token (e.g. a Docker secret)\n")
		fmt.Fprintf(w, "  GITEA_DEBUG\tSet to 'true' for debug mode\n")
		fmt.Fprintf(w, "  GITEA_EXTRA_HEADERS\tJSON object of extra headers to send to Gitea\n")
		fmt.Fprintf(w, "  GITEA_HOST\tOverride Gitea host URL\n")
		fmt.Fprintf(w, "  GITEA_UNIX_SOCKET\tConnect to Gitea through a Unix socket\n")
		fmt.Fprintf(w, "  GITEA_INSECURE\tSet to 'true' to ignore TLS errors\n")
		fmt.Fprintf(w, "  GITEA_CLIENT_CERT\tPath to PEM client certificate for mTLS\n")
		fmt.Fprintf(w, "  GITEA_CLIENT_KEY\tPath to PEM private key for the client certificate\n")
		fmt.Fprintf(w, "  GITEA_CA_CERT\tPath to PEM CA bundle to verify the Gitea host\n")
		fmt.Fprintf(w, "  GITEA_MAX_INLINE_ATTACHMENT_BYTES\tOverride inline image attachment size limit in bytes\n")
		fmt.Fprintf(w, "  GITEA_MCP_HEALTHCHECK_FILE\tFile where the server records its mode and address for -healthcheck\n")
		fmt.Fprintf(w, "  GITEA_MCP_OAUTH\tSet to 'true' to enable OAuth\n")
		fmt.Fprintf(w, "  GITEA_MCP_PUBLIC_URL\tPublic origin of this server\n")
		fmt.Fprintf(w, "  GITEA_READONLY\tSet to 'true' for read-only mode\n")
		fmt.Fprintf(w, "  GITEA_SCOPES\tComma-separated list of tool scopes to expose\n")
		fmt.Fprintf(w, "  GITEA_TOOLS\tComma-separated list of tool names to expose\n")
		fmt.Fprintf(w, "  MCP_MODE\tOverride transport mode\n")
		_ = w.Flush()
	}

	_ = fs.Parse(args)

	flagPkg.HealthcheckFile = getenv("GITEA_MCP_HEALTHCHECK_FILE")
	if healthcheck {
		return
	}

	switch {
	case host != "":
		flagPkg.Host = host
	case flagPkg.GiteaUnixSocket != "":
		flagPkg.Host = "http://unix/"
	default:
		flagPkg.Host = "https://gitea.com"
	}
	if flagPkg.GiteaUnixSocket != "" {
		if parsed, err := url.Parse(flagPkg.Host); err != nil || parsed.Scheme != "http" || parsed.Host == "" {
			fmt.Fprintf(stderr, "-gitea-unix-socket requires an http:// host, got %q\n", flagPkg.Host)
			osExit(1)
		}
	}

	flagPkg.Bind = bind
	flagPkg.MaxInlineAttachmentBytes = maxInlineAttachmentBytes

	flagPkg.Token = token
	if flagPkg.Token == "" {
		flagPkg.Token = getenv("GITEA_ACCESS_TOKEN")
	}
	if flagPkg.Token == "" {
		if tokenFile := getenv("GITEA_ACCESS_TOKEN_FILE"); tokenFile != "" {
			data, err := readFile(tokenFile)
			if err != nil {
				fmt.Fprintf(stderr, "error reading GITEA_ACCESS_TOKEN_FILE: %v\n", err)
				osExit(1)
			}
			flagPkg.Token = strings.TrimSpace(decodeTokenFile(data))
			if strings.ContainsFunc(flagPkg.Token, unicode.IsControl) {
				fmt.Fprintln(stderr, "invalid GITEA_ACCESS_TOKEN_FILE: control characters, save it as UTF-8 or UTF-16LE")
				osExit(1)
			}
		}
	}

	if getenv("MCP_MODE") != "" {
		flagPkg.Mode = getenv("MCP_MODE")
	}
	if getenv("GITEA_READONLY") == "true" {
		flagPkg.ReadOnly = true
	}

	allowed := map[string]struct{}{}
	for t := range strings.SplitSeq(tools, ",") {
		if t = strings.TrimSpace(t); t != "" {
			allowed[t] = struct{}{}
		}
	}
	if len(allowed) > 0 {
		flagPkg.AllowedTools = allowed
	}

	allowedScopes := map[string]struct{}{}
	for s := range strings.SplitSeq(scopes, ",") {
		if s = normalizeScope(s); s != "" {
			allowedScopes[s] = struct{}{}
		}
	}
	if len(allowedScopes) > 0 {
		flagPkg.AllowedScopes = allowedScopes
	}
	if getenv("GITEA_DEBUG") == "true" {
		flagPkg.Debug = true
	}
	if getenv("GITEA_INSECURE") == "true" {
		flagPkg.Insecure = true
	}
	if flagPkg.Insecure && flagPkg.CACertFile != "" {
		fmt.Fprintf(stderr, "warning: -ca-cert has no effect with -insecure, server verification is disabled\n")
	}
	if getenv("GITEA_MCP_OAUTH") == "true" {
		flagPkg.OAuth = true
	}
	if flagPkg.OAuth {
		if oauthPublicURL == "" {
			oauthPublicURL = getenv("GITEA_MCP_PUBLIC_URL")
		}
		flagPkg.OAuthPublicURL = strings.TrimSuffix(oauthPublicURL, "/")
		if flagPkg.Mode != "http" {
			fmt.Fprintf(stderr, "-oauth requires -transport http\n")
			osExit(1)
		}
		if flagPkg.GiteaUnixSocket != "" {
			fmt.Fprintf(stderr, "-oauth cannot be combined with -gitea-unix-socket\n")
			osExit(1)
		}
		if flagPkg.ClientCertFile != "" {
			fmt.Fprintf(stderr, "-oauth cannot be combined with -client-cert, Gitea may map the certificate to a user every caller would act as\n")
			osExit(1)
		}
		if flagPkg.OAuthPublicURL == "" {
			fmt.Fprintf(stderr, "-oauth requires -oauth-public-url or GITEA_MCP_PUBLIC_URL\n")
			osExit(1)
		} else if !isOrigin(flagPkg.OAuthPublicURL) {
			fmt.Fprintf(stderr, "invalid -oauth-public-url: %q must be an origin like https://mcp.example.com\n", flagPkg.OAuthPublicURL)
			osExit(1)
		}
		flagPkg.Token = "" // every request carries its own token, a static one would let a caller act as the operator
	}
	if !maxInlineAttachmentBytesFlagSet {
		if val := getenv("GITEA_MAX_INLINE_ATTACHMENT_BYTES"); val != "" {
			parsed, err := strconv.Atoi(val)
			if err != nil || parsed < 0 {
				fmt.Fprintf(stderr, "invalid GITEA_MAX_INLINE_ATTACHMENT_BYTES: %q\n", val)
				osExit(1)
			}
			flagPkg.MaxInlineAttachmentBytes = parsed
		}
	}
	if val := getenv("GITEA_EXTRA_HEADERS"); val != "" {
		if err := json.Unmarshal([]byte(val), &flagPkg.ExtraHeaders); err != nil {
			fmt.Fprintf(stderr, "invalid GITEA_EXTRA_HEADERS: %v\n", err)
			osExit(1)
		}
	}
}

// isOrigin reports whether raw is a bare origin, required because the discovery
// document is served from the root.
func isOrigin(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") &&
		parsed.Host != "" && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

// normalizeScope trims whitespace, lowercases, and converts internal spaces
// and hyphens to underscores, so "Pull Request", "pull-request", and
// "PULL_REQUEST" all normalize to "pull_request".
func normalizeScope(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

func Execute() {
	initFlagSet(flag.CommandLine, os.Args[1:], os.Getenv, os.ReadFile, os.Stderr)
	if version {
		fmt.Fprintln(os.Stdout, flagPkg.Version)
		return
	}
	if healthcheck {
		if runHealthcheck(flagPkg.HealthcheckFile, flagPkg.Port, os.Stdout, os.Stderr) {
			osExit(0)
		} else {
			osExit(1)
		}
		return
	}
	if _, err := gitea.InitTransport(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid TLS configuration: %v\n", err)
		osExit(1)
		return
	}
	if err := operation.Run(); err != nil {
		if err == context.Canceled {
			log.Info("Server shutdown due to context cancellation")
			_ = log.Default().Sync() // best-effort flush
			return
		}
		_ = log.Default().Sync() // best-effort flush
		log.Fatalf("Run Gitea MCP Server Error: %v", err)
	}
	_ = log.Default().Sync() // best-effort flush
}

// decodeTokenFile accepts UTF-8 with optional BOM and UTF-16LE with BOM.
func decodeTokenFile(data []byte) string {
	if !bytes.HasPrefix(data, []byte{0xff, 0xfe}) {
		return strings.TrimPrefix(string(data), "\ufeff")
	}
	units := make([]uint16, (len(data)-2)/2)
	_, _ = binary.Decode(data[2:], binary.LittleEndian, units)
	return string(utf16.Decode(units))
}
