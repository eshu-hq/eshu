// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 proof that a generation with no generation row, whose evidence rows or whose candidate rows another session holds, is never counted swept: the batch skips the held rows without waiting, commits the rows it could delete, counts the generation lock_busy, and the next pass sweeps it once the holder lets go; a settle read that ignored one table would count the generation swept while its rows remain
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"testing"

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
