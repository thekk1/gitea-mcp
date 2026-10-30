package label

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"gitea.com/gitea/gitea-mcp/internal/flag"
	"gitea.com/gitea/gitea-mcp/internal/gitea"
)

func TestResolveIDs(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path + "?" + r.URL.Query().Get("page") {
		case "/api/v1/repos/user/r/labels?1", "/api/v1/repos/acme/r/labels?1", "/api/v1/repos/locked/r/labels?1":
			w.Header().Set("Link", `<`+r.URL.Path+`?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"id":1,"name":"bug"},{"id":2,"name":"Priority/High"}]`))
		case "/api/v1/repos/user/r/labels?2", "/api/v1/repos/acme/r/labels?2", "/api/v1/repos/locked/r/labels?2":
			_, _ = w.Write([]byte(`[{"id":3,"name":"docs"}]`))
		case "/api/v1/orgs/acme/labels?1":
			_, _ = w.Write([]byte(`[{"id":10,"name":"org-wide"},{"id":11,"name":"priority/high"},{"id":12,"name":"dup"},{"id":13,"name":"dup"}]`))
		case "/api/v1/orgs/locked/labels?1":
			w.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL
	client, err := gitea.ClientFromContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		owner        string
		raw          any
		want         []int64
		wantErr      string
		wantRequests int
	}{
		{owner: "user", raw: []any{3.0, 7.0}, want: []int64{3, 7}},
		{owner: "user", raw: []any{"bug", "docs"}, want: []int64{1, 3}, wantRequests: 2},
		{owner: "user", raw: []any{5.0, "42"}, want: []int64{5, 42}, wantRequests: 3},
		{owner: "acme", raw: []any{"org-wide", "bug"}, want: []int64{10, 1}, wantRequests: 3},
		{owner: "acme", raw: []any{"priority/high"}, want: []int64{11}, wantRequests: 3},
		{owner: "acme", raw: []any{"dup"}, wantErr: `label "dup" is ambiguous, pass one of the IDs [12 13] instead`, wantRequests: 3},
		{owner: "locked", raw: []any{"bug", "nope"}, wantErr: "unknown labels in locked/r: nope (available: bug, Priority/High, docs)", wantRequests: 3},
		{owner: "user", raw: []any{true}, wantErr: "label must be a name or ID, got true"},
		{owner: "user", raw: nil, wantErr: "labels (array of names or IDs) is required"},
	} {
		requests = 0
		got, err := ResolveIDs(t.Context(), client, tt.owner, "r", tt.raw)
		if (err != nil || tt.wantErr != "") && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
			t.Errorf("%v: err %v, want %q", tt.raw, err, tt.wantErr)
		}
		if !slices.Equal(got, tt.want) || requests != tt.wantRequests {
			t.Errorf("%v: got %v after %d requests, want %v after %d", tt.raw, got, requests, tt.want, tt.wantRequests)
		}
	}
}
