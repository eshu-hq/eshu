// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 deadlock probes of the wait-free relationship retention batch against the real #10820 repository purge, in both arrival orders: a purge that arrives while the batch holds the generation row (the batch resumed 0, 300, 700, 800 and 1200 ms after the purge queued behind it, the delays at which the earlier lock-timeout design let the purge become the deadlock victim) completes without a 40P01 while the batch skips the generation as lock_busy with no lock timeout; a purge that arrives while the batch holds the rows it deleted waits and completes after the batch commits; a purge that rolls back leaves the generation whole and the next pass sweeps all of it
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"fmt"
	"testing"
	"time"

	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// TestPurgeArrivingWhileTheBatchHoldsTheGenerationRowLive parks the sweep after
// its classification, so the batch holds the orphan generation's row, then starts
// the real purge. The purge deletes the generation's evidence and candidates,
// which the batch has not reached, and queues behind the generation row. The
// batch then resumes after each delay and finds every child row held by the
// purge. It must not wait for one: the batch counts the generation as lock_busy
// (never a lock timeout), ends without deleting anything, and the purge's wait
// ends because the batch gave the row back, so the purge completes. The delays
// are the ones at which the earlier design, where the batch waited on the
// purge's rows under a 500 ms lock timeout, left the purge as the deadlock victim
// (700 and 800 ms) or the batch as the one that yielded (0, 300 and 1200 ms).
func TestPurgeArrivingWhileTheBatchHoldsTheGenerationRowLive(t *testing.T) {
	for _, delay := range []time.Duration{0, 300 * time.Millisecond, 700 * time.Millisecond, 800 * time.Millisecond, 1200 * time.Millisecond} {
		t.Run(fmt.Sprintf("the batch resumes %s after the purge queued", delay), func(t *testing.T) {
			f := openLiveFixture(t)
			p := f.seedPurgeScope(t, "pb")
			const generation = "gen-pb"
			f.generation(t, p.scopeID, generation, "superseded")
			f.evidence(t, generation, 3)
			f.candidates(t, generation, 2)

			sweep := f.startParkedSweep(relationshipretention.StageClassify)
			defer sweep.release()
			sweep.awaitParked(t)
			purge := f.startPurge(p)
			if !f.waitForLockWait(t, "DELETE FROM relationship_generations WHERE scope%", 30*time.Second) {
				t.Fatal("the purge never queued behind the generation row the batch holds")
			}
			time.Sleep(delay)
			sweep.release()

			swept := sweep.outcome(t)
			purged := waitForPurge(t, purge)
			// Either side may be the deadlock victim, so check both before anything else.
			requireNoDeadlock(t, "the sweep", swept.err)
			requireNoDeadlock(t, "the purge", purged.err)
			if swept.err != nil {
				t.Fatalf("Sweep: %v", swept.err)
			}
			if got := swept.result.Skipped; got[relationshipretention.SkipLockBusy] != 1 || got[relationshipretention.SkipLockTimeout] != 0 ||
				got[relationshipretention.SkipIneligible] != 0 || swept.result.GenerationsSwept != 0 || swept.result.Batches != 0 {
				t.Fatalf("pass = swept %d in %d batches, skipped %v; want the generation skipped as lock_busy and no batch committed",
					swept.result.GenerationsSwept, swept.result.Batches, got)
			}
			requirePurgeCompleted(t, purged, p)
			if got := f.of(t, generation); got != (counts{0, 0, 0, 0}) {
				t.Fatalf("generation after the purge = %+v, want it gone", got)
			}
		})
	}
}

// TestPurgeArrivingWhileTheBatchHoldsChildRowsLive is the other arrival order:
// the batch has already deleted the generation's evidence rows (it holds them,
// and the generation row) when the real purge starts. The purge's evidence
// delete waits on those rows. The batch, which waits for nothing, carries on,
// deletes the candidates and the generation row, and commits; the purge then
// wakes, finds its rows gone and completes. Neither side is a deadlock victim and
// the batch counts the generation as swept.
func TestPurgeArrivingWhileTheBatchHoldsChildRowsLive(t *testing.T) {
	f := openLiveFixture(t)
	p := f.seedPurgeScope(t, "pc")
	const generation = "gen-pc"
	f.generation(t, p.scopeID, generation, "superseded")
	f.evidence(t, generation, 3)
	f.candidates(t, generation, 2)

	sweep := f.startParkedSweep(relationshipretention.StageDeleteEvidence)
	defer sweep.release()
	sweep.awaitParked(t)
	purge := f.startPurge(p)
	if !f.waitForLockWait(t, "DELETE FROM relationship_evidence_facts WHERE generation_id IN%", 30*time.Second) {
		t.Fatal("the purge never waited on the evidence rows the batch deleted")
	}
	sweep.release()

	swept := sweep.outcome(t)
	purged := waitForPurge(t, purge)
	requireNoDeadlock(t, "the sweep", swept.err)
	requireNoDeadlock(t, "the purge", purged.err)
	if swept.err != nil {
		t.Fatalf("Sweep: %v", swept.err)
	}
	if swept.result.GenerationsSwept != 1 || swept.result.Batches != 1 ||
		swept.result.RowsSwept[relationshipretention.EvidenceTable] != 3 ||
		swept.result.RowsSwept[relationshipretention.CandidatesTable] != 2 ||
		swept.result.RowsSwept[relationshipretention.GenerationsTable] != 1 {
		t.Fatalf("pass = swept %d in %d batches, rows %v, skipped %v; want the whole generation swept",
			swept.result.GenerationsSwept, swept.result.Batches, swept.result.RowsSwept, swept.result.Skipped)
	}
	for reason, n := range swept.result.Skipped {
		if n != 0 {
			t.Fatalf("skipped %s = %d, want no skip: the batch never waited and held every row", reason, n)
		}
	}
	requirePurgeCompleted(t, purged, p)
	if got := f.of(t, generation); got != (counts{0, 0, 0, 0}) {
		t.Fatalf("generation after the purge = %+v, want it gone", got)
	}
}

// TestSweepSweepsAGenerationAfterTheHoldingPurgeRollsBackLive covers a purge that
// does not commit. A holder owns the generation's candidate rows, so the purge
// deleted the evidence and waits. The pass skips the generation (lock_busy) and
// leaves every row; cancelling the purge rolls its deletes back; and the next
// pass, with nothing held, sweeps the whole generation. A batch that had counted
// the skipped generation as swept, or deleted what the purge held, would lose
// rows the rolled-back purge restored.
func TestSweepSweepsAGenerationAfterTheHoldingPurgeRollsBackLive(t *testing.T) {
	f := openLiveFixture(t)
	p := f.seedPurgeScope(t, "pd")
	const generation = "gen-pd"
	f.generation(t, p.scopeID, generation, "superseded")
	f.evidence(t, generation, 3)
	f.candidates(t, generation, 2)

	release := f.holdCandidates(t, generation)
	purgeCtx, cancelPurge := context.WithCancel(f.ctx)
	defer cancelPurge()
	purge := f.startPurgeCtx(purgeCtx, p)
	if !f.waitForLockWait(t, "DELETE FROM relationship_candidates WHERE generation_id IN%", 30*time.Second) {
		t.Fatal("the purge never waited on the held candidate rows")
	}

	first := f.sweep(t)
	if got := first.Skipped; got[relationshipretention.SkipLockBusy] != 1 || got[relationshipretention.SkipLockTimeout] != 0 ||
		first.GenerationsSwept != 0 || first.Batches != 0 {
		t.Fatalf("pass 1 = swept %d in %d batches, skipped %v; want the generation skipped as lock_busy", first.GenerationsSwept, first.Batches, got)
	}
	if got := f.of(t, generation); got != (counts{3, 2, 1, 0}) {
		t.Fatalf("generation after pass 1 = %+v, want every row left {3 2 1 0}", got)
	}

	cancelPurge()
	purged := waitForPurge(t, purge)
	requireNoDeadlock(t, "the purge", purged.err)
	if purged.err == nil {
		t.Fatal("the cancelled purge reported success")
	}
	release()
	if got := f.of(t, generation); got != (counts{3, 2, 1, 0}) {
		t.Fatalf("generation after the purge rolled back = %+v, want every row restored {3 2 1 0}", got)
	}

	second := f.sweep(t)
	if got := second.Skipped; got[relationshipretention.SkipLockBusy] != 0 || got[relationshipretention.SkipLockTimeout] != 0 ||
		second.GenerationsSwept != 1 || second.Batches != 1 {
		t.Fatalf("pass 2 = swept %d in %d batches, skipped %v; want the generation swept", second.GenerationsSwept, second.Batches, got)
	}
	if got := f.of(t, generation); got != (counts{0, 0, 0, 0}) {
		t.Fatalf("generation after pass 2 = %+v, want it swept", got)
	}
}
