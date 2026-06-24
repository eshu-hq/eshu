// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"strings"
)

// hostedHealthzPath is the deployed liveness probe path served by the runtime
// admin mux.
const hostedHealthzPath = "/healthz"

// hostedReadyzPath is the deployed readiness probe path served by the runtime
// admin mux. A 401/403 here is treated as an authentication failure.
const hostedReadyzPath = "/readyz"

// hostedStatusPath is the bounded pipeline status read used to classify the
// indexed state of the deployed service.
const hostedStatusPath = "/api/v0/status/pipeline"

// hostedReposPath is the bounded repositories query used both to enumerate the
// indexed scope and as the final useful-answer proof.
const hostedReposPath = "/api/v0/repositories?limit=25"

// hostedProbe issues a GET against path and returns nil when the deployed
// service answers without an error status. It is the default health/ready seam;
// tests inject deterministic outcomes instead.
func hostedProbe(path string) func(*APIClient) error {
	return func(client *APIClient) error {
		if err := client.Get(path, nil); err != nil {
			return fmt.Errorf("%s probe %s failed: %w", path, client.BaseURL, err)
		}
		return nil
	}
}

// hostedFetchStatus reads the bounded pipeline status from the deployed service.
func hostedFetchStatus(client *APIClient) (scanPipelineStatus, error) {
	var status scanPipelineStatus
	if err := client.Get(hostedStatusPath, &status); err != nil {
		return scanPipelineStatus{}, err
	}
	return status, nil
}

// hostedListRepositories runs the bounded repositories query against the
// deployed service.
func hostedListRepositories(client *APIClient) (repositoryListResponse, error) {
	var response repositoryListResponse
	if err := client.Get(hostedReposPath, &response); err != nil {
		return repositoryListResponse{}, err
	}
	return response, nil
}

// classifyProbeError maps a probe error to a hosted failure category. A 401/403
// is auth-unavailable; anything else is unreachable. This keeps the
// authentication failure distinct from a transport or service-down failure so
// the operator sees the right remediation.
func classifyProbeError(err error) hostedFailCategory {
	if err == nil {
		return hostedFailNone
	}
	var httpErr *apiHTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == 401 || httpErr.StatusCode == 403 {
			return hostedFailAuthUnavailable
		}
	}
	return hostedFailUnreachable
}

// classifyReadError maps a bounded-read error to a hosted failure category. A
// 401/403 stays auth-unavailable; any other error is query-failed, because the
// failing call is a query against the deployed read surface rather than a bare
// reachability probe.
func classifyReadError(err error) hostedFailCategory {
	if cat := classifyProbeError(err); cat == hostedFailAuthUnavailable {
		return cat
	}
	if err == nil {
		return hostedFailNone
	}
	return hostedFailQueryFailed
}

// classifyIndexReadiness inspects the deployed pipeline status and the indexed
// repository count, returning the specific readiness category, a human detail,
// and whether the index is fully ready. The categories never collapse: an empty
// index, a building/partial pipeline, and a stale/degraded pipeline are each
// reported distinctly. It reuses evaluateScanReadiness for the terminal/stale
// verdict so the hosted flow agrees with the local readiness contract.
func classifyIndexReadiness(status scanPipelineStatus, repoCount int) (hostedFailCategory, string, bool) {
	verdict := evaluateScanReadiness(status)

	// A terminal verdict means failed, dead-letter, degraded, or stalled work:
	// the indexed truth is not trustworthy, so report stale-readiness.
	if verdict.Terminal {
		reason := strings.TrimSpace(verdict.Reason)
		if reason == "" {
			reason = "pipeline reported a terminal unhealthy state"
		}
		return hostedFailStaleReadiness, reason, false
	}

	if verdict.Ready {
		if repoCount == 0 {
			return hostedFailEmptyIndex, "pipeline healthy but no repository is indexed", false
		}
		return hostedFailNone, fmt.Sprintf("pipeline healthy and drained; %d repositories indexed", repoCount), true
	}

	// Not ready and not terminal: the index is still building or draining.
	reason := strings.TrimSpace(verdict.Reason)
	if reason == "" {
		reason = "pipeline is still building the index"
	}
	if repoCount == 0 {
		return hostedFailEmptyIndex, "no repository indexed yet; " + reason, false
	}
	return hostedFailPartialReadiness, reason, false
}

// hostedTokenReference returns a display-safe reference for the resolved bearer
// token, never the raw value. It reuses the #1767 tokenReference/redactToken
// helpers: when a platform supports env-var references it returns
// ${ESHU_API_KEY}; otherwise it returns a masked placeholder so the operator can
// recognize which key is configured without exposing it.
func hostedTokenReference(platform, apiKey string) string {
	if strings.TrimSpace(apiKey) == "" {
		return ""
	}
	if p, err := resolvePlatform(platform); err == nil {
		return tokenReference(p, apiKey)
	}
	return redactToken(apiKey)
}

// hostedSetupHint renders a hosted MCP setup snippet for the requested platform
// by delegating to the #1767 renderSetupSnippet helper. It returns an empty
// string when no platform was requested or the platform is unknown. The snippet
// references the ESHU_API_KEY env var and never embeds the raw token.
func hostedSetupHint(platform string, client *APIClient) string {
	if strings.TrimSpace(platform) == "" {
		return ""
	}
	p, err := resolvePlatform(platform)
	if err != nil {
		return ""
	}
	req := mcpSetupRequest{
		Mode:       modeHostedHTTP,
		ServiceURL: client.BaseURL,
		APIKey:     client.APIKey,
	}
	hint, err := renderSetupSnippet(p, req)
	if err != nil {
		return ""
	}
	return hint
}

// repositoryScopePresent reports whether the requested repository selector
// matches any indexed repository the token can see. An empty selector means no
// scope was requested, which is always satisfied.
func repositoryScopePresent(repos repositoryListResponse, selector string) bool {
	target := strings.TrimSpace(selector)
	if target == "" {
		return true
	}
	for _, repo := range repos.Repositories {
		if repositorySelectorMatches(repo, target) {
			return true
		}
	}
	return false
}
