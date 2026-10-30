package tool

type Alias struct {
	Tool    string
	Methods []string
}

// DeprecatedAliases maps tool names removed by consolidation to the methods that replaced them, so --tools allowlists keep working without widening.
var DeprecatedAliases = map[string]Alias{
	"attachment_read":      {"issue_read", []string{"get_attachments", "get_attachment", "download_attachment"}},
	"list_branches":        {"branch_read", []string{"list"}},
	"create_branch":        {"branch_write", []string{"create"}},
	"delete_branch":        {"branch_write", []string{"delete"}},
	"rename_branch":        {"branch_write", []string{"rename"}},
	"list_commits":         {"commit_read", []string{"list"}},
	"get_commit":           {"commit_read", []string{"get"}},
	"get_commit_status":    {"commit_read", []string{"get_status"}},
	"compare_commits":      {"commit_read", []string{"compare"}},
	"create_commit_status": {"commit_write", []string{"create_status"}},
	"list_releases":        {"release_read", []string{"list"}},
	"get_release":          {"release_read", []string{"get"}},
	"get_latest_release":   {"release_read", []string{"get_latest"}},
	"create_release":       {"release_write", []string{"create"}},
	"delete_release":       {"release_write", []string{"delete"}},
	"list_my_repos":        {"repo_read", []string{"list_my_repos"}},
	"list_org_repos":       {"repo_read", []string{"list_org_repos"}},
	"create_repo":          {"repo_write", []string{"create"}},
	"fork_repo":            {"repo_write", []string{"fork"}},
	"list_tags":            {"tag_read", []string{"list"}},
	"get_tag":              {"tag_read", []string{"get"}},
	"create_tag":           {"tag_write", []string{"create"}},
	"delete_tag":           {"tag_write", []string{"delete"}},
}
