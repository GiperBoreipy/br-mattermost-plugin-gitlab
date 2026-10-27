// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	gitlabLib "github.com/xanzy/go-gitlab"

	"github.com/mattermost/mattermost-plugin-gitlab/server/gitlab"
	"github.com/mattermost/mattermost-plugin-gitlab/server/subscription"
	"github.com/mattermost/mattermost-plugin-gitlab/server/webhook"
)

type deliveryRetriever struct {
	webhook.GitlabRetreiver
	subs []*subscription.Subscription
}

func (r deliveryRetriever) GetSubscribedChannelsForProject(context.Context, string, string, bool, bool) []*subscription.Subscription {
	return r.subs
}

func (deliveryRetriever) GetUsernameByID(id int) string {
	if id == 1 {
		return "recipient"
	}
	return "actor"
}
func (deliveryRetriever) GetUserURL(user string) string { return "https://gitlab/" + user }
func (deliveryRetriever) ParseGitlabUsernamesFromText(body string) []string {
	if strings.Contains(body, "@recipient") {
		return []string{"recipient"}
	}
	return nil
}

func TestWebhookFinalDMDelivery(t *testing.T) {
	for _, comment := range []bool{false, true} {
		for _, mention := range []bool{false, true} {
			for _, notifications := range []bool{false, true} {
				for _, subscribed := range []bool{false, true} {
					t.Run(fmt.Sprintf("comment=%v/mention=%v/notifications=%v/subscribed=%v", comment, mention, notifications, subscribed), func(t *testing.T) {
						api := &plugintest.API{}
						p := newTestPluginWithAPI(api)
						p.configuration = &configuration{WebhookSecret: "secret"}
						features := "merges,merge_request_comments"
						if !subscribed {
							features += `,label:"absent"`
						}
						p.WebhookHandler = webhook.NewWebhook(deliveryRetriever{subs: []*subscription.Subscription{
							{ChannelID: "dm", Repository: "g/r", Features: features},
							{ChannelID: "dm", Repository: "g/", Features: features},
							{ChannelID: "other-channel", Features: "merges,merge_request_comments"},
						}})
						info, err := json.Marshal(&gitlab.UserInfo{UserID: "user", Settings: &gitlab.UserSettings{Notifications: notifications}})
						require.NoError(t, err)
						api.On("KVGet", "actor_gitlabusername").Return(nil, nil)
						api.On("KVGet", "recipient_gitlabusername").Return([]byte("user"), nil)
						api.On("KVGet", "user"+GitlabUserInfoKey).Return(info, nil)
						api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return(nil)
						api.On("GetDirectChannel", "user", "").Return(&model.Channel{Id: "dm"}, nil)
						api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything).Return(nil)
						api.On("CreatePost", mock.Anything).Return(&model.Post{}, nil)
						fakeDedupKV(api)
						body := "event context"
						if mention {
							body += " @recipient"
						}
						project := map[string]any{"id": 1, "path_with_namespace": "g/r", "visibility": "public"}
						attrs := map[string]any{"id": 10, "iid": 3, "author_id": 1, "assignee_ids": []int{1}, "state": "opened", "action": "open", "title": "MR title", "description": body, "url": "https://gitlab/g/r/-/merge_requests/3"}
						attrs["target"] = project
						payload := map[string]any{"project": project, "user": map[string]any{"id": 2, "username": "actor"}, "object_attributes": attrs}
						payload["repository"] = map[string]any{"homepage": "https://gitlab/g/r"}
						eventType := gitlabLib.EventTypeMergeRequest
						if comment {
							eventType = gitlabLib.EventTypeNote
							payload["object_kind"] = "note"
							payload["merge_request"] = attrs
							payload["object_attributes"] = map[string]any{"id": 99, "noteable_type": "MergeRequest", "note": body, "url": "https://gitlab/g/r/-/merge_requests/3#note_99"} //nolint:misspell // GitLab webhook field name.
						}
						data, err := json.Marshal(payload)
						require.NoError(t, err)
						for range 2 {
							req := httptest.NewRequest("POST", "/", bytes.NewReader(data))
							req.Header.Set("X-Gitlab-Token", "secret")
							req.Header.Set("X-Gitlab-Event", string(eventType))
							p.handleWebhook(httptest.NewRecorder(), req)
						}
						posts := 0
						for _, call := range api.Calls {
							if call.Method != "CreatePost" {
								continue
							}
							posts++
							post := call.Arguments[0].(*model.Post)
							if post.ChannelId == "dm" {
								if mention && notifications {
									require.Contains(t, post.Message, "mentioned you")
								}
								if subscribed {
									require.Contains(t, post.Message, "MR title")
									require.Contains(t, post.Message, body)
								}
							}
						}
						if subscribed || notifications {
							require.Equal(t, 2, posts)
						} else {
							require.Equal(t, 1, posts)
						}
					})
				}
			}
		}
	}
}

func TestEventDeliverySelection(t *testing.T) {
	for _, mention := range []bool{false, true} {
		deliveries := map[string]*eventDelivery{}
		addEventDelivery(deliveries, "dm", "review request", false, false)
		addEventDelivery(deliveries, "dm", "MR title and comment", true, false)
		addEventDelivery(deliveries, "dm", "MR title and comment", true, false)
		addEventDelivery(deliveries, "channel", "MR title and comment", true, false)
		if mention {
			addEventDelivery(deliveries, "dm", "mentioned you: comment", false, true)
		}
		require.Len(t, deliveries, 2)
		if mention {
			require.Equal(t, "mentioned you: comment\n\nMR title and comment", deliveries["dm"].message())
		} else {
			require.Equal(t, "MR title and comment", deliveries["dm"].message())
		}
		require.Equal(t, "MR title and comment", deliveries["channel"].message())
	}
}

func TestWebhookEventIdentity(t *testing.T) {
	a := []byte(`{"project":{"id":1,"path_with_namespace":"group/repo"},"object_attributes":{"id":10,"note":"same"},"user":{"id":2}}`)
	b := []byte(`{"user":{"id":2},"object_attributes":{"note":"same","id":10},"project":{"path_with_namespace":"group/repo","id":1},"event_uuid":"different hook"}`)
	c := []byte(`{"project":{"id":1,"path_with_namespace":"group/repo"},"object_attributes":{"id":11,"note":"same"},"user":{"id":2}}`)
	require.Equal(t, webhookEventIdentity(gitlabLib.EventTypeNote, a), webhookEventIdentity(gitlabLib.EventTypeNote, b))
	require.NotEqual(t, webhookEventIdentity(gitlabLib.EventTypeNote, a), webhookEventIdentity(gitlabLib.EventTypeNote, c))
}

func TestWebhookDMResolutionFailureCanRetry(t *testing.T) {
	p, api := setupDedupTestPlugin(t)
	kv := fakeDedupKV(api)
	api.On("GetDirectChannel", "1", "").Unset()
	api.On("GetDirectChannel", "1", "").Return(nil, model.NewAppError("dm", "error", nil, "unavailable", 500)).Once()
	api.On("GetDirectChannel", "1", "").Return(&model.Channel{Id: "dm-channel"}, nil)
	api.On("LogWarn", mock.Anything, "user_id", "1", "err", mock.Anything).Return(nil)
	p.handleWebhook(httptest.NewRecorder(), newIssueCommentWebhookRequest())
	require.Empty(t, kv.keys())
	api.AssertNumberOfCalls(t, "CreatePost", 0)
	p.handleWebhook(httptest.NewRecorder(), newIssueCommentWebhookRequest())
	api.AssertNumberOfCalls(t, "CreatePost", 1)
}

func TestWebhookAmbiguousPostFailureKeepsClaim(t *testing.T) {
	p, api := setupDedupTestPlugin(t)
	kv := fakeDedupKV(api)
	api.On("CreatePost", mock.Anything).Unset()
	api.On("CreatePost", mock.Anything).Return(nil, model.NewAppError("post", "error", nil, "timeout", 500))
	api.On("LogWarn", mock.Anything, "channel_id", "dm-channel", "err", mock.Anything).Return(nil)
	for range 2 {
		p.handleWebhook(httptest.NewRecorder(), newIssueCommentWebhookRequest())
	}
	require.Len(t, kv.keys(), 1)
	api.AssertNumberOfCalls(t, "CreatePost", 1)
}

func TestWebhookKVFailureDelivers(t *testing.T) {
	p, api := setupDedupTestPlugin(t)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.MatchedBy(func(opts model.PluginKVSetOptions) bool { return opts.Atomic && opts.ExpireInSeconds == 86400 })).Return(false, model.NewAppError("kv", "error", nil, "unavailable", 500))
	api.On("LogWarn", mock.Anything, "dedup_key", mock.Anything, "err", mock.Anything).Return(nil)
	p.handleWebhook(httptest.NewRecorder(), newIssueCommentWebhookRequest())
	api.AssertNumberOfCalls(t, "CreatePost", 1)
}

func TestWebhookIdenticalCommentsAreIndependent(t *testing.T) {
	p, api := setupDedupTestPlugin(t)
	fakeDedupKV(api)
	for _, id := range []string{"1", "2"} {
		body := strings.Replace(noteIssueCommentBody, `"noteable_type"`, `"id":`+id+`,"noteable_type"`, 1) //nolint:misspell // GitLab webhook field name.
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.Header.Set("X-Gitlab-Token", "secret")
		req.Header.Set("X-Gitlab-Event", string(gitlabLib.EventTypeNote))
		p.handleWebhook(httptest.NewRecorder(), req)
	}
	api.AssertNumberOfCalls(t, "CreatePost", 2)
}
