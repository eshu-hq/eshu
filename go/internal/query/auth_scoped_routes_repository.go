// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

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
	const (
		prefix = "/api/v0/repositories/"
		suffix = "/freshness"
	)
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	repoID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return repoID != "" && !strings.Contains(repoID, "/")
}
