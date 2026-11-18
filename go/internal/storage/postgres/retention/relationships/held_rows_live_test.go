// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 proof that a generation with no generation row, whose evidence rows or whose candidate rows another session holds, is never counted swept: the batch skips the held rows without waiting, commits the rows it could delete, counts the generation lock_busy, and the next pass sweeps it once the holder lets go; a settle read that ignored one table would count the generation swept while its rows remain; and a generation row that appears after classification (the generation had none to lock) and is held by another session is skipped by the generation row delete, which picks the row FOR UPDATE SKIP LOCKED instead of waiting for it: the batch commits the rows it deleted, counts lock_busy and never ends on the 500 ms lock timeout, and the next pass sweeps the generation once the holder lets go

import (
	"context"
	"testing"
	"time"

	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// TestRowsHeldOnAGenerationWithoutAGenerationRowAreNotCountedSweptLive covers the
// one case the generation row cannot report: rows written but never resolved
// have no relationship_generations row, so nothing keeps the generation listed
// except its own rows. A writer holds one kind of its rows. The batch claims the
// generation (the no_rg branch needs no lock), deletes the other kind, skips the
// held rows instead of waiting, and the settle read must name the generation as
// still having rows: counted lock_busy, never swept. The deleted rows still
// commit and count. After the holder lets go the next pass finishes the job.
func TestRowsHeldOnAGenerationWithoutAGenerationRowAreNotCountedSweptLive(t *testing.T) {
	for _, tc := range []struct {
		name         string
		hold         func(f *liveFixture, t *testing.T, generationID string) (release func())
		leftAfterOne counts
		sweptByOne   map[string]int64
	}{
		{
			name:         "evidence rows held",
			hold:         (*liveFixture).holdEvidence,
			leftAfterOne: counts{evidence: 3},
			sweptByOne:   map[string]int64{relationshipretention.EvidenceTable: 0, relationshipretention.CandidatesTable: 2},
		},
		{
			name:         "candidate rows held",
			hold:         (*liveFixture).holdCandidates,
			leftAfterOne: counts{candidates: 2},
			sweptByOne:   map[string]int64{relationshipretention.EvidenceTable: 3, relationshipretention.CandidatesTable: 0},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openLiveFixture(t)
			const generation = "gen-no-rg"
			f.evidence(t, generation, 3)
			f.candidates(t, generation, 2)

			release := tc.hold(f, t, generation)
			first := f.sweep(t)
			if got := first.Skipped; got[relationshipretention.SkipLockBusy] != 1 || got[relationshipretention.SkipLockTimeout] != 0 ||
				got[relationshipretention.SkipIneligible] != 0 {
				t.Fatalf("pass 1 skipped %v, want the generation counted lock_busy once and no lock timeout", got)
			}
			if first.GenerationsSwept != 0 || first.Batches != 1 {
				t.Fatalf("pass 1 = swept %d in %d batches, want no generation swept and the one batch that deleted rows committed",
					first.GenerationsSwept, first.Batches)
			}
			for table, want := range tc.sweptByOne {
				if got := first.RowsSwept[table]; got != want {
					t.Errorf("pass 1 swept %d rows of %s, want %d", got, table, want)
				}
			}
			if got := f.of(t, generation); got != tc.leftAfterOne {
				t.Fatalf("rows after pass 1 = %+v, want only the held rows left %+v", got, tc.leftAfterOne)
			}

			release()
			second := f.sweep(t)
			if second.GenerationsSwept != 1 || second.Skipped[relationshipretention.SkipLockBusy] != 0 {
				t.Fatalf("pass 2 = swept %d, skipped %v; want the generation swept once the rows are free", second.GenerationsSwept, second.Skipped)
			}
			if got := f.of(t, generation); got != (counts{}) {
				t.Fatalf("rows after pass 2 = %+v, want none", got)
			}
		})
	}
}

// generationDeleteWaitBudget is how long the generation row delete may take in
// the test below. The batch's lock_timeout is 500 ms and the writer never lets go
// during the pass, so a statement that waits for the row can only end on that
// timeout; this leaves a margin under it.
const generationDeleteWaitBudget = 400 * time.Millisecond

// TestGenerationRowCreatedAfterClassificationIsSkippedNotWaitedForLive covers the
// one generation row classification cannot lock: a generation with evidence and
// candidate rows but no generation row is claimed without a lock (the no_rg
// branch has nothing to lock), so a writer can still create that row, and hold
// it, before the batch's generation row delete runs. A plain DELETE waits for the
// writer until the 500 ms lock_timeout and rolls the whole batch back, counting
// every generation of it lock_timeout; if the writer in turn waited for a row the
// batch had deleted, the wait would be a lock cycle. The delete must pick the row
// FOR UPDATE SKIP LOCKED instead: the batch commits the evidence and candidate
// rows it deleted, leaves the writer's row, and counts the generation lock_busy.
// The hook stages the writer after delete_candidates, so the generation row delete
// is the next statement. After the writer lets go the next pass sweeps the
// generation row.
func TestGenerationRowCreatedAfterClassificationIsSkippedNotWaitedForLive(t *testing.T) {
	f := openLiveFixture(t)
	const generation = "gen-no-rg-late"
	f.evidence(t, generation, 3)
	f.candidates(t, generation, 2)

	var (
		release          func()
		heldAt, deleteAt time.Time
	)
	opts := relationshipretention.Options{Deletes: true, RowCap: 1 << 40}
	relationshipretention.SetAfterStatement(&opts, func(stage string) error {
		switch stage {
		case relationshipretention.StageDeleteCandidates:
			f.generation(t, "scope-late", generation, "superseded") // committed
			release = f.holdGeneration(t, generation)
			heldAt = time.Now()
		case relationshipretention.StageDeleteGenerations:
			deleteAt = time.Now()
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()
	first, err := relationshipretention.Sweep(ctx, f.database, opts)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if release == nil {
		t.Fatal("the writer was never staged: the batch did not reach delete_candidates")
	}
	got := first.Skipped
	if got[relationshipretention.SkipLockBusy] != 1 || got[relationshipretention.SkipLockTimeout] != 0 || got[relationshipretention.SkipIneligible] != 0 {
		t.Fatalf("pass 1 skipped %v, want the generation counted lock_busy once and no lock timeout: the generation row delete waited for the writer's row",
			got)
	}
	if first.GenerationsSwept != 0 || first.Batches != 1 {
		t.Fatalf("pass 1 = swept %d in %d batches, want no generation swept and the batch that deleted rows committed", first.GenerationsSwept, first.Batches)
	}
	for table, want := range map[string]int64{
		relationshipretention.EvidenceTable: 3, relationshipretention.CandidatesTable: 2, relationshipretention.GenerationsTable: 0,
	} {
		if got := first.RowsSwept[table]; got != want {
			t.Errorf("pass 1 swept %d rows of %s, want %d", got, table, want)
		}
	}
	if waited := deleteAt.Sub(heldAt); deleteAt.IsZero() || waited > generationDeleteWaitBudget {
		t.Errorf("the generation row delete took %s after the writer took its row, want under %s: it waited for the row", waited, generationDeleteWaitBudget)
	} else {
		t.Logf("the generation row delete returned %s after the writer took its row", waited.Round(10*time.Microsecond))
	}
	if rows := f.of(t, generation); rows != (counts{generations: 1}) {
		t.Fatalf("rows after pass 1 = %+v, want the writer's generation row left and its children gone {0 0 1 0}", rows)
	}

	release()
	second := f.sweep(t)
	if second.GenerationsSwept != 1 || second.Skipped[relationshipretention.SkipLockBusy] != 0 || second.Skipped[relationshipretention.SkipLockTimeout] != 0 {
		t.Fatalf("pass 2 = swept %d, skipped %v; want the generation row swept once the writer let go", second.GenerationsSwept, second.Skipped)
	}
	if rows := f.of(t, generation); rows != (counts{}) {
		t.Fatalf("rows after pass 2 = %+v, want none", rows)
	}
}
