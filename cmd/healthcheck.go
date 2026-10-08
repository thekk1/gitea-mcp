package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

func runHealthcheck(stateFile string, port int, stdout, stderr io.Writer) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if state, err := os.ReadFile(stateFile); err == nil {
		addr = string(state)
	}
	if addr != "stdio" {
		client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{}} // no proxy, a bind address may be non-loopback
		resp, err := client.Get((&url.URL{Scheme: "http", Host: addr, Path: "/healthz"}).String())
		if err != nil {
			fmt.Fprintf(stderr, "healthcheck failed: %v\n", err)
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			fmt.Fprintf(stderr, "healthcheck failed: unexpected status %s\n", resp.Status)
			return false
		}
	}
	fmt.Fprintln(stdout, "healthy")
	return true
}
