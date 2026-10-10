// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// writeGateLockBoundary ends the locked_generation CTE's predicate in
// supersedeRunningProjectorWorkQuery. Everything the tests derive from the
// shipped constant is cut at this marker, never hand-copied.
const writeGateLockBoundary = "    FOR NO KEY UPDATE OF generation SKIP LOCKED\n"

// derivedWriteGateProbeQuery is the heartbeat supersede's gate, derived from
// the shipped constant: its locked_scope, locked_work and locked_generation
// CTEs up to the generation row lock, followed by a read that counts the rows
// the gate selects. It still takes the scope and work rows with SKIP LOCKED,
// so callers run it inside a transaction they roll back, with the shipped
// statement's $1-$5. It returns 1 when the heartbeat would supersede and 0
// when it would not.
func derivedWriteGateProbeQuery(t *testing.T) string {
	t.Helper()
	cut := strings.Index(supersedeRunningProjectorWorkQuery, writeGateLockBoundary)
	if cut < 0 {
		t.Fatalf("supersedeRunningProjectorWorkQuery has no %q boundary", strings.TrimSpace(writeGateLockBoundary))
	}
	// $1 (now) is unused by the CTEs; the tail references it so the
	// placeholders keep the shipped statement's numbering and types.
	return supersedeRunningProjectorWorkQuery[:cut] +
		")\nSELECT count(*) FROM locked_generation WHERE $1::timestamptz IS NOT NULL\n"
}

// lockedGatePredicate returns the locked_generation CTE's WHERE predicate
// after the generation_id equality, as shipped.
func lockedGatePredicate(t *testing.T) string {
	t.Helper()
	start := strings.Index(supersedeRunningProjectorWorkQuery, "locked_generation AS MATERIALIZED (")
	end := strings.Index(supersedeRunningProjectorWorkQuery, writeGateLockBoundary)
	if start < 0 || end < start {
		t.Fatal("supersedeRunningProjectorWorkQuery lost its locked_generation CTE")
	}
	cte := supersedeRunningProjectorWorkQuery[start:end]
	const lead = "WHERE generation.generation_id = $3\n      AND "
	at := strings.Index(cte, lead)
	if at < 0 {
		t.Fatalf("locked_generation CTE no longer starts its predicate with %q", lead)
	}
	return cte[at+len(lead):]
}

// TestSupersedeRunningGateProbeIsPrefixOfShippedQuery is the prefix guard for
// the derived gate probe: its CTE text must be a byte prefix of the shipped
// heartbeat supersede, and the boundary must appear exactly once, so the live
// gate rows can never test a stale copy.
func TestSupersedeRunningGateProbeIsPrefixOfShippedQuery(t *testing.T) {
	t.Parallel()
	if got := strings.Count(supersedeRunningProjectorWorkQuery, writeGateLockBoundary); got != 1 {
		t.Fatalf("lock boundary appears %d times, want exactly 1", got)
	}
	probe := derivedWriteGateProbeQuery(t)
	cut := strings.Index(supersedeRunningProjectorWorkQuery, writeGateLockBoundary)
	if !strings.HasPrefix(supersedeRunningProjectorWorkQuery, probe[:cut]) {
		t.Fatal("derived gate probe is not a byte prefix of supersedeRunningProjectorWorkQuery")
	}
}

// TestSupersedeRunningGateLocksGenerationWithFullPredicate pins the #7389
// shape: the write-start gate sits on the generation row the statement locks
// with SKIP LOCKED, that predicate carries the #7130 own-superseded branch,
// the marker check and the newer-generation EXISTS, and the work UPDATE joins
// the locked row and repeats the same predicate on its joined generation.
func TestSupersedeRunningGateLocksGenerationWithFullPredicate(t *testing.T) {
	t.Parallel()
	predicate := lockedGatePredicate(t)
	for _, want := range []string{
		"generation.status = 'superseded'",
		"generation.status IN ('pending', 'active')",
		"generation.projection_write_started_at IS NULL",
		"FROM scope_generations AS newer",
	} {
		if !strings.Contains(predicate, want) {
			t.Errorf("locked_generation predicate lacks %q:\n%s", want, predicate)
		}
	}
	if !strings.Contains(supersedeRunningProjectorWorkQuery, "locked_generation AS locked_generation") ||
		!strings.Contains(supersedeRunningProjectorWorkQuery,
			"current_generation.generation_id = locked_generation.generation_id") {
		t.Error("the work UPDATE no longer joins the locked generation row")
	}
	// The UPDATE's copy of the predicate is the locked one with the alias
	// renamed; a drifted copy would let the two disagree.
	joined := strings.ReplaceAll(predicate, "generation.", "current_generation.")
	joined = strings.ReplaceAll(joined, "newer.current_generation.", "newer.generation.")
	if !strings.Contains(supersedeRunningProjectorWorkQuery, dedent(joined, 4)) {
		t.Errorf("the work UPDATE's generation predicate drifted from the locked one; want it to contain:\n%s", dedent(joined, 4))
	}
}

// dedent removes up to n leading spaces from every line of s.
func dedent(s string, n int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		trim := 0
		for trim < n && trim < len(line) && line[trim] == ' ' {
			trim++
		}
		lines[i] = line[trim:]
	}
	return strings.Join(lines, "\n")
}

// TestMarkProjectionWriteStartedQueryShape pins the marker's lease fence: it
// reads the work row through EXISTS without locking it. The status list and
// the SET clause are pinned by TestWriteMarkerStatusAndSetDerivedFromShippedQuery.
func TestMarkProjectionWriteStartedQueryShape(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"AND EXISTS (",
		"AND work.lease_owner = $3",
		"AND work.attempt_count = $4",
		"AND work.status IN ('claimed', 'running')",
		"RETURNING generation_id",
	} {
		if !strings.Contains(markProjectionWriteStartedQuery, want) {
			t.Errorf("markProjectionWriteStartedQuery lacks %q", want)
		}
	}
	if strings.Contains(markProjectionWriteStartedQuery, "FOR UPDATE") ||
		strings.Contains(markProjectionWriteStartedQuery, "FOR NO KEY UPDATE") {
		t.Error("markProjectionWriteStartedQuery must not lock the work row")
	}
}

// TestUncoveredProjectionWritersQueryComparesWriteStarts pins the probe's
// covering rule: the exact write-start comparison against the newest activated
// full generation, failing closed to -infinity, never activated_at.
func TestUncoveredProjectionWritersQueryComparesWriteStarts(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"AND generation.status IN ('superseded', 'failed')",
		"AND generation.activated_at IS NULL",
		"AND generation.projection_write_started_at >\n      COALESCE((SELECT pws FROM last_full), '-infinity'::timestamptz)",
		"AND is_delta = false\n      AND activated_at IS NOT NULL",
	} {
		if !strings.Contains(uncoveredProjectionWritersQuery, want) {
			t.Errorf("uncoveredProjectionWritersQuery lacks %q", want)
		}
	}
	if strings.Contains(uncoveredProjectionWritersQuery, "SELECT activated_at") {
		t.Error("the covering bound must be a write start, never activated_at")
	}
}

// TestSupersedeRunningLocksWorkBeforeGenerationWithLeaseFence pins the #7389
// lock order on the shipped constant: locked_work, carrying the full lease
// fence, comes before locked_generation, which carries the gate and reads its
// generation through locked_work; both SKIP LOCKED.
func TestSupersedeRunningLocksWorkBeforeGenerationWithLeaseFence(t *testing.T) {
	t.Parallel()
	q := supersedeRunningProjectorWorkQuery
	work := strings.Index(q, "locked_work AS MATERIALIZED (")
	generation := strings.Index(q, "locked_generation AS MATERIALIZED (")
	update := strings.Index(q, "UPDATE fact_work_items AS work")
	if work < 0 || generation < 0 || update < 0 || work >= generation || generation >= update {
		t.Fatalf("CTE order work=%d generation=%d update=%d, want locked_work < locked_generation < the work UPDATE", work, generation, update)
	}
	workCTE := q[work:generation]
	for _, want := range []string{
		"WHERE work.stage = 'projector'",
		"AND work.generation_id = $3",
		"AND work.lease_owner = $4",
		"AND work.attempt_count = $5",
		"AND work.status IN ('claimed', 'running')",
		"FOR NO KEY UPDATE OF work SKIP LOCKED",
	} {
		if !strings.Contains(workCTE, want) {
			t.Errorf("locked_work lacks %q:\n%s", want, workCTE)
		}
	}
	generationCTE := q[generation:update]
	if !strings.Contains(generationCTE, "FROM locked_work AS owned") ||
		!strings.Contains(generationCTE, "generation.projection_write_started_at IS NULL") ||
		!strings.Contains(generationCTE, "FOR NO KEY UPDATE OF generation SKIP LOCKED") {
		t.Errorf("locked_generation must read through locked_work and carry the gate:\n%s", generationCTE)
	}
	if strings.Contains(workCTE, "projection_write_started_at") {
		t.Error("the marker gate must stay in locked_generation, not locked_work")
	}
	if !strings.Contains(q[update:], "FROM locked_work AS owned,") || !strings.Contains(q[update:], "work.work_item_id = owned.work_item_id") {
		t.Error("the work UPDATE must update exactly the locked work row")
	}
}

// TestMarkerFenceQueryShapes pins the #7819 marker fence protocol: the lock
// statement reads exactly the scope's fence row non-blocking, and the bump
// statement touches only that row. The marker must never wait on the fence
// (it already waits on the generation row under lock_timeout) and must never
// lock a work row.
func TestMarkerFenceQueryShapes(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"FROM projector_scope_claim_fences",
		"WHERE scope_id = $1",
		"FOR NO KEY UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(lockProjectorMarkerFenceQuery, want) {
			t.Errorf("lockProjectorMarkerFenceQuery lacks %q", want)
		}
	}
	for _, want := range []string{
		"UPDATE projector_scope_claim_fences",
		"SET fence = fence + 1",
		"WHERE scope_id = $1",
	} {
		if !strings.Contains(bumpProjectorMarkerFenceQuery, want) {
			t.Errorf("bumpProjectorMarkerFenceQuery lacks %q", want)
		}
	}
	for name, q := range map[string]string{
		"lockProjectorMarkerFenceQuery": lockProjectorMarkerFenceQuery,
		"bumpProjectorMarkerFenceQuery": bumpProjectorMarkerFenceQuery,
	} {
		if strings.Contains(q, "fact_work_items") {
			t.Errorf("%s must not touch work rows", name)
		}
	}
	if strings.Contains(bumpProjectorMarkerFenceQuery, "SKIP LOCKED") {
		t.Error("bumpProjectorMarkerFenceQuery updates the already-held row and must not skip")
	}
}
