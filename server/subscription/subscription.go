// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package subscription

import (
	"path"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

var allFeatures = map[string]bool{
	"merges":                 true,
	"jobs":                   true,
	"issues":                 true,
	"pushes":                 true,
	"issue_comments":         true,
	"merge_request_comments": true,
	"pipeline":               true,
	"tag":                    true,
	"pull_reviews":           true,
	"confidential_issues":    true,
	"deployments":            true,
	"releases":               true,
	"merge_request_assigns":  true,
	"issue_assigns":          true,
	// "label:":                 true,//particular case for label:XXX
}

type Subscription struct {
	ChannelID  string
	CreatorID  string
	Features   string
	Repository string
}

// extractLabels scans a comma-separated feature string for any tokens
// prefixed with `label:`, unquotes them, and returns all non-empty labels.
// Valid input examples include:
//
//	label:"bug"
//	label: "test label"
//	label: " with leading space"
//	label: "with trailing space "
//
// Valid input pattern
//
//	label:"bug",label: " with spaces ",label: "test label"
func extractLabels(features string) ([]string, error) {
	labels := []string{}
	for t := range strings.SplitSeq(features, ",") {
		t = strings.TrimSpace(t)
		raw, found := strings.CutPrefix(t, "label:")
		if found {
			raw = strings.TrimSpace(raw)
			unquoted, err := strconv.Unquote(raw)
			if err != nil {
				return nil, errors.New(`each label must be wrapped in quotes, e.g. label:"bug"`)
			}
			if unquoted != "" {
				labels = append(labels, unquoted)
			}
		}
	}
	return labels, nil
}

func New(channelID, creatorID, features, repository string) (*Subscription, error) {
	if _, err := extractPaths(features); err != nil {
		return nil, err
	}
	// Validate label format ― allow any number of label tokens, but each must be quoted
	if strings.Contains(features, "label:") {
		labels, err := extractLabels(features)
		if err != nil {
			return nil, err
		}
		if len(labels) > 0 && !hasFeature(features, "merges") && !hasFeature(features, "issues") && !hasFeature(features, "confidential_issues") && !hasFeature(features, "merge_request_comments") && !hasFeature(features, "merge_request_assigns") && !hasFeature(features, "issue_comments") && !hasFeature(features, "issue_assigns") {
			return nil, errors.New("label filters require 'merges' or 'issues' feature")
		}
	}

	badFeatures := make([]string, 0)
	for feature := range strings.SplitSeq(features, ",") {
		feature = strings.TrimSpace(feature)
		if _, ok := allFeatures[feature]; !strings.HasPrefix(feature, "label:") && !strings.HasPrefix(feature, "path:") && !ok {
			badFeatures = append(badFeatures, feature)
		}
	}

	if len(badFeatures) > 0 {
		return nil, errors.Errorf("unknown features %s", strings.Join(badFeatures, ","))
	}
	return &Subscription{
		ChannelID:  channelID,
		CreatorID:  creatorID,
		Features:   features,
		Repository: repository,
	}, nil
}

// Paths are kept in Features for backwards-compatible storage and command/API
// round trips. A trailing slash explicitly denotes a directory.
func extractPaths(features string) ([]string, error) {
	var paths []string
	for feature := range strings.SplitSeq(features, ",") {
		raw, found := strings.CutPrefix(strings.TrimSpace(feature), "path:")
		if !found {
			continue
		}
		value, err := strconv.Unquote(strings.TrimSpace(raw))
		if err != nil || value == "" || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\r\n\x00") || strings.Contains(value, ",") {
			return nil, errors.New(`paths must be quoted repository-relative paths, e.g. path:"src/"; commas are unsupported`)
		}
		trimmed := strings.TrimSuffix(value, "/")
		if trimmed == "." || path.Clean(trimmed) != trimmed || trimmed == ".." || strings.HasPrefix(trimmed, "../") {
			return nil, errors.New("paths must not contain empty, dot or parent segments")
		}
		paths = append(paths, value)
	}
	if len(paths) > 0 {
		for feature := range strings.SplitSeq(features, ",") {
			feature = strings.TrimSpace(feature)
			if strings.HasPrefix(feature, "label:") || strings.HasPrefix(feature, "path:") || feature == "merges" || feature == "merge_request_comments" || feature == "merge_request_assigns" {
				continue
			}
			return nil, errors.New("path filters support only merges, merge_request_comments and merge_request_assigns")
		}
		if !hasFeature(features, "merges") && !hasFeature(features, "merge_request_comments") && !hasFeature(features, "merge_request_assigns") {
			return nil, errors.New("path filters require a merge request event feature")
		}
	}
	return paths, nil
}

func (s *Subscription) Paths() ([]string, error) { return extractPaths(s.Features) }

func (s *Subscription) MatchesPaths(changed []string) (bool, error) {
	paths, err := s.Paths()
	if err != nil {
		return false, err
	}
	if len(paths) == 0 {
		return true, nil
	}
	for _, filter := range paths {
		for _, file := range changed {
			if file == filter || (strings.HasSuffix(filter, "/") && strings.HasPrefix(file, filter)) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *Subscription) Merges() bool {
	return hasFeature(s.Features, "merges")
}

func (s *Subscription) Jobs() bool {
	return hasFeature(s.Features, "jobs")
}

func (s *Subscription) Issues() bool {
	return hasFeature(s.Features, "issues") || s.ConfidentialIssues()
}

func (s *Subscription) ConfidentialIssues() bool {
	return hasFeature(s.Features, "confidential_issues")
}

func (s *Subscription) Pushes() bool {
	return hasFeature(s.Features, "pushes")
}

func (s *Subscription) IssueComments() bool {
	return hasFeature(s.Features, "issue_comments")
}

func (s *Subscription) MergeRequestComments() bool {
	return hasFeature(s.Features, "merge_request_comments")
}

func (s *Subscription) Pipeline() bool {
	return hasFeature(s.Features, "pipeline")
}

func (s *Subscription) Tag() bool {
	return hasFeature(s.Features, "tag")
}

func (s *Subscription) PullReviews() bool {
	return hasFeature(s.Features, "pull_reviews")
}

func (s *Subscription) Labels() ([]string, error) {
	labels, err := extractLabels(s.Features)
	return labels, err
}

func (s *Subscription) Releases() bool {
	return hasFeature(s.Features, "releases")
}

func (s *Subscription) Deployments() bool {
	return hasFeature(s.Features, "deployments")
}

func (s *Subscription) MergeRequestAssigns() bool {
	return hasFeature(s.Features, "merge_request_assigns")
}

func (s *Subscription) IssueAssigns() bool {
	return hasFeature(s.Features, "issue_assigns")
}

func hasFeature(features, wanted string) bool {
	for feature := range strings.SplitSeq(features, ",") {
		if strings.TrimSpace(feature) == wanted {
			return true
		}
	}
	return false
}
