// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-gitlab/server/gitlab"
)

func TestRewriteMRLinks(t *testing.T) {
	const link = "https://gitlab.example/group/sub/BRClient/-/merge_requests/2639?x=1#note_2"
	lookup := func(project string, iid int) (string, error) {
		require.Equal(t, "group/sub/BRClient", project)
		require.Equal(t, 2639, iid)
		return "feature: [Editor] | *test*", nil
	}
	result := rewriteMRLinks("See "+link, "https://gitlab.example", lookup)
	require.Equal(t, `See [BRClient:MR\-2639: feature: \[Editor\] \| \*test\*](`+link+")", result)
	require.Equal(t, result, rewriteMRLinks(result, "https://gitlab.example", lookup))
	for _, message := range []string{
		"`" + link + "`", "```go\n" + link + "\n```", "~~~\n" + link + "\n~~~", "    " + link,
		"[my title](" + link + ")", "![image](" + link + ")", "[ref][mr]\n\n[mr]: " + link,
		strings.ReplaceAll(link, "gitlab.example", "other.example"),
	} {
		require.Equal(t, message, rewriteMRLinks(message, "https://gitlab.example", func(string, int) (string, error) { t.Fatal("unexpected lookup", message); return "", nil }))
	}
	require.Equal(t, link, rewriteMRLinks(link, "https://gitlab.example", func(string, int) (string, error) { return "", errors.New("forbidden") }))
	calls := 0
	result = rewriteMRLinks(link+" <"+link+">", "https://gitlab.example", func(string, int) (string, error) { calls++; return "title", nil })
	require.Equal(t, 1, calls)
	require.Equal(t, 2, strings.Count(result, "[BRClient:"))
}

func TestMRLinkPostingHooks(t *testing.T) {
	for _, scenario := range []string{"success", "forbidden", "disconnected", "expired", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				if scenario == "timeout" {
					<-r.Context().Done()
					return
				}
				if scenario == "forbidden" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"title":"Editor title"}`))
			}))
			defer server.Close()
			api := &plugintest.API{}
			p := newTestPluginWithAPI(api)
			p.configuration = &configuration{GitlabURL: server.URL, EncryptionKey: testEncryptionKey, EnableCodePreview: "disable"}
			p.GitlabClient = gitlab.New(server.URL, "", p.isNamespaceAllowed)
			if scenario == "disconnected" {
				api.On("KVGet", "user"+GitlabUserInfoKey).Return(nil, nil)
				api.On("KVGet", "user"+GitlabMigrationTokenKey).Return(nil, nil)
			} else {
				api.On("KVGet", "user"+GitlabUserInfoKey).Return([]byte(`{"UserID":"user"}`), nil)
				expiry := "3022-01-01T00:00:00Z"
				if scenario == "expired" {
					expiry = "2000-01-01T00:00:00Z"
				}
				encrypted, err := encrypt([]byte(testEncryptionKey), `{"access_token":"test-token","expiry":"`+expiry+`"}`)
				require.NoError(t, err)
				api.On("KVGet", "user"+GitlabUserTokenKey).Return([]byte(encrypted), nil)
			}
			post := &model.Post{UserId: "user", Message: server.URL + "/g/r/-/merge_requests/3"}
			original := post.Message
			start := time.Now()
			updated, reason := p.MessageWillBePosted(nil, post)
			require.Empty(t, reason)
			if scenario == "success" {
				require.NotNil(t, updated)
				require.Contains(t, updated.Message, "Editor title")
				updated, reason = p.MessageWillBeUpdated(nil, post, nil)
				require.Nil(t, updated)
				require.Empty(t, reason)
				post.Message += " " + original
				updated, reason = p.MessageWillBeUpdated(nil, post, nil)
				require.NotNil(t, updated)
				require.Empty(t, reason)
				require.Equal(t, 2, strings.Count(updated.Message, "Editor title"))
			} else {
				require.Nil(t, updated)
				require.Equal(t, original, post.Message)
			}
			if scenario == "timeout" {
				require.Less(t, time.Since(start), 3*time.Second)
			}
		})
	}
}
