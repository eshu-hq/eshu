// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
	"testing"
)

var markGuardCommentPattern = regexp.MustCompile(`--[^\n]*`)

// markGuardBranch1Gate is the #7469 branch-1 marker gate: a generation that
// started writing is never supersedable, so its retry can run.
const markGuardBranch1Gate = `      AND stale_generation.status IN ('pending', 'failed')
      AND stale_generation.projection_write_started_at IS NULL
`

// markGuardLockFlag is the #7469 lock-step marker_spared flag: each locked
// generation row carries its lock-time marker truth forward, so the work lock
// step and the oldest-ready subquery agree with what the locks saw.
const markGuardLockFlag = `    SELECT supersedable.work_item_id,
           stale_generation.generation_id AS generation_id,
           stale_generation.status AS generation_status,
           (stale_generation.status IN ('pending', 'failed')
               AND stale_generation.projection_write_started_at IS NOT NULL
           ) AS marker_spared
`

// markGuardWorkExclusion is the #7469 work-lock exclusion of rows the sweep
// spared at lock time. It spans the JOIN so the anchor is unique: the
// duplicate-reclaim CTE opens with the same bare WHERE.
const markGuardWorkExclusion = `    JOIN locked_stale_scope_generations AS locked_generation
      ON locked_generation.work_item_id = stale.work_item_id
    WHERE NOT locked_generation.marker_spared
      AND stale.stage = 'projector'
`

// markGuardSnapshotHold is the #7469 oldest-ready snapshot guard: a row held
// behind an older same-scope marked generation with waiting work is skipped,
// so the holder becomes oldest. The order comparison is row-form, identical
// to the OR tiebreak in branch 1.
const markGuardSnapshotHold = `            AND NOT EXISTS (
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
                  AND waiting_generation.projection_write_started_at IS NOT NULL
                  AND (waiting_generation.ingested_at, waiting_generation.generation_id) <
                      (held_generation.ingested_at, held_generation.generation_id)
            )
`

// markGuardLockTimeHold is the #7469 oldest-ready lock-time guard: the same
// hold read from the sweep's marker_spared flag, for a marker that commits
// after the snapshot, which the snapshot guard cannot see.
const markGuardLockTimeHold = `            AND NOT EXISTS (
                SELECT 1
                FROM locked_stale_scope_generations AS spared
                JOIN scope_generations AS spared_generation
                  ON spared_generation.generation_id = spared.generation_id
                JOIN scope_generations AS held_generation
                  ON held_generation.generation_id = same.generation_id
                WHERE spared.marker_spared
                  AND spared_generation.scope_id = same.scope_id
                  AND (spared_generation.ingested_at, spared_generation.generation_id) <
                      (held_generation.ingested_at, held_generation.generation_id)
            )
`

// markGuardAckGate is the #7469 Ack obsolete-generation marker gate.
const markGuardAckGate = `      AND stale_generation.status IN ('pending', 'failed')
      AND stale_generation.projection_write_started_at IS NULL
`

// markGuardClaimBeforeText derives the pre-#7469 claim statement from the
// shipped constant by reversing each addition, so the plan-shape and
// contention proofs can never drift from what ships. It returns the before
// text and how many sites each addition rewrote.
func markGuardClaimBeforeText(shipped string) (string, map[string]int) {
	counts := map[string]int{}
	before := shipped
	reversals := []struct {
		name   string
		after  string
		before string
	}{
		{"branch_gate", markGuardBranch1Gate, "      AND stale_generation.status IN ('pending', 'failed')\n"},
		{"lock_flag", markGuardLockFlag, `    SELECT supersedable.work_item_id,
           stale_generation.status AS generation_status
`},
		{"work_exclusion", markGuardWorkExclusion, `    JOIN locked_stale_scope_generations AS locked_generation
      ON locked_generation.work_item_id = stale.work_item_id
    WHERE stale.stage = 'projector'
`},
		{"snapshot_hold", markGuardSnapshotHold, ""},
		{"locktime_hold", markGuardLockTimeHold, ""},
	}
	for _, reversal := range reversals {
		counts[reversal.name] = strings.Count(before, reversal.after)
		before = strings.ReplaceAll(before, reversal.after, reversal.before)
	}
	return before, counts
}

// TestProjectorClaimMarkedGuardQueryShape pins the #7469 additions to the
// claim and Ack statements: the branch-1 and Ack marker gates, the lock-step
// marker_spared flag and its work-lock exclusion, and both oldest-ready
// holds. The holds must correlate on the oldest-ready `same` row: a copy on
// the pool's outer `work` row would stall instead of surfacing the holder.
func TestProjectorClaimMarkedGuardQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		markGuardBranch1Gate,
		markGuardLockFlag,
		markGuardWorkExclusion,
		markGuardSnapshotHold,
		markGuardLockTimeHold,
		"waiting.scope_id = same.scope_id",
		"spared_generation.scope_id = same.scope_id",
	} {
		if !strings.Contains(claimProjectorWorkQuery, want) {
			t.Fatalf("claim query missing #7469 addition %q", want)
		}
	}
	for _, forbidden := range []string{
		"waiting.scope_id = work.scope_id",
		"spared_generation.scope_id = work.scope_id",
	} {
		if strings.Contains(claimProjectorWorkQuery, forbidden) {
			t.Fatalf("claim query contains outer-WHERE guard copy %q, which stalls", forbidden)
		}
	}
	if !strings.Contains(supersedeProjectorObsoleteGenerationsQuery, markGuardAckGate) {
		t.Fatalf("Ack obsolete query missing #7469 gate %q", markGuardAckGate)
	}
}

// TestMarkedGuardBeforeTextReversesTheChange keeps the before-text derivation
// honest without a database: each addition must reverse exactly once, the
// before text must carry no #7469 marker, and re-applying the additions must
// reproduce the shipped constant byte for byte.
func TestMarkedGuardBeforeTextReversesTheChange(t *testing.T) {
	t.Parallel()

	before, counts := markGuardClaimBeforeText(claimProjectorWorkQuery)
	for _, name := range []string{"branch_gate", "lock_flag", "work_exclusion", "snapshot_hold", "locktime_hold"} {
		if counts[name] != 1 {
			t.Fatalf("%s reversed %d sites, want exactly 1", name, counts[name])
		}
	}
	code := markGuardCommentPattern.ReplaceAllString(before, "")
	for _, gone := range []string{"marker_spared", "projection_write_started_at"} {
		if strings.Contains(code, gone) {
			t.Fatalf("before text still contains %q outside comments", gone)
		}
	}
	// waiting_generation stays: the #7473 delta hold reuses the alias. The
	// #7469 holds must be gone as blocks instead.
	for _, gone := range []string{markGuardSnapshotHold, markGuardLockTimeHold} {
		if strings.Contains(before, gone) {
			t.Fatalf("before text still contains a #7469 hold block")
		}
	}
	roundTrip := before
	roundTrip = strings.Replace(roundTrip,
		"      AND stale_generation.status IN ('pending', 'failed')\n", markGuardBranch1Gate, 1)
	roundTrip = strings.Replace(roundTrip, `    SELECT supersedable.work_item_id,
           stale_generation.status AS generation_status
`, markGuardLockFlag, 1)
	roundTrip = strings.Replace(roundTrip, `    JOIN locked_stale_scope_generations AS locked_generation
      ON locked_generation.work_item_id = stale.work_item_id
    WHERE stale.stage = 'projector'
`, markGuardWorkExclusion, 1)
	roundTrip += "" // holds append below at their anchors
	holdsAnchor := `            AND NOT EXISTS (
                SELECT 1
                FROM superseded_stale_projector_generations AS superseded_same
                WHERE superseded_same.work_item_id = same.work_item_id
            )
`
	if strings.Count(roundTrip, holdsAnchor) != 1 {
		t.Fatalf("holds anchor occurs %d times, want 1", strings.Count(roundTrip, holdsAnchor))
	}
	// The #7473 delta hold persists in the before text after this anchor,
	// so re-applying the #7469 holds ahead of it reproduces the shipped
	// query without naming it.
	roundTrip = strings.Replace(roundTrip, holdsAnchor,
		holdsAnchor+markGuardSnapshotHold+markGuardLockTimeHold, 1)
	if roundTrip != claimProjectorWorkQuery {
		t.Fatal("re-applying the #7469 additions to the before text does not reproduce the shipped query")
	}
}

// TestProjectorClaimMarkedGuardAddsNoLockClauses pins the #7469 lock-set
// contract: the additions lock no rows, so the before and after texts carry
// identical locking clauses.
func TestProjectorClaimMarkedGuardAddsNoLockClauses(t *testing.T) {
	t.Parallel()

	before, _ := markGuardClaimBeforeText(claimProjectorWorkQuery)
	for _, clause := range []string{"SKIP LOCKED", "FOR UPDATE", "FOR NO KEY UPDATE"} {
		if got, want := strings.Count(claimProjectorWorkQuery, clause), strings.Count(before, clause); got != want {
			t.Fatalf("%q occurs %d times after, %d before; want identical", clause, got, want)
		}
	}
	for _, fragment := range []string{
		markGuardBranch1Gate, markGuardLockFlag, markGuardWorkExclusion,
		markGuardSnapshotHold, markGuardLockTimeHold, markGuardAckGate,
	} {
		if strings.Contains(fragment, "LOCKED") || strings.Contains(fragment, "FOR UPDATE") {
			t.Fatalf("a #7469 fragment locks rows: %q", fragment)
		}
	}
}

// TestAckObsoleteSupersedeLocksStaleGenerations pins the #7820
// lock-then-update shape of Ack's obsolete-generation supersede: a lock step
// takes each stale generation row non-blocking in generation order (so a
// marker committed after the snapshot is caught by the lock's EvalPlanQual
// recheck and an in-flight marker is skipped without waiting), and the work
// UPDATE retires only rows whose generation the lock step holds,
// re-applying the marker gate for the recheck.
func TestAckObsoleteSupersedeLocksStaleGenerations(t *testing.T) {
	t.Parallel()
	q := supersedeProjectorObsoleteGenerationsQuery
	for _, want := range []string{
		"WITH locked_obsolete_stale_generations AS (",
		"ORDER BY stale_generation.generation_id",
		"FOR NO KEY UPDATE OF stale_generation SKIP LOCKED",
		"FROM locked_obsolete_stale_generations AS locked,",
		"AND stale.generation_id = locked.generation_id",
		"AND stale_generation.generation_id = locked.generation_id",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("Ack obsolete query lacks %q", want)
		}
	}
	if got := strings.Count(q, markGuardAckGate); got != 2 {
		t.Errorf("Ack obsolete query carries the marker gate %d times, want 2 (lock step plus re-applied UPDATE predicate)", got)
	}
	if strings.Contains(q, "FOR UPDATE OF stale") || strings.Contains(q, "FOR NO KEY UPDATE OF stale ") {
		t.Error("Ack obsolete query must not add a work-row locking clause; the UPDATE's own row locks suffice")
	}
}
