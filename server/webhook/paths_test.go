// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xanzy/go-gitlab"

	"github.com/mattermost/mattermost-plugin-gitlab/server/subscription"
)

type pathsRetriever struct {
	*fakeWebhook
	calls  map[string]int
	failed bool
}

func (r *pathsRetriever) GetMergeRequestPaths(_ context.Context, _ string, _ int, creator string) ([]string, error) {
	r.calls[creator]++
	if r.failed {
		return nil, errors.New("incomplete diff")
	}
	return []string{"src/new.go", "old/name.go"}, nil
}

func TestMRPathFilteringAndCache(t *testing.T) {
	for _, failed := range []bool{false, true} {
		subs := []*subscription.Subscription{
			{ChannelID: "folder", CreatorID: "a", Features: `merges,path:"src/"`},
			{ChannelID: "renamed", CreatorID: "a", Features: `merges,path:"old/name.go"`},
			{ChannelID: "different-access", CreatorID: "b", Features: `merges,path:"src/"`},
			{ChannelID: "boundary", CreatorID: "a", Features: `merges,path:"src-old/"`},
			{ChannelID: "label-mismatch", CreatorID: "c", Features: `merges,label:"absent",path:"src/"`},
			{ChannelID: "event-mismatch", CreatorID: "d", Features: `merge_request_comments,path:"src/"`},
			{ChannelID: "unfiltered", CreatorID: "e", Features: "merges"},
		}
		r := &pathsRetriever{fakeWebhook: newFakeWebhook(subs), calls: map[string]int{}, failed: failed}
		w := &webhook{gitlabRetreiver: r}
		event := &gitlab.MergeEvent{}
		require.NoError(t, json.Unmarshal([]byte(OpenMergeRequest), event))
		handlers, warnings, err := w.HandleMergeRequest(context.Background(), event)
		require.NoError(t, err)
		require.Equal(t, map[string]int{"a": 1, "b": 1}, r.calls)
		var channels []string
		for _, handler := range handlers {
			channels = append(channels, handler.ToChannels...)
		}
		if failed {
			require.Equal(t, []string{"unfiltered"}, channels)
			require.NotEmpty(t, warnings)
		} else {
			require.ElementsMatch(t, []string{"folder", "renamed", "different-access", "unfiltered"}, channels)
			require.Empty(t, warnings)
		}
		require.NotEmpty(t, handlers[0].ToUsers, "personal recipients survive path failures")
	}
}

func TestMRCommentChecksEntireMRPaths(t *testing.T) {
	r := &pathsRetriever{fakeWebhook: newFakeWebhook([]*subscription.Subscription{
		{ChannelID: "channel", CreatorID: "a", Features: `merge_request_comments,path:"old/name.go"`},
	}), calls: map[string]int{}}
	w := &webhook{gitlabRetreiver: r}
	event := &gitlab.MergeCommentEvent{}
	require.NoError(t, json.Unmarshal([]byte(MergeRequestComment), event))
	event.Project.PathWithNamespace = "g/r"
	// No discussion file is supplied: the renamed path from the entire MR matches.
	handlers, _, err := w.HandleMergeRequestComment(context.Background(), event)
	require.NoError(t, err)
	require.Equal(t, 1, r.calls["a"])
	require.Equal(t, []string{"channel"}, handlers[len(handlers)-1].ToChannels)
}
