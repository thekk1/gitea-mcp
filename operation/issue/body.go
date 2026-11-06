package issue

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	gitea_sdk "gitea.dev/sdk"
)

// bodyArg returns the body from body or body_base64; present is false when neither is given.
func bodyArg(args map[string]any) (body string, present bool, err error) {
	plain, hasPlain := args["body"].(string)
	encoded, _ := args["body_base64"].(string)
	if strings.TrimSpace(encoded) == "" {
		return plain, hasPlain, nil
	}
	if hasPlain && plain != "" {
		return "", false, errors.New("give body or body_base64, not both")
	}
	data, err := decodeBodyBase64(encoded)
	if err != nil {
		return "", false, err
	}
	if !utf8.Valid(data) {
		return "", false, errors.New("body_base64 is not UTF-8 text")
	}
	return string(data), true, nil
}

func requiredBodyArg(args map[string]any) (string, error) {
	body, present, err := bodyArg(args)
	if err != nil {
		return "", err
	}
	if !present {
		return "", errors.New("body or body_base64 is required")
	}
	return body, nil
}

// decodeBodyBase64 accepts standard and URL-safe base64, with or without padding and line breaks.
func decodeBodyBase64(encoded string) ([]byte, error) {
	text := strings.Join(strings.Fields(encoded), "")
	text = strings.TrimRight(strings.NewReplacer("-", "+", "_", "/").Replace(text), "=")
	data, err := base64.RawStdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("body_base64 is not valid base64: %v", err)
	}
	return data, nil
}

// issueBodyLocks serialises the read-modify-write of an issue body, so attachments uploaded in
// parallel do not overwrite each other's links.
var issueBodyLocks sync.Map

func linkAttachmentInIssueBody(ctx context.Context, client *gitea_sdk.Client, owner, repo string, index int64, filename, target string) (int, error) {
	key := strings.ToLower(fmt.Sprintf("%s/%s#%d", owner, repo, index))
	lock, _ := issueBodyLocks.LoadOrStore(key, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	issue, _, err := client.Issues.GetIssue(ctx, owner, repo, index)
	if err != nil {
		return 0, err
	}
	body, linked := replaceLinkTarget(issue.Body, filename, target)
	if linked == 0 {
		return 0, nil
	}
	if _, _, err := client.Issues.EditIssue(ctx, owner, repo, index, gitea_sdk.EditIssueOption{Body: &body}); err != nil {
		return 0, err
	}
	return linked, nil
}

// replaceLinkTarget replaces the Markdown link targets filename, <filename> and filename with
// %20 for spaces by target, with or without a link title; it returns the new text and the count.
func replaceLinkTarget(text, filename, target string) (string, int) {
	count := 0
	seen := map[string]bool{}
	for _, form := range []string{filename, "<" + filename + ">", strings.ReplaceAll(filename, " ", "%20")} {
		if seen[form] {
			continue
		}
		seen[form] = true
		for _, end := range []string{")", " \""} {
			old := "](" + form + end
			if n := strings.Count(text, old); n > 0 {
				text = strings.ReplaceAll(text, old, "]("+target+end)
				count += n
			}
		}
	}
	return text, count
}
