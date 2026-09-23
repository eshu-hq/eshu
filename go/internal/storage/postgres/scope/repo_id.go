// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopestore

import "strings"

// gitRepositoryScopePrefix is the scope_id prefix the git collector writes for
// repository scopes (go/internal/collector/repo/git/source_processing.go).
const gitRepositoryScopePrefix = "git-repository-scope:"

// RepoIDFromScopeID derives the own-repo_id performance hint ($6) from a
// (scope_id, generation_id) partition's scope_id, for the fast arm of the
// parent package's listDeferredScopedRelationshipFactRecordsQuery.
//
// git-repository-scope:<repo_id> scopes resolve to their lowercased repo_id;
// every other scope shape (gcp cloud-relationship scopes, any future scope
// kind) resolves to "". Both outcomes are safe: this value only steers the fast
// arm (own_repo_id = $6), and a row whose per-row own_repo_id does not equal $6
// always falls through to the EXISTS fallback arm, which is exactly the
// per-row self-exclusion behavior. A wrong or empty $6 can only cost fast-arm
// coverage, never correctness (see the query's doc comment for the full
// graceful-degradation argument).
//
// This is a pure string derivation, not a discovery query: it does not call
// loadActiveRepositoryGenerations (which filters to fact_kind = 'repository'
// and drops every GCP cloud-relationship scope) and does not touch
// loadActiveScopeGenerationPartitions or scopeGenerationPartition.
func RepoIDFromScopeID(scopeID string) string {
	scopeID = strings.TrimSpace(scopeID)
	if !strings.HasPrefix(scopeID, gitRepositoryScopePrefix) {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(scopeID, gitRepositoryScopePrefix)))
}
