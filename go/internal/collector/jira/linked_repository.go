// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// linkedRepositoryID resolves a typed PR/MR external-link URL to a canonical
// repository identifier before the raw URL is redacted.
//
// It returns the canonical id only when the link is a confidently typed
// pull-request or merge-request anchor whose URL canonicalizes to a known
// repository shape (the owner/repo or group/subgroup/repo prefix). The returned
// id is the same generation-independent identifier Eshu already stores for every
// repository (see repositoryidentity.CanonicalRepositoryID); it embeds no raw
// URL, query parameters, credentials, or user identity.
//
// The empty string is returned for any non-repository, un-canonicalizable, or
// ambiguous link. Callers MUST NOT persist a guessed id: an empty result means
// "no durable link", never "resolve later".
func linkedRepositoryID(link ExternalLink) string {
	anchorClass := externalLinkAnchorClass(link)
	switch anchorClass {
	case "github_pull_request", "gitlab_merge_request":
	default:
		return ""
	}

	repoRemote := repositoryRemoteFromLinkURL(anchorClass, link.Object.URL)
	if repoRemote == "" {
		return ""
	}

	repoID, err := repositoryidentity.CanonicalRepositoryID(repoRemote, "")
	if err != nil {
		return ""
	}
	return repoID
}

// repositoryRemoteFromLinkURL trims a typed PR/MR URL down to the repository
// remote URL so it can be canonicalized.
//
// GitHub pull requests use `<host>/<owner>/<repo>/pull/<n>`; GitLab merge
// requests use `<host>/<group>[/<subgroup>...]/<repo>/-/merge_requests/<n>`.
// The path segment before the anchor marker is the repository remote. An empty
// string is returned when the URL is malformed or does not contain the expected
// owner/repo (GitHub) or group/repo (GitLab) shape, so the caller persists no
// guessed link.
func repositoryRemoteFromLinkURL(anchorClass, rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return ""
	}
	lowerPath := strings.ToLower(path)

	var repoPath string
	switch anchorClass {
	case "github_pull_request":
		idx := strings.Index(lowerPath, "/pull/")
		if idx <= 0 {
			return ""
		}
		repoPath = path[:idx]
		// GitHub repository paths are exactly owner/repo. A deeper or shallower
		// prefix is an unexpected shape; persist no guess.
		if strings.Count(repoPath, "/") != 1 {
			return ""
		}
	case "gitlab_merge_request":
		idx := strings.Index(lowerPath, "/-/merge_requests/")
		if idx <= 0 {
			return ""
		}
		repoPath = path[:idx]
		// GitLab projects are group/[subgroups/]repo: at least one slash.
		if !strings.Contains(repoPath, "/") {
			return ""
		}
	default:
		return ""
	}

	return parsed.Scheme + "://" + parsed.Host + "/" + repoPath
}
