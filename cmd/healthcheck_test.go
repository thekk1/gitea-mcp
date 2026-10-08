package cmd

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHealthcheck(t *testing.T) {
	okServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer okServer.Close()
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	for _, test := range []struct {
		name   string
		state  string
		port   int
		wantOK bool
	}{
		{name: "missing state file falls back to -port", port: okServer.Listener.Addr().(*net.TCPAddr).Port, wantOK: true},
		{name: "state file address takes precedence over -port", state: okServer.Listener.Addr().String(), port: 1, wantOK: true},
		{name: "stdio state file is healthy without a listener", state: "stdio", port: 1, wantOK: true},
		{name: "error status fails", state: errorServer.Listener.Addr().String()},
		{name: "nothing listening fails", port: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateFile := filepath.Join(t.TempDir(), "state")
			if test.state != "" {
				if err := os.WriteFile(stateFile, []byte(test.state), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			var stdout, stderr bytes.Buffer
			if ok := runHealthcheck(stateFile, test.port, &stdout, &stderr); ok != test.wantOK {
				t.Fatalf("runHealthcheck() = %v, want %v, stderr %q", ok, test.wantOK, stderr.String())
			}
			if test.wantOK && (!strings.Contains(stdout.String(), "healthy") || stderr.Len() != 0) {
				t.Errorf("stdout = %q, stderr = %q, want healthy and no error", stdout.String(), stderr.String())
			}
			if !test.wantOK && stderr.Len() == 0 {
				t.Error("stderr is empty, want a failure message")
			}
		})
	}
}
