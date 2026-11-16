package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"gitea.com/gitea/gitea-mcp/pkg/annotation"
	"gitea.com/gitea/gitea-mcp/pkg/gitea"
	"gitea.com/gitea/gitea-mcp/pkg/params"
	"gitea.com/gitea/gitea-mcp/pkg/to"
	"gitea.com/gitea/gitea-mcp/pkg/tool"

	gitea_sdk "gitea.dev/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	IssueCheckListToolName = "issue_check_list"
	checkListMaxTerms      = 1000
	checkListPageSize      = 50
	checkListTitleChars    = 100
)

// IssueCheckListTool checks a whole list of identifiers or search texts against the issues of one
// repository in a single call: the server pages through all issues, the model only passes the list.
var IssueCheckListTool = tool.NewDefinition(
	IssueCheckListToolName,
	"Check a list of identifiers or search texts (for example ticket keys from another system) against "+
		"all issues and pull requests of one repository in one call. Returns which terms occur in a title "+
		"or body and which are missing. Use this instead of one search per term or paging through list_issues.",
	annotation.ReadOnly("Check a list against repository issues"),
	tool.String("owner", tool.Required(), tool.Description(params.OwnerDesc)),
	tool.String("repo", tool.Required(), tool.Description(params.RepoDesc)),
	tool.String("terms", tool.Required(), tool.Description(
		"identifiers or search texts, separated by commas or new lines; a JSON array of strings is accepted too")),
	tool.String("state", tool.Enum("open", "closed", "all"), tool.Default("all")),
	tool.String("type", tool.Enum("issues", "pulls", "all"), tool.Default("all")),
	tool.String("match_in", tool.Enum("title", "title_body"), tool.Default("title_body")),
	tool.Boolean("only_missing", tool.Description("return only the missing terms, not the matching issues")),
	tool.String("aliases", tool.Description(
		"other spellings of a term prefix, as FROM=TO separated by commas: with DIFANGPVM2=PVM2 the term "+
			"DIFANGPVM2-12 is also found where the text says PVM2-12; hits are reported under the term as given")),
)

func init() {
	Tool.RegisterRead(tool.ServerTool{
		Tool:    IssueCheckListTool,
		Handler: issueCheckListFn,
	})
}

// parseTerms accepts a JSON array or a list separated by commas, semicolons or new lines;
// duplicates and empty entries are dropped, the order is kept.
func parseTerms(raw any) []string {
	var items []string
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				items = append(items, s)
			}
		}
	case string:
		s := strings.TrimSpace(v)
		if strings.HasPrefix(s, "[") && json.Unmarshal([]byte(s), &items) == nil {
			break
		}
		items = regexp.MustCompile(`[,;\n]+`).Split(s, -1)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		key := strings.ToLower(item)
		if item == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

// termAlias is another spelling of a term prefix, both in lower case.
type termAlias struct{ from, to string }

// parseAliases reads "FROM=TO" pairs separated by commas, semicolons or new lines.
func parseAliases(raw string) ([]termAlias, error) {
	var aliases []termAlias
	for _, item := range regexp.MustCompile(`[,;\n]+`).Split(raw, -1) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		from, to, ok := strings.Cut(item, "=")
		from, to = strings.ToLower(strings.TrimSpace(from)), strings.ToLower(strings.TrimSpace(to))
		if !ok || from == "" || to == "" {
			return nil, fmt.Errorf("aliases: %q is not FROM=TO", item)
		}
		aliases = append(aliases, termAlias{from, to})
	}
	return aliases, nil
}

// termMatcher finds terms as whole words, ignoring case: "ABC-12" does not match inside "ABC-123".
// It passes over each text once, so the cost does not grow with the number of terms; one regular
// expression per term took minutes for a thousand terms and exceeded the MCP request timeout.
type termMatcher struct {
	index   map[string]int
	lengths []int
}

// With aliases a term is also found in its other spelling and reported under its own index; a
// spelling that is a term itself keeps the index of that term.
func newTermMatcher(terms []string, aliases ...termAlias) *termMatcher {
	m := &termMatcher{index: make(map[string]int, len(terms))}
	seen := map[int]bool{}
	add := func(text string, i int) {
		m.index[text] = i
		if !seen[len(text)] {
			seen[len(text)] = true
			m.lengths = append(m.lengths, len(text))
		}
	}
	for i, term := range terms {
		add(strings.ToLower(term), i)
	}
	for i, term := range terms {
		term = strings.ToLower(term)
		for _, alias := range aliases {
			if rest, ok := strings.CutPrefix(term, alias.from); ok {
				if _, taken := m.index[alias.to+rest]; !taken {
					add(alias.to+rest, i)
				}
			}
		}
	}
	return m
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

// find returns the indexes of the terms that occur in text, each once.
func (m *termMatcher) find(text string) []int {
	text = strings.ToLower(text)
	var found []int
	seen := map[int]bool{}
	wordStart := true
	for pos, r := range text {
		if wordStart {
			for _, length := range m.lengths {
				end := pos + length
				if end > len(text) {
					continue
				}
				i, ok := m.index[text[pos:end]]
				if !ok || seen[i] {
					continue
				}
				if next, size := utf8.DecodeRuneInString(text[end:]); size == 0 || !isWordRune(next) {
					seen[i] = true
					found = append(found, i)
				}
			}
		}
		wordStart = !isWordRune(r)
	}
	return found
}

type checkListHit struct {
	Number int64  `json:"number"`
	Kind   string `json:"kind"`
	State  string `json:"state"`
	Title  string `json:"title"`
}

func issueCheckListFn(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, err := params.GetString(args, "owner")
	if err != nil {
		return to.ErrorResult(err)
	}
	repo, err := params.GetString(args, "repo")
	if err != nil {
		return to.ErrorResult(err)
	}
	terms := parseTerms(args["terms"])
	if len(terms) == 0 {
		return to.ErrorResult(fmt.Errorf("terms is empty"))
	}
	if len(terms) > checkListMaxTerms {
		return to.ErrorResult(fmt.Errorf("at most %d terms per call, got %d", checkListMaxTerms, len(terms)))
	}
	state := params.GetOptionalString(args, "state", "all")
	matchBody := params.GetOptionalString(args, "match_in", "title_body") == "title_body"
	onlyMissing, _ := args["only_missing"].(bool)
	aliases, err := parseAliases(params.GetOptionalString(args, "aliases", ""))
	if err != nil {
		return to.ErrorResult(err)
	}

	opt := gitea_sdk.ListIssueOption{State: gitea_sdk.StateType(state)}
	switch params.GetOptionalString(args, "type", "all") {
	case "issues":
		opt.Type = gitea_sdk.IssueTypeIssue
	case "pulls":
		opt.Type = gitea_sdk.IssueTypePull
	}
	client, err := gitea.ClientFromContext(ctx)
	if err != nil {
		return to.ErrorResult(fmt.Errorf("get gitea client err: %v", err))
	}

	matcher := newTermMatcher(terms, aliases...)
	hits := make([][]checkListHit, len(terms))
	scanned := 0
	for page := 1; ; page++ {
		opt.Page, opt.PageSize = page, checkListPageSize
		issues, _, err := client.Issues.ListRepoIssues(ctx, owner, repo, opt)
		if err != nil {
			return to.ErrorResult(fmt.Errorf("get %v/%v/issues page %d err: %v", owner, repo, page, err))
		}
		for _, issue := range issues {
			text := issue.Title
			if matchBody {
				text += "\n" + issue.Body
			}
			for _, i := range matcher.find(text) {
				title := issue.Title
				if r := []rune(title); len(r) > checkListTitleChars {
					title = string(r[:checkListTitleChars]) + "…"
				}
				kind := "issue"
				if issue.PullRequest != nil {
					kind = "pull"
				}
				hits[i] = append(hits[i], checkListHit{Number: issue.Index, Kind: kind, State: string(issue.State), Title: title})
			}
		}
		scanned += len(issues)
		if len(issues) < checkListPageSize {
			break
		}
	}

	missing := []string{}
	found := map[string][]checkListHit{}
	for i, term := range terms {
		if len(hits[i]) == 0 {
			missing = append(missing, term)
		} else if !onlyMissing {
			found[term] = hits[i]
		}
	}
	result := map[string]any{
		"issues_scanned": scanned,
		"terms_checked":  len(terms),
		"found_count":    len(terms) - len(missing),
		"missing_count":  len(missing),
		"missing":        missing,
	}
	if !onlyMissing {
		result["found"] = found
	}
	return to.TextResult(result)
}
