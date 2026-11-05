// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strconv"
	"strings"
)

// Fragments deriveDeltaOverlayBaseQuery swaps in every fact branch of an
// active reader text. The base branch keeps the active text's filters, keyset
// cursor, ordering, and limit, so it is driven by the same indexes as the
// active read. Its generation predicate is a strict superset of
// deltaUnionBaseQuery: every activated full generation of a delta-active
// scope at the delta's union-base commit (COALESCE union base, baseline). It
// is limited to the delta-active scope ids bound at the reader's placeholder,
// so a read never scans base rows of a scope it will not overlay.
// resolveDeltaBases then picks the one base the projector overlays, so the
// base rule keeps one definition. Deriving the base from the active text keeps
// both in step when a reader's active text changes (#9611 for the manifest
// reader).
const (
	deltaOverlayActiveScopeJoin  = "AND scope.active_generation_id = fact.generation_id"
	deltaOverlayBaseJoin         = "AND scope.active_generation_id <> fact.generation_id\nJOIN scope_generations AS active_delta\n  ON active_delta.scope_id = scope.scope_id\n AND active_delta.generation_id = scope.active_generation_id"
	deltaOverlayActiveGeneration = "AND generation.status = 'active'"
	deltaOverlayBaseFullPrefix   = "AND generation.is_delta = false\n  AND generation.activated_at IS NOT NULL\n  AND generation.source_commit_sha = COALESCE(NULLIF(TRIM(active_delta.delta_union_base_commit_sha), ''), NULLIF(TRIM(active_delta.delta_baseline_commit_sha), ''))\n  AND active_delta.is_delta = true\n  AND active_delta.status = 'active'\n  AND fact.scope_id = ANY($"
	// deltaOverlayFactBranch marks one fact branch, FROM- or JOIN-shaped.
	deltaOverlayFactBranch = "fact_records AS fact"
)

// deltaOverlayBaseFull returns the base generation predicate with the
// delta-active scope ids bound at placeholder $scopeParam.
func deltaOverlayBaseFull(scopeParam int) string {
	return deltaOverlayBaseFullPrefix + strconv.Itoa(scopeParam) + "::text[])"
}

// deriveDeltaOverlayBaseQuery derives the base-candidate query from one
// active reader text, binding the delta-active scope ids at $scopeParam. A
// fact branch is each fact_records AS fact, whether it follows FROM or JOIN.
// It fails closed unless every branch carries exactly one active scope join
// and one active generation predicate, $scopeParam is unused in the active
// text, and the text does not already use the active_delta alias.
func deriveDeltaOverlayBaseQuery(active string, scopeParam int) (string, error) {
	branches := countDeltaOverlayFactBranches(active)
	switch {
	case branches == 0,
		strings.Count(active, deltaOverlayActiveScopeJoin) != branches,
		strings.Count(active, deltaOverlayActiveGeneration) != branches:
		return "", fmt.Errorf("delta overlay base query shape is unsupported: %d fact branches", branches)
	case scopeParam < 1:
		return "", fmt.Errorf("delta overlay base query scope placeholder $%d is invalid", scopeParam)
	case placeholderUsed(active, scopeParam):
		return "", fmt.Errorf("delta overlay base query scope placeholder $%d is already bound by the active text", scopeParam)
	case strings.Contains(active, "active_delta"):
		return "", fmt.Errorf("delta overlay base query shape is unsupported: the active text already uses the active_delta alias")
	}
	base := strings.ReplaceAll(active, deltaOverlayActiveScopeJoin, deltaOverlayBaseJoin)
	return strings.ReplaceAll(base, deltaOverlayActiveGeneration, deltaOverlayBaseFull(scopeParam)), nil
}

// countDeltaOverlayFactBranches counts fact_records AS fact as a whole token:
// fact_records AS fact_row or AS facts is another alias, not a fact branch.
func countDeltaOverlayFactBranches(text string) int {
	count := 0
	for rest := text; ; {
		at := strings.Index(rest, deltaOverlayFactBranch)
		if at < 0 {
			return count
		}
		end := at + len(deltaOverlayFactBranch)
		before := at == 0 || !isSQLIdentifierByte(rest[at-1])
		after := end == len(rest) || !isSQLIdentifierByte(rest[end])
		if before && after {
			count++
		}
		rest = rest[end:]
	}
}

// placeholderUsed reports whether text binds $n: $n not followed by another
// digit, so $1 is not found inside $10.
func placeholderUsed(text string, n int) bool {
	token := "$" + strconv.Itoa(n)
	for rest := text; ; {
		at := strings.Index(rest, token)
		if at < 0 {
			return false
		}
		end := at + len(token)
		if end == len(rest) || rest[end] < '0' || rest[end] > '9' {
			return true
		}
		rest = rest[end:]
	}
}

// isSQLIdentifierByte reports whether b can continue an unquoted SQL
// identifier.
func isSQLIdentifierByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// deltaOverlayTargetsQuery returns every scope whose active generation is a
// delta, with the raw baseline and union-base commits that
// deltaUnionCommitsFor resolves. It is the reader-side target lookup: a
// reader reads all active deltas once per call, before any base scan,
// because a scope whose delta touched none of the read's rows has base rows
// but no active row, so the read cannot learn its delta-active scopes from
// the active rows alone. The projector's target is the claimed generation by
// primary key instead (deltaUnionTargetQuery). With no delta-active scope the
// read skips the base scan entirely.
const deltaOverlayTargetsQuery = `
SELECT scope.scope_id,
       active.generation_id,
       COALESCE(active.delta_baseline_commit_sha, ''),
       COALESCE(active.delta_union_base_commit_sha, '')
FROM ingestion_scopes AS scope
JOIN scope_generations AS active
  ON active.scope_id = scope.scope_id
 AND active.generation_id = scope.active_generation_id
WHERE active.is_delta = true
  AND active.status = 'active'
ORDER BY scope.scope_id
`

// deltaOverlayInputsQuery reads, for each (scope, delta generation) pair,
// the rows facts.OverlayDeltaBase needs from the delta: its repository fact
// (the changed and deleted paths) and every row, tombstones included, whose
// stable key collides with a base candidate. The active reads never return
// tombstones, so they must come from here. It probes the (scope_id,
// generation_id) prefix of fact_records and reads no payload of non-matching
// rows. Its stable_fact_key = ANY($3) is a residual Filter over each delta's
// rows, the #9586 trap: it runs with deltaOverlayQueryExecMode so every
// execution gets a custom plan with a hashed Const array. On the #9991 corpus
// the forced generic plan took 1,077.8 ms against 14.9 ms custom, same plan
// and buffers.
const deltaOverlayInputsQuery = `
SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind,
       fact.stable_fact_key, fact.schema_version, fact.collector_kind,
       fact.fencing_token, fact.source_confidence, fact.source_system,
       fact.source_fact_key, COALESCE(fact.source_uri, ''),
       COALESCE(fact.source_record_id, ''), fact.observed_at,
       fact.is_tombstone, fact.payload
FROM unnest($1::text[], $2::text[]) AS target(scope_id, generation_id)
JOIN fact_records AS fact
  ON fact.scope_id = target.scope_id
 AND fact.generation_id = target.generation_id
WHERE fact.fact_kind IN ('repository', 'repositoryFact')
   OR fact.stable_fact_key = ANY($3::text[])
`
