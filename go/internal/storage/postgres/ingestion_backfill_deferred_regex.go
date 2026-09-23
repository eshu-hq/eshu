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
// row for that partition instead of skipping the fast arm. When ok is false the
// caller passes $5 to the query as SQL NULL rather than building this regex; the
// query keeps its `own_repo_id = $6 AND $5 IS NOT NULL AND payload_lower ~ $5`
// clause, but the `$5 IS NOT NULL` guard is false, so the fast arm never fires
// and every row for the partition falls through to the EXISTS fallback arm.
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
