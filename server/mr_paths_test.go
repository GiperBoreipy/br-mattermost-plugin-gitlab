// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	gitlabLib "github.com/xanzy/go-gitlab"
)

func TestLoadMergeRequestPaths(t *testing.T) {
	for _, scenario := range []string{"complete", "truncated", "overflow", "pending", "changed", "base-changed", "forbidden"} {
		t.Run(scenario, func(t *testing.T) {
			mrCalls, diffCalls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v4/projects/g/r/merge_requests/3" {
					mrCalls++
					count, sha, overflow := "3", "abc", false
					if scenario == "overflow" {
						overflow = true
						count = "1000+"
					}
					if scenario == "pending" {
						count = ""
					}
					if scenario == "changed" && mrCalls > 1 {
						sha = "new"
					}
					base := "base"
					if scenario == "base-changed" && mrCalls > 1 {
						base = "new-base"
					}
					_, _ = fmt.Fprintf(w, `{"changes_count":%q,"sha":%q,"overflow":%v,"diff_refs":{"base_sha":%q}}`, count, sha, overflow, base)
					return
				}
				diffCalls++
				if scenario == "forbidden" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				if r.URL.Query().Get("page") == "1" {
					if scenario != "truncated" {
						w.Header().Set("X-Next-Page", "2")
					}
					_, _ = w.Write([]byte(`[{"old_path":"old/name","new_path":"src/name","renamed_file":true},{"old_path":"deleted","new_path":"deleted","deleted_file":true}]`))
				} else {
					_, _ = w.Write([]byte(`[{"old_path":"src/new","new_path":"src/new","new_file":true}]`))
				}
			}))
			defer server.Close()
			client, err := gitlabLib.NewOAuthClient("token", gitlabLib.WithBaseURL(server.URL))
			require.NoError(t, err)
			paths, err := loadMergeRequestPaths(context.Background(), client, "g/r", 3)
			if scenario == "complete" {
				require.NoError(t, err)
				require.ElementsMatch(t, []string{"old/name", "src/name", "deleted", "deleted", "src/new", "src/new"}, paths)
				require.Equal(t, 2, diffCalls)
			} else {
				require.Error(t, err)
				require.Nil(t, paths)
			}
		})
	}
}
