// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

// scopedRepositorySingleResourceRoute reports whether path is a GET
// /api/v0/repositories/{repo_id}/<suffix> route, the shape shared by
// scopedRepositoryFreshnessRoute and every #5167 Group A matcher below: a
// literal suffix trailing a single non-empty {repo_id} path segment.
//
// Callers MUST pass the ESCAPED request path (r.URL.EscapedPath()), not
// r.URL.Path. An org/repo-style selector is transmitted percent-encoded
// (org%2Frepo) by the MCP dispatchers, and the slash-free guard here relies on
// that encoding surviving: on r.URL.Path the %2F is already decoded back to a
// slash, so a legitimate single-segment selector would look like two segments
// and be rejected. On the escaped path the one selector segment stays
// slash-free while a genuinely multi-segment path (a/b) still trips the guard.
func scopedRepositorySingleResourceRoute(path, suffix string) bool {
	const prefix = "/api/v0/repositories/"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	repoID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return repoID != "" && !strings.Contains(repoID, "/")
}

// scopedRepositoryFreshnessRoute reports whether the request targets the
// single-repository freshness read GET
// /api/v0/repositories/{repo_id}/freshness (#5143; PR #5150 review, codex +
// carried-forward P1). The handler (getRepositoryFreshness) resolves the
// selector through resolveRepositoryPathSelector, the same grant-filtering
// helper every other single-repository route
// (getRepositoryStats/Story/Coverage/Tree/Content/Branches) uses, so a
// repository outside the caller's grant renders not-found rather than
// leaking existence; this allowlist entry only lets that filtering run
// instead of AuthMiddleware rejecting the request with 403 first.
func scopedRepositoryFreshnessRoute(path string) bool {
	return scopedRepositorySingleResourceRoute(path, "/freshness")
}

// scopedRepositoryStatsRoute reports whether the request targets GET
// /api/v0/repositories/{repo_id}/stats (#5167 Group A). getRepositoryStats
// resolves the selector through resolveRepositoryStatsPathSelector ->
// resolveRepositorySelector -> resolveRepositorySelectorExactForAccess, the
// same grant-filtering helper scopedRepositoryFreshnessRoute already covers,
// so this only needed the allowlist entry.
func scopedRepositoryStatsRoute(path string) bool {
	return scopedRepositorySingleResourceRoute(path, "/stats")
}

// scopedRepositoryContextRoute reports whether the request targets GET
// /api/v0/repositories/{repo_id}/context (#5167 Group A). getRepositoryContext
// resolves the selector through the same resolveRepositoryPathSelector ->
// resolveRepositorySelectorExactForAccess chain as
// scopedRepositoryFreshnessRoute.
func scopedRepositoryContextRoute(path string) bool {
	return scopedRepositorySingleResourceRoute(path, "/context")
}

// scopedRepositoryStoryRoute reports whether the request targets GET
// /api/v0/repositories/{repo_id}/story (#5167 Group A). getRepositoryStory
// resolves the selector through the same resolveRepositoryPathSelector ->
// resolveRepositorySelectorExactForAccess chain as
// scopedRepositoryFreshnessRoute.
func scopedRepositoryStoryRoute(path string) bool {
	return scopedRepositorySingleResourceRoute(path, "/story")
}

// scopedRepositoryCoverageRoute reports whether the request targets GET
// /api/v0/repositories/{repo_id}/coverage (#5167 Group A).
// getRepositoryCoverage resolves the selector through
// resolveCoverageRepositoryID -> resolveRepositorySelector ->
// resolveRepositorySelectorExactForAccess, the same grant-filtering chain as
// scopedRepositoryFreshnessRoute.
func scopedRepositoryCoverageRoute(path string) bool {
	return scopedRepositorySingleResourceRoute(path, "/coverage")
}

// scopedRepositoryTreeRoute reports whether the request targets GET
// /api/v0/repositories/{repo_id}/tree (#5167 Group A). getRepositoryTree
// resolves the selector through the same resolveRepositoryPathSelector ->
// resolveRepositorySelectorExactForAccess chain as
// scopedRepositoryFreshnessRoute.
func scopedRepositoryTreeRoute(path string) bool {
	return scopedRepositorySingleResourceRoute(path, "/tree")
}
