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
		"of the Gitea server.",
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
	Len int `json:"Len"`
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
	for i := len(feed) - 1; i >= 0; i-- { // the feed is newest first; the result reads as a day
		activities = append(activities, slimActivity(feed[i]))
		counts[feed[i].OpType]++
	}
	return to.TextResult(map[string]any{
		"username":   username,
		"date":       date,
		"total":      len(activities),
		"truncated":  truncated,
		"by_type":    counts,
		"activities": activities,
	})
}

func slimActivity(a feedActivity) map[string]any {
	out := map[string]any{"created": a.Created, "op_type": a.OpType}
	if a.Repo != nil {
		out["repo"] = a.Repo.FullName
	}
	if ref := strings.TrimPrefix(strings.TrimPrefix(a.RefName, "refs/heads/"), "refs/tags/"); ref != "" {
		out["ref"] = ref
	}
	switch a.OpType {
	case "commit_repo", "mirror_sync_push":
		var push pushContent
		if json.Unmarshal([]byte(a.Content), &push) == nil {
			commits := make([]map[string]string, 0, len(push.Commits))
			for _, c := range push.Commits {
				sha := c.Sha1
				if len(sha) > 10 {
					sha = sha[:10]
				}
				subject, _, _ := strings.Cut(strings.TrimSpace(c.Message), "\n")
				commits = append(commits, map[string]string{"sha": sha, "subject": subject})
			}
			out["commits"] = commits
			// the feed keeps only the newest commits of a push; commits_total counts all of them
			out["commits_total"] = max(push.Len, len(push.Commits))
		}
	default:
		if a.Content != "" {
			if num, text, ok := strings.Cut(a.Content, "|"); ok {
				if n, err := strconv.ParseInt(num, 10, 64); err == nil {
					out["number"] = n
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
