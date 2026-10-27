// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/shared/markdown"
	gitlabLib "github.com/xanzy/go-gitlab"

	"github.com/mattermost/mattermost-plugin-gitlab/server/gitlab"
)

var mrLinkPath = regexp.MustCompile(`^(.+?)/(?:-/)?merge_requests/([0-9]+)$`)

func rewriteMRLinks(message, baseURL string, title func(project string, iid int) (string, error)) string {
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" {
		return message
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	cache := map[string]string{}
	markdown.Inspect(message, func(node any) bool {
		switch n := node.(type) {
		case *markdown.InlineLink, *markdown.ReferenceLink, *markdown.InlineImage, *markdown.ReferenceImage:
			return false
		case *markdown.Autolink:
			u, err := url.Parse(n.Destination())
			if err != nil || u.Host != base.Host || u.Scheme != base.Scheme || u.User != nil {
				return false
			}
			prefix := strings.TrimRight(base.Path, "/") + "/"
			if !strings.HasPrefix(u.Path, prefix) {
				return false
			}
			parts := mrLinkPath.FindStringSubmatch(strings.TrimPrefix(u.Path, prefix))
			if parts == nil || !strings.Contains(parts[1], "/") {
				return false
			}
			iid, err := strconv.Atoi(parts[2])
			if err != nil || iid < 1 {
				return false
			}
			key := parts[1] + "!" + parts[2]
			text, found := cache[key]
			if !found {
				text, err = title(parts[1], iid)
				if err != nil {
					text = ""
				}
				cache[key] = text
			}
			if text == "" {
				return false
			}
			name := parts[1][strings.LastIndex(parts[1], "/")+1:]
			label := escapeMRLinkText(name + ":MR-" + parts[2] + ": " + text)
			start, end := n.RawDestination.Position, n.RawDestination.End
			if start > 0 && end < len(message) && message[start-1] == '<' && message[end] == '>' {
				start--
				end++
			}
			destination := strings.NewReplacer("(", "%28", ")", "%29", "<", "%3C", ">", "%3E").Replace(n.Destination())
			edits = append(edits, edit{start, end, "[" + label + "](" + destination + ")"})
			return false
		}
		return true
	})
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		message = message[:e.start] + e.text + message[e.end:]
	}
	return message
}

func escapeMRLinkText(text string) string {
	var result strings.Builder
	for _, ch := range text {
		if ch == '\n' || ch == '\r' {
			result.WriteByte(' ')
			continue
		}
		if strings.ContainsRune(`\`+"`*_{}[]()#+-.!|><~", ch) {
			result.WriteByte('\\')
		}
		result.WriteRune(ch)
	}
	return result.String()
}

func (p *Plugin) enrichMRLinks(post *model.Post) bool {
	if post.UserId == "" || post.UserId == p.BotUserID {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var info *gitlab.UserInfo
	var client *gitlabLib.Client
	loaded := false
	message := rewriteMRLinks(post.Message, p.getConfiguration().GitlabURL, func(project string, iid int) (string, error) {
		if err := p.isNamespaceAllowed(project); err != nil {
			return "", err
		}
		if !loaded {
			info, _ = p.getGitlabUserInfoByMattermostID(post.UserId)
			loaded = true
			if info != nil {
				// A posting hook must not wait for the cluster token-refresh mutex.
				// Expired/disconnected accounts keep their original links.
				token, tokenErr := p.getGitlabUserTokenByMattermostID(post.UserId)
				if tokenErr == nil && token.Valid() {
					client, _ = p.GitlabClient.GitlabConnect(*token)
				}
			}
		}
		if client == nil {
			return "", nil
		}
		mr, _, err := client.MergeRequests.GetMergeRequest(project, iid, nil, gitlabLib.WithContext(ctx))
		if err != nil || mr == nil {
			return "", err
		}
		return mr.Title, nil
	})
	if message == post.Message {
		return false
	}
	post.Message = message
	return true
}

// Existing labelled links are preserved on edit; newly inserted bare links are
// enriched. This also makes repeated hook execution idempotent.
func (p *Plugin) MessageWillBeUpdated(_ *plugin.Context, post, _ *model.Post) (*model.Post, string) {
	if p.enrichMRLinks(post) {
		return post, ""
	}
	return nil, ""
}
