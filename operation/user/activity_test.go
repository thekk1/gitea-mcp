package user

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListUserActivities(t *testing.T) {
	// 51 activities, newest first: one full page of 50 and a second page with one
	feed := []map[string]any{
		{"op_type": "commit_repo", "ref_name": "refs/heads/main", "created": "2026-11-05T16:10:00+01:00",
			"repo":    map[string]any{"full_name": "acme/docs"},
			"content": `{"Commits":[{"Sha1":"0123456789abcdef","Message":"Fix typo\n\nlong body"}],"Len":7}`},
		{"op_type": "create_issue", "created": "2026-11-05T15:00:00+01:00",
			"repo": map[string]any{"full_name": "acme/app"}, "content": "12|Crash on save"},
		{"op_type": "comment_issue", "created": "2026-11-05T14:00:00+01:00",
			"repo": map[string]any{"full_name": "acme/app"}, "content": "12|Can reproduce",
			"comment": map[string]any{"id": 99, "html_url": "https://git.example/acme/app/issues/12#issuecomment-99"}},
	}
	for i := len(feed); i < 51; i++ {
		feed = append(feed, map[string]any{"op_type": "push_tag", "ref_name": "refs/tags/v" + strconv.Itoa(i),
			"created": "2026-11-05T08:00:00+01:00", "repo": map[string]any{"full_name": "acme/app"}})
	}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/v1/user":
			_, _ = w.Write([]byte(`{"login":"jdoe"}`))
		case "/api/v1/users/jdoe/activities/feeds":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			start, end := min((page-1)*50, len(feed)), min(page*50, len(feed))
			_ = json.NewEncoder(w).Encode(feed[start:end])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL

	result, err := ListUserActivitiesFn(t.Context(), map[string]any{"date": "2026-11-05"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/api/v1/user",
		"/api/v1/users/jdoe/activities/feeds?date=2026-11-05&limit=50&only-performed-by=true&page=1",
		"/api/v1/users/jdoe/activities/feeds?date=2026-11-05&limit=50&only-performed-by=true&page=2",
	}
	if fmt.Sprint(requests) != fmt.Sprint(want) {
		t.Errorf("requests = %v, want %v", requests, want)
	}
	var got struct {
		Username  string         `json:"username"`
		Total     int            `json:"total"`
		Truncated bool           `json:"truncated"`
		ByType    map[string]int `json:"by_type"`
		Pushed    int            `json:"commits_pushed"`
		Listed    int            `json:"commits_listed"`
		ByRepo    map[string]struct {
			Activities    int            `json:"activities"`
			ByType        map[string]int `json:"by_type"`
			CommitsPushed int            `json:"commits_pushed"`
			CommitsListed int            `json:"commits_listed"`
		} `json:"by_repo"`
		Activities []map[string]any `json:"activities"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	if got.Username != "jdoe" || got.Total != 51 || got.Truncated || got.ByType["push_tag"] != 48 {
		t.Errorf("summary = %+v", got)
	}
	if got.Pushed != 7 || got.Listed != 1 {
		t.Errorf("commits pushed %d listed %d, want 7 and 1", got.Pushed, got.Listed)
	}
	if docs, app := got.ByRepo["acme/docs"], got.ByRepo["acme/app"]; docs.Activities != 1 || docs.CommitsPushed != 7 ||
		docs.CommitsListed != 1 || app.Activities != 50 || app.ByType["push_tag"] != 48 || app.CommitsPushed != 0 {
		t.Errorf("by_repo = %+v", got.ByRepo)
	}
	last, _ := json.Marshal(got.Activities[50])
	if want := `{"commits":[{"sha":"0123456789","subject":"Fix typo"}],"commits_total":7,"created":"2026-11-05T16:10:00+01:00","op_type":"commit_repo","ref":"main","repo":"acme/docs"}`; string(last) != want {
		t.Errorf("push = %s, want %s", last, want)
	}
	comment, _ := json.Marshal(got.Activities[48])
	if want := `{"comment_id":99,"comment_url":"https://git.example/acme/app/issues/12#issuecomment-99","created":"2026-11-05T14:00:00+01:00","number":12,"op_type":"comment_issue","repo":"acme/app","text":"Can reproduce"}`; string(comment) != want {
		t.Errorf("comment = %s, want %s", comment, want)
	}
	if ref := got.Activities[0]["ref"]; ref != "v50" {
		t.Errorf("first ref = %v, want v50", ref)
	}

	result, err = ListUserActivitiesFn(t.Context(), map[string]any{"date": "05.11.2026", "username": "jdoe"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("a date that is not YYYY-MM-DD must fail")
	}
}
