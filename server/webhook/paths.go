// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package webhook

import (
	"context"
	"fmt"

	"github.com/xanzy/go-gitlab"

	"github.com/mattermost/mattermost-plugin-gitlab/server/subscription"
)

type mergeRequestPathsRetriever interface {
	GetMergeRequestPaths(context.Context, string, int, string) ([]string, error)
}

// Cache is local to this event, scoped by subscription creator's access token.
func (w *webhook) filterMRPaths(ctx context.Context, subs []*subscription.Subscription, project string, iid int, labels []*gitlab.EventLabel, eligible func(*subscription.Subscription) bool) ([]*subscription.Subscription, []string) {
	type result struct {
		paths []string
		err   error
	}
	cache := map[string]result{}
	var filtered []*subscription.Subscription
	var warnings []string
	for _, sub := range subs {
		if !eligible(sub) {
			continue
		}
		paths, err := sub.Paths()
		if err != nil {
			warnings = appendUniqueWarnings(warnings, err.Error())
			continue
		}
		if len(paths) == 0 {
			filtered = append(filtered, sub)
			continue
		}
		if ok, warning := anyEventLabelInSubs(sub, labels); !ok {
			if warning != "" {
				warnings = appendUniqueWarnings(warnings, warning)
			}
			continue
		}
		r, found := cache[sub.CreatorID]
		if !found {
			retriever, supported := w.gitlabRetreiver.(mergeRequestPathsRetriever)
			if supported {
				r.paths, r.err = retriever.GetMergeRequestPaths(ctx, project, iid, sub.CreatorID)
			} else {
				r.err = fmt.Errorf("MR paths retrieval unavailable")
			}
			cache[sub.CreatorID] = r
		}
		if r.err != nil {
			warnings = appendUniqueWarnings(warnings, fmt.Sprintf("path filter skipped for %s, channel %s: %v", project, sub.ChannelID, r.err))
			continue
		}
		if matches, _ := sub.MatchesPaths(r.paths); matches {
			filtered = append(filtered, sub)
		}
	}
	return filtered, warnings
}
