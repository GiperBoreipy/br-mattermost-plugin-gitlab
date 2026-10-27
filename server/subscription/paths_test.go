// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package subscription

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaths(t *testing.T) {
	sub, err := New("channel", "creator", `merges,merge_request_comments,path:"src/",path:"config.json"`, "group/repo")
	require.NoError(t, err)
	for _, file := range []string{"src/file.go", "src/nested/file.go", "config.json"} {
		ok, matchErr := sub.MatchesPaths([]string{file})
		require.NoError(t, matchErr)
		require.True(t, ok, file)
	}
	for _, file := range []string{"src-old/file.go", "config.json.bak", "src", "nested/config.json"} {
		ok, matchErr := sub.MatchesPaths([]string{file})
		require.NoError(t, matchErr)
		require.False(t, ok, file)
	}
	for _, value := range []string{`path:"/etc"`, `path:"../src/"`, `path:"src/../x"`, `path:"./src"`, `path:""`, `path:"src//x"`, `path:src`, `path:"src\\x"`, `path:"a,b"`} {
		_, validationErr := New("c", "u", "merges,"+value, "g/r")
		require.Error(t, validationErr, value)
	}
	var old Subscription
	require.NoError(t, json.Unmarshal([]byte(`{"ChannelID":"c","Features":"merges"}`), &old))
	ok, err := old.MatchesPaths(nil)
	require.NoError(t, err)
	require.True(t, ok)
	data, err := json.Marshal(sub)
	require.NoError(t, err)
	var restored Subscription
	require.NoError(t, json.Unmarshal(data, &restored))
	require.Equal(t, sub.Features, restored.Features)
	commentsOnly, err := New("c", "u", `merge_request_comments,path:"merges/issues/pipeline/"`, "g/r")
	require.NoError(t, err)
	require.False(t, commentsOnly.Merges())
	require.False(t, commentsOnly.Issues())
	require.False(t, commentsOnly.Pipeline())
}
