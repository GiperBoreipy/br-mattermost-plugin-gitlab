// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"fmt"
	"strconv"

	gitlabLib "github.com/xanzy/go-gitlab"
	"golang.org/x/oauth2"

	"github.com/mattermost/mattermost-plugin-gitlab/server/gitlab"
)

func (g *gitlabRetreiver) GetMergeRequestPaths(ctx context.Context, project string, iid int, creator string) ([]string, error) {
	if err := g.p.isNamespaceAllowed(project); err != nil {
		return nil, err
	}
	info, apiErr := g.p.getGitlabUserInfoByMattermostID(creator)
	if apiErr != nil {
		return nil, apiErr
	}
	var paths []string
	err := g.p.useGitlabClient(info, func(_ *gitlab.UserInfo, token *oauth2.Token) error {
		client, err := g.p.GitlabClient.GitlabConnect(*token)
		if err != nil {
			return err
		}
		paths, err = loadMergeRequestPaths(ctx, client, project, iid)
		return err
	})
	return paths, err
}

func loadMergeRequestPaths(ctx context.Context, client *gitlabLib.Client, project string, iid int) ([]string, error) {
	mr, _, err := client.MergeRequests.GetMergeRequest(project, iid, nil, gitlabLib.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	expected, err := strconv.Atoi(mr.ChangesCount)
	if err != nil || mr.Overflow {
		return nil, fmt.Errorf("incomplete MR changes count: %q", mr.ChangesCount)
	}
	var paths []string
	count := 0
	for page := 1; ; {
		diffs, resp, diffErr := client.MergeRequests.ListMergeRequestDiffs(project, iid, &gitlabLib.ListMergeRequestDiffsOptions{ListOptions: gitlabLib.ListOptions{Page: page, PerPage: 100}}, gitlabLib.WithContext(ctx))
		if diffErr != nil {
			return nil, diffErr
		}
		for _, diff := range diffs {
			if diff == nil || (diff.OldPath == "" && diff.NewPath == "") {
				return nil, fmt.Errorf("incomplete MR diff entry")
			}
			paths = append(paths, diff.OldPath, diff.NewPath)
		}
		count += len(diffs)
		if resp == nil {
			return nil, fmt.Errorf("missing diff pagination metadata")
		}
		if resp.NextPage == 0 {
			break
		}
		if resp.NextPage <= page {
			return nil, fmt.Errorf("invalid diff pagination")
		}
		page = resp.NextPage
	}
	if count != expected {
		return nil, fmt.Errorf("incomplete MR diff: received %d of %d files", count, expected)
	}
	latest, _, err := client.MergeRequests.GetMergeRequest(project, iid, nil, gitlabLib.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if latest.Overflow || latest.SHA != mr.SHA || latest.DiffRefs != mr.DiffRefs || latest.ChangesCount != mr.ChangesCount {
		return nil, fmt.Errorf("MR changed while reading diffs")
	}
	return paths, nil
}
