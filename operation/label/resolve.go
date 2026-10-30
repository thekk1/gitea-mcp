package label

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"gitea.com/gitea/gitea-mcp/internal/params"

	gitea_sdk "gitea.dev/sdk"
)

const resolvePageSize = 50

func ResolveIDs(ctx context.Context, client *gitea_sdk.Client, owner, repo string, raw any) ([]int64, error) {
	values, ok := raw.([]any)
	if !ok {
		return nil, errors.New("labels (array of names or IDs) is required")
	}
	ids := make([]int64, 0, len(values))
	var names []string
	for _, value := range values {
		switch value := value.(type) {
		case float64:
			ids = append(ids, int64(value))
		case string:
			names = append(names, value)
		default:
			return nil, fmt.Errorf("label must be a name or ID, got %v", value)
		}
	}
	if len(names) == 0 {
		return ids, nil
	}

	repoLabels, _, err := listAllLabels(func(page int) ([]*gitea_sdk.Label, *gitea_sdk.Response, error) {
		return client.Repositories.ListRepoLabels(ctx, owner, repo, gitea_sdk.ListLabelsOptions{Page: page, PageSize: resolvePageSize})
	})
	if err != nil {
		return nil, fmt.Errorf("list %v/%v/labels err: %v", owner, repo, err)
	}
	var orgLabels []*gitea_sdk.Label
	if slices.ContainsFunc(names, func(name string) bool {
		return !slices.ContainsFunc(repoLabels, func(l *gitea_sdk.Label) bool { return l.Name == name })
	}) {
		var resp *gitea_sdk.Response
		orgLabels, resp, err = listAllLabels(func(page int) ([]*gitea_sdk.Label, *gitea_sdk.Response, error) {
			return client.Organizations.ListOrgLabels(ctx, owner, gitea_sdk.ListOrgLabelsOptions{Page: page, PageSize: resolvePageSize})
		})
		if err != nil && (resp == nil || resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden) { // 404 for user owners, 403 for tokens without organization scope
			return nil, fmt.Errorf("list %v/labels err: %v", owner, err)
		}
	}

	var unknown []string
	for _, name := range names {
		id, err := findLabel(name, repoLabels, orgLabels)
		if err != nil {
			return nil, err
		}
		if id == 0 {
			id, _ = params.ToInt64(name)
		}
		if id == 0 {
			unknown = append(unknown, name)
		} else {
			ids = append(ids, id)
		}
	}
	if len(unknown) > 0 {
		var available []string
		for _, l := range slices.Concat(repoLabels, orgLabels) {
			available = append(available, l.Name)
		}
		return nil, fmt.Errorf("unknown labels in %v/%v: %s (available: %s)", owner, repo, strings.Join(unknown, ", "), strings.Join(available, ", "))
	}
	return ids, nil
}

func findLabel(name string, labelLists ...[]*gitea_sdk.Label) (int64, error) {
	for _, labels := range labelLists {
		var matches []int64
		for _, l := range labels {
			if l.Name == name {
				matches = append(matches, l.ID)
			}
		}
		if len(matches) > 1 {
			return 0, fmt.Errorf("label %q is ambiguous, pass one of the IDs %v instead", name, matches)
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
	}
	return 0, nil
}

func listAllLabels(list func(page int) ([]*gitea_sdk.Label, *gitea_sdk.Response, error)) ([]*gitea_sdk.Label, *gitea_sdk.Response, error) {
	var all []*gitea_sdk.Label
	page := 1
	for {
		labels, resp, err := list(page)
		if err != nil {
			return nil, resp, err
		}
		all = append(all, labels...)
		if resp == nil || resp.NextPage <= page {
			return all, resp, nil
		}
		page = resp.NextPage
	}
}
