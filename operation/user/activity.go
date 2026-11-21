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
		"of its commit, or of the comparison of its commits, in html_url; a push without new commits says so in "+
		"subject and links its branch; a newly created branch has subject \"created branch <name>\" and links it. An issue or pull request has its address in html_url. A commit whose author "+
		"is someone else carries the author's name in author. Syncs of pull mirrors "+
		"the user owns are no actions of the user: Gitea runs them itself and books them on the mirror's owner, and "+
		"their commits are those of the authors in the mirrored source. They are left out of activities and the "+
		"counts; mirror_syncs counts them per repository and lists in commits_by_user only the mirrored commits the "+
		"user wrote. authors_checked is false if the user's addresses could not be read; then no commit is "+
		"attributed by its author.",
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
		Sha1        string `json:"Sha1"`
		Message     string `json:"Message"`
		AuthorEmail string `json:"AuthorEmail"`
		AuthorName  string `json:"AuthorName"`
		Timestamp   string `json:"Timestamp"`
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
	emails := map[string]bool{}
	if username == "" {
		var me struct {
			Login string `json:"login"`
			Email string `json:"email"`
		}
		if _, err := gitea.DoJSON(ctx, "GET", "user", nil, nil, &me); err != nil {
			return to.ErrorResult(fmt.Errorf("get current user err: %v", err))
		}
		username = me.Login
		addEmail(emails, me.Email)
		// every address of the account, not only the primary one: commits carry the address they were made with
		var all []struct {
			Email string `json:"email"`
		}
		if _, err := gitea.DoJSON(ctx, "GET", "user/emails", nil, nil, &all); err == nil {
			for _, e := range all {
				addEmail(emails, e.Email)
			}
		}
	} else {
		var other struct {
			Email string `json:"email"`
		}
		if _, err := gitea.DoJSON(ctx, "GET", fmt.Sprintf("users/%s", url.PathEscape(username)), nil, nil, &other); err == nil {
			addEmail(emails, other.Email)
		}
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
	mirrors := map[string]*mirrorCount{}
	allShas := map[string]bool{}
	pushed := 0
	for i := len(feed) - 1; i >= 0; i-- { // the feed is newest first; the result reads as a day
		slim, own := slimActivity(feed[i], emails)
		if strings.HasPrefix(feed[i].OpType, "mirror_sync_") {
			name, _ := slim["repo"].(string)
			mc := mirrors[name]
			if mc == nil {
				mc = &mirrorCount{}
				mirrors[name] = mc
			}
			mc.Syncs++
			if n, ok := slim["commits_total"].(int); ok {
				mc.CommitsPushed += n
			}
			mc.CommitsByUser = append(mc.CommitsByUser, own...)
			continue
		}
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
	result := map[string]any{
		"username":        username,
		"date":            date,
		"total":           len(activities),
		"truncated":       truncated,
		"by_type":         counts,
		"commits_pushed":  pushed,
		"commits_listed":  len(allShas),
		"by_repo":         repos,
		"activities":      activities,
		"authors_checked": len(emails) > 0,
	}
	if len(mirrors) > 0 {
		result["mirror_syncs"] = mirrors
	}
	return to.TextResult(result)
}

// mirrorCount sums the syncs of one pull mirror: Gitea books them on the mirror's owner, so the feed of
// only-performed-by lists them, but the owner did not push these commits. commits_by_user holds the listed
// mirrored commits whose author address is one of the user's.
type mirrorCount struct {
	Syncs         int                 `json:"syncs"`
	CommitsPushed int                 `json:"commits_pushed"`
	CommitsByUser []map[string]string `json:"commits_by_user,omitempty"`
}

func addEmail(emails map[string]bool, email string) {
	if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
		emails[email] = true
	}
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

// slimActivity shortens one feed entry. With the user's addresses known, a listed commit by another author
// carries author, and the commits whose author address is the user's come back as own.
func slimActivity(a feedActivity, emails map[string]bool) (map[string]any, []map[string]string) {
	var own []map[string]string
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
		ref, _ := out["ref"].(string)
		if strings.TrimSpace(a.Content) == "" && a.OpType == "commit_repo" && strings.HasPrefix(a.RefName, "refs/heads/") {
			// Gitea books a new branch as commit_repo without content (feed notifier CreateRef);
			// its commits, if new, come in a push of their own
			out["subject"] = "created branch " + ref
			if repoURL != "" {
				out["html_url"] = repoURL + "/src/branch/" + pathEscapeRef(ref)
			}
		} else if json.Unmarshal([]byte(a.Content), &push) == nil {
			commits := make([]map[string]string, 0, len(push.Commits))
			subjects := make([]string, 0, len(push.Commits))
			for _, c := range push.Commits {
				sha := c.Sha1
				if len(sha) > 10 {
					sha = sha[:10]
				}
				subject, _, _ := strings.Cut(strings.TrimSpace(c.Message), "\n")
				commit := map[string]string{"sha": sha, "subject": subject}
				if author := strings.ToLower(strings.TrimSpace(c.AuthorEmail)); len(emails) > 0 && author != "" {
					if emails[author] {
						own = append(own, map[string]string{"sha": sha, "subject": subject, "authored": c.Timestamp})
					} else {
						commit["author"] = c.AuthorName
					}
				}
				commits = append(commits, commit)
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
			case total == 0 && ref != "":
				// a branch moved to commits it already had, e.g. pushed back or created on an old commit
				out["subject"] = "push to " + ref + " without new commits"
			case total == 0:
				out["subject"] = "push without new commits"
			}
			switch {
			case repoURL == "":
			case len(push.Commits) > 0:
				if _, compare, ok := strings.Cut(push.CompareURL, "/compare/"); ok && total > 1 && compare != "" {
					out["html_url"] = repoURL + "/compare/" + compare
				} else {
					out["html_url"] = repoURL + "/commit/" + push.Commits[0].Sha1
				}
			case ref != "" && strings.HasPrefix(a.RefName, "refs/heads/"):
				out["html_url"] = repoURL + "/src/branch/" + pathEscapeRef(ref)
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
	return out, own
}

// pathEscapeRef escapes each segment of a branch name for a URL path; a branch may contain slashes.
func pathEscapeRef(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func shorten(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > activityTextMax {
		return string(r[:activityTextMax]) + "…"
	}
	return s
}
