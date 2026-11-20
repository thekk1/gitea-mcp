package user

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const ListUserActivitiesToolName = "list_user_activities"

const (
	activityPageSize = 50
	activityMaxPages = 40
	activityTextMax  = 200
)

var ListUserActivitiesTool = tool.NewDefinition(
	ListUserActivitiesToolName,
	"List everything a user did in Gitea on one day, from the user's activity feed: pushes with their commits, "+
		"created, closed, reopened and merged issues and pull requests, comments, reviews, created repositories, "+
		"branches, tags and releases. Reads every page of the day, so the list is complete unless truncated is true. "+
		"Only actions the user performed, only in repositories the caller may see. The day is taken in the time zone "+
		"of the Gitea server. by_repo and the commits_* fields give the counts per repository and in all; take numbers "+
		"from them instead of counting the list. A push has its commit subjects in subject (one line) and the address "+
		"of its commit, or of the comparison of its commits, in html_url; an issue or pull request has its address in "+
		"html_url.",
	annotation.ReadOnly("List a user's activities on one day"),
	tool.String("date", tool.Required(), tool.Description("day as YYYY-MM-DD")),
	tool.String("username", tool.Description("login of the user; empty means the current user")),
)

func init() {
	Tool.RegisterRead(tool.ServerTool{Tool: ListUserActivitiesTool, Handler: ListUserActivitiesFn})
}

type feedActivity struct {
	OpType  string `json:"op_type"`
	RefName string `json:"ref_name"`
	Content string `json:"content"`
	Created string `json:"created"`
	Repo    *struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repo"`
	Comment *struct {
		ID      int64  `json:"id"`
		HTMLURL string `json:"html_url"`
	} `json:"comment"`
}

type pushContent struct {
	Commits []struct {
		Sha1    string `json:"Sha1"`
		Message string `json:"Message"`
	} `json:"Commits"`
	Len        int    `json:"Len"`
	CompareURL string `json:"CompareURL"`
}

func ListUserActivitiesFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	date, err := params.GetString(args, "date")
	if err != nil {
		return to.ErrorResult(err)
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return to.ErrorResult(fmt.Errorf("date must be YYYY-MM-DD, got %q", date))
	}
	username := strings.TrimSpace(params.GetOptionalString(args, "username", ""))
	if username == "" {
		var me struct {
			Login string `json:"login"`
		}
		if _, err := gitea.DoJSON(ctx, "GET", "user", nil, nil, &me); err != nil {
			return to.ErrorResult(fmt.Errorf("get current user err: %v", err))
		}
		username = me.Login
	}

	var feed []feedActivity
	truncated := false
	for page := 1; ; page++ {
		if page > activityMaxPages {
			truncated = true
			break
		}
		var batch []feedActivity
		query := url.Values{
			"only-performed-by": {"true"},
			"date":              {date},
			"page":              {strconv.Itoa(page)},
			"limit":             {strconv.Itoa(activityPageSize)},
		}
		if _, err := gitea.DoJSON(ctx, "GET", fmt.Sprintf("users/%s/activities/feeds", url.PathEscape(username)), query, nil, &batch); err != nil {
			return to.ErrorResult(fmt.Errorf("list activities of %q err: %v", username, err))
		}
		feed = append(feed, batch...)
		if len(batch) < activityPageSize {
			break
		}
	}

	activities := make([]map[string]any, 0, len(feed))
	counts := map[string]int{}
	repos := map[string]*repoCount{}
	allShas := map[string]bool{}
	pushed := 0
	for i := len(feed) - 1; i >= 0; i-- { // the feed is newest first; the result reads as a day
		slim := slimActivity(feed[i])
		activities = append(activities, slim)
		counts[feed[i].OpType]++
		name, _ := slim["repo"].(string)
		rc := repos[name]
		if rc == nil {
			rc = &repoCount{ByType: map[string]int{}, shas: map[string]bool{}}
			repos[name] = rc
		}
		rc.Activities++
		rc.ByType[feed[i].OpType]++
		if commits, ok := slim["commits"].([]map[string]string); ok {
			for _, c := range commits {
				rc.shas[c["sha"]] = true
				allShas[name+"@"+c["sha"]] = true
			}
		}
		if n, ok := slim["commits_total"].(int); ok {
			rc.CommitsPushed += n
			pushed += n
		}
	}
	for _, rc := range repos {
		rc.CommitsListed = len(rc.shas)
	}
	return to.TextResult(map[string]any{
		"username":       username,
		"date":           date,
		"total":          len(activities),
		"truncated":      truncated,
		"by_type":        counts,
		"commits_pushed": pushed,
		"commits_listed": len(allShas),
		"by_repo":        repos,
		"activities":     activities,
	})
}

// repoCount sums one repository's activities: commits_pushed adds commits_total of every push (a commit
// pushed to two branches counts twice), commits_listed counts the distinct commits the feed lists.
type repoCount struct {
	Activities    int            `json:"activities"`
	ByType        map[string]int `json:"by_type"`
	CommitsPushed int            `json:"commits_pushed"`
	CommitsListed int            `json:"commits_listed"`
	shas          map[string]bool
}

func slimActivity(a feedActivity) map[string]any {
	out := map[string]any{"created": a.Created, "op_type": a.OpType}
	repoURL := ""
	if a.Repo != nil {
		out["repo"] = a.Repo.FullName
		repoURL = strings.TrimSuffix(a.Repo.HTMLURL, "/")
	}
	if ref := strings.TrimPrefix(strings.TrimPrefix(a.RefName, "refs/heads/"), "refs/tags/"); ref != "" {
		out["ref"] = ref
	}
	switch a.OpType {
	case "commit_repo", "mirror_sync_push":
		var push pushContent
		if json.Unmarshal([]byte(a.Content), &push) == nil {
			commits := make([]map[string]string, 0, len(push.Commits))
			subjects := make([]string, 0, len(push.Commits))
			for _, c := range push.Commits {
				sha := c.Sha1
				if len(sha) > 10 {
					sha = sha[:10]
				}
				subject, _, _ := strings.Cut(strings.TrimSpace(c.Message), "\n")
				commits = append(commits, map[string]string{"sha": sha, "subject": subject})
				subjects = append(subjects, sha+" "+subject)
			}
			out["commits"] = commits
			// the feed keeps only the newest commits of a push; commits_total counts all of them
			total := max(push.Len, len(push.Commits))
			out["commits_total"] = total
			// subject and html_url give a push one line and one link, so a table of the day needs no nested field
			switch {
			case len(subjects) == 1 && total == 1:
				out["subject"] = shorten(subjects[0])
			case len(subjects) > 0:
				out["subject"] = shorten(fmt.Sprintf("%d commits: %s", total, strings.Join(subjects, "; ")))
			}
			if repoURL != "" && len(push.Commits) > 0 {
				if _, compare, ok := strings.Cut(push.CompareURL, "/compare/"); ok && total > 1 && compare != "" {
					out["html_url"] = repoURL + "/compare/" + compare
				} else {
					out["html_url"] = repoURL + "/commit/" + push.Commits[0].Sha1
				}
			}
		}
	default:
		if a.Content != "" {
			if num, text, ok := strings.Cut(a.Content, "|"); ok {
				if n, err := strconv.ParseInt(num, 10, 64); err == nil {
					out["number"] = n
					if repoURL != "" {
						kind := "issues"
						if strings.Contains(a.OpType, "pull") {
							kind = "pulls"
						}
						out["html_url"] = fmt.Sprintf("%s/%s/%d", repoURL, kind, n)
					}
					if text = strings.TrimSpace(text); text != "" {
						out["text"] = shorten(text)
					}
					break
				}
			}
			out["text"] = shorten(a.Content)
		}
	}
	if a.Comment != nil && a.Comment.ID != 0 {
		out["comment_id"] = a.Comment.ID
		if a.Comment.HTMLURL != "" {
			out["comment_url"] = a.Comment.HTMLURL
		}
	}
	return out
}

func shorten(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > activityTextMax {
		return string(r[:activityTextMax]) + "…"
	}
	return s
}
