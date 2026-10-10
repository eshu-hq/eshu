// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// fullGuardBranch1Gate is the #7473 branch-1 spare: a stale full generation
// is supersedable only when a newer full exists. A stale delta keeps today's
// rule: any newer generation supersedes it.
const fullGuardBranch1Gate = `            AND (
                stale_generation.is_delta
                OR NOT newer_generation.is_delta
            )
`

// fullGuardDeltaHold is the #7473 oldest-ready hold: a newer delta waits
// behind an older same-scope full with waiting (pending, retrying) work, so
// the full projects first. The held side stays a delta: a newer full
// supersedes the older one instead of waiting behind it. Terminal fulls never
// hold: only pending and retrying work counts as waiting.
const fullGuardDeltaHold = `            AND NOT EXISTS (
                SELECT 1
                FROM fact_work_items AS waiting
                JOIN scope_generations AS waiting_generation
                  ON waiting_generation.generation_id = waiting.generation_id
                JOIN scope_generations AS held_generation
                  ON held_generation.generation_id = same.generation_id
                WHERE waiting.stage = 'projector'
                  AND waiting.scope_id = same.scope_id
                  AND waiting.work_item_id <> same.work_item_id
                  AND waiting.status IN ('pending', 'retrying')
                  AND waiting_generation.status IN ('pending', 'failed')
                  AND NOT waiting_generation.is_delta
                  AND held_generation.is_delta
                  AND (waiting_generation.ingested_at, waiting_generation.generation_id) <
                      (held_generation.ingested_at, held_generation.generation_id)
            )
`

// fullGuardClaimBeforeText derives the pre-#7473 claim statement from the
// shipped constant by reversing each addition, so the plan-shape and
// contention proofs can never drift from what ships. It returns the before
// text and how many sites each addition rewrote.
func fullGuardClaimBeforeText(shipped string) (string, map[string]int) {
	counts := map[string]int{}
	before := shipped
	reversals := []struct {
		name   string
		after  string
		before string
	}{
		{"branch_spare", fullGuardBranch1Gate, ""},
		{"delta_hold", fullGuardDeltaHold, ""},
	}
	for _, reversal := range reversals {
		counts[reversal.name] = strings.Count(before, reversal.after)
		before = strings.ReplaceAll(before, reversal.after, reversal.before)
	}
	return before, counts
}

// TestProjectorClaimFullGuardQueryShape pins the #7473 additions to the claim
// statement: the branch-1 full spare and the oldest-ready delta hold. The
// hold must correlate on the oldest-ready `same` row: a copy on the pool's
// outer `work` row would stall instead of surfacing the holder. Ack's
// obsolete-generation sweep deliberately keeps its full-blind predicate as
// the misordering backstop (see the #7473 evidence note), so this test pins
// its absence there too.
func TestProjectorClaimFullGuardQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		fullGuardBranch1Gate,
		fullGuardDeltaHold,
	} {
		if !strings.Contains(claimProjectorWorkQuery, want) {
			t.Fatalf("claim query missing #7473 addition %q", want)
		}
	}
	if !strings.Contains(supersedeProjectorObsoleteGenerationsQuery, "stale_generation.projection_write_started_at IS NULL") {
		t.Fatalf("Ack obsolete query lost its #7469 marker gate")
	}
	if strings.Contains(supersedeProjectorObsoleteGenerationsQuery, "is_delta") {
		t.Fatalf("Ack obsolete query mentions is_delta, want the full-blind backstop predicate")
	}
}

// TestFullGuardBeforeTextReversesTheChange keeps the before-text derivation
// honest without a database: each addition must reverse exactly once, and
// re-applying the additions must reproduce the shipped constant byte for
// byte.
func TestFullGuardBeforeTextReversesTheChange(t *testing.T) {
	t.Parallel()

	before, counts := fullGuardClaimBeforeText(claimProjectorWorkQuery)
	for _, name := range []string{"branch_spare", "delta_hold"} {
		if counts[name] != 1 {
			t.Fatalf("%s reversed %d sites, want exactly 1", name, counts[name])
		}
	}
	roundTrip := strings.Replace(before, `            AND newer.status IN ('pending', 'retrying', 'claimed', 'running', 'succeeded', 'failed', 'dead_letter', 'superseded')
`, `            AND newer.status IN ('pending', 'retrying', 'claimed', 'running', 'succeeded', 'failed', 'dead_letter', 'superseded')
`+fullGuardBranch1Gate, 1)
	roundTrip += "" // hold appends below at its anchor
	lockTimeHoldAnchor := `                  AND (spared_generation.ingested_at, spared_generation.generation_id) <
                      (held_generation.ingested_at, held_generation.generation_id)
            )
`
	if strings.Count(roundTrip, lockTimeHoldAnchor) != 1 {
		t.Fatalf("hold anchor occurs %d times, want 1", strings.Count(roundTrip, lockTimeHoldAnchor))
	}
	roundTrip = strings.Replace(roundTrip, lockTimeHoldAnchor,
		lockTimeHoldAnchor+fullGuardDeltaHold, 1)
	if roundTrip != claimProjectorWorkQuery {
		t.Fatal("re-applying the #7473 additions to the before text does not reproduce the shipped query")
	}
}

// TestProjectorClaimFullGuardAddsNoLockClauses pins the #7473 lock-set
// contract: the additions lock no rows, so the before and after texts carry
// identical locking clauses.
func TestProjectorClaimFullGuardAddsNoLockClauses(t *testing.T) {
	t.Parallel()

	before, _ := fullGuardClaimBeforeText(claimProjectorWorkQuery)
	for _, clause := range []string{"SKIP LOCKED", "FOR UPDATE", "FOR NO KEY UPDATE"} {
		if got, want := strings.Count(claimProjectorWorkQuery, clause), strings.Count(before, clause); got != want {
			t.Fatalf("%q occurs %d times after, %d before; want identical", clause, got, want)
		}
	}
	for _, fragment := range []string{fullGuardBranch1Gate, fullGuardDeltaHold} {
		for _, clause := range []string{"SKIP LOCKED", "FOR UPDATE", "FOR NO KEY UPDATE"} {
			if strings.Contains(fragment, clause) {
				t.Fatalf("#7473 addition contains lock clause %q", clause)
			}
		}
	}
}
