# Gitea MCP Server

[繁體中文](README.zh-tw.md) | [简体中文](README.zh-cn.md)

**Gitea MCP Server** connects a [Gitea](https://about.gitea.com) instance to [Model Context Protocol](https://modelcontextprotocol.io) clients, so repositories, issues, pull requests and more can be browsed and managed from an MCP-compatible chat interface.

[![Install with Docker in VS Code](https://img.shields.io/badge/VS_Code-Install_Server-0098FF?style=flat-square&logo=visualstudiocode&logoColor=white)](https://insiders.vscode.dev/redirect/mcp/install?name=gitea&inputs=[{%22id%22:%22gitea_token%22,%22type%22:%22promptString%22,%22description%22:%22Gitea%20Personal%20Access%20Token%22,%22password%22:true}]&config={%22command%22:%22docker%22,%22args%22:[%22run%22,%22-i%22,%22--rm%22,%22-e%22,%22GITEA_ACCESS_TOKEN%22,%22docker.gitea.com/gitea-mcp-server%22],%22env%22:{%22GITEA_ACCESS_TOKEN%22:%22${input:gitea_token}%22}}) [![Install with Docker in VS Code Insiders](https://img.shields.io/badge/VS_Code_Insiders-Install_Server-24bfa5?style=flat-square&logo=visualstudiocode&logoColor=white)](https://insiders.vscode.dev/redirect/mcp/install?name=gitea&inputs=[{%22id%22:%22gitea_token%22,%22type%22:%22promptString%22,%22description%22:%22Gitea%20Personal%20Access%20Token%22,%22password%22:true}]&config={%22command%22:%22docker%22,%22args%22:[%22run%22,%22-i%22,%22--rm%22,%22-e%22,%22GITEA_ACCESS_TOKEN%22,%22docker.gitea.com/gitea-mcp-server%22],%22env%22:{%22GITEA_ACCESS_TOKEN%22:%22${input:gitea_token}%22}}&quality=insiders)

## Installation

Download a binary from the [releases page](https://gitea.com/gitea/gitea-mcp/releases) and put it in your `PATH`, use the `docker.gitea.com/gitea-mcp-server` image, or build from source into `$GOPATH/bin` with `make` and Go 1.27 or later:

```bash
git clone https://gitea.com/gitea/gitea-mcp.git
cd gitea-mcp
make install
```

## Configuration

Pass the Gitea host and access token as command-line flags or environment variables, flags take precedence. Run `gitea-mcp --help` for the full list of flags and environment variables. Logs go to stderr, add `-d` for debug logging, which includes every request sent to Gitea. To reach a Gitea listening on a Unix socket, pass `-gitea-unix-socket <path>`, the host then defaults to `http://unix/`. To send extra headers to Gitea, for example for Cloudflare Access, set `GITEA_EXTRA_HEADERS` to a JSON object like `{"CF-Access-Client-Id":"id","CF-Access-Client-Secret":"secret"}`. For a Gitea that requires mutual TLS, pass PEM files with `-client-cert` and `-client-key`, which `-oauth` does not support. Renewed files apply without a restart. `-ca-cert` adds a PEM CA bundle to the system roots, read once at startup.

### MCP protocol and HTTP transport

The server supports MCP up to `2026-07-28` and negotiates down to the client's version, advertising only the `tools` capability. Tool and Gitea failures return a `tools/call` result with `result.isError: true`, while malformed requests and server faults stay JSON-RPC errors.

HTTP is always stateless: `/mcp` accepts POST only, without `Mcp-Session-Id`, standalone SSE or `Last-Event-ID` resumability. Origins are validated, and reverse proxies must forward `Mcp-Protocol-Version`, `Mcp-Method` and `Mcp-Name` unchanged. `Authorization: Bearer <token>` and `Authorization: token <token>` pass a Gitea credential per request, which is credential passthrough rather than MCP OAuth. Credentials Gitea rejects get a `401`, so clients can refresh them.

HTTP mode serves `/healthz` for health probes, e.g. a Kubernetes `httpGet` probe. The Docker image has a built-in health check that works in both modes, on a read-only root filesystem it needs a tmpfs mounted at `/tmp`.

### OAuth for remote clients

Clients that cannot be given a token, such as Claude.ai on web and mobile, need the OAuth 2.1 flow from the MCP authorization spec, where `-oauth-public-url` is the public origin clients reach the server at:

```bash
gitea-mcp -t http -oauth -oauth-public-url https://mcp.example.com -H https://gitea.example.com
```

The server then serves `/.well-known/oauth-protected-resource`, naming the Gitea instance as the authorization server, and answers `/mcp` requests with a missing or rejected token with a `401` challenge. Users act with their own Gitea account instead of a shared static token, and `-r` narrows the requested scopes to read-only.

Gitea has no dynamic client registration, so register the application once under **Settings → Applications → Create OAuth2 Application**, leaving **Confidential Client** unchecked so the flow uses PKCE. Set the redirect URI to `https://claude.ai/api/mcp/auth_callback` for Claude on web, desktop and mobile, adding `http://127.0.0.1/callback` for Claude Code, which Gitea matches on any port. Users enter the client ID under **Advanced settings** when adding the connector.

Both the server and Gitea have to be reachable over HTTPS from the client's network, for hosted Claude that is Anthropic's [egress range](https://platform.claude.com/docs/en/api/ip-addresses). Behind a reverse proxy, `/mcp` only accepts the host from `-oauth-public-url` or a loopback host, so forward `Host` unchanged or leave it as the proxy default.

### Claude Code

Runs the server through `go run` and requires [Go](https://go.dev):

```bash
claude mcp add --transport stdio --scope user gitea \
  --env GITEA_ACCESS_TOKEN=token \
  --env GITEA_HOST=https://gitea.com \
  -- go run gitea.com/gitea/gitea-mcp@latest -t stdio
```

### VS Code

Use the install buttons at the top of this README, or add the block below to your User Settings (JSON), reachable via `Ctrl + Shift + P` and `Preferences: Open User Settings (JSON)`. It also works in a workspace `.vscode/mcp.json`, where the `mcp` key is omitted.

```json
{
  "mcp": {
    "inputs": [
      {
        "type": "promptString",
        "id": "gitea_token",
        "description": "Gitea Personal Access Token",
        "password": true
      }
    ],
    "servers": {
      "gitea-mcp": {
        "command": "docker",
        "args": ["run", "-i", "--rm", "-e", "GITEA_ACCESS_TOKEN", "docker.gitea.com/gitea-mcp-server"],
        "env": {
          "GITEA_ACCESS_TOKEN": "${input:gitea_token}"
        }
      }
    }
  }
}
```

### OpenCode

Add the following to the top-level `mcp` object of your [OpenCode](https://opencode.ai) config:

```json
    "gitea-mcp": {
      "enabled": true,
      "type": "local",
      "command": [
        "gitea-mcp",
        "-t", "stdio",
        "-H", "https://gitea.com",
        "-T", "<your personal access token>"
      ]
    }
```

### Mistral Vibe

Add the following to `~/.vibe/config.toml`:

```toml
[[mcp_servers]]
name = "gitea"
transport = "stdio"
command = "docker"
args = ["run", "--rm", "-i", "-e", "GITEA_ACCESS_TOKEN", "-e", "GITEA_HOST", "docker.gitea.com/gitea-mcp-server"]

[mcp_servers.env]
GITEA_ACCESS_TOKEN = "TOKEN"
GITEA_HOST = "https://gitea.com"
```

### Other clients

Clients such as Cursor take either a stdio command:

```json
{
  "mcpServers": {
    "gitea": {
      "command": "gitea-mcp",
      "args": ["-t", "stdio", "--host", "https://gitea.com"],
      "env": {
        "GITEA_ACCESS_TOKEN": "<your personal access token>"
      }
    }
  }
}
```

or an http endpoint, for a server started with `gitea-mcp -t http --port 8080`:

```json
{
  "mcpServers": {
    "gitea": {
      "url": "http://localhost:8080/mcp",
      "headers": {
        "Authorization": "Bearer <your personal access token>"
      }
    }
  }
}
```

Once configured, try `list all my repositories` in the chat box.

## Available Tools

| Tool                         | Scope        | Access | Description |
| :--------------------------- | :----------- | :----- | :---------- |
| get_gitea_mcp_server_version | version      | Read   | Get the Gitea MCP server version |
| get_me                       | user         | Read   | Get the current authenticated user |
| get_user_orgs                | user         | Read   | List the current user's organizations |
| create_organization          | user         | Write  | Create a new organization |
| search_users                 | search       | Read   | Search for users |
| search_org_teams             | search       | Read   | Search teams within an organization |
| search_repos                 | search       | Read   | Search for repositories |
| search_issues                | search       | Read   | Search issues and pull requests across repositories |
| notification_read            | notification | Read   | Read notifications: list (optionally scoped to a repo) or get a thread by ID |
| notification_write           | notification | Write  | Mark a notification or all notifications as read |
| label_read                   | label        | Read   | Read repository or organization labels |
| label_write                  | label        | Write  | Write labels (repo or org): create, edit, delete |
| milestone_read               | milestone    | Read   | Read milestones: get one or list |
| milestone_write              | milestone    | Write  | Write milestones: create, update, delete |
| wiki_read                    | wiki         | Read   | Read wiki: list pages, get content, revision history |
| wiki_write                   | wiki         | Write  | Write wiki pages: create, update, delete |
| timetracking_read            | timetracking | Read   | Read time tracking: issue/repo times, active stopwatches, your tracked times |
| timetracking_write           | timetracking | Write  | Write time tracking: stopwatches and entries |
| package_read                 | packages     | Read   | Read package registry: list packages, list versions, or get a version |
| package_write                | packages     | Write  | Delete a package version (irreversible) |
| project_read                 | project      | Read   | Read projects, columns and column issues |
| project_write                | project      | Write  | Write projects, columns and issue placement |
| list_issues                  | issue        | Read   | List repository issues |
| issue_check_list             | issue        | Read   | Check a list of terms against all repository issues |
| attachment_read              | issue        | Read   | Read issue/comment attachments: list metadata, get metadata, or download content |
| issue_read                   | issue        | Read   | Read issue: details, comments, labels, dependencies, or assignee candidates |
| issue_write                  | issue        | Write  | Write issues: create, update, manage comments, labels and dependencies |
| list_pull_requests           | pull_request | Read   | List repository pull requests |
| pull_request_read            | pull_request | Read   | Read pull request: details, diff, files, commits, status, reviews, review comments, discussion comments, lookup by branches or commit, reviewer candidates |
| pull_request_write           | pull_request | Write  | Write pull requests: create, update, close, reopen, merge, update branch, manage reviewers |
| pull_request_review_write    | pull_request | Write  | Write PR reviews: create, submit, delete, dismiss, reply to and resolve review comments |
| actions_config_read          | actions      | Read   | Read Actions secrets and variables |
| actions_config_write         | actions      | Write  | Write Actions secrets and variables: upsert, create, update, delete |
| actions_run_read             | actions      | Read   | Read Actions workflows, runs, jobs, logs, and artifacts |
| actions_run_write            | actions      | Write  | Write Actions runs: dispatch, cancel, rerun runs or jobs, approve, enable/disable workflows |
| create_repo                  | repository   | Write  | Create a new repository |
| fork_repo                    | repository   | Write  | Fork a repository |
| list_my_repos                | repository   | Read   | List repositories owned by the current user |
| list_org_repos               | repository   | Read   | List repositories in an organization |
| get_repository_tree          | repository   | Read   | Get the repository file tree |
| get_file_contents            | file         | Read   | Get file content and metadata, or list a directory |
| get_dir_contents             | file         | Read   | Get the entries in a directory |
| create_or_update_file        | file         | Write  | Write files in one commit: create, update, rename, delete |
| delete_file                  | file         | Write  | Delete a file |
| create_branch                | branch       | Write  | Create a new branch |
| delete_branch                | branch       | Write  | Delete a branch |
| list_branches                | branch       | Read   | List repository branches |
| rename_branch                | branch       | Write  | Rename a branch |
| create_tag                   | tag          | Write  | Create a tag |
| delete_tag                   | tag          | Write  | Delete a tag |
| get_tag                      | tag          | Read   | Get tag details |
| list_tags                    | tag          | Read   | List repository tags |
| list_commits                 | commit       | Read   | List repository commits |
| get_commit                   | commit       | Read   | Get commit details, optionally with its diff |
| compare_commits              | commit       | Read   | Compare two refs: commits and changed files |
| get_commit_status            | commit       | Read   | Get the combined status of a commit, branch or tag |
| create_commit_status         | commit       | Write  | Create a status on a commit |
| create_release               | release      | Write  | Create a release |
| delete_release               | release      | Write  | Delete a release |
| get_release                  | release      | Read   | Get a release by ID |
| get_latest_release           | release      | Read   | Get the latest release |
| list_releases                | release      | Read   | List repository releases |

> **Note:** Several tools are consolidated, action-based tools, a single tool exposes multiple operations through a `method` parameter. Tools with `Write` access are hidden when the server runs in read-only mode (`-r` / `GITEA_READONLY`), and the exposed tool set can be filtered by scope with `-S` / `--scope` (`GITEA_SCOPES`) and/or by individual tool name with `-O` / `--tools` (`GITEA_TOOLS`).

With neither flag set, every tool loads. `--scope` limits loading to tools whose Scope column value is in the given list; `--tools` limits loading to the named tools; setting both loads the union of the selected scopes and the individually named tools. Unknown scope names are ignored with a startup warning.

```bash
gitea-mcp -S issue,pull_request
gitea-mcp --scope repository,branch --tools get_me
```

Many tools accept `page` and `per_page` for pagination. The maximum effective page size is the Gitea server's `[api].MAX_RESPONSE_ITEMS` setting (default **50**), larger values are silently capped.
