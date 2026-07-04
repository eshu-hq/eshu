// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
)

// buildDeferredRepoIDRegex builds the $5 POSIX ARE (Postgres `~` operator)
// alternation used by the fast arm of listDeferredScopedRelationshipFactRecordsQuery
// (issue #3624 payload-hoist rewrite). It returns every value in repoIDValues
// EXCEPT ownRepoID (case-insensitive, both sides already expected lowercase),
// each escaped so every ARE metacharacter is a literal, joined into a single
// non-capturing alternation: (?:v1|v2|...).
//
// ownRepoID is a PERFORMANCE HINT, not a correctness input (see the query's doc
// comment): the fast arm only fires for rows whose per-row own_repo_id equals
// $6, so excluding the wrong value here only costs a fallback-arm evaluation for
// mismatched rows, never a wrong result.
//
// ok is false when the resulting alternation would match every string: an empty
// exclusion list produces the empty alternation "(?:)", which the ARE engine
// treats as a zero-width match present in EVERY string (verified directly
// against Postgres 18: `SELECT 'x' ~ '(?:)'` returns true). Building that regex
// would turn the fast arm into "own_repo_id = $6 AND true", over-selecting every
// row for that partition instead of skipping the fast arm. Callers MUST omit
// the `~ $5` fast-arm clause entirely when ok is false and rely solely on the
// EXISTS fallback arm.
func buildDeferredRepoIDRegex(repoIDValues []string, ownRepoID string) (string, bool) {
	ownRepoID = strings.ToLower(strings.TrimSpace(ownRepoID))

	seen := make(map[string]struct{}, len(repoIDValues))
	escaped := make([]string, 0, len(repoIDValues))
	for _, value := range repoIDValues {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || value == ownRepoID {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		escaped = append(escaped, regexp.QuoteMeta(value))
	}

	if len(escaped) == 0 {
		return "", false
	}
	return "(?:" + strings.Join(escaped, "|") + ")", true
}

// deferredScopedFactOwnRepoIDFromScope derives the $6 performance-hint value
// from a (scope_id, generation_id) partition's scope_id, for the fast arm of
// listDeferredScopedRelationshipFactRecordsQuery.
//
// git-repository-scope:<repo_id> scopes (git_source_processing.go) resolve to
// their lowercased repo_id; every other scope shape (gcp cloud-relationship
// scopes, any future scope kind) resolves to "". Both outcomes are safe: this
// value only steers the fast arm (own_repo_id = $6), and a row whose per-row
// own_repo_id does not equal $6 always falls through to the EXISTS fallback
// arm, which is exactly today's per-row self-exclusion behavior. A wrong or
// empty $6 can only cost fast-arm coverage, never correctness (see the query's
// doc comment for the full graceful-degradation argument).
//
// This is a pure string derivation, not a discovery query: it does not call
// loadActiveRepositoryGenerations (which filters to fact_kind = 'repository'
// and drops every GCP cloud-relationship scope) and does not touch
// loadActiveScopeGenerationPartitions or scopeGenerationPartition.
const gitRepositoryScopePrefix = "git-repository-scope:"

func deferredScopedFactOwnRepoIDFromScope(scopeID string) string {
	scopeID = strings.TrimSpace(scopeID)
	if !strings.HasPrefix(scopeID, gitRepositoryScopePrefix) {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(scopeID, gitRepositoryScopePrefix)))
}
