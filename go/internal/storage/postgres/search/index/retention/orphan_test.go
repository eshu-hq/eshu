// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"context"
	"strings"
	"testing"
	"time"
)

// orphanSweepScript answers an orphan census of the documents table that
// counts orphans and lists the given generations, and answers every sweep
// batch with a full page, so only the row cap ends the sweep.
func orphanSweepScript(orphans int64, listed ...string) func(string, []any) ([][]any, error) {
	return func(query string, _ []any) ([][]any, error) {
		switch {
		case query == termsLeavesQuery:
			return nil, nil
		case strings.Contains(query, "FROM eshu_search_index_terms AS relation"):
			return [][]any{{int64(0), int64(0), nil, nil}}, nil
		case strings.Contains(query, "orphan census"):
			rows := make([][]any, 0, len(listed))
			for _, generation := range listed {
				rows = append(rows, []any{orphans + 10, orphans, "scope-a", generation})
			}
			return rows, nil
		case isTryLock(query):
			return [][]any{{true}}, nil
		case strings.Contains(query, "orphan sweep batch"):
			return page(true, BatchRows, BatchRows, "k", "d"), nil
		}
		return nil, nil
	}
}

func sweptRows(sweep OrphanSweep) int64 {
	return sweep.RowsSwept[TermsTable] + sweep.RowsSwept[DocumentsTable]
}

// TestSweepOrphansHonorsTheRowCap pins #8281 review thread A: the sweep's
// row budget comes from the caller (the retention policy's search index
// row cap, the one the pre-delete uses), and a non-positive cap falls back
// to OrphanSweepRowCap.
func TestSweepOrphansHonorsTheRowCap(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rowCap int64
		want   int64
	}{
		{"one-batch-cap", BatchRows, BatchRows},
		{"three-batch-cap", 3 * BatchRows, 3 * BatchRows},
		{"unset-falls-back", 0, OrphanSweepRowCap},
	} {
		database := &scriptedDB{answer: orphanSweepScript(2, "gen-a", "gen-b")}
		sweep, err := SweepOrphans(context.Background(), database, time.Time{}, tc.rowCap, nil)
		if err != nil {
			t.Fatalf("%s: SweepOrphans() error = %v", tc.name, err)
		}
		if got := sweptRows(sweep); got != tc.want {
			t.Fatalf("%s: swept %d rows, want %d (the cap)", tc.name, got, tc.want)
		}
	}
}

// TestSweepOrphansCountsUnlistedOrphansAsUnswept pins #8281 review thread B:
// the census counts every orphan pair but lists at most OrphanListLimit per
// relation, so Unswept includes the counted-but-unlisted remainder. Here the
// documents table counts 5 orphans and lists 2; both listed drain, so the 3
// unlisted pairs are what this pass did not sweep.
func TestSweepOrphansCountsUnlistedOrphansAsUnswept(t *testing.T) {
	answer := orphanSweepScript(5, "gen-a", "gen-b")
	drained := func(query string, args []any) ([][]any, error) {
		if strings.Contains(query, "orphan sweep batch") {
			return page(true, 0, 0, "", ""), nil
		}
		return answer(query, args)
	}
	sweep, err := SweepOrphans(context.Background(), &scriptedDB{answer: drained}, time.Time{}, 0, nil)
	if err != nil {
		t.Fatalf("SweepOrphans() error = %v", err)
	}
	if sweep.Census.Unlisted != 3 || sweep.Unswept != 3 {
		t.Fatalf("Unlisted = %d, Unswept = %d; want 3 each (5 counted, 2 listed and drained)", sweep.Census.Unlisted, sweep.Unswept)
	}
}
