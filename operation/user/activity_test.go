package user

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"testing"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListUserActivities(t *testing.T) {
	// 52 activities, newest first: one full page of 50 and a second page with two; two of them are mirror syncs
	feed := []map[string]any{
		{"op_type": "commit_repo", "ref_name": "refs/heads/main", "created": "2026-11-05T16:10:00+01:00",
			"repo":    map[string]any{"full_name": "acme/docs", "html_url": "https://git.example/acme/docs"},
			"content": `{"Commits":[{"Sha1":"0123456789abcdef","Message":"Fix typo\n\nlong body","AuthorEmail":"ann@example.com","AuthorName":"Ann"}],"Len":7,"CompareURL":"acme/docs/compare/7c4e729419c61fcc36f96ceedb9a562428bc0f16...24c14df6f6b04181e817a7175d042f1909fcbc97"}`},
		{"op_type": "commit_repo", "ref_name": "refs/heads/main", "created": "2026-11-05T15:30:00+01:00",
			"repo":    map[string]any{"full_name": "acme/docs", "html_url": "https://git.example/acme/docs/"},
			"content": `{"Commits":[{"Sha1":"fedcba9876543210","Message":"Add page","AuthorEmail":"JDoe@Example.com","AuthorName":"J. Doe"}],"Len":1,"CompareURL":"acme/docs/compare/ccc...fedcba9876543210"}`},
		{"op_type": "create_issue", "created": "2026-11-05T15:00:00+01:00",
			"repo": map[string]any{"full_name": "acme/app"}, "content": "12|Crash on save"},
		{"op_type": "comment_issue", "created": "2026-11-05T14:00:00+01:00",
			"repo": map[string]any{"full_name": "acme/app", "html_url": "https://git.example/acme/app"}, "content": "12|Can reproduce",
			"comment": map[string]any{"id": 99, "html_url": "https://git.example/acme/app/issues/12#issuecomment-99"}},
		{"op_type": "mirror_sync_push", "ref_name": "refs/heads/main", "created": "2026-11-05T10:00:00+01:00",
			"repo":    map[string]any{"full_name": "jdoe/mirror", "html_url": "https://git.example/jdoe/mirror"},
			"content": `{"Commits":[{"Sha1":"1111111111111111","Message":"Upstream change","AuthorEmail":"jdoe@work.example","AuthorName":"J. Doe","Timestamp":"2026-11-04T17:00:00+01:00"}],"Len":3}`},
		{"op_type": "mirror_sync_push", "ref_name": "refs/heads/dev", "created": "2026-11-05T09:00:00+01:00",
			"repo":    map[string]any{"full_name": "jdoe/mirror", "html_url": "https://git.example/jdoe/mirror"},
			"content": `{"Commits":[{"Sha1":"2222222222222222","Message":"Other change","AuthorEmail":"dev@source.example","AuthorName":"Dev"}],"Len":2}`},
	}
	for i := len(feed); i < 52; i++ {
		feed = append(feed, map[string]any{"op_type": "push_tag", "ref_name": "refs/tags/v" + strconv.Itoa(i),
			"created": "2026-11-05T08:00:00+01:00", "repo": map[string]any{"full_name": "acme/app"}})
	}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/v1/user":
			_, _ = w.Write([]byte(`{"login":"jdoe","email":"jdoe@example.com"}`))
		case "/api/v1/user/emails":
			_, _ = w.Write([]byte(`[{"email":"jdoe@example.com"},{"email":"jdoe@work.example"}]`))
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
		"/api/v1/user/emails",
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
		Activities     []map[string]any `json:"activities"`
		AuthorsChecked bool             `json:"authors_checked"`
		MirrorSyncs    map[string]struct {
			Syncs         int                 `json:"syncs"`
			CommitsPushed int                 `json:"commits_pushed"`
			CommitsByUser []map[string]string `json:"commits_by_user"`
		} `json:"mirror_syncs"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	if got.Username != "jdoe" || got.Total != 50 || got.Truncated || got.ByType["push_tag"] != 46 ||
		got.ByType["mirror_sync_push"] != 0 || len(got.Activities) != 50 {
		t.Errorf("summary = %+v", got)
	}
	if got.Pushed != 8 || got.Listed != 2 {
		t.Errorf("commits pushed %d listed %d, want 8 and 2", got.Pushed, got.Listed)
	}
	if docs, app := got.ByRepo["acme/docs"], got.ByRepo["acme/app"]; docs.Activities != 2 || docs.CommitsPushed != 8 ||
		docs.CommitsListed != 2 || app.Activities != 48 || app.ByType["push_tag"] != 46 || app.CommitsPushed != 0 {
		t.Errorf("by_repo = %+v", got.ByRepo)
	}
	if _, ok := got.ByRepo["jdoe/mirror"]; ok {
		t.Errorf("mirror syncs counted in by_repo: %+v", got.ByRepo)
	}
	if m := got.MirrorSyncs["jdoe/mirror"]; len(got.MirrorSyncs) != 1 || m.Syncs != 2 || m.CommitsPushed != 5 {
		t.Errorf("mirror_syncs = %+v, want jdoe/mirror with 2 syncs and 5 commits", got.MirrorSyncs)
	}
	// only the mirrored commit with one of the user's addresses is the user's
	own, _ := json.Marshal(got.MirrorSyncs["jdoe/mirror"].CommitsByUser)
	if want := `[{"authored":"2026-11-04T17:00:00+01:00","sha":"1111111111","subject":"Upstream change"}]`; string(own) != want {
		t.Errorf("commits_by_user = %s, want %s", own, want)
	}
	if !got.AuthorsChecked {
		t.Error("authors_checked = false with the user's addresses known")
	}
	last, _ := json.Marshal(got.Activities[49])
	if want := `{"commits":[{"author":"Ann","sha":"0123456789","subject":"Fix typo"}],"commits_total":7,"created":"2026-11-05T16:10:00+01:00","html_url":"https://git.example/acme/docs/compare/7c4e729419...24c14df6f6","op_type":"commit_repo","ref":"main","repo":"acme/docs","subject":"7 commits: 0123456789 Fix typo"}`; string(last) != want {
		t.Errorf("push = %s, want %s", last, want)
	}
	single, _ := json.Marshal(got.Activities[48])
	if want := `{"commits":[{"sha":"fedcba9876","subject":"Add page"}],"commits_total":1,"created":"2026-11-05T15:30:00+01:00","html_url":"https://git.example/acme/docs/commit/fedcba9876","op_type":"commit_repo","ref":"main","repo":"acme/docs","subject":"fedcba9876 Add page"}`; string(single) != want {
		t.Errorf("single push = %s, want %s", single, want)
	}
	if issue := got.Activities[47]; issue["html_url"] != nil {
		t.Errorf("issue without repository address got html_url %v", issue["html_url"])
	}
	comment, _ := json.Marshal(got.Activities[46])
	if want := `{"comment_id":99,"comment_url":"https://git.example/acme/app/issues/12#issuecomment-99","created":"2026-11-05T14:00:00+01:00","html_url":"https://git.example/acme/app/issues/12","number":12,"op_type":"comment_issue","repo":"acme/app","text":"Can reproduce"}`; string(comment) != want {
		t.Errorf("comment = %s, want %s", comment, want)
	}
	if ref := got.Activities[0]["ref"]; ref != "v51" {
		t.Errorf("first ref = %v, want v51", ref)
	}

	result, err = ListUserActivitiesFn(t.Context(), map[string]any{"date": "05.11.2026", "username": "jdoe"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("a date that is not YYYY-MM-DD must fail")
	}
}

func TestPushWithoutNewCommits(t *testing.T) {
	for _, tc := range []struct{ feed, want string }{
		{`{"op_type":"commit_repo","ref_name":"refs/heads/feature/x y","created":"2026-11-05T16:06:35+01:00",
			"repo":{"full_name":"acme/docs","html_url":"https://git.example/acme/docs"},"content":"{\"Commits\":[],\"Len\":0}"}`,
			`{"commits":[],"commits_total":0,"created":"2026-11-05T16:06:35+01:00","html_url":"https://git.example/acme/docs/src/branch/feature/x%20y","op_type":"commit_repo","ref":"feature/x y","repo":"acme/docs","subject":"push to feature/x y without new commits"}`},
		{`{"op_type":"commit_repo","created":"2026-11-05T16:06:35+01:00","repo":{"full_name":"acme/docs"},"content":"{}"}`,
			`{"commits":[],"commits_total":0,"created":"2026-11-05T16:06:35+01:00","op_type":"commit_repo","repo":"acme/docs","subject":"push without new commits"}`},
		{`{"op_type":"commit_repo","ref_name":"refs/heads/fix/issue-1","created":"2026-07-12T20:29:22+02:00",
			"repo":{"full_name":"acme/docs","html_url":"https://git.example/acme/docs"},"content":""}`,
			`{"created":"2026-07-12T20:29:22+02:00","html_url":"https://git.example/acme/docs/src/branch/fix/issue-1","op_type":"commit_repo","ref":"fix/issue-1","repo":"acme/docs","subject":"created branch fix/issue-1"}`},
	} {
		var a feedActivity
		if err := json.Unmarshal([]byte(tc.feed), &a); err != nil {
			t.Fatal(err)
		}
		slim, _ := slimActivity(a, nil)
		if got, _ := json.Marshal(slim); string(got) != tc.want {
			t.Errorf("push = %s, want %s", got, tc.want)
		}
	}
}

func TestShortCompare(t *testing.T) {
	for in, want := range map[string]string{
		"7c4e729419c61fcc36f96ceedb9a562428bc0f16...24c14df6f6b04181e817a7175d042f1909fcbc97": "7c4e729419...24c14df6f6",
		"main...feature/x": "main...feature/x",
		"aaa...bbb":        "aaa...bbb",
		"7c4e729419c61fcc36f96ceedb9a562428bc0f16": "7c4e729419c61fcc36f96ceedb9a562428bc0f16",
	} {
		if got := shortCompare(in); got != want {
			t.Errorf("shortCompare(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListUserActivitiesPeriod(t *testing.T) {
	byDate := map[string]string{
		"2026-07-11": `[]`,
		"2026-07-12": `[{"op_type":"commit_repo","ref_name":"refs/heads/fix/a","created":"2026-07-12T20:29:30+02:00",
			"repo":{"full_name":"acme/app","html_url":"https://git.example/acme/app"},
			"content":"{\"Commits\":[{\"Sha1\":\"aaaaaaaaaaaaaaaa\",\"Message\":\"Fix\"}],\"Len\":1}"},
			{"op_type":"commit_repo","ref_name":"refs/heads/fix/a","created":"2026-07-12T20:29:22+02:00",
			"repo":{"full_name":"acme/app","html_url":"https://git.example/acme/app"},"content":""}]`,
		"2026-07-13": `[{"op_type":"create_pull_request","created":"2026-07-13T09:00:00+02:00",
			"repo":{"full_name":"acme/app","html_url":"https://git.example/acme/app"},"content":"7|Fix"}]`,
	}
	var mu sync.Mutex
	var dates []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/jdoe":
			_, _ = w.Write([]byte(`{"email":"jdoe@example.com"}`))
		case "/api/v1/users/jdoe/activities/feeds":
			mu.Lock()
			dates = append(dates, r.URL.Query().Get("date"))
			mu.Unlock()
			_, _ = w.Write([]byte(byDate[r.URL.Query().Get("date")]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	defer func(host string) { flag.Host = host }(flag.Host)
	flag.Host = server.URL

	result, err := ListUserActivitiesFn(t.Context(), map[string]any{"date_from": "2026-07-11", "date_to": "2026-07-13", "username": "jdoe"})
	if err != nil || result.IsError {
		t.Fatalf("err %v, result %+v", err, result)
	}
	sort.Strings(dates)
	if fmt.Sprint(dates) != "[2026-07-11 2026-07-12 2026-07-13]" {
		t.Errorf("days read = %v", dates)
	}
	var got struct {
		Total           int              `json:"total"`
		DateFrom        string           `json:"date_from"`
		DateTo          string           `json:"date_to"`
		DaysChecked     int              `json:"days_checked"`
		ByDay           map[string]int   `json:"by_day"`
		ByType          map[string]int   `json:"by_type"`
		Pushes          int              `json:"pushes"`
		BranchesCreated int              `json:"branches_created"`
		Pushed          int              `json:"commits_pushed"`
		Activities      []map[string]any `json:"activities"`
		ByRepo          map[string]struct {
			Pushes          int `json:"pushes"`
			BranchesCreated int `json:"branches_created"`
			CommitsPushed   int `json:"commits_pushed"`
			ByDay           map[string]struct {
				Activities      int            `json:"activities"`
				ByType          map[string]int `json:"by_type"`
				Pushes          int            `json:"pushes"`
				BranchesCreated int            `json:"branches_created"`
				CommitsPushed   int            `json:"commits_pushed"`
			} `json:"by_day"`
		} `json:"by_repo"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 3 || got.DateFrom != "2026-07-11" || got.DateTo != "2026-07-13" || got.DaysChecked != 3 ||
		fmt.Sprint(got.ByDay) != "map[2026-07-12:2 2026-07-13:1]" || got.ByType["commit_repo"] != 2 ||
		got.Pushes != 1 || got.BranchesCreated != 1 || got.Pushed != 1 {
		t.Errorf("summary = %+v", got)
	}
	app := got.ByRepo["acme/app"]
	d12, d13 := app.ByDay["2026-07-12"], app.ByDay["2026-07-13"]
	if app.Pushes != 1 || app.BranchesCreated != 1 || app.CommitsPushed != 1 || len(app.ByDay) != 2 ||
		d12.Activities != 2 || d12.Pushes != 1 || d12.BranchesCreated != 1 || d12.CommitsPushed != 1 ||
		d13.Activities != 1 || d13.ByType["create_pull_request"] != 1 || d13.Pushes != 0 {
		t.Errorf("by_repo = %+v", got.ByRepo)
	}
	var created []string
	for _, a := range got.Activities {
		created = append(created, a["created"].(string))
	}
	if want := "[2026-07-12T20:29:22+02:00 2026-07-12T20:29:30+02:00 2026-07-13T09:00:00+02:00]"; fmt.Sprint(created) != want {
		t.Errorf("order = %v, want %v", created, want)
	}

	for _, args := range []map[string]any{
		{"date": "2026-07-11", "date_from": "2026-07-11", "date_to": "2026-07-12"},
		{"date_from": "2026-07-13", "date_to": "2026-07-11"},
		{"date_from": "2026-01-01", "date_to": "2026-12-31"},
		{"date_from": "2026-07-11"},
		{},
	} {
		if result, err := ListUserActivitiesFn(t.Context(), args); err != nil || !result.IsError {
			t.Errorf("%v: want an error result, got %+v (%v)", args, result, err)
		}
	}
}
