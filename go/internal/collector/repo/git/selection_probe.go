// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
)

// probeHTTPTimeout bounds one upstream classification probe. The probe runs
// at most ProbeCapPerCycle sequential requests per collector cycle, so the
// timeout, not a retry budget, bounds the added cycle cost.
const probeHTTPTimeout = 15 * time.Second

// githubUpstreamProbe returns the membership.ProbeFunc that classifies one
// Confirmed not_listed scope against the GitHub REST API with the listing
// credential (#8454): by stored GitHub id via GET /repositories/{id}, with
// the /repos/{slug} fallback when the row carries no id. A 200 decodes the
// repository's current full_name for membership.Classify; a 404 is
// not_found; any other status, or a transport or decode failure, is
// inconclusive and keeps the prior verdict. Exactly one request per target:
// the probe is advisory and the re-probe gate retries it in a later window,
// so no retry budget may stretch the collector cycle. A nil client gets the
// listing's timeout. The token travels only in the Authorization header,
// never in an error, a log, or the probe result.
func githubUpstreamProbe(client *http.Client, baseURL, token string) membership.ProbeFunc {
	if client == nil {
		client = &http.Client{Timeout: probeHTTPTimeout}
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return func(ctx context.Context, target membership.ProbeTarget) (membership.ProbeResult, error) {
		path := "/repos/" + escapeProbeSlug(target.Slug)
		if target.GitHubRepoID != 0 {
			path = "/repositories/" + strconv.FormatInt(target.GitHubRepoID, 10)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
		if err != nil {
			return membership.ProbeResult{}, fmt.Errorf("probe upstream repository: %w", err)
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		request.Header.Set("User-Agent", "eshu-go")
		response, err := client.Do(request)
		if err != nil {
			return membership.ProbeResult{}, fmt.Errorf("probe upstream repository: %w", err)
		}
		defer func() { _ = response.Body.Close() }()
		switch response.StatusCode {
		case http.StatusOK:
			var payload struct {
				FullName string `json:"full_name"`
			}
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				return membership.ProbeResult{}, fmt.Errorf("decode upstream repository: %w", err)
			}
			return membership.ProbeResult{Found: true, FullName: payload.FullName}, nil
		case http.StatusNotFound:
			return membership.ProbeResult{NotFound: true}, nil
		default:
			return membership.ProbeResult{}, fmt.Errorf("probe upstream repository: status %d", response.StatusCode)
		}
	}
}

// escapeProbeSlug escapes a scope slug for the /repos path without escaping
// its owner/name separator.
func escapeProbeSlug(slug string) string {
	segments := strings.Split(strings.TrimSpace(slug), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

// upstreamProbeForConfig binds the listing credential to the probe for a
// githubOrg observation request. A blank token disables probing: without a
// credential no upstream answer is trustworthy.
func upstreamProbeForConfig(config RepoSyncConfig, token string) membership.ProbeFunc {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	baseURL, _ := config.GitHubAPI.Resolve()
	return githubUpstreamProbe(nil, baseURL, token)
}

// observeGitHubOrgSelection evaluates one githubOrg discovery result and
// probes the Confirmed not_listed rows with the listing credential (#8454).
// Only shard 0 reaches it: observeSelection filters earlier, because every
// shard sees the same selection and N writers would race on the same
// observation rows.
func observeGitHubOrgSelection(s NativeRepositorySelector, ctx context.Context, discovered RepositorySelection, observedAt time.Time, token string) {
	request := githubOrgSelectionRequest(s.Config, discovered, observedAt)
	request.Probe = upstreamProbeForConfig(s.Config, token)
	s.SelectionObserver.Observe(ctx, request)
}
