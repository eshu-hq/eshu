// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// candidateLockedBoundary is the byte sequence in claimReducerWorkBatchQuery
// that closes the read-only `candidate` CTE and opens the data-modifying
// `locked` CTE (FOR UPDATE OF ... SKIP LOCKED). deriveReadOnlyCandidateSelect
// truncates the shipped query here.
const candidateLockedBoundary = "),\nlocked AS ("

// readOnlyCandidateTail replaces the shipped query's `locked`/`claimed`
// data-modifying tail with a side-effect-free SELECT over the `candidate` CTE.
// It reproduces the `locked` CTE's deterministic ORDER BY and LIMIT $8 verbatim
// (so the projected candidate set and its order match exactly what the real
// claim would lock) and still consumes $3/$4 — which belong only to the omitted
// `claimed` UPDATE — so the parameter list is identical to the live query.
const readOnlyCandidateTail = `
SELECT work_item_id
FROM candidate
WHERE ($3::text IS NULL OR TRUE) AND ($4::timestamptz IS NULL OR TRUE)
ORDER BY reducer_domain_priority ASC, reducer_source_inflight_count ASC, reducer_source_fair_rank ASC, reducer_domain_fair_rank ASC, updated_at ASC, work_item_id ASC
LIMIT $8
`

// deriveReadOnlyCandidateSelect turns the shipped batch-claim query into a
// read-only candidate SELECT by truncating it at the end of the `candidate` CTE
// and appending readOnlyCandidateTail. Deriving from the production string —
// instead of freezing a hand-maintained copy — guarantees the "new" side of the
// #3624 rank-once differential is byte-identical to what ships through the
// candidate CTE, so the shipped query cannot silently drift out from under the
// 0/0 proof. ok is false when the boundary marker is absent (the query was
// restructured); callers assert on it so the drift fails loudly and hermetically
// instead of the differential silently validating a stale query shape.
func deriveReadOnlyCandidateSelect(full string) (query string, ok bool) {
	idx := strings.Index(full, candidateLockedBoundary)
	if idx < 0 {
		return "", false
	}
	// full[:idx] ends just before the `)` that closes the candidate CTE (the
	// boundary marker consumes it); re-add that `)` to close the CTE, then graft
	// the read-only tail in place of the locked/claimed CTEs.
	return full[:idx] + ")" + readOnlyCandidateTail, true
}

// newReducerBatchCandidateSelectQuery is the "new" side of the #3624 rank-once
// differential (TestClaimBatchRankOnceRewriteMatchesPreRewriteCandidateSetAndOrder),
// derived directly from the shipped claimReducerWorkBatchQuery so it always
// reflects the query that actually runs. newReducerBatchCandidateDerivable
// records whether the derivation found its boundary marker.
var newReducerBatchCandidateSelectQuery, newReducerBatchCandidateDerivable = deriveReadOnlyCandidateSelect(claimReducerWorkBatchQuery)

// TestNewCandidateSelectDerivedFromShippedQuery is the hermetic (no-DSN) guard
// that the #3624 differential's "new" side is the query that actually ships.
// Without this, the DSN-gated differential can silently validate a stale,
// non-shipping candidate shape (the exact false-green a hand-frozen copy caused
// during review). It asserts the derived query is a byte-prefix of the shipped
// query through the candidate CTE, fences on the rank-once representative, does
// not reintroduce the removed correlated same-representative subquery (the
// O(N^2) source #3624 eliminated), and carries no data-modifying tail.
func TestNewCandidateSelectDerivedFromShippedQuery(t *testing.T) {
	t.Parallel()

	if !newReducerBatchCandidateDerivable {
		t.Fatalf("could not derive read-only candidate SELECT from claimReducerWorkBatchQuery: "+
			"boundary %q not found — the shipped query was restructured; update "+
			"candidateLockedBoundary and re-verify the #3624 differential", candidateLockedBoundary)
	}

	idx := strings.Index(claimReducerWorkBatchQuery, candidateLockedBoundary)
	shippedPrefix := claimReducerWorkBatchQuery[:idx] + ")"
	if !strings.HasPrefix(newReducerBatchCandidateSelectQuery, shippedPrefix) {
		t.Fatalf("derived candidate SELECT is not a byte-prefix of the shipped query through the "+
			"candidate CTE:\n--- derived ---\n%s", newReducerBatchCandidateSelectQuery)
	}

	if !strings.Contains(newReducerBatchCandidateSelectQuery, "WHERE reps.same_rn = 1") {
		t.Fatalf("derived candidate SELECT missing the rank-once representative fence "+
			"(reps.same_rn = 1):\n%s", newReducerBatchCandidateSelectQuery)
	}

	// The removed correlated same-representative subquery must not creep back.
	// Ban the CTE at line start ("\nsame AS (") rather than the bare "same AS ("
	// substring, which also occurs inside the w_fairsame window definition.
	for _, banned := range []string{"\nsame AS (", "FROM same\n", "SELECT same.work_item_id"} {
		if strings.Contains(newReducerBatchCandidateSelectQuery, banned) {
			t.Fatalf("derived candidate SELECT reintroduced the removed correlated "+
				"same-representative construct %q (the #3624 O(N^2) source)", banned)
		}
	}

	// The derivation must strip the claim's data-modifying tail: the
	// FOR UPDATE OF ... SKIP LOCKED row lock, the `claimed` UPDATE that sets the
	// lease (UPDATE fact_work_items AS work), and the final SELECT FROM claimed.
	// (The `superseded_stale_reducer_generations` CTE's UPDATE ... AS stale is a
	// deliberate part of the query — it also runs in the pre-rewrite OLD
	// reference — so it is NOT banned here; only the claim tail is.)
	for _, banned := range []string{"FOR UPDATE OF lock_target", "UPDATE fact_work_items AS work", "FROM claimed"} {
		if strings.Contains(newReducerBatchCandidateSelectQuery, banned) {
			t.Fatalf("derived candidate SELECT must omit the claim tail but contains %q", banned)
		}
	}
}
